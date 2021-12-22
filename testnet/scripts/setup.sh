#!/usr/bin/env bash

export $(grep -v '^#' vagrant.env | xargs)

./lh_setup.sh
./lh_start_testnet.sh
./ssv_deploy_contracts.sh
./ssv_create_operators.sh
./ssv_setup_resources.sh
./ssv_create_validators.sh
./start_ssv.sh
