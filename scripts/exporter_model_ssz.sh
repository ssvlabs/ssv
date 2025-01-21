#!/usr/bin/env bash

set -eo pipefail
go mod vendor

go generate ./exporter/v2/model.go
