#!/usr/bin/env bash
# Run TLC against a TLA+ spec, capturing a log + producing a summary file.
# Designed for unattended (overnight) runs of the L_Bid / L_Bid_New SAFETY
# specs at full coverage.  See tla/Makefile for the user-facing entry points.
#
# Usage:   tlc-run.sh <spec-name>
# Example: tlc-run.sh LBid_Safety
#
# Environment variables (override on the command line):
#   HEAP        Java heap size (default: 24g)
#   WORKERS     TLC worker count (default: auto)
#   JAVA        java binary (default: /opt/homebrew/opt/openjdk/bin/java)
#   TLA_TOOLS   tla2tools.jar location (default: tla2tools.jar)
#   RUNS_DIR    output directory (default: runs)

# Note: NOT using `set -e` — TLC may exit non-zero (counterexample, OOM,
# Ctrl-C) and we still want to write the summary in those cases.
set -u

SPEC="${1:?usage: $0 <spec-name>  (e.g. LBid_Safety)}"
HEAP="${HEAP:-24g}"
WORKERS="${WORKERS:-auto}"
JAVA="${JAVA:-/opt/homebrew/opt/openjdk/bin/java}"
TLA_TOOLS="${TLA_TOOLS:-tla2tools.jar}"
RUNS_DIR="${RUNS_DIR:-runs}"

# Resolve to the tla/ directory regardless of where the script is invoked from.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tla_dir="$(dirname "$script_dir")"
cd "$tla_dir"

# Sanity: java + tla2tools.jar + spec files are all accessible.
if [ ! -x "$JAVA" ]; then
    echo "error: JAVA binary not found or not executable: $JAVA" >&2
    echo "       override with: JAVA=/path/to/java make verify-..." >&2
    exit 2
fi
if [ ! -f "$TLA_TOOLS" ]; then
    echo "error: tla2tools.jar not found at: $TLA_TOOLS" >&2
    echo "       see tla/README.md for download instructions" >&2
    exit 2
fi
if [ ! -f "${SPEC}.tla" ] || [ ! -f "${SPEC}.cfg" ]; then
    echo "error: spec ${SPEC}.tla and/or ${SPEC}.cfg not found in $(pwd)" >&2
    exit 2
fi

mkdir -p "$RUNS_DIR"
ts="$(date +%Y%m%d-%H%M%S)"
log="$RUNS_DIR/${SPEC}-${ts}.log"
summary="${log%.log}.summary"

echo "==> TLC run on $SPEC"
echo "    Heap:    $HEAP"
echo "    Workers: $WORKERS"
echo "    Log:     $log"
echo "    Summary: $summary"
echo "    (Ctrl-C to interrupt; summary will still be written.)"
echo ""

start_epoch=$(date +%s)
start_human="$(date '+%Y-%m-%d %H:%M:%S %Z')"

# Run TLC.  PIPESTATUS captures TLC's exit code (not tee's).
"$JAVA" -Xmx"$HEAP" -XX:+UseParallelGC -cp "$TLA_TOOLS" \
    tlc2.TLC -workers "$WORKERS" -config "${SPEC}.cfg" "$SPEC" 2>&1 | tee "$log"
exit_code="${PIPESTATUS[0]}"

end_epoch=$(date +%s)
end_human="$(date '+%Y-%m-%d %H:%M:%S %Z')"
elapsed=$((end_epoch - start_epoch))
hours=$((elapsed / 3600))
mins=$(((elapsed % 3600) / 60))
secs=$((elapsed % 60))

# Determine outcome by inspecting the log.
if grep -q "^Model checking completed\. No error has been found\." "$log" 2>/dev/null; then
    outcome="COMPLETED -- no counterexample found (full coverage at this config)"
    glyph="OK"
elif grep -qE "^Error: (Invariant|.*violated)" "$log" 2>/dev/null; then
    outcome="COUNTEREXAMPLE -- invariant violated; see log for trace"
    glyph="FAIL"
elif grep -qi "OutOfMemoryError\|Out of memory" "$log" 2>/dev/null; then
    outcome="OUT OF MEMORY -- increase HEAP and re-run"
    glyph="OOM"
elif [ "$exit_code" -eq 130 ]; then
    outcome="INTERRUPTED -- partial coverage (Ctrl-C / SIGINT)"
    glyph="PARTIAL"
elif [ "$exit_code" -ne 0 ]; then
    outcome="ABNORMAL EXIT -- exit code $exit_code (likely killed by SIGTERM/SIGKILL); partial coverage"
    glyph="PARTIAL"
else
    outcome="UNKNOWN -- TLC exited 0 but no completion line found; check log"
    glyph="UNKNOWN"
fi

# Write summary file.
{
    echo "# TLC verification run summary"
    echo
    echo "Spec:      $SPEC"
    echo "Started:   $start_human"
    echo "Finished:  $end_human"
    printf 'Duration:  %dh %dm %ds (%d seconds total)\n' "$hours" "$mins" "$secs" "$elapsed"
    echo
    echo "## Outcome"
    echo "  [$glyph] $outcome"
    echo "  Exit code: $exit_code"
    echo
    echo "## Configuration (from ${SPEC}.cfg)"
    if [ -f "${SPEC}.cfg" ]; then
        # Constants block (stop at first blank or comment line).
        awk '/^CONSTANTS/{flag=1; next} flag && (/^$/ || /^\\\*/) {flag=0} flag' \
            "${SPEC}.cfg" | sed 's/^/  /'
        # Symmetry / state-constraint / invariants / spec name
        grep -E '^(SYMMETRY|CONSTRAINT|INVARIANT|SPECIFICATION)' "${SPEC}.cfg" 2>/dev/null \
            | sed 's/^/  /' || true
    fi
    echo
    echo "## Run flags"
    echo "  -Xmx$HEAP -XX:+UseParallelGC -workers $WORKERS"
    echo
    echo "## Final progress line (from log)"
    progress="$(grep '^Progress' "$log" 2>/dev/null | tail -1 || true)"
    echo "  ${progress:-(no progress lines logged)}"
    echo
    echo "## Last 25 lines of log (TLC final stats / error trace)"
    tail -25 "$log" 2>/dev/null | sed 's/^/  /'
    echo
    echo "## Files"
    echo "  Full log:     $tla_dir/$log"
    echo "  This summary: $tla_dir/$summary"
} > "$summary"

echo
echo "=== Summary written to $summary ==="
echo
cat "$summary"

exit "$exit_code"
