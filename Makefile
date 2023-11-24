ifndef $(GOPATH)
    GOPATH=$(shell go env GOPATH)
    export GOPATH
endif

ifndef $(HOST_ADDRESS)
    HOST_ADDRESS=$(shell dig @resolver4.opendns.com myip.opendns.com +short)
    export HOST_ADDRESS
endif

ifndef $(BUILD_PATH)
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
UNFORMATTED=$(shell gofmt -s -l .)

#Lint
.PHONY: lint-prepare
lint-prepare:
	@echo "Preparing Linter"
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s latest

.PHONY: lint
lint:
	./bin/golangci-lint run -v ./...
	@if [ ! -z "${UNFORMATTED}" ]; then \
		echo "Some files requires formatting, please run 'go fmt ./...'"; \
		exit 1; \
	fi

.PHONY: full-test
full-test:
	@echo "Running all tests"
	@go test -tags blst_enabled -timeout 20m ${COV_CMD} -p 1 -v ./...

.PHONY: integration-test
integration-test:
	@echo "Running integration tests"
	@go test -tags blst_enabled -count=1 -timeout 20m ${COV_CMD} -p 1 -v ./integration/...

.PHONY: unit-test
unit-test:
	@echo "Running unit tests"
	@go test -tags blst_enabled -timeout 20m ${COV_CMD} -race -p 1 -v `go list ./... | grep -ve "spectest\|integration\|ssv/scripts/"`

.PHONY: spec-test
spec-test:
	@echo "Running spec tests"
	@go test -tags blst_enabled -timeout 90m ${COV_CMD} -race -count=1 -p 1 -v `go list ./... | grep spectest`

.PHONY: spec-test-raceless
spec-test-raceless:
	@echo "Running spec tests without race flag"
	@go test -tags blst_enabled -timeout 20m -count=1 -p 1 -v `go list ./... | grep spectest`

#Test
.PHONY: docker-spec-test
docker-spec-test:
	@echo "Running spec tests in docker"
	@docker build -t ssv_tests -f tests.Dockerfile .
	@docker run --rm ssv_tests make spec-test

#Test
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

#Build
.PHONY: build
build:
	CGO_ENABLED=1 go build -o ./bin/ssvnode -ldflags "-X main.Commit=`git rev-parse HEAD` -X main.Branch=`git symbolic-ref --short HEAD` -X main.Version=`git describe --tags $(git rev-list --tags --max-count=1)`" ./cmd/ssvnode/

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
	@sudo docker rm -f ssv_node && docker run -d --env-file .env --restart unless-stopped --name=ssv_node -p 13000:13000 -p 12000:12000/udp 'bloxstaking/ssv-node:latest' make BUILD_PATH=/go/bin/ssvnode start-node

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

MONITOR_NODES=prometheus grafana
.PHONY: docker-monitor
docker-monitor:
	@echo $(MONITOR_NODES)
	@docker-compose up --build $(MONITOR_NODES)

.PHONY: mock
mock:
	go generate ./...

.PHONY: mockgen-install
mockgen-install:
	go install github.com/golang/mock/mockgen@v1.6.0
	@which mockgen || echo "Error: ensure `go env GOPATH` is added to PATH"

.PHONY: format
format:
# both format commands must ignore generated files which are named *mock* or enr_fork_id_encoding.go
# the argument to gopls format can be a list of files
	find . -name "*.go" ! -path '*mock*' ! -name 'enr_fork_id_encoding.go' -type f -print0 | xargs -0 -P 1 sh -c 'gopls -v format -w $0'
# the argument to gopls imports must be a single file so this entire command takes a few mintues to run
	find . -name "*.go" ! -path '*mock*' ! -name 'enr_fork_id_encoding.go' -type f -print0 | xargs -0 -P 10 -I{} sh -c 'gopls -v imports -w "{}"'
