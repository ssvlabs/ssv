ifndef GOPATH
    GOPATH=$(shell go env GOPATH)
    export GOPATH
endif

ifndef HOST_ADDRESS
    HOST_ADDRESS=$(shell dig @resolver4.opendns.com myip.opendns.com +short)
    export HOST_ADDRESS
endif

ifndef BUILD_PATH
    BUILD_PATH="/go/bin/ssvnode"
    export BUILD_PATH
endif

NODE_COMMAND_ARGS=--config=${CONFIG_PATH}
ifneq ($(SHARE_CONFIG),)
  NODE_COMMAND_ARGS+= --share-config=${SHARE_CONFIG}
endif

BOOTNODE_COMMAND_ARGS=--config=${CONFIG_PATH}

COV_CMD="-cover"
ifeq ($(COVERAGE),true)
	COV_CMD=-coverpkg=./... -covermode="atomic" -coverprofile="coverage.out"
endif

RUN_TOOL=go tool -modfile=tool.mod
SSVSIGNER_RUN_TOOL=go tool -modfile=../tool.mod

.PHONY: lint
lint: golangci-lint deadcode-lint openapi-lint ssvsigner-boundary-lint

.PHONY: golangci-lint
golangci-lint:
	GOWORK=off $(RUN_TOOL) github.com/golangci/golangci-lint/v2/cmd/golangci-lint run -v ./...
	@$(MAKE) ssvsigner-golangci-lint

.PHONY: ssvsigner-golangci-lint
ssvsigner-golangci-lint:
	cd ssvsigner && GOWORK=off $(SSVSIGNER_RUN_TOOL) github.com/golangci/golangci-lint/v2/cmd/golangci-lint run -c ../.golangci.yaml -v ./...

.PHONY: deadcode-lint
deadcode-lint:
	./scripts/deadcode.sh

.PHONY: ssvsigner-boundary-lint
ssvsigner-boundary-lint:
	./scripts/ssvsigner_boundary.sh

.PHONY: unit-test
unit-test:
	@echo "Running unit tests"
	@go test -tags "blst_enabled lfs" -timeout 10m -race -covermode=atomic -coverprofile=coverage1.out -p 16 -parallel 256 `go list ./... | grep -ve "spectest\|ssv/scripts/\|/network/p2p"`
	# running tests in `./network/p2p` separately because they get flaky when run concurrently with others for some reason
	@go test -tags "blst_enabled lfs" -timeout 10m -race -covermode=atomic -coverprofile=coverage2.out -p 16 -parallel 256 ./network/p2p
	@cat coverage1.out > coverage.out && tail -n +2 coverage2.out >> coverage.out

.PHONY: unit-test-all
unit-test-all:
	@$(MAKE) unit-test
	@$(MAKE) ssvsigner-test

.PHONY: ssvsigner-test
ssvsigner-test:
	@echo "Running ssv-signer unit tests"
	@cd ssvsigner && go test -tags blst_enabled -timeout 10m -race -covermode=atomic -coverprofile=coverage.out -p 16 -parallel 256 `go list ./... | grep -ve "ssvsigner/e2e"`

.PHONY: spec-test
spec-test:
	@echo "Running spec tests"
	@go test -tags blst_enabled -timeout 10m ${COV_CMD} -race -p 16 -parallel 256 ./protocol/v2/qbft/spectest
	@go test -tags blst_enabled -timeout 10m ${COV_CMD} -race -p 16 -parallel 256 ./protocol/v2/ssv/spectest

.PHONY: benchmark
benchmark:
	@echo "Running benchmark for specified directory"
	@go test -run=^# -bench . -benchmem -v TARGET_DIR_PATH -count 3

# fuzz-validation runs each OBFT message-validation fuzz target for FUZZTIME
# (default 60s). Override with `make fuzz-validation FUZZTIME=10m`. Each
# fuzz target runs separately because Go's `-fuzz` flag accepts only one
# target at a time. Discovered failing inputs are written to
# message/validation/testdata/fuzz/<TestName>/.
FUZZTIME ?= 60s
FUZZ_VALIDATION_TARGETS = \
	FuzzOBFTUnwrap \
	FuzzOBFTPhase1BundleDecode \
	FuzzOBFTCommitDecode \
	FuzzOBFTCertificateDecode \
	FuzzOBFTPhase1BundleRoundtrip \
	FuzzOBFTCommitRoundtrip \
	FuzzValidateOBFTMessage \
	FuzzOBFTAdmissionsAdmit

.PHONY: fuzz-validation
fuzz-validation:
	@echo "Running OBFT validation fuzz tests (FUZZTIME=$(FUZZTIME) per target)"
	@for target in $(FUZZ_VALIDATION_TARGETS); do \
		echo ">> $$target"; \
		go test -tags blst_enabled -run='^$$' -fuzz="^$$target"'$$' -fuzztime=$(FUZZTIME) ./message/validation/ || exit 1; \
	done

# consensustest-real-bls runs the consensustest framework's real-BLS suite
# (gated behind the `real_bls` build tag). The suite exercises the OBFT
# adapter's threshold-IBE + real-BLS signing path end-to-end across cluster
# sizes, scenarios, and seeds. Default `unit-test` runs stub-crypto only;
# this target adds real-crypto coverage on demand. Budget: <10 min wall time.
.PHONY: consensustest-real-bls
consensustest-real-bls:
	@echo "Running consensustest real-BLS suite"
	@go test -tags "blst_enabled lfs real_bls" -timeout 15m -v ./protocol/v2/consensustest/...

# consensustest-report runs the catalog matrix and writes HTML / CSV /
# Markdown reports to REPORT_DIR (default ./consensustest-reports relative to
# repo root). Override the dir with `make consensustest-report REPORT_DIR=path/to/dir`.
# `$(abspath ...)` resolves the path before passing to `go test` so the
# reports land where the user expects regardless of `go test`'s package CWD.
REPORT_DIR ?= ./consensustest-reports
.PHONY: consensustest-report
consensustest-report:
	@echo "Generating consensustest reports to $(abspath $(REPORT_DIR))"
	@REPORT_DIR=$(abspath $(REPORT_DIR)) go test -tags "blst_enabled lfs" -run TestGenerateReport -v ./protocol/v2/consensustest/

# consensustest-batch-report runs the multi-sim batch-comparison framework
# (5 curated sweeps × OBFT/QBFT × BATCH_ITERATIONS iterations) and writes
# per-(sweep, point) HTML / CSV / Markdown reports plus a top-level
# index.html to BATCH_REPORT_DIR (default ./consensustest-batch-reports).
#
# Iteration count: override via BATCH_ITERATIONS (default 100). 100 gives
# stable P99 stats for success-rate ≥ 50% scenarios; bump to 1000 for
# rare-event scenarios at proportionally longer wallclock (~12-15 min).
#
# See docs/CONSENSUSTEST-BATCH-PLAN.md for the framework design.
BATCH_REPORT_DIR ?= ./consensustest-batch-reports
BATCH_ITERATIONS ?= 100
.PHONY: consensustest-batch-report
consensustest-batch-report:
	@echo "Generating consensustest batch reports to $(abspath $(BATCH_REPORT_DIR)) (BATCH_ITERATIONS=$(BATCH_ITERATIONS))"
	@BATCH_REPORT_DIR=$(abspath $(BATCH_REPORT_DIR)) BATCH_ITERATIONS=$(BATCH_ITERATIONS) \
		go test -tags "blst_enabled lfs" -timeout 30m -run TestGenerateBatchReport -v ./protocol/v2/consensustest/

.PHONY: docker-spec-test
docker-spec-test:
	@echo "Running spec tests in docker"
	@docker build -t ssv_tests -f tests.Dockerfile .
	@docker run --rm ssv_tests make spec-test

.PHONY: docker-unit-test
docker-unit-test:
	@echo "Running unit tests in docker"
	@docker build -t ssv_tests -f tests.Dockerfile .
	@docker run --rm ssv_tests make unit-test

.PHONY: docker-benchmark
docker-benchmark:
	@echo "Running benchmark in docker"
	@docker build -t ssv_tests -f tests.Dockerfile .
	@docker run --rm ssv_tests make benchmark

.PHONY: build
.DEFAULT_GOAL := build # this makes `make` default to `make build`
build:
	CGO_ENABLED=1 go build -o ./bin/ssvnode -ldflags "-X main.Commit=`git rev-parse HEAD` -X main.Version=`git describe --tags --exact-match HEAD 2>/dev/null || echo "untagged"`" ./cmd/ssvnode/

.PHONY: spec-alignment-diff
spec-alignment-diff:
	cd ./scripts/differ && go install .
	cd ./scripts/spec-alignment && ./differ.sh

# TLA+ formal-verification targets — delegate to tla/Makefile.
# See tla/README.md for prerequisites and tla/Makefile for full usage.
.PHONY: tla-verify-bare tla-verify-bareliveness tla-verify-lbid tla-verify-lbidnew tla-verify-all tla-clean
tla-verify-bare tla-verify-bareliveness tla-verify-lbid tla-verify-lbidnew tla-verify-all tla-clean:
	$(MAKE) -C tla $(@:tla-%=%)

.PHONY: start-node
start-node:
	@echo "Build binary: ${BUILD_PATH}"
	@echo "Config path: ${CONFIG_PATH}"
	@echo "Share config path: ${SHARE_CONFIG}"
	@echo "Command provided: ${NODE_COMMAND_ARGS}"
ifdef DEBUG_PORT
	@echo "Running node-${NODE_ID} in debug mode"
	@dlv  --continue --accept-multiclient --headless --listen=:${DEBUG_PORT} --api-version=2 exec \
	 ${BUILD_PATH} start-node -- ${NODE_COMMAND_ARGS}
else
	@echo "Running node on address: ${HOST_ADDRESS}"
	@${BUILD_PATH} start-node ${NODE_COMMAND_ARGS}
endif

# docker-run builds and runs docker image in foreground (also mounting a Docker-managed volume `data`)
.PHONY: docker-run
docker-run:
	@echo "node ${NODES_ID}"
	@docker rm -f ssv_node && docker build -t ssv_node . && docker run --env-file .env --name=ssv_node -p 16000:16000 -p 13001:13001 -p 12001:12001/udp -v data:/data -it ssv_node make BUILD_PATH=/go/bin/ssvnode start-node && docker logs ssv_node --follow

# docker builds and runs docker image in background
.PHONY: docker
docker:
	@echo "node ${NODES_ID}"
	@docker rm -f ssv_node && docker build -t ssv_node . && docker run -d --env-file .env --restart unless-stopped --name=ssv_node -p 13000:13000 -p 12000:12000/udp -it ssv_node make BUILD_PATH=/go/bin/ssvnode  start-node && docker logs ssv_node --follow

# docker-image runs existing docker image in background
.PHONY: docker-image
docker-image:
	@echo "node ${NODES_ID}"
	@sudo docker rm -f ssv_node && docker run -d --env-file .env --restart unless-stopped --name=ssv_node -p 13000:13000 -p 12000:12000/udp 'ssvlabs/ssv-node:latest' make BUILD_PATH=/go/bin/ssvnode start-node

NODES=ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
.PHONY: docker-all
docker-all:
	@echo "nodes $(NODES)"
	@docker-compose up --build $(NODES)

NODES=ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
.PHONY: docker-local
docker-local:
	@echo "nodes $(NODES)"
	@docker-compose -f docker-compose-local.yaml up --build $(NODES)

DEBUG_NODES=ssv-node-1-dev ssv-node-2-dev ssv-node-3-dev ssv-node-4-dev
.PHONY: docker-debug
docker-debug:
	@echo $(DEBUG_NODES)
	@docker-compose up --build $(DEBUG_NODES)

.PHONY: stop
stop:
	@docker-compose down

.PHONY: start-boot-node
start-boot-node:
	@echo "Running start-boot-node"
	${BUILD_PATH} start-boot-node ${BOOTNODE_COMMAND_ARGS}

.PHONY: mock
mock:
	make generate

.PHONY: generate
generate:
	go generate ./...

SWAG := $(RUN_TOOL) github.com/swaggo/swag/cmd/swag

.PHONY: openapi openapi-lint

openapi:
	@SWAG='$(SWAG)' bash scripts/openapi.sh --write

openapi-lint:
	@SWAG='$(SWAG)' bash scripts/openapi.sh --lint

.PHONY: format
format:
	# `goimports` doesn't support simplify option ("-s"), hence we run `gofmt` separately
	# just for that - see https://github.com/golang/go/issues/21476 for details.
	gofmt -s -w $$(find . -name '*.go' -not -path "*mock*")
	# Formatters such as goimports do not deem necessary to "skip" generated code and
	# rather want generators to generate code that complies with the desired formats
	# (see https://github.com/golang/go/issues/71676 for details). In practice however
	# it's not always possible to fix that on the generator side, so we have to use a
	# work-around here to filter out generated files on our own.
	$(RUN_TOOL) goimports -l -w -local github.com/ssvlabs/ssv/ $$(find . -name '*.go' -not -path "*mock*")
