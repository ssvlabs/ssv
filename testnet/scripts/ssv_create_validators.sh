#!/usr/bin/env bash

LH_DATA_DIR="$SSV_TESTNET_DIR/.lighthouse/local-testnet"

## TODO: populate OPS_PUB_KEYS

function install_ssv_web() {
  git clone https://github.com/bloxapp/ssv-web.git \
    && cd ssv-web && yarn && yarn build && yarn link
}

## extract validators information and create ssv validators
cd "$SSV_TESTNET_DIR" || exit 1
cd ssv-web || install_ssv_web
cd "$LH_DATA_DIR" || exit 1
mkdir -p txs > /dev/null
NODES=$(ls | grep -E "node_")
for node in $NODES; do
  cd "$node" || exit 1
  i=0
  for vks in ./validators/**/*.json; do
    pk=$(jq eval ".pubkey" vks)
    pass=$(cat "./secrets/0x$pk")
    touch "./txs/0x$pk"
    ssv-cli --filePath="$vks" --password="$pass" \
      --operators="${OPS_PUB_KEYS[1]} ${OPS_PUB_KEYS[2]} ${OPS_PUB_KEYS[3]} ${OPS_PUB_KEYS[4]}" > "./txs/0x$pk"
    i+=1
  done
done

## TODO: call contract
