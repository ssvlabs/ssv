eth_network_package = import_module("github.com/kurtosis-tech/eth-network-package/main.star")
validator_keystores = import_module("./validator_keystores/validator_keystore_generator.star")
hardhat_module = import_module("./hardhat/hardhat.star")

LATEST_BLOCK_NUMBER_GENERIC = "latest"
BLOCK_NUMBER_FIELD = "block-number"
BLOCK_HASH_FIELD = "block-hash"
JQ_PAD_HEX_FILTER = """{} | ascii_upcase | split("") | map({{"x": 0, "0": 0, "1": 1, "2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "8": 8, "9": 9, "A": 10, "B": 11, "C": 12, "D": 13, "E": 14, "F": 15}}[.]) | reduce .[] as $item (0; . * 16 + $item)"""

def run(plan, args):
    participants, _, _ = eth_network_package.run(plan, args)

    el_ip_addr = participants[0].el_client_context.ip_addr
    el_client_port = participants[0].el_client_context.rpc_port_num
    el_url = "http://{0}:{1}".format(el_ip_addr, el_client_port)

    beacon_node_addr = participants[0].cl_client_context.ip_addr
    beacon_node_port = participants[0].cl_client_context.http_port_num
    beacon_url = "http://{0}:{1}".format(beacon_node_addr, beacon_node_port)


    validator_data = validator_keystores.generate_validator_keystores(
        plan, args["network_params"]["preregistered_validator_keys_mnemonic"],
        args["network_params"]["num_validator_keys_per_node"]
    )

    hardhat_project = "./../ssv-network" # TODO: try to use https://github.com/bloxapp/ssv-network instead of copying its files into this repo

    hardhat_env_vars = {
        "RPC_URI": el_url,
        "HOLESKY_ETH_NODE_URL": el_url,
        "BATCH_INDEX": str(0),
        "VALIDATORS_TO_REGISTER": str(1),
        "GAS_PRICE": str(900000),
        "GAS_LIMIT": str(4000000),
        "SSV_NETWORK_ADDRESS_STAGE": "0x776137553470cBf7a4EB1e30bb201e4931A26a49",
        "SSV_TOKEN_ADDRESS": "0x4c849Ff66a6F0A954cbf7818b8a763105C2787D6",
        "SSVTOKEN_ADDRESS": "0x4c849Ff66a6F0A954cbf7818b8a763105C2787D6",
        "SSV_TOKEN_APPROVE_AMOUNT": str(1000000000000000),
        "TOKEN_AMOUNT": str(1000000000000000),
        # End of New Variables
        "MINIMUM_BLOCKS_BEFORE_LIQUIDATION": str(100800),
        "MINIMUM_LIQUIDATION_COLLATERAL": str(200000000),
        "OPERATOR_MAX_FEE_INCREASE": str(3),
        "DECLARE_OPERATOR_FEE_PERIOD": str(259200),
        "EXECUTE_OPERATOR_FEE_PERIOD": str(345600),
        "VALIDATORS_PER_OPERATOR_LIMIT": str(500),
    }

    hardhat = hardhat_module.init(
        plan,
        hardhat_project,
        hardhat_env_vars,
        {
            # "/tmp/validator-keys/": validator_keys,
            # "/tmp/validator-secrets/": validator_secrets,
        },
    )

    hardhat_module.compile(plan)

    # have to wait for at least block to be mined before deploying contract
    wait_until_node_reached_block(plan, "el-1-geth-lighthouse", 1)

    hardhat_module.task(plan, "deploy:all", "holesky_testnet")
    # hardhat_module.run(plan, "scripts/register-operators.ts", "holesky_testnet")



def wait_until_node_reached_block(plan, node_id, target_block_number_int):
    """
    This function blocks until the node `node_id` has reached block number `target_block_number_int` (which should
    be an integer)
    If node has already produced this block, it returns immediately.
    """
    plan.wait(
        recipe=get_block_recipe(LATEST_BLOCK_NUMBER_GENERIC),
        field="extract." + BLOCK_NUMBER_FIELD,
        assertion=">=",
        target_value=target_block_number_int,
        timeout="20m",  # Ethereum nodes can take a while to get in good shapes, especially at the beginning
        service_name=node_id,
    )


def get_block_recipe(block_number_hex):
    """
    Returns the recipe to run to get the block information for block number `block_number_hex` (which should be a
    hexadecimal string starting with `0x`, i.e. `0x2d`)
    """
    request_body = """{{
    "method": "eth_getBlockByNumber",
    "params":[
        "{}",
        true
    ],
    "id":1,
    "jsonrpc":"2.0"
}}""".format(
        block_number_hex
    )
    return PostHttpRequestRecipe(
        port_id="rpc",
        endpoint="/",
        content_type="application/json",
        body=request_body,
        extract={
            BLOCK_NUMBER_FIELD: JQ_PAD_HEX_FILTER.format(".result.number"),
            BLOCK_HASH_FIELD: ".result.hash",
        },
    )