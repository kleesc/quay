import json
import logging
import time

from peewee import fn

import features
from app import app
from data.database import Manifest
from util.bytes import Bytes
from util.log import logfile_path
from util.migrate.allocator import yield_random_entries
from workers.gunicorn_worker import GunicornWorker
from workers.worker import Worker

logger = logging.getLogger(__name__)

WORKER_FREQUENCY = app.config.get("MANIFEST_SUBJECT_BACKFILL_WORKER_FREQUENCY", 60)


class ManifestJSONBackfillWorker(Worker):
    """
    Worker which backfills the newly added jsonb fields onto Manifest.
    """

    def __init__(self):
        super().__init__()
        self.add_operation(self._backfill_manifest_subject, WORKER_FREQUENCY)

    def _backfill_manifest_subject(self):
        try:
            Manifest.select().where(Manifest.manifest_json == None).get()
        except Manifest.DoesNotExist:
            logger.debug("Manifest json backfill worker has completed; skipping")
            return False

        iterator = yield_random_entries(
            lambda: Manifest.select().where(Manifest.manifest_json == None),
            Manifest.id,
            250,
            Manifest.select(fn.Max(Manifest.id)).scalar(),
            1,
        )

        for manifest_row, abt, _ in iterator:
            if manifest_row.manifest_json:
                logger.debug("Another worker preempted this worker")
                abt.set()
                continue

            logger.debug("Setting manifest json for manifest %s", manifest_row.id)
            manifest_bytes = Bytes.for_string_or_unicode(manifest_row.manifest_bytes)

            updated = (
                Manifest.update(
                    manifest_json=json.loads(manifest_row.manifest_bytes)
                )
                .where(Manifest.id == manifest_row.id, Manifest.manifest_json == None)
                .execute()
            )
            if updated != 1:
                logger.debug("Another worker preempted this worker")
                abt.set()
                continue

        return True


def create_gunicorn_worker():
    """
    follows the gunicorn application factory pattern, enabling
    a quay worker to run as a gunicorn worker thread.

    this is useful when utilizing gunicorn's hot reload in local dev.

    utilizing this method will enforce a 1:1 quay worker to gunicorn worker ratio.
    """
    worker = GunicornWorker(
        __name__, app, ManifestJSONBackfillWorker(), features.MANIFEST_JSON_BACKFILL
    )
    return worker


def main():
    logging.config.fileConfig(logfile_path(debug=False), disable_existing_loggers=False)

    if app.config.get("ACCOUNT_RECOVERY_MODE", False):
        logger.debug("Quay running in account recovery mode")
        while True:
            time.sleep(100000)

    if not features.MANIFEST_JSON_BACKFILL:
        logger.debug("Manifest backfill worker not enabled; skipping")
        while True:
            time.sleep(100000)

    worker = ManifestJSONBackfillWorker()
    worker.start()


if __name__ == "__main__":
    main()
