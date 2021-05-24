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

#ifndef $(CONFIG_PATH)
#    CONFIG_PATH="./config/config.yaml"
#    export CONFIG_PATH
#endif

#ifneq (,$(wildcard ./.env))
#    include .env
#endif

# node command builder
NODE_COMMAND=--config=${CONFIG_PATH}


ifneq ($(SHARE_CONFIG),)
  NODE_COMMAND+= --share-config=${SHARE_CONFIG}
endif

#Lint
.PHONY: lint-prepare
lint-prepare:
	@echo "Preparing Linter"
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s latest

.PHONY: lint
lint:
	./bin/golangci-lint run -v ./...

#Test

#Test
.PHONY: full-test
full-test:
	@echo "Running the full test..."
	@go test -tags blst_enabled -timeout 20m -cover -race -p 1 -v ./...

#Test Coverage
.PHONY: test-cov
full-test:
	@echo "Running the full test..."
	@go test -tags blst_enabled -timeout 20m -coverpkg=${$(go list ./... | grep -v mocks | tr '\n' ',')} -coverprofile=coverage.out -covermode=atomic -race -p 1 -v ./...



# TODO: Intgrate use of short flag (unit tests) + running tests through docker
#.PHONY: unittest
#unittest:
#	@go test -v -short -race ./...

#.PHONY: full-test-local
#full-test-local:
#	@docker-compose -f test.docker-compose.yaml up -d mysql_test
#	@make full-test
#	# @docker-compose -f test.docker-compose.yaml down --volumes
#
#
#.PHONY: docker-test
#docker-test:
#	@docker-compose -f test.docker-compose.yaml up -d mysql_test
#	@docker-compose -f test.docker-compose.yaml up --build --abort-on-container-exit
#	@docker-compose -f test.docker-compose.yaml down --volumes


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
	@docker rm -f ssv_node && docker build -t ssv_node . && docker run -d --env-file .env --restart unless-stopped --name=ssv_node -p 13000:13000 -p 12000:12000 -it ssv_node make BUILD_PATH=/go/bin/ssvnode  start-node && docker logs ssv_node --follow


.PHONY: docker-image
docker-image:
	@echo "node ${NODES_ID}"
	@sudo docker rm -f ssv_node && docker run -d --env-file .env --restart unless-stopped --name=ssv_node -p 13000:13000 -p 12000:12000 'bloxstaking/ssv-node:latest' make BUILD_PATH=/go/bin/ssvnode start-node


NODES=ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
.PHONY: docker-all
docker-all:
	@echo "nodes $(NODES)"
	@docker-compose up --build $(NODES)

DEBUG_NODES=ssv-node-1-dev ssv-node-2-dev ssv-node-3-dev ssv-node-4-dev
.PHONY: docker-debug
docker-debug:
	@echo $(DEBUG_NODES)
	@docker-compose up --build $(DEBUG_NODES)

.PHONY: stop
stop:
	@docker-compose  down


.PHONY: start-boot-node
start-boot-node:
	@echo "Running start-boot-node"
	${BUILD_PATH} start-boot-node --private-key=${BOOT_NODE_PRIVATE_KEY} --external-ip=${BOOT_NODE_EXTERNAL_IP}
