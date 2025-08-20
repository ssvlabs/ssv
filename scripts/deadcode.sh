#!/usr/bin/env bash
set -eo pipefail

echo -e "\033[36mINFO\033[0m [deadcode] Running deadcode lint"

TAGS="blst_enabled"
MOD="$(go list -m -f '{{.Path}}')"

# Analyze the same entrypoint you build (adjust if you have multiple mains)
OUT="$(GOFLAGS="-tags=$TAGS" go tool -modfile=tool.mod deadcode -filter="^${MOD}($|/)" ./cmd/ssvnode \
  | grep -Ev 'ssvsigner|ssv-spec|test|mock' || true)"

if [[ -n "$OUT" ]]; then
  echo "$OUT"
  echo -e "\033[31mERROR\033[0m [deadcode] Unused code found"
  exit 1
fi


echo -e "\033[32mINFO\033[0m [deadcode] No unused code found"
