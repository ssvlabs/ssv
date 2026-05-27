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

# runner-safety-stress amplifies the OBFT-family safety-bridge tests
# (TestSafetyBridge_OBFT_* + TestSafetyBridge_2abOBFT_*, 48 sub-tests
# total: 3 scenarios × 8 (n, K) cells × 2 protocols) under -race +
# -count=N + -cpu=X,Y,Z stress. Each iteration is an independent
# goroutine-scheduling realization; varying GOMAXPROCS varies the
# timing-pressure regime.
#
# The bridge reconstructs a ct.Outcome from the captured wire trace
# + per-Instance Resolve trace, then applies consensustest's 10 safety
# invariants. Where unit-test under -race only asserts "no error",
# this asserts "safety holds across the reconstructed Outcome" —
# catching race-induced wire-state inconsistencies that the -race
# detector alone can't surface (those are semantic races, not Go-level
# data races).
#
# Iteration count: -count=80 × 3 cpu points = 240 iterations per test.
# Measured ≈ 38 min wall on a typical dev machine (deep variant
# ≈ 2.5h). Wire into nightly CI, not every-PR gate. Override -count
# via SAFETY_STRESS_COUNT for local tuning (e.g. SAFETY_STRESS_COUNT=10
# for a fast smoke run).
#
# TODO — tiered extension plan (build when motivated by real-world
# findings; preemptive expansion not justified by current evidence):
#
# Today's 3 scenarios cover the high-probability + high-severity
# production race classes. Other race windows exist but are guarded
# by mutex discipline and not actively tested:
#   * cert-during-Resolve (peer cert arrives mid-Resolve)
#   * EndInstance-during-Cert (slot teardown races inbound cert)
#   * Phase-1-during-Phase-2a (bundle arrives at backstop-fire instant)
#   * NR-vs-σ pool quorum simultaneity (both fill at near-identical wall-clock)
#
# Both buses already expose the delayFn injection point
# (broadcastBus in OBFT base, blsBus in twoab — see *_test.go).
# Adding a scenario per untested class is mechanical on top of that.
# Three expansion tiers, increasing cost / coverage:
#
#   Tier 1: 1 scenario per untested class (~4 new). Wall ≈ 2× current
#           (~1.4h default / ~5h deep). Fits in nightly CI.
#   Tier 2: 3-5 timing variants per class (~16-20 new). Wall ≈ 5×
#           (~3h default / ~12h deep). Weekly run.
#   Tier 3: full delay-sweep across all wire kinds and offset values
#           (Coyote-lite). Wall ≈ 10-15×; impractical without dedicated
#           CI hardware.
#
# Tighter race windows may also need higher -count to hit reliably:
# random scheduling hits LateCommit-class races every iter, but tight
# cert-vs-Resolve windows may fire on only 1-5% of iters → need ~300+
# iters for 95% confidence. When adding scenarios, prefer cranking
# -count for that specific scenario over uniform inflation.
#
# Trigger to build: (a) production race incident the bridge missed, or
# (b) code-review-identified concrete race class with reproducer.
SAFETY_STRESS_COUNT ?= 80
.PHONY: runner-safety-stress
runner-safety-stress:
	@echo "Running runner-safety stress (real goroutines + safety invariants + -race × -cpu=1,4,8 × -count=$(SAFETY_STRESS_COUNT))"
	@for cpu in 1 4 8; do \
		echo ">> -cpu=$$cpu"; \
		go test -tags blst_enabled -race -count=$(SAFETY_STRESS_COUNT) -cpu=$$cpu -timeout 120m \
			-run '^TestSafetyBridge_' \
			./protocol/v2/ssv/runner/obft/... || exit 1; \
	done

# runner-safety-stress-deep is the same target with deeper amplification
# for nightly / weekly findings — increases -count to 320 (≈ 4× the
# default) and expands the timeout accordingly. Use when looking for
# rare race windows that the default count might not hit.
.PHONY: runner-safety-stress-deep
runner-safety-stress-deep:
	@$(MAKE) runner-safety-stress SAFETY_STRESS_COUNT=320

# stresstest runs the stress-tier batch-comparison framework
# (7 curated sweeps × the default protocol set — see PROTOCOLS below —
# × per-scenario iterations) and writes / merges data.js into REPORT_DIR,
# consumed by the static UI (index.html + app.js + styles.css) already
# in that folder.
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
#   - p2p_partitions        (SeverProb ∈ {0, 0.05, 0.10, 0.20} —
#                            per-pair sustained severance over the
#                            mesh's delivery graph (eager ∪ gossip-
#                            reachable from the TopicMaxPeers≈10
#                            bounded candidate set). Unlike packet
#                            loss, cuts persist the whole slot —
#                            gossip recovery routes only through
#                            surviving connections. n=4 stays near-
#                            flat because the bound is inactive at
#                            that subnet size; n=7 and n=13 carry
#                            the degradation signal.)
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
# Output:
#   - REPORT_DIR (default ./stresstest-report) — directory the report's
#     data.js is written / merged into; consumed by the static UI in that
#     folder. `$(abspath ...)` resolves it before passing to `go test` so
#     reports land where the user expects regardless of `go test`'s
#     package CWD.
#   - While running, a live progress display is drawn to the terminal: one bar
#     per protocol (each aggregating all of that protocol's runs) under a
#     centered overall header, stretched to the terminal width and redrawn in
#     place. The block always fits the terminal so the in-place redraw stays
#     exact (otherwise it can't reach scrolled-off / wrapped lines and stacks a
#     fresh copy each tick): if the protocol set is taller than the window, the
#     rows that don't fit collapse into a single "+N more" aggregate line, and on
#     a window too narrow for even one bar it shows just the header line. The
#     total sim count is computed up front, so the percentages are run-wide, not
#     per-sweep. When the output isn't a terminal (CI / piped) it falls back to a
#     periodic one-line summary.
#   - Interrupting the run (Ctrl-C / SIGINT) stops gracefully: it abandons the
#     in-progress batch, writes the results computed so far to data.js, and
#     exits — so a long run can be cut short without losing completed data. A
#     second Ctrl-C force-quits immediately.
#
# Operating-point env vars (all have defaults; override to scope runs):
#   - CLUSTER_SIZES_N (default 4) — comma-separated cluster sizes ∈ {4, 7}.
#     Multiple values → one run per size, all merging into the same data.js.
#   - LAYERS_K        (default 2) — comma-separated K values ∈ {2, 3, 4}.
#     Defaults to the BFT-liveness floor at n=4 (K=2 = f+1, per the
#     repo's current default K convention). A K value is skipped for
#     any n where K < MinK(n). Override to e.g. `LAYERS_K=2,3,4` to
#     fill in the matrix.
#   - P2P_PROFILES    (default = all eight) — comma-separated calibrated
#     mesh-hop profile names. Valid: prod, stage1a, stage1b, stage2a,
#     stage2b, slow, heavy_tail, slow_heavy_tail. Each name becomes one
#     point in the BTT × profile × instability × BFT_start cross-product,
#     with both cfg.Network and cfg.Mesh.HopDelay sourced from the named
#     profile.
#   - BTT_VALUES_MS   (default 100,200,300,400) — comma-separated BTT
#     values in ms. Shared by the p2p_baseline and p2p_increasing_BTT
#     sweeps; drives the protocol's internal timing budgets (the network
#     itself is the profile, ≈ 1-10 ms in prod).
# BFT_start is NOT a sweep parameter — the sim always runs at BFT_start=0
# and the report UI derives every other BFT_start value post-hoc (reuse of
# the BFT_start=0 cell below each variant's independence threshold, n/a
# above it, pipeline-shift for QBFT/PSigs). See stresstest-report/app.js.
#
# Protocol set:
#   - PROTOCOLS (default = curated subset below) — comma-separated
#     protocol names to include in the sweep (e.g. `OBFT-700,QBFT-700,PSigs`).
#     Setting it empty (`PROTOCOLS=`) runs ALL registered protocols. The
#     curated default is a deliberate subset: it omits the PSigs
#     baseline-cost reference. To include it, override per-run (e.g. `make
#     stresstest PROTOCOLS=OBFT-700,2abOBFT-700,QBFT-700,PSigs`) or run the
#     full set with `PROTOCOLS=`. Names must exactly match Protocol.Name()
#     values defined in stress_test.go.
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
# Driver docstring: protocol/v2/consensustest/stress_test.go TestStress.
# Sweep definitions: protocol/v2/consensustest/sweep.go DefaultSweeps.
REPORT_DIR ?= ./stresstest-report
CLUSTER_SIZES_N ?= 4
LAYERS_K ?= 2
P2P_PROFILES ?= prod,stage1a,stage1b,stage2a,stage2b,slow,heavy_tail,slow_heavy_tail
BTT_VALUES_MS ?= 100,200,300
PROTOCOLS ?= OBFT-0,OBFT-300,OBFT-500,OBFT-700,2abOBFT-0,2abOBFT-300,2abOBFT-500,2abOBFT-700,QBFT-0,QBFT-300,QBFT-500,QBFT-700,QBFT-SSV
ITERATIONS_BASELINE_OPERATIONS ?= 4000
ITERATIONS_UNSTABLE_OPERATIONS ?= 1
.PHONY: stresstest
stresstest:
	@echo "Generating stress test report to $(abspath $(REPORT_DIR)) (CLUSTER_SIZES_N=$(CLUSTER_SIZES_N) LAYERS_K=$(LAYERS_K) P2P_PROFILES=$(P2P_PROFILES) BTT_VALUES_MS=$(BTT_VALUES_MS) PROTOCOLS=$(if $(PROTOCOLS),$(PROTOCOLS),<all>) baseline=$(ITERATIONS_BASELINE_OPERATIONS) unstable=$(ITERATIONS_UNSTABLE_OPERATIONS))"
	@REPORT_DIR=$(abspath $(REPORT_DIR)) \
		CLUSTER_SIZES_N=$(CLUSTER_SIZES_N) \
		LAYERS_K=$(LAYERS_K) \
		P2P_PROFILES=$(P2P_PROFILES) \
		BTT_VALUES_MS=$(BTT_VALUES_MS) \
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

# stresstest-negative aggregates the safety-machinery negative tests
# into a single fast CI smoke check. Three regex branches:
#   - TestAdapter_.*_TriggersSafetyDetection — real-byz adapter tests
#     in obft/ and twoab/ that exercise NoOfflineDoubleV firing under
#     ByzAggregatorBypass / ByzWitnessForgery (forged-identity attacks
#     against the safety machinery).
#   - TestSafety_Honest* — synthetic-outcome tests in
#     consensustest/safety_test.go that exercise the per-op invariants
#     B1 (HonestCrossPhaseExclusive), B2 (HonestSingleSigmaV), D1
#     (HonestWalkConsistent). These hand-craft Outcome literals
#     simulating hypothetical EKM / Resolve-side regressions and
#     assert the corresponding SafetyReport field fires.
#   - TestAdapter_C[0-9]+_ — Phase 2 adapter-wiring tests that verify
#     CommitAttestation fields populate correctly (C1
#     QuorumBackedDecision from ResolveLayerAttempts; C3
#     OBFTHostValidityRespect from the RecordingHostPattern wrapper
#     cross-referenced with SigmaByEmitter). Run sims and check field
#     values rather than hand-crafting Outcomes.
#
# Together: prove that EVERY load-bearing safety invariant the
# framework checks (per docs/CONSENSUSTEST-SAFETY-INVARIANTS-PLAN.md)
# fires correctly on the inputs that should trigger it. A machinery
# regression — e.g., a refactor that silently breaks SigmaByEmitter
# population, or a check-gating bug — would surface here as a test
# failure, not as a silent green stresstest.
#
# Run-time: seconds. Wire into the same CI job as `make unit-test`.
.PHONY: stresstest-negative
stresstest-negative:
	@echo "Running stresstest negative-test smoke (machinery regression check)"
	@go test -tags blst_enabled -timeout 5m -v \
		-run '^TestAdapter_.*_TriggersSafetyDetection$$|^TestSafety_Honest|^TestAdapter_C[0-9]+_' \
		./protocol/v2/consensustest/... \
		./protocol/v2/consensustest/obft/... \
		./protocol/v2/consensustest/twoab/...

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
