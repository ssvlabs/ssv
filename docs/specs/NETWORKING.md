# SSV Specifications - Networking

**Status: WIP**

This document contains the networking specification for SSV.

## Overview

- [x] [Fundamentals](#fundamentals)
  - [x] [Stack](#stack)
  - [x] [Transport](#transport)
  - [x] [Messaging](#messaging)
  - [x] [Network Peers](#network-peers)
  - [x] [Identity](#identity)
  - [x] [Network Discovery](#network-discovery)
- [ ] [Protocols](#protocols)
  - [x] [Consensus](#consensus-protocol)
  - [ ] [Sync](#sync-protocol)
  - [ ] [Authentication](#authentication-protocol)
- [x] [Networking](#networking)
  - [x] [PubSub](#pubsub)
  - [x] [User Agent](#user-agnet)
  - [x] [Discovery](#discovery)
  - [x] [Netowrk ID](#network-id)
  - [x] [Subnets](#subnets)
  - [x] [Peers Connectivity](#peers-connectivity)
  - [x] [Forks](#forks)


## Fundamentals

### Stack

SSV is a decentralized P2P network, built with [Libp2p](https://libp2p.io/), a modular framework for P2P networking.

### Transport

Network peers should support the following transports:
- `TCP` is used by libp2p for setting up communication channels between peers
- `UDP` is used for discovery purposes.

[go-libp2p-noise](https://github.com/libp2p/go-libp2p-noise) is used to secure transport channels (based on [noise protocol](https://noiseprotocol.org/noise.html)).

Multiplexing of protocols over channels is achieved using [yamux](https://github.com/libp2p/go-libp2p-yamux) protocol.

### Messaging

Messages in the network are formatted with `protobuf` (NOTE: `v0` messages are encoded/decoded with JSON),
and being transported p2p with one of the following methods:

**Streams** 

Streams are used for direct messages between peers.

Libp2p allows to create a bidirectional stream between two peers and implement the corresponding wire messaging protocol. \
See more information in [IPFS specs > communication-model - streams](https://ipfs.io/ipfs/QmVqNrDfr2dxzQUo4VN3zhG4NV78uYFmRpgSktWDc2eeh2/specs/7-properties/#71-communication-model---streams).

**PubSub**

GossipSub ([v1.1](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md)) is the pubsub protocol used in SSV.

The main purpose is for broadcasting messages to a group (AKA subnet) of nodes. \
In addition, the machinary helps to determine liveliness and maintain peers scoring.


### Network Peers

There are several types of nodes in the network:

`Operator` node is responsible for signing validators duties. \
It holds relevant registry data and the validators consensus data.

`Bootnode` is a public peer which is responsible for helping new peers to find other peers in the network.
It has a static (and stable) ENR so other peers could join the network easily.

`Exporter` node role is to export information from the network. \
It collects registry data (validators / operators) and consensus data (decided messages chains) of all validators in the network.


### Identity

Identity in the network is based on two types of keys:

`Network Key` is used to create peer ID by all network peers. \
All messages from a peer are signed using this key and verified by other peers with the corresponding public key. \
Unless provided, the key will be generated and saved locally for future use.

`Operator Key` is used for decryption of shares keys that are used for signing consensus messages and duties. \
Exporter and Bootnode does not hold this key.


### Network Discovery

[discv5](https://github.com/ethereum/devp2p/blob/master/discv5/discv5.md) is used in `SSV.network` to complement discovery capalities that don't come with libp2p.

More information is available in [Networking / Discovery](#discovery)

------


## Protocols

Network interaction includes several types of protocols, as detailed below.

## Consensus Protocol

`IBFT`/`QBFT` consensus protocol is used to govern `SSV` network.
`IBFT` ensures that consensus can be reached by a committee of `n` operator nodes while tolerating a certain amount of `f` faulty nodes as defined by `n ≥ 3f + 1`.

As part of the algorithm, nodes are exchanging messages with other nodes in the committee. \
Once the committee reaches consensus, the nodes will publish the decided message across the network.

More information regarding the protocol can be found in [iBFT annotated paper (By Blox)](/ibft/IBFT.md)

#### Message Structure

`SignedMessage` is a wrapper for IBFT messages, it holds a message and its signature with a list of signer IDs:

```protobuf
syntax = "proto3";
import "gogo.proto";

// SignedMessage is a wrapper on top of Message for supporting signatures
message SignedMessage{
  // message is the raw message to sign
  Message message            = 1 [(gogoproto.nullable) = false];
  // signature is a signature of the message
  bytes signature            = 2 [(gogoproto.nullable) = false];
  // signer_ids are the IDs of the signing operators
  repeated uint64 signer_ids = 3;
}

// Message represents an IBFT message
message Message {
  // type is the IBFT state / stage
  Stage type        = 1;
  // round is the current round where the message was sent
  uint64 round      = 2;
  // lambda is the message identifier
  bytes lambda      = 3;
  // sequence number is an incremental number for each instance, much like a block number would be in a blockchain
  uint64 seq_number = 4;
  // value holds the message data in bytes
  bytes value       = 5;
}
```

`SignedMessage` JSON example:
```json
{
  "message": {
    "type": 3,
    "round": 1,
    "lambda": "OTFiZGZjOWQxYzU4NzZkYTEwY...",
    "seq_number": 28276,
    "value": "mB0aAAAAAAA4AAAAAAAAADpTC1djq..."
  },
  "signature": "jrB0+Z9zyzzVaUpDMTlCt6Om9mj...",
  "signer_ids": [4, 2, 3]
}
```

**NOTE** all pubsub messages in the network are wrapped with libp2p's message structure

## Sync Protocol

History sync is the procedure of syncing decided messages from other peers. \
It is a prerequisite for taking part in some validator's consensus.

Sync is done over streams as pubsub is not suitable for this case due to several reasons such as:
- API nature is request/response, unlike broadcasting in consensus messages
- Bandwidth - only one peer (usually) needs the data, it would be a waste to send redundant messages across the network.
#### Protocols

**TODO: add example request/response**

SSV nodes use the following stream protocols:


##### 1. Highest Decided

This protocol is used by a node to find out what is the highest decided message among a specific committee.
In case there are no decided messages, it will return an empty array of messages.

`/ssv/sync/highest_decided/0.0.1`


##### 2. Decided By Range

This protocol enables to sync decided messages in some specific range.

`/ssv/sync/decided_by_range/0.0.1`


##### 3. Last Change Round

This protocol enables a node that was online to catch up with change round messages.

`/ssv/sync/last_change_round/0.0.1`

#### Message Structure

TODO: refine structure

`SyncMessage` structure is used by all sync protocols, the type of message is specified in a dedicated field:

```protobuf
message SyncMessage {
  // MsgType is the type of sync message
  SyncMsgType MsgType                   = 1;
  // FromPeerID is the ID of the sender
  string FromPeerID                     = 2;
  // Identifier of the message (validator + role)
  bytes Identifier                      = 3;
  // Params holds the requests parameters
  repeated uint64 Params                = 4;
  // Messages holds the results (decided messages) of some request
  repeated proto.SignedMessage Messages = 5;
  // Error holds an error response if exist
  string Error                          = 6;
}

// SyncMsgType is an enum that represents the type of sync message 
enum SyncMsgType {
  // GetHighestType is a request from peers to return the highest decided/ prepared instance they know of
  GetHighestType       = 0;
  // GetInstanceRange is a request from peers to return instances and their decided/ prepared justifications
  GetInstanceRange     = 1;
  // GetCurrentInstance is a request from peers to return their current running instance details
  GetLatestChangeRound = 2;
}
```

## Authentication protocol

Authentication protocol enables ssv nodes to prove ownership of its operator key.

`/ssv/auth/0.0.1`

```protobuf
syntax = "proto3";
import "gogo.proto";

// AuthMessage is a message that is used for authenticating nodes
message AuthMessage {
  // message is the raw message to sign
  AuthPayload payload = 1 [(gogoproto.nullable) = false];
  // signature is a signature of the message
  bytes signature     = 2 [(gogoproto.nullable) = false];
}

// AuthPayload is the payload used for auth messages
message AuthPayload {
  // peer_id of the authenticating node
  bytes peer_id     = 1 [(gogoproto.nullable) = false];
  // operator_id of the authenticating node
  bytes operator_id = 2 [(gogoproto.nullable) = true];
  // node_type is the type of the authenticating node
  uint64 node_type  = 3;
}
```

**TODO: heartbeat?**


---


## Networking

### Pubsub

The main purpose is for broadcasting messages to a group (AKA subnet) of nodes. \
In addition, the following are achieved as well:

- subscriptions metadata helps to get liveliness information of nodes
- pubsub scoring enables to prune bad/malicious peers based on network behavior and application-specific rules

The following parameters are used for initializing pubsub:

- `floodPublish` was turned on for better reliability, as peer's own messages will be propagated to a larger set of peers 
  (see [Flood Publishing](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md#flood-publishing)) 
- `peerOutboundQueueSize` / `validationQueueSize` were increased to `600`, to avoid dropped messages on bad connectivity or slow processing
- `directPeers` includes the exporter peer ID, to ensure it gets all messages
- (fork `v1`) `msgID` is a custom function that calculates a `msg-id` based on the message content hash. the default function uses the `sender` and `msg_seq` and therefore causes redundancy in message processing (nodes might process the same message because it came from different senders).
- (fork `v1`) `signPolicy` was set to `StrictNoSign` (required for custom `msg-id`) to avoid producing and verifying message signatures in the pubsub router, which is redunant as messages are being signed and verified using the operator key
  - `signID` was set to empty (no author)

**TODO: add peer scoring**


### User Agent

Libp2p provides user agent mechanism with the [identify](https://github.com/libp2p/specs/tree/master/identify) protocol, 
which is used to exchange basic information with other peers in the network.

User Agent contains the node version and type, and in addition the operator id which might be reduced in future versions. \
See detailed format in [Forks / user agent](#fork-v0)


### Discovery

[discv5](https://github.com/ethereum/devp2p/blob/master/discv5/discv5.md) is used in `SSV.network` to complement discovery.

DiscV5 works on top of UDP, it uses a DHT to store node records (`ENR`) of discovered peers.
The discovery process walks randomaly on the nodes in the table that are not connected, filters them according to `ENR` entries and connects to relevant ones, as detailed in [Peers Connectivity](#peers-connectivity).

Bootnode is a special kind of peers that have a public static ENR to enable new peers to join the network. \. 
It doesn't start a libp2p host for TCP communication, its role ends once a new peer finds existing peers in the network.

#### ENR

[Ethereum Node Records](https://github.com/ethereum/devp2p/blob/master/enr.md) is a format that holds peer information.
Records contain a signature, sequence (for republishing record) and arbitrary key/value pairs.

`ENR` structure in ssv network contains the following key/value pairs:

| Key         | Value                                                          | Status          |
|:------------|:---------------------------------------------------------------|:----------------|
| `id`        | name of identity scheme, e.g. "v4"                             | Done            |
| `secp256k1` | compressed secp256k1 public key, 33 bytes                      | Done            |
| `ip`        | IPv4 address, 4 bytes                                          | Done            |
| `tcp`       | TCP port, big endian integer                                   | Done            |
| `udp`       | UDP port, big endian integer                                   | Done            |
| `type`      | node type, integer -> 1 (operator), 2 (exporter), 3 (bootnode) | Done (`v0.1.9`) |
| `oid`       | operator id, 32 bytes                                          | Done (`v0.1.9`) |
| `version`   | fork version, integer                                          | -               |


### Network ID

Network ID is a `32byte` key, that is used to distinguish between other networks (ssv and others).
Peers from other public/private libp2p networks (with different network ID) won't be able to read or write messages in the network, 
meaning that the key to be known and used by all network members.

It is done with [libp2p's private network](https://github.com/libp2p/specs/blob/master/pnet/Private-Networks-PSK-V1.md),
which encrypts/decrypts all traffic with the corresponding key,
regardless of the regular transport security protocol ([go-libp2p-noise](https://github.com/libp2p/go-libp2p-noise)).

**NOTE** discovery communication (UDP) won't use the network ID, as unknown nodes will be filtered anyway due to missing fields in their `ENR` entry as specified below.



### Subnets

Consensus messages are being sent in the network over a subnet (pubsub topic), which the relevant peers should be subscribed to.

In addition to subnets, there is a global topic (AKA `main topic`) to publish all the decided messages in the network.

There are several options for how to setup topics in the network, see below.

#### Subnets - fork v0

Each validator committee has a dedicated pubsub topic with all the relevant peers subscribed to it (committee + exporter).

It helps to reduce amount of messages in the network, but increases the number of topics which will grow up to the number of validators.

#### Subnet - fork v1

A subnet of several validators contains multiple committees,
reusing the topic to communicate on behalf of multiple validators.

The number of topics will be reduced but the number of messages sent over the network should grow.

As messages will be propagated to a larget set of nodes, we can expect better reliability (arrival of messages to all operators in the committee).

In addition, a larger group of operators provides:
- redundancy of decided messages across multiple nodes
- better security for subnets as more nodes will validate messages and can score bad/malicious nodes that will be pruned accordingly.

**TBD: main topic**

**Validators Mapping**

Validator's public key is mapped to a subnet using a hash function, which helps to distribute validators across subnets in a balanced way:

`hash(validatiorPubKey) % num_of_subnets`

The number of subnets is fixed (TBD 32 / 64 / 128).

**TBD** A dynamic number of subnets (e.g. `log(num_of_peers)`) which requires a different approach.


### Peers Connectivity

In a fully-connected network, where each peer is connected to all other peers in the network,
running nodes will consume many resources to process all network related tasks e.g. parsing, peers management etc.

To lower resource consumption, there is a limitation for the number of connected peers, currently set to `250`. \
Once reached to peer limit, the node will connect only to relevant nodes with score above treshold, which is currently set to zero.

**TBD** Scores are based on the following properties:

- Shared subnets / committees
- Static nodes (`exporter` or `bootnode`)

### Forks

Future network forks will follow the general forks mechanism and design in SSV. \
The idea is to wrap procedures that have potential to be changed in future versions.

Currently, the following are covered:

- validator topic mapping
- message encoding/decoding
- user agent

#### Fork v0

**validator topic mapping**

Validator public key is used as the topic name:

`bloxstaking.ssv.<hex(validator-public-key)>`

**message encoding/decoding**

JSON is used for encoding/decoding of messages.

**user agent**

User Agent contains the node version and type, and in addition the operator id.

`SSV-Node:v0.x.x:<node-type>:<?operator-id>`

#### Fork v1 (TBD)

**validator topic mapping**

Validator public key hash is used to determine the validator's subnet which is the topic name:

`bloxstaking.ssv.<hash(validatiorPubKey) % num_of_subnets>`


### NAT port map

libp2p enables to configure a `NATManager` that will attempt to open a port in the network's firewall using `UPnP`.


-----


## Open points

### High Availability

HA for an ssv node is not trivial - as it participats in a decentralized p2p network, running multiple instances can cause conflicts in signatures.

Ideas:

#### Hub Node

`Hub Node` is a node that handles the network layer of ssv node, 
it could be connected to multiple `Worker` nodes and stream the entire network layer messages to/from them.

A possible implmentation could be an SSV node with a proxied network layer that uses websocket to communicate with worker nodes.

#### Subnet Partitions

Subnet partitions referrs breaking relevant subnets into an indenpendant subsets of subnets, 
creating a seperation for its tasks and therefore enables to run multiple instances of the same operator, working on different subnets.

That will help decrease the damage in case some node fails, as only a portion of the assigned validators will be affected, while the other instances keeps doing tasks in their subnets.
