# SSV - Networking

This document contains the networking layer design.

## Overview

- [Links](#links)
- [Design](#design)
  - [Codebase Structure](#codebase-structure)
  - [Interfaces](#Interfaces)
  - [Connections](#connections)
  - [Streams](#streams)
  - [Pubsub Topics](#pubsub-topics)
  - [Discovery](#discovery)
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

**TODO: add diagram**

### Connections

**TODO: complete**

### Streams

**TODO: complete**

### Pubsub Topics

**TODO: complete**

### Discovery

**TODO: complete**

---

## Testing

**TODO: complete**

