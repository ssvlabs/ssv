#!/bin/bash

# clean up everything including exited dockers and volumes before start
services=$(docker-compose config --services)
docker-compose down
for service in $services; do
    docker rm -f $(docker ps -aqf "name=${service}*")
done
docker compose down -v

# Exit on error
set -e

export BEACON_NODE_URL=http://bn-h-2.stage.bloxinfra.com:3502/
export EXECUTION_NODE_URL=ws://bn-h-2.stage.bloxinfra.com:8557/ws

# Step 1: Start the beacon_proxy and ssv-node services
docker compose up -d --build beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4

# Step 2: Run logs_catcher in Mode Slashing
docker compose run --build logs_catcher logs-catcher --mode Slashing

# Step 3: Stop the services
docker compose down

# Step 4. Run share_update for BlsVerification test
docker compose run --build share_update

# Step 6: Start the beacon_proxy and ssv-nodes again
docker compose up -d beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4

# Step 7: Run logs_catcher in Mode BlsVerification
docker compose run logs_catcher logs-catcher --mode BlsVerification
