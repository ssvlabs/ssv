#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -lt 1 ]; then
  echo "Usage: ./scripts/lint.sh <project>"
  exit 1
fi

PROJECT="$1"
RC=0

pushd "$PROJECT"

PACKAGES=$(find . -iname '*.go' -not -path "./vendor/*" -exec dirname {} \; | sort | uniq)
for PACKAGE in $PACKAGES; do
  FILES=$(find $PACKAGE -maxdepth 1 -name '*.go' -not -name '*_test.go' -not -name "*.pb.go" -not -name '*_mock.go')
  
  echo "==> golint $PACKAGE"
  golint -set_exit_status $FILES || RC=1

  echo "==> goimports $PACKAGE"
  BADLY_FORMATTED=$(goimports -l -local "github.com/bloxapp/ssv" $PACKAGE || true)
  if [[ -n $BADLY_FORMATTED ]]; then
    RC=1
    echo "Error: These files are badly formatted: $BADLY_FORMATTED"
    goimports -d -local "github.com/bloxapp/ssv" $PACKAGE
  fi
done

popd

exit $RC
