import logging.config
import os
import threading
import time

import features
from app import app
from data.secscan_model import secscan_model
from endpoints.v2 import v2_bp
from util.locking import GlobalLock, LockNotAcquiredException
from util.log import logfile_path
from workers.gunicorn_worker import GunicornWorker
from workers.worker import Worker

logger = logging.getLogger(__name__)

# Redis client for tracking scan ranges (lazy-initialized)
_redis_client = None


DEFAULT_INDEXING_INTERVAL = 30
REDIS_RANGE_KEY = "secscan:full_catalog:last_range"
REDIS_RANGE_TTL = 60  # 60 seconds - short TTL to avoid stale data


def _get_redis_client():
    """Lazy-initialize Redis client for tracking scan ranges."""
    global _redis_client
    if _redis_client is None:
        try:
            from redis import Redis
            redis_config = app.config.get("USER_EVENTS_REDIS", {})
            if redis_config:
                _redis_client = Redis(
                    host=redis_config.get("host"),
                    port=redis_config.get("port", 6379),
                    db=redis_config.get("db", 0),
                    password=redis_config.get("password"),
                    socket_connect_timeout=5,
                    socket_timeout=5,
                )
        except Exception as e:
            logger.debug("Could not initialize Redis for scan deduplication: %s", e)
            _redis_client = False  # Mark as unavailable

    return _redis_client if _redis_client is not False else None


def _store_full_catalog_range(start_id, end_id):
    """Store the ID range just processed by full catalog indexing."""
    redis_client = _get_redis_client()
    if redis_client:
        try:
            range_data = f"{start_id}-{end_id}"
            redis_client.setex(REDIS_RANGE_KEY, REDIS_RANGE_TTL, range_data)
            logger.debug("Stored full catalog range: %s", range_data)
        except Exception as e:
            logger.debug("Could not store full catalog range in Redis: %s", e)


def _check_overlap_with_full_catalog(start_id, end_id):
    """
    Check if the given range overlaps with what the full catalog just scanned.
    Returns True if there's overlap (i.e., should skip), False otherwise.
    """
    redis_client = _get_redis_client()
    if not redis_client:
        return False

    try:
        last_range = redis_client.get(REDIS_RANGE_KEY)
        if last_range:
            last_range_str = last_range.decode('utf-8') if isinstance(last_range, bytes) else last_range
            full_start, full_end = map(int, last_range_str.split('-'))

            # Check for overlap: ranges overlap if start1 <= end2 AND start2 <= end1
            has_overlap = start_id <= full_end and full_start <= end_id

            if has_overlap:
                logger.info(
                    "Recent manifest range [%d-%d] overlaps with full catalog range [%d-%d], "
                    "skipping to avoid duplicate work",
                    start_id, end_id, full_start, full_end
                )
                return True

        return False
    except Exception as e:
        logger.debug("Could not check overlap with Redis: %s", e)
        return False


class SecurityWorker(Worker):
    def __init__(self):
        super(SecurityWorker, self).__init__()
        self._next_token = None
        self._model = secscan_model

        interval = app.config.get("SECURITY_SCANNER_INDEXING_INTERVAL", DEFAULT_INDEXING_INTERVAL)
        self.add_operation(self._index_in_scanner, interval)
        self.add_operation(self._index_recent_manifests_in_scanner, interval)

    def _index_in_scanner(self):
        batch_size = app.config.get("SECURITY_SCANNER_V4_BATCH_SIZE", 0)

        # Store the range we're about to process for deduplication with recent manifests
        # Get the current scan position to track the range
        from data.database import Manifest
        from peewee import fn

        try:
            max_id = Manifest.select(fn.Max(Manifest.id)).scalar()
            if max_id:
                start_id = self._next_token.min_id if self._next_token is not None else 1
                # Estimate end_id based on max_id (actual range may be smaller)
                estimated_end_id = min(start_id + batch_size if batch_size > 0 else max_id, max_id)

                # Store the range we're processing
                _store_full_catalog_range(start_id, estimated_end_id)
        except Exception as e:
            logger.debug("Could not track full catalog range: %s", e)

        if app.config.get("SECURITY_SCANNER_V4_LOCK", False):
            try:
                with GlobalLock("SECURITY_SCANNER_V4_INDEXING"):
                    self._next_token = self._model.perform_indexing(self._next_token, batch_size)
            except LockNotAcquiredException:
                return

        else:
            self._next_token = self._model.perform_indexing(self._next_token, batch_size)

    def _index_recent_manifests_in_scanner(self):
        batch_size = app.config.get("SECURITY_SCANNER_V4_RECENT_MANIFEST_BATCH_SIZE", 1000)

        # Check if this range was already scanned by the full catalog operation
        from data.database import Manifest
        from peewee import fn

        try:
            end_id = Manifest.select(fn.Max(Manifest.id)).scalar()
            if end_id:
                start_id = max(end_id - batch_size, 1)

                # Check for overlap with recently processed full catalog range
                if _check_overlap_with_full_catalog(start_id, end_id):
                    logger.info(
                        "Skipping recent manifest indexing - range already covered by full catalog scan"
                    )
                    return
        except Exception as e:
            logger.debug("Could not check overlap for recent manifests: %s", e)

        if app.config.get("SECURITY_SCANNER_V4_RECENT_MANIFEST_BATCH_LOCK", False):
            try:
                with GlobalLock(
                    "SECURITYWORKER_INDEX_RECENT_MANIFEST", lock_ttl=300, auto_renewal=True
                ):
                    self._model.perform_indexing_recent_manifests(batch_size)
            except LockNotAcquiredException:
                logger.warning(
                    "Could not acquire global lock for recent manifest indexing. Skipping"
                )

        else:
            self._model.perform_indexing_recent_manifests(batch_size)


def create_gunicorn_worker():
    """
    follows the gunicorn application factory pattern, enabling
    a quay worker to run as a gunicorn worker thread.

    this is useful when utilizing gunicorn's hot reload in local dev.

    utilizing this method will enforce a 1:1 quay worker to gunicorn worker ratio.
    """
    app.register_blueprint(v2_bp, url_prefix="/v2")
    worker = GunicornWorker(__name__, app, SecurityWorker(), features.SECURITY_SCANNER)
    return worker


if __name__ == "__main__":
    pydev_debug = os.getenv("PYDEV_DEBUG", None)
    if pydev_debug:
        import pydevd_pycharm

        host, port = pydev_debug.split(":")
        pydevd_pycharm.settrace(
            host, port=int(port), stdoutToServer=True, stderrToServer=True, suspend=False
        )

    app.register_blueprint(v2_bp, url_prefix="/v2")

    if app.config.get("ACCOUNT_RECOVERY_MODE", False):
        logger.debug("Quay running in account recovery mode")
        while True:
            time.sleep(100000)

    if not features.SECURITY_SCANNER:
        logger.debug("Security scanner disabled; skipping SecurityWorker")
        while True:
            time.sleep(100000)

    GlobalLock.configure(app.config)
    logging.config.fileConfig(logfile_path(debug=False), disable_existing_loggers=False)
    worker = SecurityWorker()
    worker.start()
