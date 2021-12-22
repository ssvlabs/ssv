#!/usr/bin/env bash

VALIDATOR=$SSV_VALIDATORS
BEACON_NODES=4

LH_DATA_DIR=~/testnet/.lighthouse/local-testnet
LH_DIR="$SSV_TESTNET_DIR/lighthouse"

function prepareEnv() {
    VALIDATOR_COUNT=$1
    BN_COUNT=$2
    DATADIR=$3

    mkdir -p "$DATADIR"

    if [ ! -f "vars.env_backup" ]; then
      cp vars.env vars.env_backup
    else
      cp vars.env_backup vars.env
    fi

    LINE=$(grep -E "^VALIDATOR_COUNT=.+" vars.env); sed -i "s/$LINE/VALIDATOR_COUNT=$VALIDATOR_COUNT/" vars.env
    LINE=$(grep -E "^BN_COUNT=.+" vars.env); sed -i "s/$LINE/BN_COUNT=$BN_COUNT/" vars.env
    LINE=$(grep -E "^VC_COUNT=.+" vars.env); sed -i "s/$LINE/VC_COUNT=0/" vars.env
    LINE=$(grep -E "^DATADIR=.+" vars.env); sed -i "s|$LINE| |" vars.env && echo -e "DATADIR=$DATADIR\n$(cat vars.env)" > vars.env
}

cd "$LH_DIR/scripts/local_testnet" || exit 1

prepareEnv "$VALIDATOR" "$BEACON_NODES" "$LH_DATA_DIR"

printf "starting lighthouse testnet with: \n
  - %s beacon nodes\n
  - %s deposited validators" $BEACON_NODES $VALIDATOR

./start_local_testnet.sh