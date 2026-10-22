#!/usr/bin/env bash
set -eo pipefail

# Library consumers of github.com/ssvlabs/ssv/v2 ignore our `replace => ./ssvsigner`
# and build against the pinned ssvsigner version instead, which CI never builds.
# Run before cutting a release tag (tags are immutable on the Go proxy).
# Needs full git history.
#
# To re-pin, on stage after the ssvsigner/ change has landed (a PR branch commit
# is not what squash/rebase merges put on stage):
#
#   c=$(git log -1 --format=%H HEAD -- ssvsigner/)
#   go mod edit -require="github.com/ssvlabs/ssv/ssvsigner@$(cd "$(mktemp -d)" && GOWORK=off GOFLAGS= go list -m -f '{{.Version}}' "github.com/ssvlabs/ssv/ssvsigner@$c")"

echo -e "\033[36mINFO\033[0m [ssvsigner-pin] Verifying the root go.mod ssvsigner pin matches ssvsigner/"

fail() {
  echo -e "\033[31mERROR\033[0m [ssvsigner-pin] $1"
  exit 1
}

VERSION="$(go mod edit -json | jq -r '.Require[] | select(.Path == "github.com/ssvlabs/ssv/ssvsigner") | .Version')"
[[ -n "$VERSION" ]] || fail "github.com/ssvlabs/ssv/ssvsigner not found in go.mod require"

# Pseudo-version: vX.Y.Z-[pre.0.]yyyymmddhhmmss-abcdefabcdef
if [[ "$VERSION" =~ ([0-9]{14})-([0-9a-f]{12})$ ]]; then
  TIMESTAMP="${BASH_REMATCH[1]}"
  REV="${BASH_REMATCH[2]}"
else
  TIMESTAMP=""
  REV="ssvsigner/$VERSION"
fi

COMMIT="$(git rev-parse --verify --quiet "$REV^{commit}")" \
  || fail "pinned revision $REV ($VERSION) not found; fetch full history (git fetch --unshallow --tags)"

if [[ -n "$TIMESTAMP" ]]; then
  COMMIT_TIME="$(TZ=UTC git show -s --format=%cd --date=format-local:%Y%m%d%H%M%S "$COMMIT")"
  [[ "$TIMESTAMP" == "$COMMIT_TIME" ]] \
    || fail "invalid pseudo-version $VERSION: timestamp $TIMESTAMP, but commit time is $COMMIT_TIME"
fi

git merge-base --is-ancestor "$COMMIT" HEAD \
  || fail "pinned commit $COMMIT is not in this branch's history (pre-squash commit?)"

if ! git diff --quiet "$COMMIT" HEAD -- ssvsigner/; then
  git diff --stat "$COMMIT" HEAD -- ssvsigner/
  fail "pin $VERSION is stale; re-pin it to the latest commit touching ssvsigner/ (see $0)"
fi

echo -e "\033[32mINFO\033[0m [ssvsigner-pin] ssvsigner pin check passed"
