#!/usr/bin/env bash

# see pre-requests:
# - https://grpc.io/docs/languages/go/quickstart/
# - gocosmos plugin is automatically installed during scaffolding.

set -eo pipefail

# update vendor dir
go mod vendor


proto_dirs=$(find ./ibft/proto -path -prune -o -name '*.proto' -print0 | xargs -0 -n1 dirname | sort | uniq)
for dir in $proto_dirs; do
  protoc \
  -I "./ibft/proto" \
  -I "./vendor"\
  -I "./vendor/github.com/prysmaticlabs/ethereumapis"\
  --go_out=./ibft\
  $(find "${dir}" -maxdepth 1 -name '*.proto')
done

# cleanup
rm -rf vendor
rm -rf ibft/types


# move proto files to the right places
mkdir ibft/types
cp -r ibft/github.com/bloxapp/ssv/ibft/types/* ./ibft/types
rm -rf ibft/github.com

