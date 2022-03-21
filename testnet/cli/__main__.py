# import os
import sys
import click

from .validators import generate_validators
from .eth_testnet import setup_lighthouse, start_lighthouse
from .ssv_registry import setup_ssv_registry, deploy_contracts, register_operators, register_validators
from .operators import generate_operators


@click.group()
@click.version_option("1.0.0")
def main():
    """SSV local testnet CLI"""
    pass


@main.command()
@click.option('--testnet-dir', envvar='SSV_TESTNET_DIR', help="Testnet directory")
@click.option('--beacon', default=4, help='Number of beacon nodes')
@click.option('--validators', default=100, help='Number of validators')
@click.option('--eth-data-dir', default='', help='Data dir of ETH testnet')
def eth_up(testnet_dir, beacon, validators, eth_data_dir):
    """Spin up local beacon (lighthouse) and eth1 (ganache)"""
    if not setup_lighthouse(validators, beacon, testnet_dir, eth_data_dir):
        print("could not setup lighthouse")
        return
    start_lighthouse()


@main.command()
def eth_down():
    """Take down the local beacon / eth1"""
    # TODO: implement


@main.command()
@click.option('--testnet-dir', envvar='SSV_TESTNET_DIR', help="Testnet directory")
@click.option('--operators', default=4, help='Number of operators')
@click.option('--eth-data-dir', default='', help='Data dir of ETH testnet')
def ssv_up(testnet_dir, operators, eth_data_dir):
    """Starts a local SSV network"""
    setup_ssv_registry(testnet_dir)
    addr = deploy_contracts()
    print(f"registry was deployed on address {addr}")
    ops = generate_operators(operators, testnet_dir)
    print(f"created {str(operators)} operators")
    validators = generate_validators(testnet_dir, eth_data_dir)
    print(f"created SSV validators")
    if not register_operators(addr, ops):
        print(f"failed to register operators")
        return
    if not register_validators(addr, validators):
        print(f"failed to register validators")
        return
    # TODO: start nodes


if __name__ == '__main__':
    args = sys.argv
    if "--help" in args or len(args) == 1:
        print("ssvt")
    main()
