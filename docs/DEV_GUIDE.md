[<img src="./resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Development Guide

- [SSV - Development Guide](#ssv---development-guide)
  - [Usage](#usage)
    - [Common Commands](#common-commands)
      - [Build](#build)
      - [Test](#test)
      - [Lint](#lint)
      - [Specify Version](#specify-version)
      - [Splitting a Validator Key](#splitting-a-validator-key)
      - [Generating an Operator Key](#generating-an-operator-key)
    - [Config Files](#config-files)
      - [Node Config](#node-config)
  - [Running a Local Network of Operators](#running-a-local-network-of-operators)
    - [Install](#install)
      - [Prerequisites](#prerequisites)
      - [Clone Repository](#clone-repository)
      - [Build Binary](#build-binary)
    - [Configuration](#configuration)
      - [Use script](#use-script)
      - [Use manual steps](#use-manual-steps)
    - [Run](#run)
      - [Local network with 4 nodes with Docker Compose](#local-network-with-4-nodes-with-docker-compose)
      - [Local network with 4 nodes for debugging with Docker Compose](#local-network-with-4-nodes-for-debugging-with-docker-compose)
      - [Prometheus and Grafana for local network](#prometheus-and-grafana-for-local-network)
  - [Coding Standards](#coding-standards)

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
* go (1.20)
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

#### Use script

By using this script, developers can simulate a real SSV environment, run multiple nodes, and start those nodes performing duties with the passed validator's keystore. This is incredibly beneficial for debugging, testing functionalities, or preparing for deployment in a live setting. It provides a realistic but controlled environment where developers can observe the interaction of multiple nodes in the SSV network. \
The script simplifies configuration by automatically generating YAML files for each operator and an 'events.yaml' file. The 'events.yaml' emulates a 'happy flow' scenario, which includes the registration of four operators and one validator

1. Download the latest executable version (v1.0.0 or later) from [ssv-keys](https://github.com/bloxapp/ssv-keys/releases).
    - After downloading, follow these [steps](https://github.com/bloxapp/ssv-keys#option-1-running-an-executable-recommended-route) to provide the necessary permissions to the executable.

2. Generate a local configuration using the provided [script](../scripts/generate_local_config.sh).
    - Execute the script by typing the following command in your terminal:
      ```shell
        ./generate_local_config.sh $OP_SIZE $KS_PATH $KS_PASSWORD $OA $NONCE $SSV_KEYS_PATH
      ```
    - Please replace each variable with the following details:
        - `OP_SIZE`: Number of operators to create [3f+1]. (e.g., 4 or 7 or 10, etc.)
        - `KS_PATH`: Path to your keystore.json file (e.g., ./keystore-m_12381_3600_0_0_0-1639058279.json).
        - `KS_PASSWORD`: Your keystore password (e.g., 12345678).
        - `OA`: Owner address (e.g., 0x1234567890123456789012345678901234567890).
        - `NONCE`: Nonce (e.g., 0).
        - `SSV_KEYS_PATH`: Path to ssv-keys executable [optional]. The default path is ./bin/ssv-keys-mac.

3. Move the generated .yaml files to the `./config` [directory](../config).

4. Update the [config.yaml](../config/config.yaml) file to include the local events path:
    ```yaml
    LocalEventsPath: ./config/events.yaml
    ```

5. Add "mdns" under the p2p configuration in the [config.yaml](../config/config.yaml) file:
    ```yaml
    p2p:
      Discovery: mdns
    ```

6. To enable debugging for all components, add the debug services line in the [config.yaml](../config/config.yaml) file:
    ```yaml 
    global:
      DebugServices: ssv/.*
    ```   

7. Finally, build and run 4 local nodes with this command:
    ```shell
    docker-compose up --build ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
    ``` 

#### Use manual steps

These steps offer a detailed manual alternative to the script. They provide a step-by-step guide for setting up a local SSV environment, generating the necessary keys, creating and configuring YAML files, and building and running multiple SSV nodes. They are beneficial for those who prefer more control over the setup process or need to understand it in greater detail.

1. Generate 4 operator keys. You can refer to the [Generating an Operator Key](#generating-an-operator-key) section for guidance.

2. Create 4 .yaml files (`share1.yaml`, `share2.yaml`, `share3.yaml`, `share4.yaml`) with the corresponding configurations, based on the provided [template file](../config/example_share.yaml).
    - Save these files in the `./config` directory.

3. Populate the `OperatorPrivateKey` field in the `share[1..4].yaml` files with the operator private keys that you generated in step 1.

4. Generate share keys using the 4 operator public keys generated in step 1. You can do this using [ssv-keys](https://github.com/bloxapp/ssv-keys#example).

5. Create an `events.yaml` file based on the provided [template file](../config/events.example.yaml). Follow the validator registration happy flow example for proper configuration.
    - Populate the operator registration events with the data generated in step 4.
    - Populate the validator registration event with the data generated in step 4.

6. Place the `events.yaml` file in the `./config` directory (`./config/events.yaml`).

7. Add the local events path to the [config.yaml](../config/config.yaml) file:
    ```yaml
    LocalEventsPath: ./config/events.yaml
    ```

8. If you want to debug all components, add debug services to the [config.yaml](../config/config.yaml) file:
    ```yaml 
    global:
      DebugServices: ssv/.*
    ```

9. Add the discovery "mdns" under the p2p section in the [config.yaml](../config/config.yaml) file:
    ```yaml
    p2p:
      Discovery: mdns
    ```

10. Finally, build and run 4 local nodes with the following command:
    ```bash
    docker-compose up --build ssv-node-1 ssv-node-2 ssv-node-3 ssv-node-4
    ```

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
