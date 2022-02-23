# SSV Specifications - Networking

| Authors                                    | Status | Last Revision |
|:-------------------------------------------|:-------|:--------------|
| [@amir-blox](https://github.com/amir-blox) | WIP    | FEB 22        |

This document contains the networking specification for `SSV.Network`.

## Overview

- [Fundamentals](#fundamentals)
  - [Stack](#stack)
  - [Transport](#transport)
  - [Messaging](#messaging)
  - [Network Peers](#network-peers)
  - [Identity](#identity)
  - [Network Discovery](#network-discovery)
  - [Peer Scoring](#peer-scoring)
- [Wire](#wire)
  - [Consensus](#consensus-protocol)
  - [Sync](#sync-protocol)
  - [Handshake](#handshake-protocol)
- [Networking](#networking)
  - [PubSub](#pubsub)
  - [PubSub Scoring](#pubsub-scoring)
  - [Message Scoring](#consensus-scoring)
  - [Discovery](#discovery)
  - [Subnets](#subnets)
  - [Peers Connectivity](#peers-connectivity)
  - [Connection Gating](#connection-gating)
  - [Security](#security)
  - [Forks](#forks)
  - [Network ID](#network-id)

## Fundamentals

### Stack

`SSV.Network` is a permission-less P2P network, consists of operator nodes that are grouped in multiple subnets, 
signing validators' duties after reaching to consensus for each duty.

The networking layer is built with [Libp2p](https://libp2p.io/), 
a modular framework for P2P networking that is used by multiple decentralized projects, including ETH 2.0.

### Transport

Network peers must support the following transports:
- `TCP` is used by libp2p for setting up communication channels between peers. 
default port: `12001`
- `UDP` is used for discovery by[discv5](https://github.com/ethereum/devp2p/blob/master/discv5/discv5.md). 
default port: `13001`

[go-libp2p-noise](https://github.com/libp2p/go-libp2p-noise) 
is used to secure transport, for more details see [noise protocol](https://noiseprotocol.org/noise.html) 
and [libp2p spec](https://github.com/libp2p/specs/blob/master/noise/README.md).

Multiplexing of protocols over channels is achieved using [yamux](https://github.com/libp2p/go-libp2p-yamux) protocol.


### Messaging

Messages in the network are formatted with `protobuf` (NOTE: `v0` messages are encoded/decoded with JSON),
and being transported p2p with one of the following methods:

**Streams** 

Libp2p allows to create a bidirectional stream between two peers and implement the corresponding wire messaging protocol.

[Streams](https://ipfs.io/ipfs/QmVqNrDfr2dxzQUo4VN3zhG4NV78uYFmRpgSktWDc2eeh2/specs/7-properties/#71-communication-model---streams) 
are used in the network for direct messages between peers.

**PubSub**

GossipSub ([v1.1](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md)) 
is the pubsub protocol used in `SSV.Network`

The main purpose is for broadcasting messages to a group (AKA subnet) of nodes. \
In addition, the machinery helps to determine liveliness and maintain peers scoring.


### Network Peers

There are several types of nodes in the network:

`Operator` is responsible for executing validators duties. \
It holds registry data and the validators consensus data.

`Exporter` is responsible for collecting and exporting information from the network. \
It collects registry data and consensus data (decided messages) of all the validators in the network.

`Bootnode` is a public peer which is responsible for helping new peers to find other peers in the network.
It has a stable ENR that is provided with default configuration, so other peers could join the network easily.


### Identity

Identity in the network is based on two types of keys:

`Network Key` is used to create network/[libp2p identity](https://docs.libp2p.io/concepts/peer-id) (`peer.ID`), 
will be used by all network peers to setup a secured connection. \
Unless provided, the key will be generated and stored locally for future use, 
and can be revoked in case it was compromised. 

`Operator Key` is used for decryption of share's keys that are used for signing/verifying consensus messages and duties. \
Exporter and Bootnode does not hold this key.

Operator nodes will expose an `Operator ID` that is calculated from the operator public key. \
Note that this value is used as a hint for better observability of the network. 
Malicious peers with fake operator ID will be pruned due to bad scores 
as they don't have the key to sign messages.


### Network Discovery

[discv5](https://github.com/ethereum/devp2p/blob/master/discv5/discv5.md) 
is used in `SSV.Network` as the discovery system.

More information is available in [Networking > Discovery](#discovery)


### Peer Scoring

Peer scoring in `SSV.Network` is how we protect the network from bad peers,
by scoring them according to a predefined set of scores.

For more info please refer to the following sections:

- [Pubsub Scoring](#pubsub-scoring) - calculated by libp2p, configured by us
- [Consensus Scoring](#consensus-scoring) - calculated by internal components during message processing


------


## Wire

All the messages that are being transmitted over the network must be wrapped with the following structure:

```protobuf
syntax = "proto3";
import "gogo.proto";

// SignedMessage holds a message and it's corresponding signature
message SSVMessage {
  // type of the message
  MsgType MsgType = 1 [(gogoproto.nullable) = false];
  // id of the message
  bytes MsgID     = 2 [(gogoproto.nullable) = false];
  // message data (encoded)
  bytes Data      = 3 [(gogoproto.nullable) = false];
}

// MsgType is an enum that represents the type of message 
enum MsgType {
  // consensus/QBFT messages
  Consensus              = 0;
  // sync messages
  Sync                   = 1;
  // partial signatures sent post consensus
  Signature = 2;
}
```

Note that all pubsub messages in the network are wrapped with libp2p's message structure ([see RPC](https://github.com/libp2p/specs/blob/master/pubsub/README.md#the-rpc)).

## Consensus Protocol

`IBFT`/`QBFT` consensus protocol is used to govern `SSV` network.
`QBFT` ensures that consensus can be reached by a committee of `n` 
operator nodes while tolerating a certain amount of `f` faulty nodes as defined by `n ≥ 3f + 1`.

As part of the algorithm, nodes are exchanging messages with other nodes in the committee. \
Once the committee reaches consensus, the nodes will publish the decided message across the network.

Consensus messages are being sent in the network over pubsub topics (see [subnets](#subnets))

More information regarding the protocol can be found in [iBFT annotated paper (By Blox)](/ibft/IBFT.md)

### Message Structure

`SignedMessage` is a wrapper for QBFT messages, it holds a message and its signature with a list of signer IDs:

More details can be found in the [QBFT spec](https://github.com/bloxapp/ssv/blob/ssv_spec/docs/spec/qbft/messages.go).

<details>
  <summary><b>protobuf</b></summary>
  
  ```protobuf
  syntax = "proto3";
  import "gogo.proto";
  
  // SignedMessage holds a message and it's corresponding signature
  message SignedMessage {
    // message is the QBFT message
    Message message            = 1 [(gogoproto.nullable) = false];
    // signature is a signature of the QBFT message
    bytes signature            = 2 [(gogoproto.nullable) = false];
    // signer_ids is a sorted list of the IDs of the signing operators
    repeated uint64 signer_ids = 3;
  }
  
  // Message represents an QBFT message
  message Message {
    // type is the QBFT state / stage
    Stage type       = 1;
    // round is the current round where the message was sent
    uint64 round     = 2;
    // identifier is the message identifier
    bytes identifier = 3;
    // height is the instance height
    uint64 height    = 4;
    // value holds the message data in bytes
    bytes value      = 5;
  }
  ```
</details>

<details>
  <summary><b>JSON example</b></summary>

  ```json
  {
    "message": {
      "type": 3,
      "round": 1,
      "identifier": "OTFiZGZjOWQxYzU4NzZkYTEwY...",
      "height": 28276,
      "value": "mB0aAAAAAAA4AAAAAAAAADpTC1djq..."
    },
    "signature": "jrB0+Z9zyzzVaUpDMTlCt6Om9mj...",
    "signer_ids": [2, 3, 4]
  }
  ```
</details>

---

## Sync Protocols

There are several sync protocols, tha main purpose is to enable operator nodes to sync past decided message or to catch up with round changes.

In order to participate in some validator's consensus, a peer will first use sync protocols to align on past information.

Sync is done over streams as pubsub is not suitable in this case due to several reasons such as:
- API nature is request/response, unlike broadcasting in consensus messages
- Bandwidth - only one peer (usually) needs the data, it would be a waste to send redundant messages across the network.

### Message Structure

`SyncMessage` structure is used by all sync protocols, the type of message is specified in a dedicated field:


<details>
  <summary><b>protobuf</b></summary>

  ```protobuf
  syntax = "proto3";

  message SyncMessage {
    // protocol is the type of sync message
    string protocol       = 1;
    // identifier of the message
    bytes identifier      = 2;
    // params holds the requests parameters
    repeated bytes params = 3;
    // data holds the results
    repeated bytes data   = 4;
    // status code of the operation
    uint32 status_code  = 5;
  }
  
  enum StatusCode {
    Success = 0;
    // no results were found
    NotFound = 1;
    // failed due to bad request
    BadRequest = 2;
    // failed due to internal error
    InternalError = 3;
    // limits were exceeded
    Backoff = 4;
  }
  ```
</details>

A successful response message usually includes a list of results and the corresponding message type and identifier:
```
{
  "protocol": "<protocol>",
  "identifier": "..."
  "data": [ ... ],
  "statusCode": 0,
}
```

An error response includes an error string (as bytes) in the `data` field, plus the corresponding `status code`:
```
{
  "protocol": "<protocol>",
  "identifier": "..."
  "data": [],
  "statusCode": 1, // not found
}
```

### Protocols

SSV nodes use the following stream protocols:

### 1. Highest Decided

This protocol is used by a node to find out what is the highest decided message for a specific QBFT instance.

`/ssv/sync/decided/highest/0.0.1`

<details>
  <summary>examples</summary>

  Request:
  ```json
  {
    "protocol": "/ssv/sync/decided/highest/0.0.1",
    "identifier": "...",
  }
  ```

  Response:
  ```json
  {
    "protocol": "/ssv/sync/decided/highest/0.0.1",
    "identifier": "...",
    "statusCode": 0,
    "data": [
      {
        "message": {
          "type": 3,
          "round": 1,
          "identifier": "...",
          "height": 7943,
          "value": "Xmcg...sPM="
        },
        "signature": "g5y....7Dv",
        "signer_ids": [1,2,4]
      }
    ]
  }
  ```
</details>

### 2. Decided History

This protocol enables to sync historical decided messages in some specific range.

The request should specify the desired range, while the response will include all the found messages for that range.

**NOTE** that this protocol is optional. by default nodes won't save history, 
only those who turn on the corresponding flag will support this protocol. 

`/ssv/sync/decided/history/0.0.1`

<details>
  <summary>examples</summary>
  
  Request:
  ```json
  {
    "protocol": "/ssv/sync/decided/history/0.0.1",
    "identifier": "...",
    "params": ["1200", "1225"]
  }
  ```

  Response:
  ```json
{
  "protocol": "/ssv/sync/decided/history/0.0.1",
  "identifier": "...",
  "params": ["1200", "1225"]
  "statusCode": 0,
  "data": [{
      "message": {
        "type": 3,
        "round": 1,
        "identifier": "...",
        "height": 1200,
        "value": "Xmcg...sPM="
      },
      "signature": "g5y....7Dv",
      "signer_ids": [1,2,4]
    },
    // ... 1201-1224
    {
      "message": {
        "type": 3,
        "round": 1,
        "identifier": "...",
        "height": 1225,
        "value": "Xmcg...sPM="
      },
      "signature": "g5y....7Dv",
      "signer_ids": [1,2,4]
    }
  ]
}
  ```
</details>


### 3. Last Change Round

This protocol enables a node to catch up with change round messages.

`/ssv/sync/last_change_round/0.0.1`

<details>
  <summary>examples</summary>
  
  Request:
  ```json
  {
    "protocol": "/ssv/sync/decided/history/0.0.1",
    "identifier": "...",
    "params": ["7554"]
  }
  ```

  Response:
  ```json
  {
    "protocol": "/ssv/sync/decided/history/0.0.1",
    "identifier": "...",
    "params": ["7554"],
    "statusCode": 0,
    "data": [
      {
        "message": {
          "type": 4,
          "round": 6,
          "identifier": "...",
          "seq_number": 7554,
          "value": "Xmcg...sPM="
        },
        "signature": "g5y....7Dv",
        "signer_ids": [1]
      }
    ]
  }
  ```
</details>

---

## Handshake protocol

`/ssv/handshake/0.0.1`

The handshake protocol allows peers to identify by exchanging information 
when a new connection is established.

**TBD** The operator ID will be validated at a later point in time, 
when the peer will send consensus or decided messages.


### Message Structure

The following information will be exchanged as part of the handshake:

<details>
  <summary><b>protobuf</b></summary>

```protobuf
syntax = "proto3";
import "gogo.proto";

// NodeIdentity contains node's identity information
message NodeIdentity {
  // peer_id of the authenticating node
  bytes peer_id       = 1 [(gogoproto.nullable) = false];
  // operator_id of the authenticating node
  bytes operator_id   = 2 [(gogoproto.nullable) = true];
  // node_type is the type of the authenticating node
  uint64 node_type    = 3 [(gogoproto.nullable) = false];
  // fork_version is the current fork used by the node
  uint32 fork_version = 4;
  // metadata is the NodeMetadata in bytes, to allow future flexibility
  bytes metadata      = 5;
}

// NodeMetadata contains node's general information
message NodeMetadata {
  // execution_node is the "name/version" of the eth1 node
  string execution_node  = 1;
  // consensus_node is the "name/version" of the eth2 node
  string consesnsus_node = 2;
  // fork_version is the current ssv-node version
  string node_version    = 3;
}

```
</details>
---


## Networking


### Pubsub

The main purpose is for broadcasting messages to a group (AKA subnet) of nodes. \
In addition, the following are achieved as well:

- subscriptions metadata helps to get liveliness information of nodes
- pubsub scoring enables to prune bad/malicious peers based on network behavior and application-specific rules

The following sections details on how pubsub is used in `SSV.network`:


#### Message ID

`msg-id` is a function that calculates the IDs of messages. 
It reduces the overhead of duplicated messages as the pubsub router ignores messages with known ID. \
The default `msg-id` function uses the `sender` + `msg_seq` which we don't track, 
and therefore creates multiple IDs for the same logical message, causing it to be processed more than once.

See [pubsub spec > message identification](https://github.com/libp2p/specs/blob/master/pubsub/README.md#message-identification) for more details.

The `msg-id` function that is used in `SSV.Network` creates the ID based on the message content:

`msg-id = hash(signed-consensus-msg)`

**TBD** As hashing is CPU intensive, an optimized version of this function would be to hash specific values from the message, 
which will reduce the overhead created by hashing the entire message:

`msg-id = hash(identifier + height + signature + signers)`



#### Pubsub Scoring

`gossipsub v1.1` introduced pubsub [scoring](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md#peer-scoring),
the idea is that each individual peer maintains a score for other peers. 
The score is locally computed by each individual peer based on observed behaviour and is not shared.

`SSV.network` injects application specific scoring to apply connection and message scoring as part of pubsub scoring system. \
See [Connection Scoring](#connection-scoring) and [Message Scoring](#message-scoring) for more information.

Score thresholds are used by libp2p to determine whether a peer should be removed from topic's mesh, 
penalized or even ignored if the score drops too low. \
See [this section](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md#score-thresholds) 
for more details regards the different thresholds. \
Thresholds values **TBD**, this section will be updated once that work is complete:

- `gossipThreshold`: -4000
- `publishThreshold`: -8000
- `graylistThreshold`: -16000
- `acceptPXThreshold`: 100
- `opportunisticGraftThreshold`: 5

Pubsub runs a `Score Function` periodically to determine the score of peers. 
During heartbeat, the score it checked and bad peers are handled accordingly. see
[gossipsub v1.1 spec](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md#the-score-function)
for more information.



#### Consensus Scoring

Message scorers track on operators' behavior w.r.t incoming QBFT messages.

The full validation of messages is done by other components,
but will be reported asynchronously and scores will be set
with application specific scoring upon gossipsub heartbeat.

Other components will report severity for each of the given scores below,
which then will be converted to actual scores according to predefined scale.

The following scores will be reported if applies:

- Late arrival of valid message - `-10 <= s <= -100`
- Wrong sequence number - `-25 <= s <= -50`
- Invalid message signature - `-250 <= s <= -500`



#### Message Validation

Basic message validation should be applied on the topic level,
each incoming message will be validated to avoid relaying bad messages.

Peers that will send badly structured or corrupted messages will be scored accordingly.

**TBD: value**


#### Flood Publishing

`floodPublish` was turned on for ensuring better reliability, as peer's own messages will be propagated to a larger set of peers
(see [Flood Publishing](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md#flood-publishing))
In addition it is mentioned as a mitigation strategy for attacks,
as it helps to overcome situations where most of the node’s mesh connections were occupied by Sybils.

[Gossipsub v1.1 Evaluation Report > 2.2 Mitigation Strategies](https://gateway.ipfs.io/ipfs/QmRAFP5DBnvNjdYSbWhEhVRJJDFCLpPyvew5GwCCB4VxM4)


#### Subscription Filter

[SubscriptionFilter](https://github.com/libp2p/go-libp2p-pubsub/blob/master/subscription_filter.go)
is used to apply access control for topics. \
It is invoked whenever the peer wants to subscribe to some topic,
it helps in case other peers are suggesting topics that we don't want to join,
e.g. if we are already subscribed to a large number of topics.

---

### Subnets

Consensus messages are being sent in the network over a pubsub topic.

In `v0`, each validator had a topic for its committee.
The issue with that approach is the number of topics. It will grow up to the number of validators, 
which is not scalable. \
In order to have more redundancy, a global topic (AKA `main topic`) was used
to publish all the decided messages in the network.

`v1` introduces **subnets** - a subnet of peers consists of 
operators that are responsible for multiple committees,
reusing the same topic to communicate on behalf of multiple validators.

Operator nodes, will validate and store highest decided messages (and potentially historical data)
of all the committees in the subnets they participate.

In comparison to `v0`, the number of messages sent over the network should grow. \
To calculate the amount of messages, when each operator get every message (once) for all committees in subnet,
w/o taking into account gossip overhead, we use the following function:

`msgs_in_subnet = msgs_per_committee * operators_per_subnet * committees_per_subnet`

| Validators | Operators | Subnets | Committee - Messages (6min) | Subnet - Messages (6min) | Total Messages (6min) |
|:-----------|:----------|:--------|:----------------------------|:-------------------------|:----------------------|
| 10000      | 1000      | 64      | 12                          | ~29300                   | 1875000               |
| 10000      | 1000      | 128     | 12                          | ~7325                    | 937500                |
| 10000      | 1000      | 256     | 12                          | ~1830                    | 468750                |

**TODO: define a more accurate function to calculate the amount of messages**

The amount of message a peer will get from a single subnet is defined in 
`Subnet - Messages` column, and in `Total Messages` 
you can find the amount of messages for a peer that is subscribed to all subnets.

**TBD** number of subnets (32 / 64 / 128 / 256)

On the other hand, more nodes in a topic results
increased reliability and security as more nodes will validate messages 
and score peers accordingly.

**TBD** Main topic will be used to propagate decided messages across all the nodes in the network,
which will store the last decided message of each committee.
This will provide more redundancy that helps to maintain a consistent state across the network.

**Validators Mapping**

Validator's public key is mapped to a subnet using a hash function,
which helps to distribute validators across subnets in a balanced, distributed way:

`hash(validatiorPubKey) % num_of_subnets`

Deterministic mapping is ensured as long as the number of subnets doesn't change,
therefore it's a fixed number.

A dynamic number of subnets (e.g. `log(numOfValidators)`) was also considered,
but might require a different approach to ensure deterministic mapping.



### Discovery

[discv5](https://github.com/ethereum/devp2p/blob/master/discv5/discv5.md) 
is a system for finding other participants in a peer-to-peer network, 
it is used in `SSV.network` to complement discovery.

DiscV5 works on top of UDP, it uses a DHT to store node records (`ENR`) of discovered peers.
It allows walking randomly on the nodes in the table, and act according to application needs.

In SSV, new nodes are filtered by score, that is calculated from past behavior and properties (`ENR` entries). 
If the score is above threshold, the node tries to connect and handshake with the new node.

As discv5 is standalone (i.e. not depends on libp2p), the communication is encrypted and authenticated using session keys,
established in a separate [handshake process](https://github.com/ethereum/devp2p/blob/master/discv5/discv5-theory.md#sessions).

**Bootnode** 

A peer that have a public, static ENR to enable new peers to join the network. For the sake of flexibility, 
bootnode/s ENR values are configurable and can be changed on demand by operators. \
Bootnode doesn't start a libp2p host for TCP communication, its role ends once a new peer finds existing peers in the network.

#### ENR

[Ethereum Node Records](https://github.com/ethereum/devp2p/blob/master/enr.md) is a format that holds peer information.
Records contain a signature, sequence (for republishing record) and arbitrary key/value pairs. 

`ENR` structure in `SSV.Network` consists of the following key/value pairs:

| Key         | Value                                                          | Status          |
|:------------|:---------------------------------------------------------------|:----------------|
| `id`        | name of identity scheme, e.g. "v4"                             | Done            |
| `secp256k1` | compressed secp256k1 public key, 33 bytes                      | Done            |
| `ip`        | IPv4 address, 4 bytes                                          | Done            |
| `tcp`       | TCP port, big endian integer                                   | Done            |
| `udp`       | UDP port, big endian integer                                   | Done            |
| `type`      | node type, integer; 1 (operator), 2 (exporter), 3 (bootnode)   | Done (`v0.1.9`) |
| `oid`       | operator id, 32 bytes                                          | Done (`v0.1.9`) |
| `version`   | fork version, integer                                          | TODO            |
| `subnets`   | bitlist, 0 for irrelevant and 1 for assigned subnet            | TODO            |


#### Subnets Discovery

As `ENR` has a size limit (`< 300` bytes), 
discv5 won't support multiple key/value pairs for storing subnets of operators, 
which could have made it easier to find nodes with common subnets.

Instead, an array of flags is used, 
representing the assignment of subnets for an operator.
Similar to how it implemented in Ethereum 2.0 
[phase 0](https://github.com/ethereum/consensus-specs/blob/dev/specs/phase0/p2p-interface.md#attestation-subnet-bitfield).

[Discovery v5.2](https://github.com/ethereum/devp2p/milestone/3) will introduce 
[Topic Index](https://github.com/ethereum/devp2p/blob/master/discv5/discv5-rationale.md#the-topic-index) 
which helps to lookup nodes by their advertised topics. In SSV these topics would be the operator's subnets. \
For more information:
  - [DiscV5 Theory > Topic Advertisement](https://github.com/ethereum/devp2p/blob/master/discv5/discv5-theory.md#topic-advertisement)
  - [discv5: topic index design thread](https://github.com/ethereum/devp2p/issues/136)

See [Consensus specs > phase 0 > p2p interface](https://github.com/ethereum/consensus-specs/blob/dev/specs/phase0/p2p-interface.md#why-are-we-using-discv5-and-not-libp2p-kademlia-dht) 
for details on why discv5 was chosen over libp2p Kad DHT in Ethereum.

In `v0` discv5 is used, `v1` TBD, this section will be updated once that work is complete.

---

### Peers Connectivity

In a fully-connected network, where each peer is connected to all other peers in the network,
running nodes will consume many resources to process all network related tasks e.g. parsing, peers management etc.

To lower resource consumption, the number of connected peers is limited, configurable via flag. \
Once reached to peer limit, the node will stop looking for new nodes, 
but will accept incoming connections from relevant peers.


#### Connection Gating

Connection Gating allows safeguarding against bad/pruned peers that tries to reconnect multiple times. 
Inbound and outbound connections are intercepted and being checked before other components process the connection.

See libp2p's [ConnectionGater](https://github.com/libp2p/go-libp2p-core/blob/master/connmgr/gater.go) 
interface for more info.


### Security

As mentioned above, `gossipsub v1.1` comes with a set of tools to protect the network from bad peers.
[Gossipsub v1.1 Evaluation Report](https://gateway.ipfs.io/ipfs/QmRAFP5DBnvNjdYSbWhEhVRJJDFCLpPyvew5GwCCB4VxM4)
describes some potential attacks and how they are mitigated.

Connection gating protects against peers which were pruned in the past and tries to reconnect again before backoff timeout (5 min).
it kicks in in an early stage, before the other components processes the request to avoid resources consumption.

In addition, the discovery system is naturally a good candidate for security problems. \
DiscV5 specs specifies potential vulnerabilities in their system and how they were (or will be) mitigated,
see [DiscV5 Rationale > security-goals](https://github.com/ethereum/devp2p/blob/master/discv5/discv5-rationale.md#security-goals).
The major ones includes routing table pollution, traffic redirection, spamming or replayed messages.



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


**message encoding/decoding**

**TBD** check regards other encodings such as `SSZ` which is aligned with Ethereum, 
or `protobuf` that should show faster results than JSON.


**user agent**

User Agent in `v1` will be reduced in the favor of the handshake process.

A short and simple user agent is kept for acheiving libp2p interoperability.

`SSV-Node/v0.x.x`


### Network ID

**TBD**

Network ID is a `32byte` key, that is used to distinguish between other networks (ssv and others).
Peers from other public/private libp2p networks (with different network ID) won't be able to read or write messages in the network,
meaning that the key to be known and used by all network members.

It is done with [libp2p's private network](https://github.com/libp2p/specs/blob/master/pnet/Private-Networks-PSK-V1.md),
which encrypts/decrypts all traffic with the corresponding key,
regardless of the regular transport security protocol ([go-libp2p-noise](https://github.com/libp2p/go-libp2p-noise)).

**NOTE** discovery communication (UDP) won't use the network ID,
and it's not needed as unknown peers will be filtered.