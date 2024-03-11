#!/bin/bash

services=$(docker-compose config --services)
docker-compose down
for service in $services; do
    docker rm -f $(docker ps -aqf "name=${service}*")
done
docker compose down -v

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

  # Define a list of container patterns to save logs from
  declare -a containers=("ssv-node-1" "ssv-node-2" "ssv-node-3" "ssv-node-4" "beacon_proxy")

  for container_pattern in "${containers[@]}"; do
    container_ids=$(docker ps -a --filter name=$container_pattern --format "{{.Names}}")

    for container_id in $container_ids; do
      if [ ! -z "$container_id" ]; then
        echo "Saving logs for $container_id..."
        docker logs "$container_id" > "$LOG_DIR/$container_id.txt"
      fi
    done
  done

  # Special handling for logs_catcher to get the most recent container
  logs_catcher_container=$(docker ps -a --filter ancestor=logs_catcher:latest --format "{{.Names}}" | head -n 1)
  if [ ! -z "$logs_catcher_container" ]; then
    echo "Saving logs for the most recent logs_catcher container: $logs_catcher_container..."
    docker logs "$logs_catcher_container" > "$LOG_DIR/$logs_catcher_container.txt"
  fi
}

# Step 1: Start the beacon_proxy and ssv-node services
docker compose up -d --build beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4 rsa_signature

# Step 2: Run logs_catcher in Mode Slashing
nohup docker-compose run --build logs_catcher logs-catcher --mode Slashable & docker-compose run --build logs_catcher logs-catcher --mode RsaVerification


# Step 3: Stop the services
docker compose down

# Step 4. Run share_update for BlsVerification test
docker compose run --build share_update

# Step 6: Start the beacon_proxy and ssv-nodes again
docker compose up -d beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4

# Step 7: Run logs_catcher in Mode BlsVerification
docker compose run logs_catcher logs-catcher --mode BlsVerification