#!/bin/bash

# Navigate to the directory containing the docker-compose.yml file
cd ./e2e

# Run docker-compose commands
docker compose up -d --build beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4

docker compose up --build logs_catcher

docker compose rm -s -v --force
