# import os
import sys
import click

from .validators import generate_validators
from .eth_testnet import setup_lighthouse, start_lighthouse
from .ssv_registry import setup_ssv_net_repo, deploy_contracts
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
def lh_up(testnet_dir, beacon, validators, eth_data_dir):
    """Spin up local lighthouse testnet + ganache"""
    setup_lighthouse(validators, beacon, testnet_dir, eth_data_dir)
    start_lighthouse()


@main.command()
@click.option('--testnet-dir', envvar='SSV_TESTNET_DIR', help="Testnet directory")
@click.option('--operators', default=4, help='Number of operators')
@click.option('--eth-data-dir', default='', help='Data dir of ETH testnet')
def setup_ssv(testnet_dir, operators, eth_data_dir):
    """Setup resources for local SSV testnet"""
    setup_ssv_net_repo(testnet_dir)
    addr = deploy_contracts()
    print(f"registry was deployed on address {addr}")
    generate_operators(operators, testnet_dir)
    print(f"created {str(operators)} operators")
    generate_validators(testnet_dir, eth_data_dir)
    print(f"created SSV validators")


if __name__ == '__main__':
    args = sys.argv
    if "--help" in args or len(args) == 1:
        print("ssvt")
    main()
