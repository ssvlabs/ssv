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

# Node command.
NODE_COMMAND=--config=${CONFIG_PATH}
ifneq ($(SHARE_CONFIG),)
  NODE_COMMAND+= --share-config=${SHARE_CONFIG}
endif

# Bootnode command.
BOOTNODE_COMMAND=--config=${CONFIG_PATH}

COV_CMD="-cover"
ifeq ($(COVERAGE),true)
	COV_CMD=-coverpkg=./... -covermode="atomic" -coverprofile="coverage.out"
endif

GET_TOOL=go get -modfile=tool.mod -tool
RUN_TOOL=go tool -modfile=tool.mod
SSVSIGNER_RUN_TOOL=go tool -modfile=../tool.mod

.PHONY: lint
lint:
	GOWORK=off $(RUN_TOOL) github.com/golangci/golangci-lint/v2/cmd/golangci-lint run -v ./...
	@$(MAKE) ssvsigner-lint

.PHONY: ssvsigner-lint
ssvsigner-lint:
	cd ssvsigner && GOWORK=off $(SSVSIGNER_RUN_TOOL) github.com/golangci/golangci-lint/v2/cmd/golangci-lint run -c ../.golangci.yaml -v ./...

.PHONY: full-test
full-test:
	@echo "Running all tests"
	@go test -tags blst_enabled -timeout 20m ${COV_CMD} -p 1 -v ./...
	@cd ssvsigner && go test -tags blst_enabled -timeout 20m ${COV_CMD} -p 1 -v ./...

.PHONY: integration-test
integration-test:
	@echo "Running integration tests"
	@go test -tags blst_enabled -count=1 -timeout 20m ${COV_CMD} -p 1 -v ./integration/...

.PHONY: unit-test
unit-test:
	@echo "Running unit tests"
	@go test -tags blst_enabled -timeout 20m -race -covermode=atomic -coverprofile=coverage.out -p 1 `go list ./... | grep -ve "spectest\|integration\|ssv/scripts/"`
	@$(MAKE) ssvsigner-test

.PHONY: ssvsigner-test
ssvsigner-test:
	@echo "Running ssv-signer unit tests"
	@cd ssvsigner && go test -tags blst_enabled -timeout 20m -race -covermode=atomic -coverprofile=coverage.out -p 1 ./...

.PHONY: spec-test
spec-test:
	@echo "Running spec tests"
	@go test -tags blst_enabled -timeout 90m ${COV_CMD} -race -count=1 -p 1 -v `go list ./... | grep spectest`

.PHONY: all-spec-test-raceless
all-spec-test-raceless:
	@echo "Running spec tests"
	@go test -tags blst_enabled -timeout 90m ${COV_CMD} -p 1 -v ./protocol/...

.PHONY: spec-test-raceless
spec-test-raceless:
	@echo "Running spec tests without race flag"
	@go test -tags blst_enabled -timeout 20m -count=1 -p 1 -v `go list ./... | grep spectest`

.PHONY: benchmark
benchmark:
	@echo "Running benchmark for specified directory"
	@go test -run=^# -bench . -benchmem -v TARGET_DIR_PATH -count 3

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

.PHONY: docker-integration-test
docker-integration-test:
	@echo "Running integration tests in docker"
	@docker build -t ssv_tests -f tests.Dockerfile .
	@docker run --rm ssv_tests make integration-test

.PHONY: docker-benchmark
docker-benchmark:
	@echo "Running benchmark in docker"
	@docker build -t ssv_tests -f tests.Dockerfile .
	@docker run --rm ssv_tests make benchmark

.PHONY: build
build:
	CGO_ENABLED=1 go build -o ./bin/ssvnode -ldflags "-X main.Commit=`git rev-parse HEAD` -X main.Version=`git describe --tags $(git rev-list --tags --max-count=1)`" ./cmd/ssvnode/

.PHONY: start-node
start-node:
	@echo "Build ${BUILD_PATH}"
	@echo "Build ${CONFIG_PATH}"
	@echo "Build ${CONFIG_PATH2}"
	@echo "Command ${NODE_COMMAND}"
ifdef DEBUG_PORT
	@echo "Running node-${NODE_ID} in debug mode"
	@dlv  --continue --accept-multiclient --headless --listen=:${DEBUG_PORT} --api-version=2 exec \
	 ${BUILD_PATH} start-node -- ${NODE_COMMAND}
else
	@echo "Running node on address: ${HOST_ADDRESS})"
	@${BUILD_PATH} start-node ${NODE_COMMAND}
endif

.PHONY: docker
docker:
	@echo "node ${NODES_ID}"
	@docker rm -f ssv_node && docker build -t ssv_node . && docker run -d --env-file .env --restart unless-stopped --name=ssv_node -p 13000:13000 -p 12000:12000/udp -it ssv_node make BUILD_PATH=/go/bin/ssvnode  start-node && docker logs ssv_node --follow

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
	${BUILD_PATH} start-boot-node ${BOOTNODE_COMMAND}

.PHONY: mock
mock:
	make generate

.PHONY: generate
generate:
	go generate ./...

.PHONY: tools
tools:
	$(GET_TOOL) golang.org/x/tools/cmd/goimports
	$(GET_TOOL) go.uber.org/mock/mockgen
	$(GET_TOOL) github.com/ferranbt/fastssz/sszgen
	$(GET_TOOL) github.com/ethereum/go-ethereum/cmd/abigen
	$(GET_TOOL) github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	$(RUN_TOOL)

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
