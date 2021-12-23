#!/usr/bin/env bash

LH_DATA_DIR="$SSV_TESTNET_DIR/.lighthouse/local-testnet"

function install_ssv_web() {
  git clone https://github.com/bloxapp/ssv-web.git \
    && cd ssv-web && yarn && yarn build && yarn link
}

function get_operators() {
  x=$1
  OPS_PUB_KEYS=()
  for ((i=1;i<=SSV_OPERATORS;i++)); do
    ii=$((((x + i) % SSV_OPERATORS + 1) - 1))
    OPS_PUB_KEYS[i]=$(ii="$ii" yq e '.publicKeys.[env(ii)]' "$SSV_TESTNET_DIR/operators.yaml")
  done
  echo "${OPS_PUB_KEYS[1]} ${OPS_PUB_KEYS[2]} ${OPS_PUB_KEYS[3]} ${OPS_PUB_KEYS[4]}"
}

## extract validators information and create ssv validators
cd "$SSV_TESTNET_DIR" || exit 1
cd ssv-web || install_ssv_web
cd "$LH_DATA_DIR" || exit 1
mkdir -p txs > /dev/null
for node in ./node_*; do
  cd "$node" || exit 1
  i=0
  for vks in ./validators/**/*.json; do
    pk=$(yq e ".pubkey" "$vks")
    pass=$(cat "./secrets/0x$pk")
    ops=$(get_operators "$i")
    touch "../txs/0x$pk"
    ssv-cli --filePath="$vks" --password="$pass" \
      --operators="$ops" > "../txs/0x$pk"
    ## TODO: call contract
    i=$((i+1))
  done
  cd ../
done
