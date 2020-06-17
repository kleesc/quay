import os.path
from container_cloud_config import CloudConfigContext
from jinja2 import FileSystemLoader, Environment


ENV = Environment(loader=FileSystemLoader(os.path.join("templates")))
TEMPLATE = ENV.get_template("example-fcc.yaml.jnj")
CloudConfigContext().populate_jinja_environment(ENV)


if __name__ == '__main__':
    print(
        TEMPLATE.render(
            realm="test1",
            token="test2",
            build_uuid="test3",
            quay_username="test4",
            quay_password="test5",
            manager_hostname="test6",
            websocket_scheme="test7",
            worker_image="quay.io/coreos/registry-build-worker",
            worker_tag="WORKER_TAG",
            logentries_token="LOGENTRIES_TOKEN",
            volume_size="42G",
            max_lifetime_s=10800,
            ssh_authorized_keys=[],
        )
    )
