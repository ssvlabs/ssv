[<img src="./../../resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Secret Shared Validator

## Introduction

Secret Shared Validator ('SSV') is a unique technology that enables the distributed control and operation of an Ethereum validator.

SSV uses an MPC threshold scheme with a consensus layer on top ([Istanbul BFT](https://arxiv.org/pdf/2002.03613.pdf)), 
that governs the network. \
Its core strength is in its robustness and fault tolerance which leads the way for an open network of staking operators 
to run validators in a decentralized and trustless way.

## SSV Spec
This repo contains the spec for SSV.Network node.

### SSVMessage
SSV network message is called SSVMessage, it includes a MessageID and MsgType to route messages within the SSV node code, and, data for the actual message (QBFT/ pre/ Post consensus messages for example).

Any message data struct must be signed and nested within a signed message struct which follows the MessageSignature interface. 
A signed message structure includes the signature over the data structure, the signed root and signer list.

#### QBFT Message
This type of message is used for all consensus messages 
```go
type Message struct {
	MsgType    MessageType
	Height     Height // QBFT instance Height
	Round      Round  // QBFT round for which the msg is for
	Identifier []byte // instance Identifier this msg belongs to
	Data       []byte
}
```

#### Decided Message
Once a QBFT instance is decdied, this message is broadcasted with at least 2f+1 aggrageted commit messages in it 
```go
type DecidedMessage struct {
    SignedMessage *SignedMessage
}
```

#### Partial Signature Message
Used for pre and post consensus sigantures for collecting partial BN signatures and then reconstructing them
```go
type PartialSignatureMessage struct {
    Type             PartialSigMsgType
    PartialSignature []byte // The beacon chain partial Signature for a duty
    SigningRoot      []byte // the root signed in PartialSignature
    Signers          []types.OperatorID
}
```

### Signing messages
The KeyManager interface has a function to sign roots, a slice of bytes. 
The root is computed over the original data structure (which follows the MessageRoot interface), domain and signature type.

**Use ComputeSigningRoot and ComputeSignatureDomain functions for signing**
```go
func ComputeSigningRoot(data MessageRoot, domain SignatureDomain) ([]byte, error) {
    dataRoot, err := data.GetRoot()
    if err != nil {
        return nil, errors.Wrap(err, "could not get root from MessageRoot")
    }

    ret := sha256.Sum256(append(dataRoot, domain...))
    return ret[:], nil
}
```
```go
func ComputeSignatureDomain(domain DomainType, sigType SignatureType) SignatureDomain {
    return SignatureDomain(append(domain, sigType...))
}
```

Domain Constants:

| Domain         | Value                         | Description                       |
|----------------|-------------------------------|-----------------------------------|
| Primus Testnet | DomainType ("primus_testnet") | Domain for the the Primus testnet |

Signature type Constants:

| Signature Type       | Value                | Description                              |
|----------------------|----------------------|------------------------------------------|
| QBFTSignatureType          | [] byte {1, 0, 0, 0} | SignedMessage specific signatures        |
| PartialSignatureType | [] byte {2, 0, 0, 0} | PostConsensusMessage specific signatures |

## Validator and Runners
A validator instance is created for each validator independently, each validator will have multiple Runner for each beacon chain duty type (Attestations, Blocks, etc.)
Duty runners are responsible for processing incoming messages and act upon them, completing a full beacon chain duty cycle.

Each duty starts by calling the StartNewDuty func in the respective Runner.
StartNewDuty might return error if can't start a new duty, depending on the previous duty life cycle.
As a general rule, when a runner is executing a duty in the consensus phase, a new duty can't start.
Pre/ Post partial signature collection will not enable starting a new duty if not completed except if timed out.

Constants:

| Constant                              | Value | Description                                                                                                                            |
|---------------------------------------|-------|----------------------------------------------------------------------------------------------------------------------------------------|
| DutyExecutionSlotTimeout | 32    | How many slots pass until a new QBFT instance can start without waiting for all pre/ post consensus partial signatures to be collected |

**Attestation Duty Full Cycle:**\
-> Wait to slot 1/3\
&nbsp;&nbsp;&nbsp;-> Received new beacon chain duty\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Check can start a new consensus instance\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Come to consensus on duty + attestation data\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Broadcast and collect partial signature to reconstruct signature\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Reconstruct signature, broadcast to BN

**Block Proposal Duty Full Cycle:**\
-> Received new beacon chain duty\
&nbsp;&nbsp;&nbsp;-> Check can start a new consensus instance\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Sign partial RANDAO and wait for other signatures\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Come to consensus on duty + beacon block\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Broadcast and collect partial signature to reconstruct signature\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Reconstruct signature, broadcast to BN

**Attestation Aggregator Duty Full Cycle:**\
-> Received new beacon chain duty\
&nbsp;&nbsp;&nbsp;-> Check can start a new consensus instance\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Sign partial selection proof and wait for other signatures\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Wait to slot 2/3\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Come to consensus on duty + aggregated selection proof\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Broadcast and collect partial signature to reconstruct signature\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Reconstruct signature, broadcast to BN

**Sync Committee Duty Full Cycle:**\
-> Wait to slot 1/3\
&nbsp;&nbsp;&nbsp;-> Received new beacon chain duty\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Check can start a new consensus instance\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Come to consensus on duty + sync message\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Broadcast and collect partial signature to reconstruct signature\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Reconstruct signature, broadcast to BN

**Sync Committee Aggregator Duty Full Cycle:**\
-> Received new beacon chain duty\
&nbsp;&nbsp;&nbsp;-> Check can start a new consensus instance\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Locally get sync subcommittee indexes for slot\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Partial sign contribution proofs (for each subcommittee) and wait for other signatures\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> wait to slot 2/3\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Come to consensus on duty + contribution (for each subcommittee)\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Broadcast and collect partial signature to reconstruct signature\
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;-> Reconstruct signature, broadcast to BN

A runner holds a QBFT controller for processing QBFT messages and a State which keeps progress for all stages of duty execution: pre/ post consensus messages.
Partial signatures are collected and reconstructed (when threshold reached) to be broadcasted to the BN network.

## Validator Share
A share is generated and broadcasted publicly when a new SSV validator is registered to its operators.
Shares include: 
- Node ID: The Operator ID the share belongs to
- Validator Public Key
- Committee: An array of Nodes that constitute the SSV validator committee. A node must include it's NodeID and share public key.
- Domain

```go
type Share struct {
    OperatorID            OperatorID
    ValidatorPubKey       ValidatorPK
    SharePubKey           []byte
    Committee             []*Operator
    Quorum, PartialQuorum uint64
    DomainType            DomainType
    Graffiti              []byte
}
```

## Node
A node represents a registered SSV operator, each node has a unique ID and encryption key which is used to encrypt assigned shares.
NodeIDs are extremely important as they are used when splitting a validator key via Shamir-Secret-Sharing, later on they are used to verify messages and reconstruct signatures.

Shares use the Node data (for committee) to verify that incoming messages were signed by a committee member

```go
type Node struct {
    NodeID NodeID
    PubKey []byte
}
```

### NodeID and share creation example
NodeID is unique to each node, starting from ID 1 and incrementally going froward. 
Each ValidatorPK has a committee of nodes, each with a unique ID, share and share public key. 

f(x) = a0 + a1X + a2X^2+a3X^3 + ... + ak-1X^(k-1)\
f(0) = a0 = secret\
Share1 = f(NodeID1)\
Share1 = f(NodeID1)\
...

### Spec tests
The [spec tests](spectest) are a generated as a json file that can be run in any implementation. They test the various flows within the SSV package, treating the consensus protocol as as black box.

## TODO
- [//] Proposal duty execution + spec test + consensus validator
- [//] Aggregator duty execution + spec test + consensus validator
- [//] Sync committee duty execution + spec test + consensus validator
- [ ] Sync committee aggregator duty
- [ ] Duty data validation (how do we ensure malicious leader doesn't proposer a non slashable attestation/ block but with invalid data)
- [ ] Wait 1/3 or 2/3 of slot during duty execution? how else?
- [X] implement 7,10,13 committee sizes
- [X] improve pre-consensus partial sigs to support signed roots which are not necessarily what the local node signed (example: node 1 signed randao/ selection proof root X but nodes 2,3,4 signed Y)
- [X] pre and post consensus timeout redesign as 32 slot timeout can cause the next duty not to start (if it starts in less than 32 slots)
- [ ] Move ConsensusData struct to ssv package
- [ ] Remove? storage interface? do we use it?