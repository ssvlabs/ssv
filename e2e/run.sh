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
trap 'catch $?' EXIT

# Function to run docker-compose with or without the override file based on NODE_GROUP_1_IMAGE_TAG
docker_compose_up() {
  local build_option="$1"
  local compose_command="up -d"

  # Check if "build" argument is provided
  if [ "$build_option" = "build" ]; then
    compose_command+=" --build"
  fi

  # Check if NODE_GROUP_1_IMAGE_TAG is set
  if [ -z "$NODE_GROUP_1_IMAGE_TAG" ]; then
    echo "NODE_GROUP_1_IMAGE_TAG is not set. Using default Docker Compose configuration."
    # Run Docker Compose without the override file, include build flag conditionally
    docker compose $compose_command beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
  else
    echo "NODE_GROUP_1_IMAGE_TAG is set to ${NODE_GROUP_1_IMAGE_TAG}. Using override Docker Compose configuration."
    # Run Docker Compose with the override file, include build flag conditionally
    docker compose -f docker-compose.yml -f docker-compose.override-images.yml $compose_command beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
  fi
}

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

# Get the directory of the script itself
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
# Set LOG_DIR to a 'logs' directory within the same directory as the script
LOG_DIR="$SCRIPT_DIR/crash-logs"

export BEACON_NODE_URL=http://bn-h-2.stage.bloxinfra.com:3502/
export EXECUTION_NODE_URL=ws://bn-h-2.stage.bloxinfra.com:8557/ws

# Step 1: Start the beacon_proxy and ssv-node services
docker_compose_up build

# Step 2: Run logs_catcher in Mode Slashing
docker compose run --build logs_catcher logs-catcher --mode Slashing

# Step 3: Stop the services
docker compose down

# Step 4. Run share_update for BlsVerification test
docker compose run --build share_update

# Step 6: Start the beacon_proxy and ssv-nodes again
docker_compose_up

# Step 7: Run logs_catcher in Mode BlsVerification
docker compose run logs_catcher logs-catcher --mode BlsVerification