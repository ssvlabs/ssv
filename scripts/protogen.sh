#!/usr/bin/env bash

set -eo pipefail

# Generate protoc script
# We use the vendor dir to link 3rd party dependencies into the generate proto files.
# It is necessary to delete the vendor dir when we finish

# Requirements
# 1, make sure project path is in go/src/github.com
# 2, clone https://github.com/gogo/protobuf to the same folder


go generate ./ibft/proto/generate.go
go generate ./network/generate.go
