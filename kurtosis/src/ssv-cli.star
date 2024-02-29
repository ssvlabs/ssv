SSV_NODE_IMAGE = "bloxstaking/ssv-node:latest"
SSV_CLI_SERVICE_NAME = "ssv-cli"

def start_cli(plan):
    files = {}
    plan.add_service(
        name=SSV_CLI_SERVICE_NAME,
        config=ServiceConfig(
            image=SSV_NODE_IMAGE,
            entrypoint=["tail", "-f", "/dev/null"],
            files=files,
        ),
    )

def generate_operator_keys(plan):
    plan.print("generating operator keys")

    plan.exec(
        service_name=SSV_CLI_SERVICE_NAME,
        recipe=ExecRecipe(
            command=[
                "/bin/sh", "-c",
                "/go/bin/ssvnode generate-operator-keys > /tmp/ssv_keys"
            ]
        ),
    )

    public_key = plan.exec(service_name=SSV_CLI_SERVICE_NAME, recipe=ExecRecipe(command=["/bin/sh", "-c", "cat /tmp/ssv_keys | grep 'generated public key' | awk -F'\"' '{print $(NF-1)}'"]))["output"]
    private_key = plan.exec(service_name=SSV_CLI_SERVICE_NAME, recipe=ExecRecipe(command=["/bin/sh", "-c", "cat /tmp/ssv_keys | grep 'generated private key' | awk -F'\"' '{print $(NF-1)}'"]))["output"]

    plan.print("generated operator keys")
    plan.print(public_key)
    plan.print(private_key)

    return struct(
        public_key=public_key,
        private_key=private_key,
    )
