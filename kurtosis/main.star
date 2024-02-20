eth_network_package = import_module(
    "github.com/kurtosis-tech/eth-network-package/main.star"
)

validator_keystores = import_module("./validator_keystores/validator_keystore_generator.star")

def run(plan, args):
    _, _, _ = eth_network_package.run(plan, args)

    validator_data = validator_keystores.generate_validator_keystores(
        plan, args["network_params"]["preregistered_validator_keys_mnemonic"], args["network_params"]["num_validator_keys_per_node"]
    )


