# HardHat has problems with node 20 so we use an older version of node
NODE_ALPINE = "node:16-alpine"
HARDHAT_PROJECT_DIR = "/tmp/hardhat/"
HARDHAT_SERVICE_NAME = "hardhat"


# creates a container with Node JS and installs the required depenencies of the hardhat project passed
# plan - is the Kurtosis plan
# hardhat_project_url - a Kurtosis locator to a directory containing the hardhat files (with hardhat.config.ts at the root of the dir)
# env_vars - Optional argument to set some environment variables in the container; can use this to set the RPC_URI as an example
# returns - hardhat_service; a Kurtosis Service object containing .name, .ip_address, .hostname & .ports
def init(plan, hardhat_project_url, env_vars = None, more_files = {}):
    hardhat_project = plan.upload_files(hardhat_project_url)

    files = {
        HARDHAT_PROJECT_DIR: hardhat_project,
    }

    for filepath, file_artifact in more_files.items():
        files[filepath] = file_artifact

    hardhat_service = plan.add_service(
        name = "hardhat",
        config = ServiceConfig(
            image = NODE_ALPINE,
            entrypoint = ["sleep", "999999"],
            files = files,
            env_vars = env_vars,
        )
    )

    plan.exec(
        service_name="hardhat",
        recipe=ExecRecipe(
            command=[
                "/bin/sh",
                "-c",
                "apk add --update --no-cache python3 && ln -sf python3 /usr/bin/python",
            ]
        ),
    )

    plan.exec(
        service_name="hardhat",
        recipe=ExecRecipe(command=["/bin/sh", "-c", "apk add git"]),
    )

    plan.exec(
        service_name="hardhat",
        recipe=ExecRecipe(command=["/bin/sh", "-c", "apk add make g++ py3-pip"]),
    )


    plan.exec(
        service_name = HARDHAT_SERVICE_NAME,
        recipe = ExecRecipe(
            command = ["/bin/sh", "-c", "cd {0} && npm install".format(HARDHAT_PROJECT_DIR)]
        )
    )

    return hardhat_service


# runs npx hardhat test with the given contract
# plan - is the Kurtosis plan
# smart_contract - the path to smart_contract relative to the hardhat_project passed to `init`; if you pass nothing it runs all suites via npx hardhat test
# network - the network to run npx hardhat run against; defaults to local
def test(plan, smart_contract = None, network = "local"):
    command_arr = ["cd", HARDHAT_PROJECT_DIR, "&&", "npx", "hardhat", "test", "--network", network]
    if smart_contract:
        command_arr = ["cd", HARDHAT_PROJECT_DIR, "&&", "npx", "hardhat", "test", smart_contract, "--network", network]
    return plan.exec(
        service_name = HARDHAT_SERVICE_NAME,
        recipe = ExecRecipe(
            command = ["/bin/sh", "-c", " ".join(command_arr)]
        )
    )


# runs npx hardhat compile with the given smart contract
# plan is the Kurtosis plan
def compile(plan):
    command_arr = ["cd", HARDHAT_PROJECT_DIR, "&&", "npx", "hardhat", "compile"]
    return plan.exec(
        service_name = HARDHAT_SERVICE_NAME,
        recipe = ExecRecipe(
            command = ["/bin/sh", "-c", " ".join(command_arr)]
        )
    )


# runs npx hardhat run with the given contract
# plan - is the Kurtosis plan
# smart_contract - the path to smart_contract relative to the hardhat_project passed to `init`
# network - the network to run npx hardhat run against; defaults to local
def run(plan, smart_contract, network = "local"):
    command_arr = ["cd", HARDHAT_PROJECT_DIR, "&&", "npx", "hardhat", "run", smart_contract, "--network", network]
    return plan.exec(
        service_name = HARDHAT_SERVICE_NAME,
        recipe = ExecRecipe(
            command = ["/bin/sh", "-c", " ".join(command_arr)]
        )
    )

# runs npx hardhat run with the given contract
# plan - is the Kurtosis plan
# task_name - the taskname to run
# network - the network to run npx hardhat run against; defaults to local
def task(plan, task_name, network = "local"):
    command_arr = ["cd", HARDHAT_PROJECT_DIR, "&&", "npx", "hardhat", task_name, "--network", network]
    return plan.exec(
        service_name = HARDHAT_SERVICE_NAME,
        recipe = ExecRecipe(
            command = ["/bin/sh", "-c", " ".join(command_arr)]
        )
    )

# destroys the hardhat container; running this is optional
def cleanup(plan):
    plan.remove_service(HARDHAT_SERVICE_NAME)