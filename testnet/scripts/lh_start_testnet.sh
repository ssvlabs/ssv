#!/usr/bin/env bash

VALIDATOR=$SSV_VALIDATORS
BEACON_NODES=4

LH_DATA_DIR="$SSV_TESTNET_DIR/.lighthouse/local-testnet"
LH_DIR="$SSV_TESTNET_DIR/lighthouse"

function prepareEnv() {
    VALIDATOR_COUNT=$1
    BN_COUNT=$2
    DATADIR=$3

    if [ ! -f "vars.env_backup" ]; then
      cp vars.env vars.env_backup
    fi

    LINE=$(cat vars.env | grep -E "^VALIDATOR_COUNT=\d+")
    sed -i "'s/$LINE/VALIDATOR_COUNT=$VALIDATOR_COUNT/'" vars.env
    LINE=$(cat vars.env | grep -E "^BN_COUNT=\d+")
    sed -i "'s/$LINE/BN_COUNT=$BN_COUNT/'" vars.env
    LINE=$(cat vars.env | grep -E "^DATADIR=.+")
    sed -i "'s/$LINE/DATADIR=$DATADIR/'" vars.env

}

cd "$LH_DIR/scripts/local_testnet" || exit 1

prepareEnv "$VALIDATOR" "$BEACON_NODES" "$LH_DATA_DIR"

printf "starting lighthouse testnet with: \n
  - %s beacon nodes\n
  - %s deposited validators" $BEACON_NODES $VALIDATOR

./start_local_testnet.sh