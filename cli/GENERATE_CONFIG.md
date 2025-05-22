# SSV Config Generator

**SSV Config Generator** is a command-line tool designed to generate configuration file for SSV Node. It streamlines the setup process by allowing users to specify various parameters through flags, which are then compiled into a YAML configuration file. This tool ensures that the SSV network setup is consistent, customizable, and easy to manage.

## Table of Contents

- [Usage](#usage)
  - [Flags](#flags)
- [Examples](#examples)
- [Configuration](#configuration)

## Usage

The `generate-config` command allows you to generate a YAML configuration file by specifying various parameters through command-line flags.

### Syntax

```bash
  ssvnode generate-config [flags]
```

### Flags

| Flag                                 | Type   | Default                           | Description                                                                |
|--------------------------------------|--------|-----------------------------------|----------------------------------------------------------------------------|
| `--output-path`                      | string | `./config/config.local.yaml`      | Output path for the generated configuration file.                          |
| `--log-level`                        | string | `info`                            | Sets the logging level (e.g., `debug`, `info`, `warn`, `error`).           |
| `--db-path`                          | string | `./data/db`                       | Path to the database directory.                                            |
| `--discovery`                        | string | `mdns`                            | Discovery method.                                                          |
| `--consensus-client`                 | string | _Mandatory_                       | Address of the consensus client (e.g., `http://localhost:9000`).           |
| `--execution-client`                 | string | _Mandatory_                       | Address of the execution client (e.g., `http://localhost:8545`).           |
| `--operator-private-key`             | string |                                   | Secret key for the operator.                                               |
| `--metrics-api-port`                 | int    | `0`                               | Port number for the Metrics API (set to `0` to disable).                   |
| `--ssv-domain`                       | string | Derived from local testnet config | Hex-encoded domain type (prefixed with `0x`).                              |
| `--ssv-registry-sync-offset`         | uint64 | Derived from local testnet config | Registry sync offset for the network.                                      |
| `--ssv-registry-contract-addr`       | string | Derived from local testnet config | Ethereum address of the network registry contract (e.g., `0xYourAddress`). |
| `--ssv-bootnodes`                    | string | Derived from local testnet config | Comma-separated list of network bootnodes.                                 |
| `--ssv-discovery-protocol-id`        | string | Derived from local testnet config | Hex-encoded discovery protocol ID (prefixed with `0x`).                    |
| `--ssv-alan-fork-epoch`              | uint64 | Derived from local testnet config | Epoch at which the Alan fork occurs in the network.                        |
| `--ssv-max-validators-per-committee` | int    | Derived from local testnet config | Max validators per committee.                                              |


**Note:** The `--consensus-client` and `--execution-client` flags are mandatory and must be provided when running the CLI.

## Examples

### Basic Configuration

Generate a configuration file with default settings, specifying only the mandatory flags:

```bash
ssvnode generate-config \
  --consensus-client "http://localhost:9000" \
  --execution-client "http://localhost:8545"
```

This command generates a `config.local.yaml` file in the `./config` directory with default settings for all other parameters.

### Custom Output Path and Log Level

Generate a configuration file with a custom output path and set the log level to `debug`:

```bash
ssvnode generate-config \
  --consensus-client "http://consensus.example.com:9000" \
  --execution-client "http://execution.example.com:8545" \
  --output-path "/etc/ssv/config.yaml" \
  --log-level "debug"
```

### Specify Operator Private Key and Enable Metrics API

Generate a configuration with an operator's private key and enable the Metrics API on port `8080`:

```bash
ssvnode generate-config \
  --consensus-client "http://consensus.example.com:9000" \
  --execution-client "http://execution.example.com:8545" \
  --operator-private-key "your-operator-private-key" \
  --metrics-api-port 8080
```

### Advanced Network Configuration

Customize network settings such as bootnodes, discovery protocol ID, fork epoch, etc:

```bash
ssvnode generate-config \
  --consensus-client "http://consensus.example.com:9000" \
  --execution-client "http://execution.example.com:8545" \
  --ssv-domain "0x12345678" \
  --ssv-registry-sync-offset 50 \
  --ssv-registry-contract-addr "0xYourRegistryContractAddress" \
  --ssv-bootnodes "enode://bootnode1@127.0.0.1:30303,enode://bootnode2@127.0.0.1:30304" \
  --ssv-discovery-protocol-id "0x1234567890ab" \
  --ssv-max-validators-per-committee 560
```

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
    DomainType: "0x12345678"
    RegistrySyncOffset: 0
    RegistryContractAddr: "0xYourRegistryContractAddress"
    Bootnodes:
      - "enode://bootnode1@127.0.0.1:30303"
      - "enode://bootnode2@127.0.0.1:30304"
    DiscoveryProtocolID: "0x1234567890ab"
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
        - `DomainType`: Hex-encoded domain type (prefixed with `0x`).
        - `RegistrySyncOffset`: Registry sync offset for the network.
        - `RegistryContractAddr`: Ethereum address of the network registry contract.
        - `Bootnodes`: List of network bootnodes.
        - `DiscoveryProtocolID`: Hex-encoded discovery protocol ID (prefixed with `0x`).

- **OperatorPrivateKey**
    - `OperatorPrivateKey`: Secret key for the operator.

- **MetricsAPIPort**
    - `MetricsAPIPort`: Port number for the Metrics API.