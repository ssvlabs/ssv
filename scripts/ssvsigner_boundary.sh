#!/usr/bin/env bash
set -eo pipefail

echo -e "\033[36mINFO\033[0m [ssvsigner-boundary] Verifying ssvsigner does not import root ssv packages"

OUT="$(
  cd ssvsigner
  go list -f '{{range .Imports}}{{println .}}{{end}}{{range .TestImports}}{{println .}}{{end}}{{range .XTestImports}}{{println .}}{{end}}' ./... \
    | sort -u \
    | grep '^github.com/ssvlabs/ssv/' \
    | grep -Ev '^github.com/ssvlabs/ssv/ssvsigner($|/)' || true
)"

if [[ -n "$OUT" ]]; then
  echo "$OUT"
  echo -e "\033[31mERROR\033[0m [ssvsigner-boundary] Found forbidden imports from root ssv module"
  exit 1
fi

echo -e "\033[32mINFO\033[0m [ssvsigner-boundary] Import boundary check passed"
