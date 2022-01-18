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
- [ ] [Protocols](#protocols)
  - [x] [1. Consensus](#1-consensus)
  - [ ] [2. Sync](#2-sync)
- [ ] [Networking](#networking)
  - [x] [Netowrk ID](#network-id)
  - [ ] [Discovery](#discovery)
  - [ ] [Authentication](#authentication)
  - [ ] [Subnets](#subnets)
  - [ ] [Peers Connectivity](#peers-connectivity)
  - [x] [Forks](#forks)
  - [ ] [Configuration](#configuration)

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

Messages in the network are formatted with `protobuf`,
and being transported p2p with one of the following methods:

**Streams** 

Streams are used for direct messages between peers.

Libp2p allows to create a bidirectional stream between two peers and implement the corresponding wire messaging protocol. \
See more information in [IPFS specs > communication-model - streams](https://ipfs.io/ipfs/QmVqNrDfr2dxzQUo4VN3zhG4NV78uYFmRpgSktWDc2eeh2/specs/7-properties/#71-communication-model---streams).


**PubSub**

PubSub is used as an infrastructure for broadcasting messages among a group (AKA subnet) of operator nodes.

GossipSub ([v1.1](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md)) is the pubsub protocol used in SSV. \
In short, each node saves metadata regards topic subscriptions of other peers in the network. \
Based on that information, messages are propagated only to the most relevant peers (subscribed or neighbors of a subscribed peer) and therefore reduce the overall traffic.



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


------


## Protocols

Network interaction includes several types of protocols, as detailed below.

### 1. Consensus

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
  Message message = 1 [(gogoproto.nullable) = false];
  // signature is a signature of the message
  bytes signature = 2 [(gogoproto.nullable) = false];
  // signer_ids are the IDs of the signing operators
  repeated uint64 signer_ids = 3;
}

// Message represents an IBFT message
message Message {
  // type is the IBFT state / stage
  Stage type   = 1;
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

---

### 2. Sync

History sync is the procedure of syncing decided messages from other peers. \
It is a prerequisite for taking part in some validator's consensus.

Sync is done over streams as pubsub is not suitable for this case due to several reasons such as:
- API nature is request/response, unlike broadcasting in consensus messages
- Bandwidth - only one peer (usually) needs the data, it would be a waste to send redundant messages across the network.

#### Message Structure

TODO: refine structure

`SyncMessage` structure is used by all sync protocols, the type of message is specified in a dedicated field:

```protobuf
message SyncMessage {
  // MsgType is the type of sync message
  SyncMsgType MsgType                      = 1;
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
  GetHighestType = 0;
  // GetInstanceRange is a request from peers to return instances and their decided/ prepared justifications
  GetInstanceRange = 1;
  // GetCurrentInstance is a request from peers to return their current running instance details
  GetLatestChangeRound = 2;
}
```

#### Stream Protocols

**TODO: add example request/response**

SSV nodes use the following stream protocols:


##### 1. Highest Decided

This protocol is used by a node to find out what is the highest decided message among a specific committee.
In case there are no decided messages, it will return an empty array of messages.

`/sync/highest_decided/0.0.1`


##### 2. Decided By Range

This protocol enables to sync decided messages in some specific range.

`/sync/decided_by_range/0.0.1`


##### 3. Last Change Round

This protocol enables a node that was online to catch up with change round messages.

`/sync/last_change_round/0.0.1`


-----



## Networking

### Network ID

Network ID is a 32byte hash, which has to be known and used by all members across the network.
Peers from other public/private libp2p networks (with different network ID) won't be able to read or write messages in the network.

It works with [libp2p's private network](https://github.com/libp2p/specs/blob/master/pnet/Private-Networks-PSK-V1.md),
which encrypts/decrypts all traffic of a given network with the corresponding key (network id/hash),
regardless of the regular transport security ([go-libp2p-noise](https://github.com/libp2p/go-libp2p-noise)).



### Discovery

As libp2p doesn't provide a module for decentralized discovery,
[discv5](https://github.com/ethereum/devp2p/blob/master/discv5/discv5.md) is used in ssv as a complementary module for discovery.

Peers are represented by an ENR record (see below) that is published across the network and stored in a DHT.

Bootnode is a special kind of peers that have a public static ENR to enable new peers to join the network. \
The role of the bootnode ends once a new peer finds existing peers in the network.

#### ENR

[Ethereum Node Records](https://github.com/ethereum/devp2p/blob/master/enr.md) is a format for connectivity information.
Each record contains a signature, sequence (for republishing) and arbitrary key/value pairs.

`ENR` structure in ssv network contains the following key/value pairs:

| Key         | Value                                           |
|:------------|:------------------------------------------------|
| `id`        | name of identity scheme, e.g. "v4"              |
| `secp256k1` | compressed secp256k1 public key, 33 bytes       |
| `ip`        | IPv4 address, 4 bytes                           |
| `tcp`       | TCP port, big endian integer                    |
| `udp`       | UDP port, big endian integer                    |
| `type`      | node type, integer -> 1 (operator), 2 (exporter), 3 (bootnode) |
| `oid`       | operator id, 32 bytes                           |
| `version`   | fork version, integer                           |



### Authentication

This protocol enables ssv nodes to exchange payloads that authenticate an operator/exporter in relation to the peer.


### Subnets

Messages in the network are being sent over a subnet/topic, which the relevant peers should be subscribed to. \
This helps to reduce the overall bandwidth, related resources etc.

There are several options for how setup topics in the network, see below.

The first version of SSV testnet is using a topic per validator, as described below.

#### 1. Topic per validator

Each validator committee has a dedicated pubsub topic with all the relevant peers subscribed to it (committee + exporter).

It helps to reduce amount of messages in the network, but increases the number of topics. \
In addition, it a node can instantly understand the status of committee members by checking how many
peers are subscribed on the validator's topic.

#### 2. Subnet 

A subnet of several validators contains multiple committees,
reusing the topic to communicate on behalf of multiple validators.

The number of topics will be reduced but the number of messages each peer handles should grow. \
As the topics interest across the network also increased,
we can expect a better propagation of messages and therefore lower probability of missed messages.

Subnet approach also enables redundancy of decided messages across multiple peers,
not only among committee peers (+ exporter), and therefore increasing network security.

##### Validator to subnet mapping

A simple approach that can be taken is hash partitioning: \
`hash(validatiorPubKey) % num_of_subnets`,
where the number of subnets is fixed (TBD 32 / 64 / 128).
The hash function helps to distribute validators across subnets in a balanced way

**TBD** A dynamic number of subnets (e.g. `log(num_of_peers)`) which requires a different approach.

How to check how many peers are online for a specific validator? \
Assuming peers are authenticated, and published their operator public key in that process,
nodes can iterate the subnet peers and lookup the desired committee members by their public key.

### Peers Connectivity

**TBD**

In a fully-connected network, where each peer is connected to all other peers in the network,
running nodes will consume many resources to process all network related tasks e.g. parsing, peers management etc.

To lower resource consumption, there is a limitation for the total connected peers, currently set to `250`. \
Once reached to peer limit, the node will connect only to operators that shares at least one subnet / committee.

### Forks

Future network forks will follow the general forks mechanism and design in SSV. \
The idea is to wrap procedures that have potential to be changed in future versions.
Currently, the following are covered:

- validator topic mapping
- message encoding/decoding

#### Fork v0

validator public key is used as the topic name and JSON is used for encoding/decoding of messages.

---

### Configuration

**TODO: filter configs**

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

-----

## Open points

* Configurations
* Heartbeat ?
* Home setup
* UPnP (NATPortMap)
* Hub/HA
    * proxied network layer?
* History sync changes?

