#!/usr/bin/env bash

set -eo pipefail

# Generate protoc script
# We use the vendor dir to link 3rd party dependencies into the generate proto files.
# It is necessary to delete the vendor dir when we finish

go generate $GOPATH/src/github.com/bloxapp/ssv/ibft/proto/generate.go
go generate $GOPATH/src/github.com/bloxapp/ssv/network/generate.go
