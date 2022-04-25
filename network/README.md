# SSV - Networking

| Contributors                               | Status | Last Revision |
|:-------------------------------------------|:-------|:--------------|
| [@amir-blox](https://github.com/amir-blox) | DRAFT  | APR 22        |

This document contains the networking specification for `SSV.Network`.

## Overview

- [Links](#links)
- [Design](#design)
  - [Codebase Structure](#codebase-structure)
  - [Interfaces](#Interfaces)
  - [Connections](#connections)
  - [Streams](#streams)
  - [Pubsub](#pubsub-topics)
  - [Discovery](#discovery)
  - [Configurations](#configurations)
- [Testing](#Testing)

## Links

- **[Networking Spec](/docs/specs/NETWORKING.md)**


## Design

Network package holds components that are responsible for the networking aspects in the project,
such as subscribing to topics or reading, syncing and broadcasting messages.

### Codebase Structure

`network` package contains the following:
- interfaces and structs
- implementation packages:
    - `p2p` contains implementation of a p2p network based on libp2p
    - `local` contains implementation of a mocked network, for tests
- forks interface and implementation
- common utilities
- **TBD** websocket package

### Interfaces

Network interface is composed of several dedicated interfaces.
It is mostly based on `protcolp2p.Network` which is located in `./protocol/v1/p2p/network.go`:

```go 
package protocolp2p

type Network interface {
    // Subscriber manages topics subscription
	Subscriber
	// Broadcaster enables to broadcast messages
	Broadcaster
	// Syncer holds the interface for syncing data from other peerz
	Syncer
	// ValidationReporting is the interface for reporting on message validation results
	ValidationReporting
}
```

The interface exported by `./network` extend the protocol interface with `MessageRouting` 
and ops functions to manage the network layer:

```go
package network
// P2PNetwork is a facade interface that provides the entire functionality of the different network interfaces
type P2PNetwork interface {
	protocolp2p.Network
    // MessageRouting allows to register a MessageRouter that handles incoming connections
    MessageRouting
	// Setup initialize the network layer and starts the libp2p host
	Setup() error
	// Start starts the network
	Start() error
    io.Closer
}
```

The separation of interfaces allows to use a minimal api by other components, as expressed in the following diagram:

**TODO: components diagram**

### Connections

**TODO: complete**

### Streams

**TODO: complete**

### Pubsub Topics

**TODO: complete**

### Discovery

**TODO: complete**

### Configurations

**TODO: update**

| ENV                  | YAML                 | Default Value          | Required |  Description                           |
| ---                  | ---                  | ---                    | ---      | ---                                    |
| `NETWORK_PRIVATE_KEY`| `NetworkPrivateKey`  | -                      | No       | Key to use for libp2p/network identity |
| `ENR_KEY`            | `p2p.Enr`            | Bootnode ENR (Testnet) | No       | Bootnode ENR                           |
| `DISCOVERY_TYPE_KEY` | `p2p.DiscoveryType`  | `discv5`               | No       | discovery method                       |
| `TCP_PORT`           | `p2p.TcpPort`        | `13000`                | No       | TCP port to use                        |
| `UDP_PORT`           | `p2p.UdpPort`        | `12000`                | No       | UDP port to use                        |
| `HOST_ADDRESS`       | `p2p.HostAddress`    | -                      | No       | External IP address                    |
| `HOST_DNS`           | `p2p.HostDNS`        | -                      | No       | External DNS address                   |
| `NETWORK_TRACE`      | `p2p.NetworkTrace`   | false                  | No       | Flag to turn on/off network trace logs |
| `REQUEST_TIMEOUT`    | `p2p.RequestTimeout` | `5s`                   | No       | Requests timeout                       |
| `MAX_BATCH_RESPONSE` |`p2p.MaxBatchResponse`| `50`                   | No       | Max batch size                         |
| `PUBSUB_TRACE_OUT`   | `p2p.PubSubTraceOut` | -                      | No       | PubSub trace output file               |

An example config yaml:
```yaml
p2p:
  HostAddress: 82.210.33.146
  TcpPort: 13001
  UdpPort: 12001
```

---

## Testing

**TODO: complete**

