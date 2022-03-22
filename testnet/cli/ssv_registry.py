import os
import re
import subprocess


# setup_ssv_net_repo clears
def setup_ssv_registry(main_dir):
    # main_dir = os.environ.get('SSV_TESTNET_DIR')
    os.chdir(main_dir)
    if not os.path.isdir(os.path.join(main_dir, 'ssv-network')):
        cli = subprocess.run(['git', 'clone', 'https://github.com/bloxapp/ssv-network.git'])
        if cli.returncode is not 0:
            print("failed to clone ssv-network")
            return False
        os.chdir('ssv-network')
        cli = subprocess.run(['npm', 'i'])
        if cli.returncode is not 0:
            print("failed to install dependencies")

            return False
        return True
    os.chdir('ssv-network')
    return True


def deploy_contracts():
    cmd = ["npx", "hardhat", "run", "scripts/ssv-deploy-test.ts", "--network", "localhost"]
    env = dict(os.environ, GAS_PRICE="0x0", MINIMUM_BLOCKS_BEFORE_LIQUIDATION=100, OPERATOR_MAX_FEE_INCREASE=3)
    cli = subprocess.run(cmd, capture_output=True, env=env)
    for i, line in cli.stdout:
        if re.search('SSVRegistry:', line) is not None:
            line_match = re.search('(0x.+)$', line)
            addr = line_match.group(1)
            return addr
    return ""


def register_operators(registry_addr, operators):
    # TODO: complete
    return False


def register_validators(registry_addr, validators):
    # TODO: complete
    return False
