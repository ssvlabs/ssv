#!/usr/bin/env bash

# Compile all test packages without running them, show compilation errors,
# and enforce a timeout. Handles both the root module and the ssvsigner module.

set -o pipefail

# Resolve repository root so the script works from any CWD
SCRIPT_DIR="$(cd -- "$(dirname "$0")" >/dev/null 2>&1 && pwd -P)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." >/dev/null 2>&1 && pwd -P)"

# Timeout (seconds). Can be overridden via TIMEOUT env var.
TIMEOUT="${TIMEOUT:-60}"

# Determine parallelism
if command -v sysctl >/dev/null 2>&1; then
	NPROC="$(sysctl -n hw.ncpu 2>/dev/null || echo 4)"
else
	NPROC="$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)"
fi

# Pick a timeout tool
if command -v gtimeout >/dev/null 2>&1; then
	TMOUT_TOOL="gtimeout"
elif command -v timeout >/dev/null 2>&1; then
	TMOUT_TOOL="timeout"
elif command -v perl >/dev/null 2>&1; then
	TMOUT_TOOL="perl"
else
	echo "error: no timeout tool found (gtimeout, timeout, or perl)" >&2
	exit 2
fi

run_with_timeout() {
	if [ "$TMOUT_TOOL" = "perl" ]; then
		perl -e "alarm $TIMEOUT; exec @ARGV" "$@"
	else
		"$TMOUT_TOOL" "$TIMEOUT" "$@"
	fi
}

compile_dir() {
	dir="$1"
	label="$2"

	tmpfile="$(mktemp)"

	echo "compiling test packages ($label) with $NPROC workers, timeout ${TIMEOUT}s..."

	# Compile tests only (-c), discard binaries (-o /dev/null). Errors are printed inline.
	run_with_timeout /usr/bin/time -p sh -c "cd '$dir' && \
		go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... | sed '/^$/d' | \
		xargs -n1 -P $NPROC -I{} sh -c 'go test -tags blst_enabled,testutils -c "{}" -o /dev/null 2>&1 || { printf \"%s\\n\" \"{}\" >> '"$tmpfile"'; }'"

	status=0
	if [ -s "$tmpfile" ]; then
		echo
		echo "failed test compiles ($label):"
		sort -u "$tmpfile"
		status=1
	else
		echo
		echo "all $label test packages compiled"
	fi

	rm -f "$tmpfile"
	return "$status"
}

overall=0
compile_dir "$ROOT_DIR" "root" || overall=1
compile_dir "$ROOT_DIR/ssvsigner" "ssvsigner" || overall=1

exit "$overall"
