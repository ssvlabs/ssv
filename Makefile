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
# (6 curated sweeps × the default protocol set — see the PROTOCOLS
# variable below — × per-scenario iterations) and writes / merges
# data.js into
# REPORT_DIR (default ./stresstest-report)
# — consumed by the static UI (index.html + app.js + styles.css)
# already in that folder.
#
# Each `make stresstest` run produces data for one or more (n, K) pairs
# (controlled by CLUSTER_SIZES_N and LAYERS_K). The reporting layer
# merges new (n, K) slices into the existing data.js by Fields-tuple,
# so multiple runs at different (n, K) compose into one report instead
# of overwriting. Example:
#
#   make stresstest CLUSTER_SIZES_N=4 LAYERS_K=4
#   make stresstest CLUSTER_SIZES_N=4 LAYERS_K=2
#   make stresstest CLUSTER_SIZES_N=7 LAYERS_K=3,4
#
# leaves a single data.js with all four (n, K) slices, selectable in
# the UI via the N and K pickers (greyed out where a slice is missing).
#
# Sweeps (p2p_baseline uses calibrated empirical mesh-hop profiles
# fitted to real SSV gossipsub telemetry; the synthetic-axis sweeps
# retain LogNormal anchors so the parameter axis stays meaningful.
# See protocol/v2/consensustest/sweep.go for full docs):
#   - p2p_baseline          (BTT × profile × instability × BFT_start;
#                            heatmap source. The instability axis
#                            applies only to the Baseline-group
#                            scenario, Healthy — non-Baseline rows
#                            are instability-invariant. BFT_start > 0
#                            emits OBFT-family cells only; pipeline-
#                            shift protocols (PSigs / QBFT) are
#                            covered by the BFT_start=0 cell + UI
#                            pipeline-shift.)
#   - p2p_increasing_BTT    (BTT ∈ {100, 200, 400, 600, 800, 1000} ms)
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
# Operating-point env vars (all have defaults; override to scope runs):
#   - CLUSTER_SIZES_N (default 4) — comma-separated cluster sizes ∈ {4, 7}.
#     Multiple values → one run per size, all merging into the same data.js.
#   - LAYERS_K        (default 2) — comma-separated K values ∈ {2, 3, 4}.
#     Defaults to the BFT-liveness floor at n=4 (K=2 = f+1, per the
#     repo's current default K convention). A K value is skipped for
#     any n where K < MinK(n). Override to e.g. `LAYERS_K=2,3,4` to
#     fill in the matrix.
#   - P2P_PROFILES    (default = all six) — comma-separated calibrated
#     mesh-hop profile names. Valid: prod, stage1, stage2, slow,
#     heavy_tail, slow_heavy_tail. Each name becomes one point in the
#     BTT × profile × instability × BFT_start cross-product, with both
#     cfg.Network and cfg.Mesh.HopDelay sourced from the named profile.
#   - BTT_VALUES_MS   (default 100,200,300,400) — comma-separated BTT
#     values in ms. Shared by the p2p_baseline and p2p_increasing_BTT
#     sweeps; drives the protocol's internal timing budgets (the network
#     itself is the profile, ≈ 1-10 ms in prod).
#   - BFT_STARTS      (default 0,2400,2800,3200) — comma-separated
#     BFT_start values in ms for the p2p_baseline sweep's BFT_start
#     axis. The UI picker exposes more values; a picker value with no
#     matching sim point uses the BFT_start=0 cell when below the cell's
#     emitted independence threshold (exact reuse), else rounds UP to the
#     nearest emitted cell (worst-case), and shows n/a only past the
#     highest emitted cell. Pipeline-shift protocols always pull from
#     BFT_start=0.
#
# Iteration count split into two budgets:
#   - ITERATIONS_BASELINE_OPERATIONS (default 10000) — high-confidence
#     count for scenarios with Group == "Baseline" (currently just
#     "Healthy"). Keeps the headline CDF tail well-sampled.
#   - ITERATIONS_UNSTABLE_OPERATIONS (default 1) — single-sample probe
#     for every other scenario (adversarial / rare-event groups). At
#     iter=1, per-cell SuccessRate is binary {0, 1} and percentile
#     distributions collapse — adversarial cells become "did the
#     deterministic path through seed=SeedStart succeed?" rather than
#     a statistical estimate. The default trades adversarial signal for
#     a ~100× shorter total wallclock so the Baseline tail can be
#     sampled deeper; bump this (e.g. `make stresstest
#     ITERATIONS_UNSTABLE_OPERATIONS=100`) when you need a real CDF on
#     the adversarial rows.
#
# `$(abspath ...)` resolves the path before passing to `go test` so
# reports land where the user expects regardless of `go test`'s package CWD.
#
# Driver docstring: protocol/v2/consensustest/stress_test.go TestStress.
# Sweep definitions: protocol/v2/consensustest/sweep.go DefaultSweeps.
REPORT_DIR ?= ./stresstest-report
ITERATIONS_BASELINE_OPERATIONS ?= 10000
ITERATIONS_UNSTABLE_OPERATIONS ?= 1
CLUSTER_SIZES_N ?= 4
LAYERS_K ?= 2
P2P_PROFILES ?= prod,stage1,stage2,slow,heavy_tail,slow_heavy_tail
BTT_VALUES_MS ?= 100,200,300,400
BFT_STARTS ?=
# PROTOCOLS — comma-separated protocol names to include in the sweep (e.g.
# `OBFT,QBFT,PSigs`). Setting it empty (`PROTOCOLS=`) runs ALL registered
# protocols. The curated default below is a deliberate subset: it omits the
# PSigs baseline-cost reference and the OBFTx2 / OBFTx3 multiplier variants.
# To include them, override per-run (e.g.
# `make stresstest PROTOCOLS=OBFT,2abOBFT,QBFT,PSigs`) or run the full set
# with `PROTOCOLS=`. Names must exactly match Protocol.Name() values
# defined in stress_test.go.
PROTOCOLS ?= OBFT-no-reflood,OBFT,2abOBFT-no-reflood,2abOBFT-lean,2abOBFT,QBFT-no-reflood,QBFT,QBFT-SSV
.PHONY: stresstest
stresstest:
	@echo "Generating stress test report to $(abspath $(REPORT_DIR)) (CLUSTER_SIZES_N=$(CLUSTER_SIZES_N) LAYERS_K=$(LAYERS_K) P2P_PROFILES=$(P2P_PROFILES) BTT_VALUES_MS=$(BTT_VALUES_MS) BFT_STARTS=$(if $(BFT_STARTS),$(BFT_STARTS),<default>) PROTOCOLS=$(if $(PROTOCOLS),$(PROTOCOLS),<all>) baseline=$(ITERATIONS_BASELINE_OPERATIONS) unstable=$(ITERATIONS_UNSTABLE_OPERATIONS))"
	@REPORT_DIR=$(abspath $(REPORT_DIR)) \
		CLUSTER_SIZES_N=$(CLUSTER_SIZES_N) \
		LAYERS_K=$(LAYERS_K) \
		P2P_PROFILES=$(P2P_PROFILES) \
		BTT_VALUES_MS=$(BTT_VALUES_MS) \
		BFT_STARTS=$(BFT_STARTS) \
		PROTOCOLS=$(PROTOCOLS) \
		ITERATIONS_BASELINE_OPERATIONS=$(ITERATIONS_BASELINE_OPERATIONS) \
		ITERATIONS_UNSTABLE_OPERATIONS=$(ITERATIONS_UNSTABLE_OPERATIONS) \
		go test -tags "blst_enabled lfs" -timeout=0 -run TestStress -v ./protocol/v2/consensustest/

# stresstest-all is a convenience alias for the full (n × K) matrix:
# CLUSTER_SIZES_N=4,7 × LAYERS_K=2,3,4. Equivalent to:
#   make stresstest CLUSTER_SIZES_N=4,7 LAYERS_K=2,3,4
# All other variables (ITERATIONS_*, P2P_PROFILES, REPORT_DIR) use their
# defaults or can be overridden on the command line.
.PHONY: stresstest-all
stresstest-all:
	@$(MAKE) stresstest CLUSTER_SIZES_N=4,7 LAYERS_K=2,3,4

# stresstest-clean removes the generated data.js from REPORT_DIR,
# leaving the static UI files (index.html, app.js, styles.css) intact.
# Run this to start a fresh sweep rather than merging into existing data.
# The `data.js.*.tmp` glob picks up debris from `os.CreateTemp` (atomic-
# write tempfiles) — names include a per-process unique suffix so two
# parallel runs don't race on a fixed name.
.PHONY: stresstest-clean
stresstest-clean:
	@rm -f "$(abspath $(REPORT_DIR))/data.js" "$(abspath $(REPORT_DIR))"/data.js.*.tmp
	@echo "Cleaned $(abspath $(REPORT_DIR))/data.js"

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
