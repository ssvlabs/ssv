#!/usr/bin/env bash

LH_DIR="$SSV_TESTNET_DIR/lighthouse"
LH_TESTNET_IP="127.0.0.1"

function install_ssv_network() {
  git clone https://github.com/bloxapp/ssv-network.git && cd ssv-network && npm i
}

cd "$SSV_TESTNET_DIR" && touch ssv-deploy.log
cd ssv-network || install_ssv_network
GANACHE_MNEMONIC=$(cat "$LH_DIR/scripts/local_testnet/vars.env" | grep -E -o "^ETH1_NETWORK_MNEMONIC=(.+)" | sed 's/ETH1_NETWORK_MNEMONIC=//g') \
GAS_PRICE="0x0" GANACHE_ETH_NODE_URL="http://$LH_TESTNET_IP:8545" \
  npx hardhat run scripts/ssv-deploy-test.ts --network ganache > ../ssv-deploy.log
