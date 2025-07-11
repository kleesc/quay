import hashlib
import json
import logging
import os
from functools import partial

from authlib.jose import JsonWebKey
from flask import Flask, Request, request
from flask_login import LoginManager
from flask_mail import Mail
from flask_principal import Principal
from opentelemetry.instrumentation.flask import FlaskInstrumentor
from opentelemetry.instrumentation.psycopg2 import Psycopg2Instrumentor
from werkzeug.exceptions import HTTPException
from werkzeug.middleware.proxy_fix import ProxyFix

import features
from _init import (
    IS_BUILDING,
    IS_KUBERNETES,
    IS_TESTING,
    OVERRIDE_CONFIG_DIRECTORY,
    config_provider,
)
from avatars.avatars import Avatar
from buildman.manager.buildcanceller import BuildCanceller
from data import database, logs_model, model
from data.archivedlogs import LogArchive
from data.billing import Billing
from data.buildlogs import BuildLogs
from data.cache import get_model_cache
from data.model.user import LoginWrappedDBUser
from data.queue import WorkQueue
from data.registry_model import registry_model
from data.secscan_model import secscan_model
from data.userevent import UserEventsBuilderModule
from data.userfiles import Userfiles
from data.users import UserAuthentication, UserManager
from image.oci import register_artifact_type
from oauth.loginmanager import OAuthLoginManager
from oauth.services.github import GithubOAuthService
from oauth.services.gitlab import GitLabOAuthService
from path_converters import (
    APIRepositoryPathConverter,
    RegexConverter,
    RepositoryPathConverter,
    RepositoryPathRedirectConverter,
    V1CreateRepositoryPathConverter,
)
from storage import Storage
from util import get_app_url
from util.config import URLSchemeAndHostname
from util.config.configutil import generate_secret_key
from util.greenlet_tracing import enable_tracing
from util.ipresolver import IPResolver
from util.label_validator import LabelValidator
from util.log import filter_logs
from util.marketplace import MarketplaceSubscriptionApi, MarketplaceUserApi
from util.metrics.otel import init_exporter
from util.metrics.prometheus import PrometheusPlugin
from util.names import urn_generator
from util.repomirror.api import RepoMirrorAPI
from util.saas.analytics import Analytics
from util.saas.exceptionlog import Sentry
from util.security.instancekeys import InstanceKeys
from util.tufmetadata.api import TUFMetadataAPI

OVERRIDE_CONFIG_YAML_FILENAME = os.path.join(OVERRIDE_CONFIG_DIRECTORY, "config.yaml")
OVERRIDE_CONFIG_PY_FILENAME = os.path.join(OVERRIDE_CONFIG_DIRECTORY, "config.py")

OVERRIDE_CONFIG_KEY = "QUAY_OVERRIDE_CONFIG"

DOCKER_V2_SIGNINGKEY_FILENAME = "docker_v2.pem"
INIT_SCRIPTS_LOCATION = "/conf/init/"

app = Flask(__name__)

logger = logging.getLogger(__name__)

# Instantiate the configuration.
is_testing = IS_TESTING
is_kubernetes = IS_KUBERNETES
is_building = IS_BUILDING

if is_testing:
    from test.testconfig import TestConfig

    logger.debug("Loading test config.")
    app.config.from_object(TestConfig())
else:
    from config import DefaultConfig

    logger.debug("Loading default config.")
    app.config.from_object(DefaultConfig())
    app.teardown_request(database.close_db_filter)

# Load the override config via the provider.
config_provider.update_app_config(app.config)

# Check if TESTING is properly set
if app.config.get("TESTING", False):
    logger.warning(
        "🟡🟡🟡 Detected TESTING: true on startup. TESTING property is either missing from config.yaml or set to 'true'."
    )
    logger.warning(
        "🟡🟡🟡 Quay starting in TESTING mode, some functionality such as e-mail sending will not be available."
    )

# Update any configuration found in the override environment variable.
environ_config = json.loads(os.environ.get(OVERRIDE_CONFIG_KEY, "{}"))
app.config.update(environ_config)

# Fix remote address handling for Flask.
if app.config.get("PROXY_COUNT", 1):
    app.wsgi_app = ProxyFix(app.wsgi_app)  # type: ignore[method-assign]

# Allow user to define a custom storage preference for the local instance.
_distributed_storage_preference = os.environ.get("QUAY_DISTRIBUTED_STORAGE_PREFERENCE", "").split()
if _distributed_storage_preference:
    app.config["DISTRIBUTED_STORAGE_PREFERENCE"] = _distributed_storage_preference

# Generate a secret key if none was specified.
if app.config["SECRET_KEY"] is None:
    logger.debug("Generating in-memory secret key")
    app.config["SECRET_KEY"] = generate_secret_key()

# If the "preferred" scheme is https, then http is not allowed. Therefore, ensure we have a secure
# session cookie.
if app.config["PREFERRED_URL_SCHEME"] == "https" and not app.config.get(
    "FORCE_NONSECURE_SESSION_COOKIE", False
):
    app.config["SESSION_COOKIE_SECURE"] = True

# Load features from config.
features.import_features(app.config)

# Register additional experimental artifact types.
# TODO: extract this into a real, dynamic registration system.
if features.GENERAL_OCI_SUPPORT:
    for media_type, layer_types in app.config["ALLOWED_OCI_ARTIFACT_TYPES"].items():
        register_artifact_type(media_type, layer_types)

if features.HELM_OCI_SUPPORT:
    HELM_CHART_CONFIG_TYPE = "application/vnd.cncf.helm.config.v1+json"
    HELM_CHART_LAYER_TYPES = [
        "application/tar+gzip",
        "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
    ]
    register_artifact_type(HELM_CHART_CONFIG_TYPE, HELM_CHART_LAYER_TYPES)


CONFIG_DIGEST = hashlib.sha256(json.dumps(app.config, default=str).encode("utf-8")).hexdigest()[0:8]

logger.debug("Loaded config", extra={"config": app.config})


class RequestWithId(Request):
    request_gen = staticmethod(urn_generator(["request"]))

    def __init__(self, *args, **kwargs):
        super(RequestWithId, self).__init__(*args, **kwargs)
        self.request_id = self.request_gen()


@app.before_request
def _request_start():
    if os.getenv("PYDEV_DEBUG", None):
        import pydevd_pycharm

        host, port = os.getenv("PYDEV_DEBUG").split(":")
        pydevd_pycharm.settrace(
            host,
            port=int(port),
            stdoutToServer=True,
            stderrToServer=True,
            suspend=False,
        )

    debug_extra = {}
    x_forwarded_for = request.headers.get("X-Forwarded-For", None)
    if x_forwarded_for is not None:
        debug_extra["X-Forwarded-For"] = x_forwarded_for

    logger.debug("Starting request: %s (%s) %s", request.request_id, request.path, debug_extra)


DEFAULT_FILTER = lambda x: "[FILTERED]"
FILTERED_VALUES = [
    {"key": ["password"], "fn": DEFAULT_FILTER},
    {"key": ["upstream_registry_password"], "fn": DEFAULT_FILTER},
    {"key": ["upstream_registry_username"], "fn": DEFAULT_FILTER},
    {"key": ["user", "password"], "fn": DEFAULT_FILTER},
    {"key": ["user", "repeatPassword"], "fn": DEFAULT_FILTER},
    {"key": ["blob"], "fn": lambda x: x[0:8]},
]


@app.after_request
def _request_end(resp):
    try:
        jsonbody = request.get_json(force=True, silent=True)
    except HTTPException:
        jsonbody = None

    values = request.values.to_dict()

    if isinstance(jsonbody, dict):
        filter_logs(jsonbody, FILTERED_VALUES)

    if jsonbody and not isinstance(jsonbody, dict):
        jsonbody = {"_parsererror": jsonbody}

    if isinstance(values, dict):
        filter_logs(values, FILTERED_VALUES)

    extra = {
        "endpoint": request.endpoint,
        "request_id": request.request_id,
        "remote_addr": request.remote_addr,
        "http_method": request.method,
        "original_url": request.url,
        "path": request.path,
        "parameters": values,
        "json_body": jsonbody,
        "confsha": CONFIG_DIGEST,
    }

    if request.user_agent is not None:
        extra["user-agent"] = request.user_agent.string

    logger.debug("Ending request: %s (%s) %s", request.request_id, request.path, extra)
    return resp


if app.config.get("GREENLET_TRACING", True):
    enable_tracing()

root_logger = logging.getLogger()

app.request_class = RequestWithId

# Register custom converters.
app.url_map.converters["regex"] = RegexConverter
app.url_map.converters["repopath"] = RepositoryPathConverter
app.url_map.converters["apirepopath"] = APIRepositoryPathConverter
app.url_map.converters["repopathredirect"] = RepositoryPathRedirectConverter
app.url_map.converters["v1createrepopath"] = V1CreateRepositoryPathConverter

Principal(app, use_sessions=False)

tf = app.config["DB_TRANSACTION_FACTORY"]

model_cache = get_model_cache(app.config)
avatar = Avatar(app)
login_manager = LoginManager(app)
mail = Mail(app)
prometheus = PrometheusPlugin(app)
chunk_cleanup_queue = WorkQueue(app.config["CHUNK_CLEANUP_QUEUE_NAME"], tf)
instance_keys = InstanceKeys(app)
ip_resolver = IPResolver(app)
storage = Storage(app, chunk_cleanup_queue, instance_keys, config_provider, ip_resolver)
userfiles = Userfiles(app, storage)
log_archive = LogArchive(app, storage)
analytics = Analytics(app)
billing = Billing(app)
sentry = Sentry(app)
build_logs = BuildLogs(app)
userevents = UserEventsBuilderModule(app)
label_validator = LabelValidator(app)
build_canceller = BuildCanceller(app)

github_trigger = GithubOAuthService(app.config, "GITHUB_TRIGGER_CONFIG")
gitlab_trigger = GitLabOAuthService(app.config, "GITLAB_TRIGGER_CONFIG")

oauth_login = OAuthLoginManager(app.config)
oauth_apps = [github_trigger, gitlab_trigger]

authentication = UserAuthentication(app, config_provider, OVERRIDE_CONFIG_DIRECTORY, oauth_login)
usermanager = UserManager(app, authentication)

image_replication_queue = WorkQueue(app.config["REPLICATION_QUEUE_NAME"], tf, has_namespace=False)
proxy_cache_blob_queue = WorkQueue(
    app.config["PROXY_CACHE_BLOB_QUEUE_NAME"], tf, has_namespace=True
)
dockerfile_build_queue = WorkQueue(
    app.config["DOCKERFILE_BUILD_QUEUE_NAME"], tf, has_namespace=True
)
notification_queue = WorkQueue(app.config["NOTIFICATION_QUEUE_NAME"], tf, has_namespace=True)
secscan_notification_queue = WorkQueue(
    app.config["SECSCAN_V4_NOTIFICATION_QUEUE_NAME"], tf, has_namespace=False
)
export_action_logs_queue = WorkQueue(
    app.config["EXPORT_ACTION_LOGS_QUEUE_NAME"], tf, has_namespace=True
)

repository_gc_queue = WorkQueue(app.config["REPOSITORY_GC_QUEUE_NAME"], tf, has_namespace=True)

# Note: We set `has_namespace` to `False` here, as we explicitly want this queue to not be emptied
# when a namespace is marked for deletion.
namespace_gc_queue = WorkQueue(app.config["NAMESPACE_GC_QUEUE_NAME"], tf, has_namespace=False)

all_queues = [
    image_replication_queue,
    dockerfile_build_queue,
    notification_queue,
    chunk_cleanup_queue,
    repository_gc_queue,
    namespace_gc_queue,
]

url_scheme_and_hostname = URLSchemeAndHostname(
    app.config["PREFERRED_URL_SCHEME"], app.config["SERVER_HOSTNAME"]
)

repo_mirror_api = RepoMirrorAPI(
    app.config,
    app.config["SERVER_HOSTNAME"],
    app.config["HTTPCLIENT"],
    instance_keys=instance_keys,
)

tuf_metadata_api = TUFMetadataAPI(app, app.config)

marketplace_users = MarketplaceUserApi(app)
marketplace_subscriptions = MarketplaceSubscriptionApi(app)

# Check for a key in config. If none found, generate a new signing key for Docker V2 manifests.
_v2_key_path = os.path.join(OVERRIDE_CONFIG_DIRECTORY, DOCKER_V2_SIGNINGKEY_FILENAME)
if os.path.exists(_v2_key_path):
    with open(_v2_key_path) as key_file:
        docker_v2_signing_key = JsonWebKey.import_key(key_file.read())
else:
    docker_v2_signing_key = JsonWebKey.generate_key("RSA", 2048, is_private=True)

# Check if georeplication is turned on and whether env. variables exist:
if os.environ.get("QUAY_DISTRIBUTED_STORAGE_PREFERENCE") is None and app.config.get(
    "FEATURE_STORAGE_REPLICATION", False
):
    raise Exception(
        "Missing storage preference, did you perhaps forget to define QUAY_DISTRIBUTED_STORAGE_PREFERENCE variable?"
    )

# Configure the database.
if app.config.get("DATABASE_SECRET_KEY") is None and app.config.get("SETUP_COMPLETE", False):
    raise Exception("Missing DATABASE_SECRET_KEY in config; did you perhaps forget to add it?")

database.configure(app.config)

model.config.app_config = app.config
model.config.store = storage
model.config.register_repo_cleanup_callback(tuf_metadata_api.delete_metadata)

secscan_model.configure(app, instance_keys, storage)

logs_model.configure(app.config)

# NOTE: We re-use the page token key here as this is just to obfuscate IDs for V1, and
# does not need to actually be secure.
registry_model.set_id_hash_salt(app.config.get("PAGE_TOKEN_KEY"))


@login_manager.user_loader
def load_user(user_uuid):
    logger.debug("User loader loading deferred user with uuid: %s", user_uuid)
    return LoginWrappedDBUser(user_uuid)


get_app_url = partial(get_app_url, app.config)

if features.OTEL_TRACING:
    FlaskInstrumentor().instrument_app(
        app,
        excluded_urls=app.config.get("OTEL_TRACING_EXCLUDED_URLS", None),
    )
    Psycopg2Instrumentor().instrument()
    init_exporter(app.config)
