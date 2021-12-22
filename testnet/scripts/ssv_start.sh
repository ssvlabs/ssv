#!/usr/bin/env bash

## cleanup if arg provided
if [ -n "$1" ]; then
  docker-compose down
fi

## start prometheus
if [ -z "$(docker ps -a --format "{{.Names}}" | grep "prometheus")" ]; then
  docker-compose up prometheus
fi

## start grafana
if [ -z "$(docker ps -a --format "{{.Names}}" | grep "grafana")" ]; then
  docker-compose up grafana
fi

if [ -n "$1" ]; then
  docker build --build-arg APP_VERSION="local" -t "ssv-node:local" .
fi

if [ -z "$(docker ps -a --format "{{.Names}}" | grep "ssv-node-v2")" ]; then
  mkdir -p "$SSV_TESTNET_DIR/data/containers" > /dev/null
  for ((i=1;i<=SSV_OPERATORS;i++)); do
    docker-compose up "ssv-node-v2-$i"
    sleep 2
  done
fi
