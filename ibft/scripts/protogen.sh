#!/usr/bin/env bash

set -eo pipefail

# Generate protoc script
# We use the vendor dir to link 3rd party dependencies into the generate proto files.
# It is necessary to delete the vendor dir when we finish

protogen_base() {
  BASE_PATH=$1

  proto_dirs=$(find "$BASE_PATH"/proto -path -prune -o -name '*.proto' -print0 | xargs -0 -n1 dirname | sort | uniq)
  for dir in $proto_dirs; do
    protoc \
    -I "$BASE_PATH/proto" \
    -I "./ibft/proto" \
    -I "./vendor"\
    -I "./vendor/github.com/prysmaticlabs/ethereumapis"\
    --go_out="$BASE_PATH"\
    $(find "${dir}" -maxdepth 1 -name '*.proto')
  done

  # move proto files to the right places
  mkdir -p "$BASE_PATH"/types
  # shellcheck disable=SC2086
  cp -r $BASE_PATH/github.com/bloxapp/ssv/$BASE_PATH/* "$BASE_PATH"
  rm -rf "$BASE_PATH"/github.com
}

# update vendor dir
go mod vendor

# global
protogen_base ibft

# ssv implementation
protogen_base ibft/implementations/ssv_eth2

# cleanup
rm -rf vendor