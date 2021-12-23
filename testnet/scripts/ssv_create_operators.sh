#!/usr/bin/env bash

LH_TESTNET_IP="127.0.0.1"

## preparing config.yaml
function prepare_config() {
  mkdir -p "$SSV_TESTNET_DIR/data/config" > /dev/null
  cp "$SSV_DIR/testnet/resources/config.yaml" "$SSV_TESTNET_DIR/data/config/config.yaml"
  val="http://$LH_TESTNET_IP:8001" yq e '.eth2.BeaconNodeAddr = env(val)' -i "$SSV_TESTNET_DIR/data/config/config.yaml" \
    && val="http://$LH_TESTNET_IP:8545" yq e '.eth1.ETH1Addr = env(val)' -i "$SSV_TESTNET_DIR/data/config/config.yaml" \
    && yq e '.eth1.RegistryContractAddr = "..."' -i "$SSV_TESTNET_DIR/data/config/config.yaml" \
    && yq e '.eth1.ETH1SyncOffset = "..."' -i "$SSV_TESTNET_DIR/data/config/config.yaml"
}

function extract_pubkey() {
    LOGFILE=$1
    echo "$(grep "generated public key (base64)" "$LOGFILE" | grep -E -o '(\{.+\})' | jq -r ".pk")"
}

function extract_privkey() {
    LOGFILE=$1
    echo "$(grep "generated private key (base64)" "$LOGFILE" | grep -E -o '(\{.+\})' | jq -r ".sk")"
}

function create_operators() {
  SSV_OPERATORS=$1
  echo "creating $SSV_OPERATORS ssv operators"
  touch tmp_ops.log
  echo "publicKeys:" > "$SSV_TESTNET_DIR/operators.yaml"
  echo "nodes:" >> "$SSV_TESTNET_DIR/operators.yaml"
  for ((i=1;i<=SSV_OPERATORS;i++)); do
    docker run --rm -it 'bloxstaking/ssv-node:latest' /go/bin/ssvnode generate-operator-keys > tmp.log
    PUB="$(extract_pubkey "tmp.log")"
    val="$PUB" yq e '.publicKeys += [env(val)]' -i "$SSV_TESTNET_DIR/operators.yaml"
    val="$i" yq e '.nodes += [env(val)]' -i "$SSV_TESTNET_DIR/operators.yaml"
    PRIV="$(extract_privkey "tmp.log")"
    val="./data/db-$i" yq e '.db.Path = env(val)' -n | tee "$SSV_TESTNET_DIR/data/config/share$i.yaml" \
      && val="1500$i" yq e '.MetricsAPIPort = env(val)' -i "$SSV_TESTNET_DIR/data/config/share$i.yaml" \
      && val="$PRIV" yq e '.OperatorPrivateKey = env(val)' -i "$SSV_TESTNET_DIR/data/config/share$i.yaml"
  done
  rm tmp.log
}

mkdir -p "$SSV_TESTNET_DIR/data/config" > /dev/null

cd "$SSV_TESTNET_DIR" || exit 1
prepare_config

cd "$SSV_TESTNET_DIR" || exit 1
create_operators $SSV_OPERATORS


