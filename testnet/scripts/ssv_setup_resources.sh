#!/usr/bin/env bash

mkdir -p "$SSV_TESTNET_DIR/data/config" > /dev/null
mkdir -p "$SSV_TESTNET_DIR/data/grafana" > /dev/null
mkdir -p "$SSV_TESTNET_DIR/resources/grafana" > /dev/null
mkdir -p "$SSV_TESTNET_DIR/resources/prometheus" > /dev/null


cd "$SSV_DIR/testnet/scripts" || exit 1
## creates templates cli if not exist
if [ ! -f "tmpl-cli" ]; then
  go build -o ./tmpl-cli ../tmpl/main.go
fi
## create resources from template
./tmpl-cli "$SSV_DIR/testnet/resources/docker-compose.yaml.tmpl" "$(yq e '.nodes' -o=j -I=0 "$SSV_TESTNET_DIR/operators.yaml")" \
  > "$SSV_TESTNET_DIR/docker-compose.yaml"
./tmpl-cli "$SSV_DIR/testnet/resources/prometheus/prometheus.yaml.tmpl" "$(yq e '.nodes' -o=j -I=0 "$SSV_TESTNET_DIR/operators.yaml")" \
  > "$SSV_TESTNET_DIR/resources/prometheus/prometheus.yaml"

## copy grafana testnet config
cp -r "$SSV_DIR/testnet/resources/grafana" "$SSV_TESTNET_DIR/resources/grafana"
# copy grafana dashboards
sed 's/${DS_PROMETHEUS}/Prometheus/g' "$SSV_DIR/monitoring/grafana/dashboard_ssv_operator.json" > "$SSV_TESTNET_DIR/data/grafana/dashboard_ssv_operator.json"
sed 's/${DS_PROMETHEUS}/Prometheus/g' "$SSV_DIR/monitoring/grafana/dashboard_ssv_validator.json" > "$SSV_TESTNET_DIR/data/grafana/dashboard_ssv_validator.json"
