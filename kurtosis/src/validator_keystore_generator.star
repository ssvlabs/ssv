NODE_KEYSTORES_OUTPUT_DIRPATH_FORMAT_STR = "/node-keystores"

PRYSM_PASSWORD = "password"
PRYSM_PASSWORD_FILEPATH_ON_GENERATOR = "/tmp/prysm-password.txt"

KEYSTORES_GENERATION_TOOL_NAME = "/app/eth2-val-tools"

ETH_VAL_TOOLS_IMAGE = "protolambda/eth2-val-tools:latest"

SUCCESSFUL_EXEC_CMD_EXIT_CODE = 0

RAW_KEYS_DIRNAME = "keys"
RAW_SECRETS_DIRNAME = "secrets"

NIMBUS_KEYS_DIRNAME = "nimbus-keys"
PRYSM_DIRNAME = "prysm"

TEKU_KEYS_DIRNAME = "teku-keys"
TEKU_SECRETS_DIRNAME = "teku-secrets"

KEYSTORE_GENERATION_FINISHED_FILEPATH_FORMAT = "/tmp/keystores_generated-{0}-{1}"

SERVICE_NAME = "validator-key-generation-ssv-validator-keystore"

ENTRYPOINT_ARGS = [
    "sleep",
    "99999",
]

SERVICE_CONFIG = ServiceConfig(
    image=ETH_VAL_TOOLS_IMAGE,
    entrypoint=ENTRYPOINT_ARGS,
    files={},
)

ARTIFACT_PREFIX = 'ssv-validators'


def generate_validator_keystores(plan, mnemonic, validator_count):
    plan.add_service(SERVICE_NAME, SERVICE_CONFIG)

    start_index = 0
    stop_index = start_index + validator_count

    generate_keystores_cmd = '{0} keystores --insecure --prysm-pass {1} --out-loc {2} --source-mnemonic "{3}" --source-min {4} --source-max {5}'.format(
        KEYSTORES_GENERATION_TOOL_NAME,
        PRYSM_PASSWORD,
        NODE_KEYSTORES_OUTPUT_DIRPATH_FORMAT_STR,
        mnemonic,
        start_index,
        stop_index,
    )
    teku_permissions_cmd = (
            "chmod 0777 -R " + NODE_KEYSTORES_OUTPUT_DIRPATH_FORMAT_STR + "/" + TEKU_KEYS_DIRNAME
    )
    raw_secret_permissions_cmd = (
            "chmod 0600 -R " + NODE_KEYSTORES_OUTPUT_DIRPATH_FORMAT_STR + "/" + RAW_SECRETS_DIRNAME
    )

    all_sub_command_strs = [generate_keystores_cmd, teku_permissions_cmd, raw_secret_permissions_cmd]

    command_str = " && ".join(all_sub_command_strs)

    command_result = plan.exec(
        recipe=ExecRecipe(command=["sh", "-c", command_str]), service_name=SERVICE_NAME
    )
    plan.verify(command_result["code"], "==", SUCCESSFUL_EXEC_CMD_EXIT_CODE)

    artifact_name = "{0}-{1}-{2}".format(
        ARTIFACT_PREFIX,
        start_index,
        stop_index - 1,
    )
    artifact_name = plan.store_service_files(
        SERVICE_NAME, NODE_KEYSTORES_OUTPUT_DIRPATH_FORMAT_STR, name=artifact_name
    )

    base_dirname_in_artifact = path_base(NODE_KEYSTORES_OUTPUT_DIRPATH_FORMAT_STR)
    keystore_files = struct(
        files_artifact_uuid=artifact_name,
        raw_root_dirpath=path_join(base_dirname_in_artifact),
        raw_keys_relative_dirpath=path_join(base_dirname_in_artifact, RAW_KEYS_DIRNAME),
        raw_secrets_relative_dirpath=path_join(base_dirname_in_artifact, RAW_SECRETS_DIRNAME),
        nimbus_keys_relative_dirpath=path_join(base_dirname_in_artifact, NIMBUS_KEYS_DIRNAME),
        prysm_relative_dirpath=path_join(base_dirname_in_artifact, PRYSM_DIRNAME),
        teku_keys_relative_dirpath=path_join(base_dirname_in_artifact, TEKU_KEYS_DIRNAME),
        teku_secrets_relative_dirpath=path_join(base_dirname_in_artifact, TEKU_SECRETS_DIRNAME),
    )

    return keystore_files


def path_join(*args):
    joined_path = "/".join(args)
    return joined_path.replace("//", "/")


def path_base(path):
    split_path = path.split("/")
    return split_path[-1]
