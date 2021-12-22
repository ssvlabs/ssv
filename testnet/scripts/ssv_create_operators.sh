#!/usr/bin/env bash

LH_TESTNET_IP="127.0.0.1"

## preparing config.yaml
function prepare_config() {
  mkdir -p "$SSV_TESTNET_DIR/data/config" > /dev/null
  cp "$SSV_DIR/testnet/resources/config.yaml" "$SSV_TESTNET_DIR/data/config/config.yaml"
  yq w -i "$SSV_TESTNET_DIR/data/config/config.yaml" eth2.BeaconNodeAddr "http://$LH_TESTNET_IP:8001" \
    && yq w -i "$SSV_TESTNET_DIR/data/config.yaml" eth1.ETH1Addr "http://$LH_TESTNET_IP:8545" \
    && yq w -i "$SSV_TESTNET_DIR/data/config.yaml" eth1.RegistryContractAddr "..." \
    && yq w -i "$SSV_TESTNET_DIR/data/config.yaml" eth1.ETH1SyncOffset "..."
}

## create ssv operators
function extract_key() {
    LOGFILE=$1
    KTYPE=$2

    echo "$(grep "generated $KTYPE key (base64)" "$LOGFILE" | grep -E -o '(\{.+\})' | jq -r ".pk")"
}

#declare -a OPS_PUB_KEYS
function create_operators() {
  SSV_OPERATORS=$1
  echo "creating $SSV_OPERATORS ssv operators"
  touch tmp_ops.log
  echo "publicKeys:" > "$SSV_TESTNET_DIR/operators.yaml"
  echo "nodes:" >> "$SSV_TESTNET_DIR/operators.yaml"
  for ((i=1;i<=SSV_OPERATORS;i++)); do
    docker run --rm -it 'bloxstaking/ssv-node:latest' /go/bin/ssvnode generate-operator-keys > tmp.log
    PUB="$(extract_key "tmp.log" "public")"
#    OPS_PUB_KEYS[i]="$PUB"
    yq w -i "$SSV_TESTNET_DIR/operators.yaml" publicKeys[+] "$PUB"
    yq w -i "$SSV_TESTNET_DIR/operators.yaml" nodes[+] "$i"
    PRIV="$(extract_key "tmp.log" "private")"
    yq n db.Path "./data/db-$i" | tee "$SSV_TESTNET_DIR/data/config/share$i.yaml" \
      && yq w -i "$SSV_TESTNET_DIR/data/config/share$i.yaml" MetricsAPIPort "1500$i" \
      && yq w -i "$SSV_TESTNET_DIR/data/config/share$i.yaml" OperatorPrivateKey "$PRIV"
  done
  rm tmp.log
}

mkdir -p "$SSV_TESTNET_DIR/data/config" > /dev/null

cd "$SSV_TESTNET_DIR" || exit 1
prepare_config

cd "$SSV_TESTNET_DIR" || exit 1
create_operators $SSV_OPERATORS


