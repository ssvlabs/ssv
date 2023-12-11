#!/bin/bash

cleanup() {
    docker compose down
}
trap cleanup EXIT

check_exit_code () {
   if [ $1 -ne 0 ]; then
       echo "Tests failed with exit code $1"
       exit $exit_code
   else
       echo "Tests passed successfully"
   fi
}


docker compose up -d --build beacon_proxy ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4

docker compose run -e FATALERS='{"level":"error"}' --build logs_catcher
check_exit_code $?

# run more tests
