#!/bin/bash


docker compose up -d --build beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
echo $EXECUTION_NODE_URL
docker compose run --build logs_catcher