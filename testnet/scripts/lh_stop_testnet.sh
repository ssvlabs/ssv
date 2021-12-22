#!/usr/bin/env bash

LH_DIR="$SSV_TESTNET_DIR/lighthouse"

cd "$LH_DIR/scripts/local_testnet" || exit 1

./stop_local_testnet.sh
