#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT/docs/api"
OUT_JSON="$OUT_DIR/ssvnode.openapi.json"
OUT_YAML="$OUT_DIR/ssvnode.openapi.yaml"

# Build the swag command:
# - If $SWAG is set (e.g. "go run github.com/swaggo/swag/cmd/swag"), use that.
# - Else fall back to "swag" on PATH.
if [[ -n "${SWAG:-}" ]]; then
  # Split $SWAG into an argv array (handles things like: "go run github.com/...").
  IFS=' ' read -r -a SWAG_CMD <<< "$SWAG"
elif command -v swag >/dev/null 2>&1; then
  SWAG_CMD=(swag)
else
  echo "error: SWAG not provided and 'swag' not found on PATH. Set SWAG or install swag." >&2
  exit 2
fi

generate_to() {
  local outdir="$1"
  mkdir -p "$outdir"
  (
    cd "$ROOT"
    GOWORK=off "${SWAG_CMD[@]}" init \
      -g server/openapi_info.go \
      --dir api \
      -o "$outdir" \
      --outputTypes json,yaml \
      --generatedTime=false
  )
  mv -f "$outdir/swagger.json" "$outdir/ssvnode.openapi.json"
  mv -f "$outdir/swagger.yaml" "$outdir/ssvnode.openapi.yaml"
}

case "${1:-}" in
  --write)
    mkdir -p "$OUT_DIR"
    generate_to "$OUT_DIR"
    ;;
  --lint)
    tmp="$(mktemp -d 2>/dev/null || mktemp -d -t openapi)"
    trap 'rm -rf "$tmp"' EXIT
    generate_to "$tmp"

    # Require committed specs to exist
    if ! [[ -f "$OUT_JSON" && -f "$OUT_YAML" ]]; then
      echo "OpenAPI spec not found in docs/api. Run 'make openapi' and commit the results." >&2
      exit 1
    fi

    # Byte-for-byte compare (no diffs printed)
    if ! cmp -s "$OUT_JSON" "$tmp/ssvnode.openapi.json" || \
       ! cmp -s "$OUT_YAML" "$tmp/ssvnode.openapi.yaml"; then
      echo "OpenAPI spec is out of date. Run 'make openapi' and commit the results." >&2
      exit 1
    fi
    ;;
  *)
    echo "Usage: $0 --write | --lint" >&2
    exit 64
    ;;
esac
