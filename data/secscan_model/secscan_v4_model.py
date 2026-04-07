import logging
import urllib
from collections import namedtuple
from datetime import datetime, timedelta

from peewee import JOIN, Case, IntegrityError, fn

import features
from data.cache import cache_key
from data.database import (
    IndexerVersion,
    IndexStatus,
    Manifest,
    ManifestSecurityStatus,
    db_for_update,
    db_transaction,
    get_epoch_timestamp_ms,
)
from data.registry_model import registry_model
from data.registry_model.datatypes import Manifest as ManifestDataType
from data.secscan_model.datatypes import (
    NVD,
    CVSSv3,
    Feature,
    Layer,
    Metadata,
    PaginatedNotificationResult,
    PaginatedNotificationStatus,
    ScanLookupStatus,
    SecurityInformation,
    SecurityInformationLookupResult,
    UpdatedVulnerability,
    Vulnerability,
    link_to_cves,
)
from data.secscan_model.interface import (
    InvalidConfigurationException,
    SecurityScannerInterface,
)
from image.docker.schema1 import DOCKER_SCHEMA1_CONTENT_TYPES
from image.docker.schema2 import DOCKER_SCHEMA2_LAYER_CONTENT_TYPE
from image.docker.schema2.manifest import DockerV2ManifestImageLayer
from image.oci import OCI_IMAGE_LAYER_CONTENT_TYPES
from image.oci.manifest import OCIManifestImageLayer
from util.metrics.prometheus import secscan_result_duration
from util.secscan import (
    PRIORITY_LEVELS,
    fetch_vuln_severity,
    get_priority_from_cvssscore,
)
from util.secscan.blob import BlobURLRetriever
from util.secscan.v4.api import (
    APIRequestFailure,
    ClairSecurityScannerAPI,
    InvalidContentSent,
    LayerTooLargeException,
)
from util.secscan.validator import V4SecurityConfigValidator

logger = logging.getLogger(__name__)


DEFAULT_SECURITY_SCANNER_V4_REINDEX_THRESHOLD = 86400  # 1 day
STALE_IN_PROGRESS_HOURS = 6  # Hours before an IN_PROGRESS manifest is considered stale
TAG_LIMIT = 100

IndexReportState = namedtuple("IndexReportState", ["Index_Finished", "Index_Error"])(  # type: ignore[call-arg]
    "IndexFinished", "IndexError"
)


class ScanToken(namedtuple("NextScanToken", ["min_id"])):
    """
    ScanToken represents an opaque token that can be passed between runs of the security worker
    to continue scanning whereever the previous run left off. Note that the data of the token is
    *opaque* to the security worker, and the security worker should *not* pull any data out or modify
    the token in any way.
    """


class NoopV4SecurityScanner(SecurityScannerInterface):
    """
    No-op implementation of the security scanner interface for Clair V4.
    """

    def load_security_information(
        self, manifest_or_legacy_image, include_vulnerabilities=False, model_cache=None
    ):
        return SecurityInformationLookupResult.for_request_error("security scanner misconfigured")

    def perform_indexing(self, start_token=None, batch_size=None):
        return None

    def register_model_cleanup_callbacks(self, data_model_config):
        pass

    @property
    def legacy_api_handler(self):
        raise NotImplementedError("Unsupported for this security scanner version")

    def lookup_notification_page(self, notification_id, page_index=None):
        return None

    def process_notification_page(self, page_result):
        raise NotImplementedError("Unsupported for this security scanner version")

    def mark_notification_handled(self, notification_id):
        raise NotImplementedError("Unsupported for this security scanner version")

    def garbage_collect_manifest_report(self, manifest_digest):
        raise NotImplementedError("Unsupported for this security scanner version")


def _has_container_layers(layers):
    """
    Checks if the layers are proper container image layers. For images that do not contain image layers, such as
    Singularity containers, Helm charts, artifacts and similar, we skip the scanning process.
    """

    CONTAINER_LAYER_TYPES = set(OCI_IMAGE_LAYER_CONTENT_TYPES + [DOCKER_SCHEMA2_LAYER_CONTENT_TYPE])

    if not layers:
        return False

    for layer in layers:
        layer_info = getattr(layer, "layer_info", None)

        if layer_info is None or not hasattr(layer_info, "internal_layer"):
            return False

        internal = layer_info.internal_layer

        # Docker Schema2 layers don't have a mediatype attribute - they're always container images
        if isinstance(internal, DockerV2ManifestImageLayer):
            continue

        # OCI image layers - check blob_layer.mediatype for artifact detection
        if isinstance(internal, OCIManifestImageLayer):
            # If blob_layer is None, it's a processed container image layer
            if internal.blob_layer is None:
                continue
            # If blob_layer exists, check its mediatype
            elif hasattr(internal.blob_layer, "mediatype"):
                if internal.blob_layer.mediatype not in CONTAINER_LAYER_TYPES:
                    return False
                continue
            else:
                # Unknown OCI layer structure
                return False

        # OCI layers with explicit mediatype (alternative format)
        if hasattr(internal, "mediatype"):
            if internal.mediatype not in CONTAINER_LAYER_TYPES:
                return False

        # Other layer formats with blob_layer
        elif hasattr(internal, "blob_layer") and hasattr(internal.blob_layer, "mediatype"):
            if internal.blob_layer.mediatype not in CONTAINER_LAYER_TYPES:
                return False

        # Unknown layer structure
        else:
            return False

    return True


def maybe_urlencoded(fixed_in: str) -> str:
    """
    Handles Clair's `fixed_in_version`, which _may_ be URL-encoded.
    The API guarantee is only that the field is a string, so encoding it's
    slightly weaselly, but only slightly.
    """
    try:
        d = urllib.parse.parse_qs(fixed_in)
        # There may be additional known-good keys in the future.
        return d["fixed"][0]
    except (ValueError, KeyError):
        return fixed_in


class V4SecurityScanner(SecurityScannerInterface):
    """
    Implementation of the security scanner interface for Clair V4 API-compatible implementations.
    """

    def __init__(self, app, instance_keys, storage):
        self.app = app
        self.storage = storage

        if app.config.get("SECURITY_SCANNER_V4_ENDPOINT", None) is None:
            raise InvalidConfigurationException(
                "Missing SECURITY_SCANNER_V4_ENDPOINT configuration"
            )

        validator = V4SecurityConfigValidator(
            app.config.get("FEATURE_SECURITY_SCANNER", False),
            app.config.get("SECURITY_SCANNER_V4_ENDPOINT", None),
        )

        if not validator.valid():
            msg = "Failed to validate security scanner V4 configuration"
            logger.warning(msg)
            raise InvalidConfigurationException(msg)

        self._secscan_api = ClairSecurityScannerAPI(
            endpoint=app.config.get("SECURITY_SCANNER_V4_ENDPOINT"),
            client=app.config.get("HTTPCLIENT"),
            blob_url_retriever=BlobURLRetriever(storage, instance_keys, app),
            jwt_psk=app.config.get("SECURITY_SCANNER_V4_PSK", None),
            max_layer_size=app.config.get("SECURITY_SCANNER_V4_INDEX_MAX_LAYER_SIZE", None),
        )

    def load_security_information(
        self, manifest_or_legacy_image, include_vulnerabilities=False, model_cache=None
    ):
        if not isinstance(manifest_or_legacy_image, ManifestDataType):
            return SecurityInformationLookupResult.with_status(
                ScanLookupStatus.UNSUPPORTED_FOR_INDEXING
            )

        status = None
        try:
            status = ManifestSecurityStatus.get(manifest=manifest_or_legacy_image._db_id)
        except ManifestSecurityStatus.DoesNotExist:
            return SecurityInformationLookupResult.with_status(ScanLookupStatus.NOT_YET_INDEXED)

        if status.index_status == IndexStatus.FAILED:
            return SecurityInformationLookupResult.with_status(ScanLookupStatus.FAILED_TO_INDEX)

        if status.index_status == IndexStatus.MANIFEST_UNSUPPORTED:
            return SecurityInformationLookupResult.with_status(
                ScanLookupStatus.UNSUPPORTED_FOR_INDEXING
            )

        if status.index_status == IndexStatus.MANIFEST_LAYER_TOO_LARGE:
            return SecurityInformationLookupResult.with_status(
                ScanLookupStatus.MANIFEST_LAYER_TOO_LARGE
            )

        if status.index_status in (IndexStatus.IN_PROGRESS, IndexStatus.QUEUED):
            return SecurityInformationLookupResult.with_status(ScanLookupStatus.NOT_YET_INDEXED)

        assert status.index_status == IndexStatus.COMPLETED

        def security_report_loader():
            return self._secscan_api.vulnerability_report(manifest_or_legacy_image.digest)

        try:
            if model_cache:
                security_report_key = cache_key.for_security_report(
                    manifest_or_legacy_image.digest, model_cache.cache_config
                )
                report = model_cache.retrieve(security_report_key, security_report_loader)
            else:
                report = security_report_loader()
        except APIRequestFailure as arf:
            return SecurityInformationLookupResult.for_request_error(str(arf))

        if report is None:
            return SecurityInformationLookupResult.with_status(ScanLookupStatus.NOT_YET_INDEXED)

        # TODO(alecmerdler): Provide a way to indicate the current scan is outdated (`report.state != status.indexer_hash`)

        return SecurityInformationLookupResult.for_data(
            SecurityInformation(Layer(report["manifest_hash"], "", "", 4, features_for(report)))
        )

    def perform_indexing(self, start_token=None, batch_size=None):
        try:
            indexer_state = self._secscan_api.state()
        except APIRequestFailure:
            return None

        if not batch_size:
            batch_size = self.app.config.get("SECURITY_SCANNER_V4_BATCH_SIZE", 50)

        reindex_threshold = datetime.utcnow() - timedelta(
            seconds=self.app.config.get(
                "SECURITY_SCANNER_V4_REINDEX_THRESHOLD",
                DEFAULT_SECURITY_SCANNER_V4_REINDEX_THRESHOLD,
            )
        )

        while True:
            self._enqueue_manifests(batch_size)
            claimed = self._claim_manifests(batch_size, reindex_threshold, indexer_state)
            if not claimed:
                break
            self._process_claimed(claimed)

        return ScanToken(0)

    def _enqueue_manifests(self, batch_size):
        """
        Find manifests without a ManifestSecurityStatus row and insert them
        with QUEUED status. Uses ON CONFLICT DO NOTHING so concurrent workers
        inserting the same manifest are harmless.
        """
        candidates = list(
            Manifest.select(Manifest.id, Manifest.repository)
            .where(
                ~fn.EXISTS(
                    ManifestSecurityStatus.select(ManifestSecurityStatus.id).where(
                        ManifestSecurityStatus.manifest == Manifest.id
                    )
                )
            )
            .order_by(Manifest.id.desc())
            .limit(batch_size)
        )

        if not candidates:
            return 0

        rows = [
            {
                ManifestSecurityStatus.manifest: c.id,
                ManifestSecurityStatus.repository: c.repository_id,
                ManifestSecurityStatus.index_status: IndexStatus.QUEUED,
                ManifestSecurityStatus.indexer_hash: "",
                ManifestSecurityStatus.indexer_version: IndexerVersion.V4,
                ManifestSecurityStatus.metadata_json: {},
                ManifestSecurityStatus.error_json: {},
            }
            for c in candidates
        ]

        ManifestSecurityStatus.insert_many(rows).on_conflict_ignore().execute()
        return len(candidates)

    def _claim_manifests(self, batch_size, reindex_threshold, indexer_state):
        """
        Atomically claim a batch of manifests for scanning using
        FOR UPDATE SKIP LOCKED. Returns claimed ManifestSecurityStatus rows.

        Priority order: QUEUED -> FAILED -> stale IN_PROGRESS -> needs reindex.
        """
        stale_threshold = datetime.utcnow() - timedelta(hours=STALE_IN_PROGRESS_HOURS)
        indexer_hash = indexer_state.get("state", "")

        claimable = (
            (ManifestSecurityStatus.index_status == IndexStatus.QUEUED)
            | (
                (ManifestSecurityStatus.index_status == IndexStatus.FAILED)
                & (ManifestSecurityStatus.last_indexed < reindex_threshold)
            )
            | (
                (ManifestSecurityStatus.index_status == IndexStatus.IN_PROGRESS)
                & (ManifestSecurityStatus.last_indexed < stale_threshold)
            )
            | (
                (
                    ManifestSecurityStatus.index_status.not_in(
                        [
                            IndexStatus.MANIFEST_UNSUPPORTED,
                            IndexStatus.MANIFEST_LAYER_TOO_LARGE,
                            IndexStatus.QUEUED,
                        ]
                    )
                )
                & (ManifestSecurityStatus.indexer_hash != indexer_hash)
                & (ManifestSecurityStatus.last_indexed < reindex_threshold)
            )
        )

        priority = Case(
            None,
            [
                (ManifestSecurityStatus.index_status == IndexStatus.QUEUED, 0),
                (ManifestSecurityStatus.index_status == IndexStatus.FAILED, 1),
                (ManifestSecurityStatus.index_status == IndexStatus.IN_PROGRESS, 2),
            ],
            3,
        )

        with db_transaction():
            query = (
                ManifestSecurityStatus.select()
                .where(claimable)
                .order_by(priority.asc(), ManifestSecurityStatus.manifest.desc())
                .limit(batch_size)
            )
            claimed = list(db_for_update(query, skip_locked=True))

            if not claimed:
                return []

            claimed_ids = [c.id for c in claimed]
            ManifestSecurityStatus.update(
                index_status=IndexStatus.IN_PROGRESS,
                indexer_hash="in_progress",
                last_indexed=datetime.utcnow(),
            ).where(ManifestSecurityStatus.id.in_(claimed_ids)).execute()

        return claimed

    def _update_status(self, mss, index_status, indexer_hash, error_json=None):
        """
        Update ManifestSecurityStatus to final state. Simple UPDATE replaces
        the previous DELETE+INSERT pattern.
        """
        ManifestSecurityStatus.update(
            index_status=index_status,
            indexer_hash=indexer_hash,
            indexer_version=IndexerVersion.V4,
            error_json=error_json or {},
            metadata_json={},
            last_indexed=datetime.utcnow(),
        ).where(ManifestSecurityStatus.id == mss.id).execute()

    def _maybe_notify_new_vulnerabilities(self, manifest):
        """
        If configured, fetch the vulnerability report and emit notifications
        for vulnerabilities meeting the minimum severity threshold.
        """
        if not features.SECURITY_SCANNER_NOTIFY_ON_NEW_INDEX:
            return

        try:
            vulnerability_report = self._secscan_api.vulnerability_report(manifest.digest)
        except APIRequestFailure:
            return

        if vulnerability_report is None:
            return

        found_vulnerabilities = vulnerability_report.get("vulnerabilities")
        if found_vulnerabilities is None:
            return

        level = (
            self.app.config.get("NOTIFICATION_MIN_SEVERITY_ON_NEW_INDEX")
            if self.app.config.get("NOTIFICATION_MIN_SEVERITY_ON_NEW_INDEX")
            else "High"
        )
        lowest_severity = PRIORITY_LEVELS[level]

        import notifications  # circular import if moved to top level

        logger.debug("Attempting to create notifications for manifest %s", manifest)

        for key in list(found_vulnerabilities):
            vuln = found_vulnerabilities[key]
            found_severity = PRIORITY_LEVELS.get(
                vuln["normalized_severity"], PRIORITY_LEVELS["Unknown"]
            )

            if found_severity["score"] >= lowest_severity["score"]:
                tag_names = list(
                    registry_model.tag_names_for_manifest(manifest, TAG_LIMIT)
                )
                event_data = {
                    "tags": list(tag_names) if tag_names else [manifest.digest],
                    "vulnerable_index_report_created": "true",
                    "vulnerability": {
                        "id": vuln["id"],
                        "description": vuln["description"],
                        "link": vuln["links"],
                        "priority": vuln["severity"],
                        "has_fix": bool(vuln["fixed_in_version"]),
                    },
                }

                logger.debug("Created notification with event_data: %s", event_data)
                notifications.spawn_notification(
                    manifest.repository, "vulnerability_found", event_data
                )

    def _process_claimed(self, claimed_rows):
        """
        Process each claimed manifest: validate, scan via Clair, update status.
        """
        for mss in claimed_rows:
            try:
                candidate = Manifest.get(Manifest.id == mss.manifest_id)
            except Manifest.DoesNotExist:
                ManifestSecurityStatus.delete().where(
                    ManifestSecurityStatus.id == mss.id
                ).execute()
                continue

            manifest = ManifestDataType.for_manifest(candidate, None)

            if manifest.is_manifest_list:
                self._update_status(mss, IndexStatus.MANIFEST_UNSUPPORTED, "none")
                continue

            layers = registry_model.list_manifest_layers(manifest, self.storage, True)

            if layers is None or len(layers) == 0:
                logger.warning(
                    "Cannot index %s/%s@%s: manifest has no layers",
                    candidate.repository.namespace_user,
                    candidate.repository.name,
                    manifest.digest,
                )
                self._update_status(mss, IndexStatus.MANIFEST_UNSUPPORTED, "none")
                continue

            if manifest.media_type not in DOCKER_SCHEMA1_CONTENT_TYPES:
                if not _has_container_layers(layers):
                    logger.info(
                        "Cannot index %s/%s@%s: appears to be an artifact image",
                        candidate.repository.namespace_user,
                        candidate.repository.name,
                        manifest.digest,
                    )
                    self._update_status(mss, IndexStatus.MANIFEST_UNSUPPORTED, "none")
                    continue

            logger.debug(
                "Indexing manifest [%d] %s/%s@%s",
                manifest._db_id,
                candidate.repository.namespace_user,
                candidate.repository.name,
                manifest.digest,
            )

            try:
                (report, state) = self._secscan_api.index(manifest, layers)
            except InvalidContentSent:
                self._update_status(mss, IndexStatus.MANIFEST_UNSUPPORTED, "none")
                logger.exception("Failed to perform indexing, invalid content sent")
                continue
            except APIRequestFailure as ex:
                self._update_status(
                    mss, IndexStatus.FAILED, "api_failure", {"error": str(ex)}
                )
                logger.exception("Failed to perform indexing, security scanner API error")
                continue
            except LayerTooLargeException:
                self._update_status(mss, IndexStatus.MANIFEST_LAYER_TOO_LARGE, "none")
                logger.exception("Failed to perform indexing, layer too large")
                continue

            if report["state"] == IndexReportState.Index_Finished:
                self._update_status(mss, IndexStatus.COMPLETED, state, report["err"])

                if not manifest.has_been_scanned:
                    created_at = manifest.created_at
                    if created_at is not None:
                        dur_ms = get_epoch_timestamp_ms() - created_at
                        dur_sec = dur_ms / 1000
                        secscan_result_duration.observe(dur_sec)

                    self._maybe_notify_new_vulnerabilities(manifest)

            elif report["state"] == IndexReportState.Index_Error:
                self._update_status(mss, IndexStatus.FAILED, state, report["err"])
            else:
                self._update_status(
                    mss,
                    IndexStatus.FAILED,
                    "unknown_state",
                    {"error": "unknown_state", "state": report.get("state")},
                )
                logger.warning(
                    "Unknown index state '%s' for manifest %d, marked as FAILED",
                    report.get("state"),
                    candidate.id,
                )

    def lookup_notification_page(self, notification_id, page_index=None):
        try:
            notification_page_results = self._secscan_api.retrieve_notification_page(
                notification_id, page_index
            )

            # If we get back None, then the notification no longer exists.
            if notification_page_results is None:
                return PaginatedNotificationResult(
                    PaginatedNotificationStatus.FATAL_ERROR, None, None
                )
        except APIRequestFailure:
            return PaginatedNotificationResult(
                PaginatedNotificationStatus.RETRYABLE_ERROR, None, None
            )

        # FIXME(alecmerdler): Debugging tests failing in CI
        return PaginatedNotificationResult(
            PaginatedNotificationStatus.SUCCESS,
            notification_page_results["notifications"],
            notification_page_results.get("page", {}).get("next"),
        )

    def mark_notification_handled(self, notification_id):
        try:
            self._secscan_api.delete_notification(notification_id)
            return True
        except APIRequestFailure:
            return False

    def process_notification_page(self, page_result):
        for notification_data in page_result:
            if notification_data["reason"] != "added":
                continue

            yield UpdatedVulnerability(
                notification_data["manifest"],
                Vulnerability(
                    Severity=notification_data["vulnerability"].get("normalized_severity"),
                    Description=notification_data["vulnerability"].get("description"),
                    NamespaceName=notification_data["vulnerability"].get("package", {}).get("name"),
                    Name=notification_data["vulnerability"].get("name"),
                    FixedBy=maybe_urlencoded(
                        notification_data["vulnerability"].get("fixed_in_version")
                    ),
                    Link=notification_data["vulnerability"].get("links"),
                    Metadata={},
                ),
            )

    def register_model_cleanup_callbacks(self, data_model_config):
        pass

    @property
    def legacy_api_handler(self):
        raise NotImplementedError("Unsupported for this security scanner version")

    def garbage_collect_manifest_report(self, manifest_digest):
        def manifest_digest_exists():
            query = Manifest.select(can_use_read_replica=True).where(
                Manifest.digest == manifest_digest
            )

            try:
                query.get()
            except Manifest.DoesNotExist:
                return False

            return True

        with db_transaction():
            if not manifest_digest_exists():
                try:
                    self._secscan_api.delete(manifest_digest)
                    return True
                except APIRequestFailure:
                    logger.exception("Failed to delete manifest, security scanner API error")

        return None


def features_for(report):
    """
    Transforms a Clair v4 `VulnerabilityReport` dict into the standard shape of a
    Quay Security scanner response.
    """

    features = []
    dedupe_vulns = {}
    for pkg_id, pkg in report["packages"].items():
        pkg_env = report["environments"][pkg_id][0]
        pkg_vulns = []
        # Quay doesn't care about vulnerabilities reported from different
        # repos so dedupe them. Key = package_name + package_version + vuln_name.
        for vuln_id in report["package_vulnerabilities"].get(pkg_id, []):
            vuln_key = (
                pkg["name"]
                + "_"
                + pkg["version"]
                + "_"
                + report["vulnerabilities"][vuln_id].get("name", "")
            )
            if not dedupe_vulns.get(vuln_key, False):
                pkg_vulns.append(report["vulnerabilities"][vuln_id])
            dedupe_vulns[vuln_key] = True

        enrichments = (
            {
                key: sorted(val, key=lambda x: x["baseScore"], reverse=True)[0]
                for key, val in list(report["enrichments"].values())[0][0].items()
            }
            if report.get("enrichments", {})
            else {}
        )

        base_scores = []
        if report.get("enrichments", {}):
            for enrichment_list in report["enrichments"].values():
                for pkg_vuln in enrichment_list:
                    for k, v in pkg_vuln.items():
                        if not isinstance(v, list):
                            logger.error(f"Unexpected type for value of key '{k}': {type(v)}")
                            continue
                        for item in v:
                            if not isinstance(item, dict) or "baseScore" not in item:
                                logger.error(f"Invalid item format or missing 'baseScore': {item}")
                                continue
                            base_scores.append(item["baseScore"])

        cve_ids = [link_to_cves(v["links"]) for v in pkg_vulns]

        features.append(
            Feature(
                pkg["name"],
                "",
                "",
                pkg_env["introduced_in"],
                pkg["version"],
                base_scores,
                cve_ids,
                [
                    Vulnerability(
                        fetch_vuln_severity(vuln, enrichments),
                        vuln["updater"],
                        vuln["links"],
                        maybe_urlencoded(
                            vuln["fixed_in_version"] if vuln["fixed_in_version"] != "0" else ""
                        ),
                        vuln["description"],
                        vuln["name"],
                        Metadata(
                            vuln["updater"],
                            vuln.get("repository", {}).get("name"),
                            vuln.get("repository", {}).get("uri"),
                            vuln.get("distribution", {}).get("name"),
                            vuln.get("distribution", {}).get("version"),
                            NVD(
                                CVSSv3(
                                    enrichments.get(vuln["id"], {}).get("vectorString", ""),
                                    enrichments.get(vuln["id"], {}).get("baseScore", ""),
                                )
                            ),
                        ),
                    )
                    for vuln in pkg_vulns
                ],
            )
        )

    return features
