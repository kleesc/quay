"""A Python Pulumi program"""

import pulumi
import pulumi_docker as docker
from pulumi_kubernetes.apps.v1 import Deployment
from pulumi_kubernetes.core.v1 import Service

config = pulumi.Config()

stack = pulumi.get_stack()

print("TESTING STACK:", stack)

if stack == "dev":

    postgres_image = docker.RemoteImage(
        "postgres_image",
        name="postgres:12"
    )

    redis_image = docker.RemoteImage(
        "redis_image",
        name="redis:5"
    )

    network = docker.Network("network", name=f"quay-net")

    postgres_container = docker.Container(
        "postgres_container",
        image=postgres_image.repo_digest,
        name=f"postgres",
        envs=[
            f"POSTGRES_USER=devtable",
            f"POSTGRES_PASSWORD=password",
	    f"POSTGRES_DB=registry_database",
        ],
        ports=[docker.ContainerPortArgs(
            internal=5432,
            external=5432
        )],
        networks_advanced=[docker.ContainerNetworksAdvancedArgs(
            name=network.name,
            aliases=["postgres"]
        )]
    )


    redis_container = docker.Container(
        "redis_container",
        image=redis_image.repo_digest,
        name=f"redis",
        ports=[docker.ContainerPortArgs(
            internal=6379,
            external=6379
        )],
        networks_advanced=[docker.ContainerNetworksAdvancedArgs(
            name=network.name,
            aliases=["redis"]
        )]
    )

    pulumi.export("quay-postgres", pulumi.Output.format("http://localhost:{0}", 5432))

else:
    app_name = "redis"
    app_labels = { "app": app_name }

    deployment = Deployment(
        app_name,
        spec={
            "selector": { "match_labels": app_labels },
            "replicas": 1,
            "template": {
                "metadata": { "labels": app_labels },
                "spec": { "containers": [{ "name": app_name, "image": "redis:6" }] }
            }
        })

    frontend = Service(
        app_name,
        metadata={
            "labels": deployment.spec["template"]["metadata"]["labels"],
        },
        spec={
            "type": "LoadBalancer",
            "ports": [{ "port": 6379, "target_port": 6379, "protocol": "TCP" }],
            "selector": app_labels,
        })

    result = None
    ingress = frontend.status.load_balancer.apply(lambda v: v["ingress"][0] if "ingress" in v else "output<string>")
    result = ingress.apply(lambda v: v["ip"] if v and "ip" in v else (v["hostname"] if v and "hostname" in v else "output<string>"))

    pulumi.export("ip", result)
