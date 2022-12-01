[<img src="./resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Development Guide

* [Usage](#usage)
  + [Common Commands](#common-commands)
    - [Build](#build)
    - [Test](#test)
    - [Lint](#lint)
    - [Specify Version](#specify-version)
    - [Splitting a Validator Key](#splitting-a-validator-key)
    - [Generating an Operator Key](#generating-an-operator-key)
  + [Config Files](#config-files)
    - [Node Config](#node-config)
* [Running a Local Network of Operators](#running-a-local-network-of-operators)
  + [Install](#install)
    - [Prerequisites](#prerequisites)
    - [Clone Repository](#clone-repository)
    - [Build Binary](#build-binary)
  + [Configuration](#configuration)
  + [Run](#run)
    - [Local network with 4 nodes with Docker Compose](#local-network-with-4-nodes-with-docker-compose)
    - [Local network with 4 nodes for debugging with Docker Compose](#local-network-with-4-nodes-for-debugging-with-docker-compose)
    - [Prometheus and Grafana for local network](#prometheus-and-grafana-for-local-network)
* [Coding Standards](#coding-standards)

## Usage

### Common Commands

#### Build
```bash
$ make build
```

#### Test
```bash
$ make full-test
```

#### Lint
```bash
$ make lint-prepare
$ make lint
```

#### Specify Version

```bash
$ ./bin/ssvnode version
```

#### Splitting a Validator Key

We split an eth2 BLS validator key into shares via Shamir-Secret-Sharing(SSS) to be used between the SSV nodes.

```bash
# Extract Private keys from mnemonic (optional, skip if you have the public/private keys ) 
$ ./bin/ssvnode export-keys --mnemonic="<mnemonic>" --index={keyIndex}

# Generate threshold keys
$ ./bin/ssvnode create-threshold --count <number of ssv nodes> --private-key <privateKey>
```

#### Generating an Operator Key

```bash
$ ./bin/ssvnode generate-operator-keys
```

### Config Files

Config files are located in `./config` directory:

#### Node Config 

Specifies general configuration regards the current node. \
Example yaml - [config.yaml](../config/config.yaml)

## Running a Local Network of Operators

This section details the steps to run a local network of operator nodes.

### Install

#### Prerequisites

In order to run a local environment, install the following:
* git
* go (1.17)
* docker
* make
* yq

#### Clone Repository

```shell
$ git clone https://github.com/bloxapp/ssv.git
```

#### Build Binary

```shell
$ make build
```

### Configuration

#### Use script:

1. Download the latest executable from [ssv-keys](https://github.com/bloxapp/ssv-keys/releases)
   1. Adjust permissions for ssv-keys executable ```chmod +x ssv-keys-mac```
   2. Locate the executable in the same folder you are running the script
2. Generate local config using [script](../scripts/generate_local_config.sh) \
   1. Adjust permissions for the script ```chmod +x generate_local_config.sh```
   2. Execute ```./generate_local_config.sh $OP_SIZE $KS_PATH $KS_PASSWORD $SSV_KYES_PATH``` \
      `OP_SIZE` - number of operators to create [3f+1]. (e.g. 4 or 7 or 10 ...) \
      `KS_PATH` - path to keystore.json (e.g. ./keystore-m_12381_3600_0_0_0-1639058279.json)\
      `KS_PASSWORD` - keystore password (e.g. 12345678)
      `SSV_KEYS_PATH` - path to ssv-keys executable (default. ./bin/ssv-keys-mac)
3. Place the generated yaml files to `./config` [directory](../config)
4. Add the local events path to [config.yaml](../config/config.yaml) file `LocalEventsPath: ./config/events.yaml`
5. Override the Bootnodes default value to empty string in [network config](../network/p2p/config.go) in order to use MDNS network. \
   Validate you are not passing Bootnodes param in [config.yaml](../config/config.yaml)
6. Build and run 4 local nodes ```docker-compose up --build ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4```

#### Use manual steps:

1. Generate 4 operator keys - [Generating an Operator Key](#generating-an-operator-key)
2. Create 4 .yaml files with the corresponding configuration, based on the [template file](../config/example_share.yaml). \
   The files should be placed in the `./config` directory (`./config/share1.yaml`, `./config/share2.yaml`, etc.)
3. Populate the `OperatorPrivateKey` in the created share[1..4].yaml with operator private keys generated in section 1 
4. Generate share keys using 4 operator public keys generated in section 1 using [ssv-keys](https://github.com/bloxapp/ssv-keys#option-1-running-an-executable-recommended-route)
5. Create `events.yaml` file with the corresponding configuration [use validator registration happy flow example], based on the [template file](../config/example_events.yaml)
   1. fill the operator registration events with the data generated in section 4
   2. fill the validator registration event with the data generated in section 4
6. Place the `events.yaml` file in the `./config` directory (`./config/events.yaml`)
7. Add the local events path to [config.yaml](../config/config.yaml) file `LocalEventsPath: ./config/events.yaml`
8. Override the Bootnodes default value to empty string in [network config](../network/p2p/config.go) in order to use MDNS network. \
   Validate you are not passing Bootnodes param in [config.yaml](../config/config.yaml)
9. Build and run 4 local nodes ```docker-compose up --build ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4```

### Run

Run a local network using `docker`

#### Local network with 4 nodes with Docker Compose

```shell
$ make docker-all 
```

#### Local network with 4 nodes for debugging with Docker Compose

```shell
$ make docker-debug 
```

#### Prometheus and Grafana for local network

In order to spin up local prometheus and grafana use:
```shell
$ make docker-monitor
```

For a grafana dashboard, use the [SSV Operator dashboard](../monitoring/grafana/dashboard_ssv_operator.json) as explained in [monitoring/README.md#grafana](../monitoring/README.md#grafana) 

## Coding Standards

Please make sure your contributions adhere to our coding guidelines:

* Code must adhere to the official Go [formatting](https://golang.org/doc/effective_go.html#formatting)
  guidelines (i.e. uses [gofmt](https://golang.org/cmd/gofmt/)).
* Code must be documented adhering to the official Go [commentary](https://golang.org/doc/effective_go.html#commentary)
  guidelines.
* Pull requests need to be based on and opened against the `stage` branch, and its commits should be squashed on merge.
