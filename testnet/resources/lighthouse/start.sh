#!/bin/bash

# bind the HTTP API on 0.0.0.0
sed -i '/--http-port $http_port/a\\t--http-address 0.0.0.0 \\' /lighthouse/scripts/local_testnet/beacon_node.sh

# Remove last run's PIDs
rm -f /root/.lighthouse/local-testnet/testnet/PIDS.pid

cd /lighthouse/scripts/local_testnet && ./start_local_testnet.sh