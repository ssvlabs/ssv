#!/bin/bash

set -e
trap 'catch $?' EXIT

# Get the directory of the script itself
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

# Set LOG_DIR to a 'logs' directory within the same directory as the script
LOG_DIR="$SCRIPT_DIR/crash-logs"

catch() {
  if [ "$1" != "0" ]; then
    echo "Error $1 occurred. Saving logs..."
    save_logs
  fi
}

save_logs() {
  echo "Creating directory at: $LOG_DIR"
  mkdir -p "$LOG_DIR"
  # Saving logs of each container separately
  docker compose logs ssv-node-1 > "$LOG_DIR/ssv-node-1.txt"
  docker compose logs ssv-node-2 > "$LOG_DIR/ssv-node-2.txt"
  docker compose logs ssv-node-3 > "$LOG_DIR/ssv-node-3.txt"
  docker compose logs ssv-node-4 > "$LOG_DIR/ssv-node-4.txt"
  docker compose logs beacon_proxy > "$LOG_DIR/beacon_proxy.txt"
}

#export BEACON_NODE_URL=http://bn-h-2.stage.bloxinfra.com:3502/
#export EXECUTION_NODE_URL=ws://bn-h-2.stage.bloxinfra.com:8557/ws

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
