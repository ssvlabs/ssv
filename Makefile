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

# consensustest-with-real-bls runs the consensustest framework's real-BLS suite
# (gated behind the `real_bls` build tag). The suite exercises the OBFT
# adapter's threshold-IBE + real-BLS signing path end-to-end across cluster
# sizes, scenarios, and seeds. Default `unit-test` runs stub-crypto only;
# this target adds real-crypto coverage on demand. Budget: <10 min wall time.
.PHONY: consensustest-real-bls
consensustest-with-real-bls:
	@echo "Running consensustest real-BLS suite"
	@go test -tags "blst_enabled lfs real_bls" -timeout 15m -v ./protocol/v2/consensustest/...

# stresstest runs the stress-tier batch-comparison framework
# (7 curated sweeps × OBFT family + 2abOBFT family + QBFT family ×
# per-scenario iterations) and writes / merges data.js into REPORT_DIR
# (default ./stresstest-report)
# — consumed by the static UI (index.html + app.js + styles.css)
# already in that folder.
#
# Each `make stresstest` run produces data for a SINGLE (n, K) operating
# point (set via CLUSTER_SIZE_N and LAYERS_K). The reporting layer
# merges new (n, K) slices into the existing data.js by Fields-tuple,
# so multiple runs at different (n, K) compose into one report instead
# of overwriting. Example:
#
#   make stresstest CLUSTER_SIZE_N=4 LAYERS_K=4
#   make stresstest CLUSTER_SIZE_N=4 LAYERS_K=2
#   make stresstest CLUSTER_SIZE_N=7 LAYERS_K=3
#
# leaves a single data.js with all three (n, K) slices, selectable in
# the UI via the n and K pickers (greyed out where a slice is missing).
#
# Sweeps (all use LogNormalDelay as the production-shaped propagation
# model; see protocol/v2/consensustest/sweep.go for full docs):
#   - p2p_baseline          (BTT × σ × instability = 5 × 5 × 5 = 125
#                            points per run; heatmap source. The
#                            instability axis applies only to the
#                            Baseline-group scenario, Healthy — non-
#                            Baseline rows are instability-invariant.)
#   - p2p_increasing_BTT    (BTT ∈ {100, 200, 400, 600, 800, 1000} ms)
#   - p2p_heavy_tail        (σ ∈ {0.1, 0.3, 0.4, 0.5, 0.6, 0.7, 0.9})
#   - p2p_packet_loss       (LossRate ∈ {0, 0.01, 0.05, 0.10, 0.20})
#   - p2p_correlated_delays (BadLinkProb ∈ {0, 0.05, 0.10, 0.20})
#   - p2p_node_slowness     (slow op count ∈ {0, 1, 2, 3}, markov
#                            persistence 0.8)
#   - p2p_instability       (5 levels: none / low / moderate / high /
#                            extreme — Healthy-only "production p2p"
#                            chart; layers MarkovianSlowness +
#                            LossyNetwork on top of the LogNormal
#                            jitter. See InstabilityLevels in
#                            protocol/v2/consensustest/instability.go
#                            for the per-level params.)
#
# Operating-point env vars:
#   - CLUSTER_SIZE_N (default 4) — the SSV cluster size n.
#   - LAYERS_K       (default = n; SSV's K=N convention) — OBFT layer
#                    count. Production SSV uses K = n; smaller K trades
#                    fall-through depth for tighter per-layer budgets.
#
# Iteration count split into two budgets:
#   - ITERATIONS_BASELINE_OPERATIONS (default 1000) — high-confidence
#     count for scenarios with Group == "Baseline" (currently just
#     "Healthy"). Keeps the headline CDF tail well-sampled.
#   - ITERATIONS_UNSTABLE_OPERATIONS (default 100) — lower count for
#     every other scenario (adversarial / rare-event groups), where a
#     smaller sample is enough to surface non-zero behaviour without
#     paying the full baseline cost across dozens of cells.
#
# ITERATIONS (legacy single knob) overrides both — set it when you
# want to bump every scenario to the same higher count (e.g.
# `ITERATIONS=10000 make stresstest` for rare-event tail stability).
#
# `$(abspath ...)` resolves the path before passing to `go test` so
# reports land where the user expects regardless of `go test`'s package CWD.
#
# See docs/STRESSTEST-REPORT.md for the usage guide and
# docs/CONSENSUSTEST-BATCH-PLAN.md for the design rationale.
REPORT_DIR ?= ./stresstest-report
ITERATIONS_BASELINE_OPERATIONS ?= 1000
ITERATIONS_UNSTABLE_OPERATIONS ?= 100
# CLUSTER_SIZE_N / LAYERS_K behavior:
#   - BOTH unset (quick default): runs the curated pair {(n=4, K=2),
#     (n=4, K=4)} — fast, representative, brackets the BFT-liveness
#     floor (MinK(4)=2) and the SSV K=N convention.
#   - Only CLUSTER_SIZE_N set: iterates every valid K for that n
#     (MinK(N)..N).
#   - Only LAYERS_K set: iterates every supported n in [4, 7, 10, 13],
#     keeping only (n, K) pairs where K is valid for that n.
#   - Both set: a single (n, K) point.
# Reruns merge into the same data.js via the WriteReportData merge
# path, so iterative widening (start at the default, then add specific
# (n, K) slices) composes naturally.
CLUSTER_SIZE_N ?=
LAYERS_K ?=
.PHONY: stresstest
stresstest:
	@echo "Generating stress test report to $(abspath $(REPORT_DIR)) (CLUSTER_SIZE_N=$(if $(CLUSTER_SIZE_N),$(CLUSTER_SIZE_N),default) LAYERS_K=$(if $(LAYERS_K),$(LAYERS_K),default) baseline=$(ITERATIONS_BASELINE_OPERATIONS) unstable=$(ITERATIONS_UNSTABLE_OPERATIONS)$(if $(ITERATIONS), ITERATIONS=$(ITERATIONS) [override]))"
	@REPORT_DIR=$(abspath $(REPORT_DIR)) \
		$(if $(CLUSTER_SIZE_N),CLUSTER_SIZE_N=$(CLUSTER_SIZE_N)) \
		$(if $(LAYERS_K),LAYERS_K=$(LAYERS_K)) \
		ITERATIONS_BASELINE_OPERATIONS=$(ITERATIONS_BASELINE_OPERATIONS) \
		ITERATIONS_UNSTABLE_OPERATIONS=$(ITERATIONS_UNSTABLE_OPERATIONS) \
		$(if $(ITERATIONS),ITERATIONS=$(ITERATIONS)) \
		go test -tags "blst_enabled lfs" -timeout=0 -run TestStress -v ./protocol/v2/consensustest/

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
