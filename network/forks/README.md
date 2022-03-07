# SSV - Network Forks

This document contains information regards network forks `SSV.Network`.

## Overview

- [Forks](#forks)
    - [v0](#fork-v0)
    - [v1](#fork-v1)
    - [v2](#v2)

## Forks

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


**message encoding**

JSON is used for encoding/decoding of messages.


**user agent**

User Agent contains the node version and type, and in addition the operator id.

`SSV-Node:v0.x.x:<node-type>:<?operator-id>`


#### Fork v1

**validator topic mapping**

Validator public key hash is used to determine the validator's subnet which is the topic name:

`bloxstaking.ssv.<hash(validatiorPubKey) % num_of_subnets>`


**message encoding**

[SSZ](https://github.com/ethereum/consensus-specs/blob/v0.11.1/ssz/simple-serialize.md) will be used to
encode/decode network messages.
It is efficient with traversing on fields, and is the standard encoding in ETH 2.0.


**user agent**

User Agent in `v1` will be reduced in the favor of the handshake process.

A short and simple user agent is kept for acheiving libp2p interoperability.

`SSV-Node/v0.x.x`

