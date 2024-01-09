#!/bin/bash

#!/usr/bin/env bash

COMPOSE_FILE=$(dirname "$0")/docker-compose.yml

# Step 1: Start the beacon_proxy and ssv-node services
docker compose -f $COMPOSE_FILE up -d --build beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4

# Step 2: Run logs_catcher in Mode Slashing
docker compose -f $COMPOSE_FILE run --build logs_catcher logs-catcher --mode Slashing

# Step 3: Stop the services
docker compose -f $COMPOSE_FILE down

# Step 4. Run share_update for BlsVerification test
docker compose -f $COMPOSE_FILE run --build share_update

# Step 6: Start the beacon_proxy and ssv-nodes again
docker compose -f $COMPOSE_FILE up -d beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4

# Step 7: Run logs_catcher in Mode BlsVerification
docker compose -f $COMPOSE_FILE run logs_catcher logs-catcher --mode BlsVerification
