# SSV Config Generator

**SSV Config Generator** is a command-line tool designed to generate configuration file for SSV Node. It streamlines the setup process by allowing users to specify various parameters through flags, which are then compiled into a YAML configuration file. This tool ensures that the SSV network setup is consistent, customizable, and easy to manage.

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Build](#build)
- [Usage](#usage)
  - [Flags](#flags)
- [Examples](#examples)
- [Configuration](#configuration)

## Build

```bash
go build -o ./bin/cfggen ./cmd/cfggen
```

This command compiles the CLI tool and generates an executable named `cfggen` in the current directory.

## Usage

The `cfggen` CLI allows you to generate a YAML configuration file by specifying various parameters through command-line flags.

### Syntax

```bash
cfggen [flags]
```

### Flags

| Flag                                | Type   | Default                           | Description                                                                 |
| -----------------------------------| ------ |-----------------------------------| --------------------------------------------------------------------------- |
| `--output-path`                     | string | `./config/config.local.yaml`      | Output path for the generated configuration file.                           |
| `--log-level`                       | string | `info`                            | Sets the logging level (e.g., `debug`, `info`, `warn`, `error`).             |
| `--db-path`                         | string | `./data/db`                       | Path to the database directory.                                             |
| `--discovery`                       | string | `mdns`                            | Discovery method.                                 |
| `--consensus-client`                | string | _Mandatory_                       | Address of the consensus client (e.g., `http://localhost:9000`).           |
| `--execution-client`                | string | _Mandatory_                       | Address of the execution client (e.g., `http://localhost:8545`).           |
| `--operator-private-key`            | string |                                   | Secret key for the operator.                                                |
| `--metrics-api-port`                | int    | `0`                               | Port number for the Metrics API (set to `0` to disable).                    |
| `--network-name`                    | string | `LocalTestnetSSV`                 | Name of the network.                                                        |
| `--network-genesis-domain`          | string | Derived from local testnet config | Hex-encoded genesis domain type (prefixed with `0x`).                       |
| `--network-alan-domain`             | string | Derived from local testnet config | Hex-encoded Alan domain type (prefixed with `0x`).                          |
| `--network-genesis-epoch`           | uint64 | Derived from local testnet config | Genesis epoch for the network.                                              |
| `--network-registry-sync-offset`    | uint64 | Derived from local testnet config | Registry sync offset for the network.                                       |
| `--network-registry-contract-addr`  | string | Derived from local testnet config | Ethereum address of the network registry contract (e.g., `0xYourAddress`).  |
| `--network-bootnodes`               | string | Derived from local testnet config | Comma-separated list of network bootnodes.                                  |
| `--network-discovery-protocol-id`   | string | Derived from local testnet config | Hex-encoded discovery protocol ID (prefixed with `0x`).                     |
| `--network-alan-fork-epoch`         | uint64 | Derived from local testnet config | Epoch at which the Alan fork occurs in the network.                         |

**Note:** The `--consensus-client` and `--execution-client` flags are mandatory and must be provided when running the CLI.

## Examples

### Basic Configuration

Generate a configuration file with default settings, specifying only the mandatory flags:

```bash
cfggen \
  --consensus-client "http://localhost:9000" \
  --execution-client "http://localhost:8545"
```

This command generates a `config.local.yaml` file in the `./config` directory with default settings for all other parameters.

### Custom Output Path and Log Level

Generate a configuration file with a custom output path and set the log level to `debug`:

```bash
cfggen \
  --consensus-client "http://consensus.example.com:9000" \
  --execution-client "http://execution.example.com:8545" \
  --output-path "/etc/ssv/config.yaml" \
  --log-level "debug"
```

### Specify Operator Private Key and Enable Metrics API

Generate a configuration with an operator's private key and enable the Metrics API on port `8080`:

```bash
cfggen \
  --consensus-client "http://consensus.example.com:9000" \
  --execution-client "http://execution.example.com:8545" \
  --operator-private-key "your-operator-private-key" \
  --metrics-api-port 8080
```

### Advanced Network Configuration

Customize network settings such as bootnodes, discovery protocol ID, and fork epoch:

```bash
cfggen \
  --consensus-client "http://consensus.example.com:9000" \
  --execution-client "http://execution.example.com:8545" \
  --network-name "CustomNet" \
  --network-genesis-domain "0xabcdef12" \
  --network-alan-domain "0x12345678" \
  --network-genesis-epoch 100 \
  --network-registry-sync-offset 50 \
  --network-registry-contract-addr "0xYourRegistryContractAddress" \
  --network-bootnodes "enode://bootnode1@127.0.0.1:30303,enode://bootnode2@127.0.0.1:30304" \
  --network-discovery-protocol-id "0x1234567890ab" \
  --network-alan-fork-epoch 200
```

This command sets up a custom network with specified domains, epochs, registry settings, and bootnodes.

## Configuration

The generated YAML configuration file (`config.local.yaml` by default) follows the structure defined by the `Config` struct. Below is an overview of each section and its corresponding fields:

### YAML Structure

```yaml
global:
  LogLevel: info
db:
  Path: ./data/db
eth2:
  BeaconNodeAddr: http://localhost:9000
eth1:
  ETH1Addr: http://localhost:8545
p2p:
  Discovery: mdns
ssv:
  Network: LocalTestnetSSV
  CustomNetwork:
    Name: LocalTestnetSSV
    GenesisDomainType: "0xabcdef12"
    AlanDomainType: "0x12345678"
    GenesisEpoch: 0
    RegistrySyncOffset: 0
    RegistryContractAddr: "0xYourRegistryContractAddress"
    Bootnodes:
      - "enode://bootnode1@127.0.0.1:30303"
      - "enode://bootnode2@127.0.0.1:30304"
    DiscoveryProtocolID: "0x1234567890ab"
    AlanForkEpoch: 200
OperatorPrivateKey: your-operator-private-key
MetricsAPIPort: 8080
```

### Sections

- **global**
    - `LogLevel`: Specifies the logging level (e.g., `debug`, `info`, `warn`, `error`).

- **db**
    - `Path`: Path to the database directory.

- **eth2**
    - `BeaconNodeAddr`: Address of the consensus client (Beacon Node).

- **eth1**
    - `ETH1Addr`: Address of the execution client (ETH1 Node).

- **p2p**
    - `Discovery`: Peer-to-peer discovery method (e.g., `mdns`).

- **ssv**
    - `Network`: Name of the network.
    - `CustomNetwork`: Contains custom network parameters.
        - `Name`: Name of the custom network.
        - `GenesisDomainType`: Hex-encoded genesis domain type (prefixed with `0x`).
        - `AlanDomainType`: Hex-encoded Alan domain type (prefixed with `0x`).
        - `GenesisEpoch`: Genesis epoch for the network.
        - `RegistrySyncOffset`: Registry sync offset for the network.
        - `RegistryContractAddr`: Ethereum address of the network registry contract.
        - `Bootnodes`: List of network bootnodes.
        - `DiscoveryProtocolID`: Hex-encoded discovery protocol ID (prefixed with `0x`).
        - `AlanForkEpoch`: Epoch at which the Alan fork occurs in the network.

- **OperatorPrivateKey**
    - `OperatorPrivateKey`: Secret key for the operator.

- **MetricsAPIPort**
    - `MetricsAPIPort`: Port number for the Metrics API.