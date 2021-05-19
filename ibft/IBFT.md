## Istanbul Byzantine Fault Tolerance (IBFT) Consensus - Annotated Spec

### Introduction

Blox SSV (Secret Shared Validators) is governed by the IBFT consensus algorithm. IBFT ensures that consensus can be reached by a committee of validator nodes (n) while tolerating for a certain amount of faulty nodes (f) as defined by n≥3f+1. In SSV, each validator node represents a &#39;share&#39; of a private validator key for Ethereum 2.0 (Eth2). Effectively, the network allows for the splitting of an Eth2 validator key into a multisig, governed by IBFT consensus.

At a very high level, the protocol can be expressed across three phases:

1. **PRE-PREPARE**
2. **PREPARE**
3. **COMMIT**

_IBFT was first defined in EIP-650 and discussed at length in_ [_The Istanbul BFT Consensus Algorithm_](https://arxiv.org/pdf/2002.03613.pdf)_. Please refer to that paper for advanced reading._

### Terminology

- **Validator Node** : block validation participant. Each node represents a portion (share) of an Ethereum validator.
- **Round** : a consensus turn. Starting with a leader (defined below) creating a duty block proposal and ending with duty block consensus or a round change.
- **Leader** :the validator node responsible for broadcasting the PRE-PREPARE message in a given round.
- **Consensus** : process of reaching agreement on the data to be signed between validator nodes.
- **Committee** : group of validator nodes responsible for consensus voting to assure generation of a valid duty block.

### Variables

- **f** : the amount of faulty validator nodes that can be tolerated (and still reach consensus) out of n validator nodes, according to n≥3f+1 (f must be a whole number and is always rounded down); e.g., if there are 7 validator nodes (n=7), it is still possible to reach consensus with 2 faulty nodes (f=2), and if there are 4, 5 or 6 validator nodes, it is possible to reach consensus with 1 faulty node.
- **p(i)**: process - a single validator node in a committee. (i) is the index identifier of the specific node.
- **λ** : IBFT instance (changes for every slot); in the beacon chain system, λ corresponds to the slot number.
- **r:** IBFT round within an instance (starts with r=1 and resets every instance; in a perfect situation, there will be only one round per instance).
- **inputValue** : the duty data from the beacon chain (for attestation/proposal); when it&#39;s sent by a validator node rather than fetched directly from the beacon chain, it&#39;s referred to as &quot;value&quot; (or &quot;v&quot;) rather than inputValue.
- **pr** : used only in ROUND-CHANGE messages, indicates the highest round in which the node received a quorum of valid PREPARE messages; pr must always be lower than r (because in round change message, r is always the NEXT round) in a valid ROUND-CHANGE message. If the round change happens before the node receives a quorum of valid PREPARE messages during the current instance, pr will retain the default value of ⊥ (null).
- **pv** : the corresponding inputValue of round pr. If the round change happens before the node receives a quorum of valid PREPARE messages during the current instance, pv will retain the default value of ⊥ (null).
- **timer** : set when the START procedure is initiated at the beginning of a new instance, and expires at time t(r) if consensus hasn&#39;t been reached. If the timer expires, this will trigger broadcasting a ROUND-CHANGE message, after which the timer will be reset. If consensus is reached before t(r), the timer is stopped.
- **t(r)**: the time (Unix) in which the round should end if the nodes fail to reach consensus.
- **Q** : Quorum - 2f+1 valid messages, where f is the number of allowed fails according to the formula n≥3f+1 (refer to f above).
  \*Note that a message sent by a node is included in its count towards a quorum, e.g., in a 4 node system, if a node sent a message it must receive only two more valid messages to reach a quorum.
- **Qcommit** : a private case of Q indicating a quorum of COMMIT messages, indicating that consensus has been reached.
- **Qprepare** : a private case of Q indicating a quorum of PREPARE messages.
- **Qrc** : a private case of Q indicating a quorum of ROUND-CHANGE messages.
  Note that unlike Frc (see below), in Qrc the r values of all messages must be the same; each pr and pv however can be of different values (if so, the pr with the highest value is referred to as pr(max)).
- **Frc** : indicates f+1 valid ROUND-CHANGE messages. In an Frc, unlike Qrc, the r values need not be identical in all of the group&#39;s messages.
- **m** : message; there are four types of messages - PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE.

### Consensus Instance

#### START Procedure

START procedure is initiated at the beginning of an instance, and only once per instance. It includes the slot number λ (which is also used as the instance identifier) and the duty data inputValue. Both are taken from the Eth2 node.

When the START procedure is initiated, all of the validator nodes set the timer to expire after t(r).

Leader selection occurs right after the START procedure is initiated.

#### Leader Selection

The leader is the validator node responsible for broadcasting the PRE-PREPARE message in a given round.

At the beginning of each instance (when START procedure is initiated), one of the validator nodes is chosen to be the leader based on two values - the slot number λand the round number r.

The leader choosing mechanism is deterministic, i.e., for a given round and slot, there can be only one specific leader, which is crucial because each node must always know who should be the correct leader each round, and discard any PRE-PREPARE message broadcasted by an incorrect leader.

As there is no way to predict future slot numbers for which the validator will have a duty (beyond the very next slot), there is no way to predict future leaders, which makes this deterministic mechanism secure.

When there is more than one round in an instance, a new leader is chosen each round. In this case, only the round number r changes (the slot number λ always remains the same throughout all rounds in an instance), and leaders subsequent to the first will be selected according to their node indexes (e.g., if in round r=1 the node with index 2 (p(2)) is selected as the leader, in round r=2 the node with index 3 (p(3)) will be the leader).

Once the leader node is selected, it broadcasts the PRE-PREPARE message <PRE-PREPARE, λ, r, inputValue> to all nodes.



#### Consensus Phases:

##### PRE-PREPARE

When a leader is selected, it broadcasts the PRE-PREPARE message <PRE-PREPARE, λ, r, inputValue> to all validator nodes.

A validator node that receives the PRE-PREPARE message validates that it came from the correct leader, and if so, the inputValue data is validated against the data from the Eth2 node, and if the data is valid, the validator node sets the timer to time t(r) and broadcasts the PREPARE message <PREPARE, λ, r, value> to all other nodes, and then waits for a quorum of valid PREPARE messages. Note - the leader itself also receives the PRE-PREPARE message that it sent, and then broadcasts PREPARE as the other nodes do.

If a PRE-PREPARE message is invalid (for any reason), it is discarded and the nodes continue to wait for a valid PRE-PREPARE message.
\*Note that for any round after the first round (r>1), the PRE-PREPARE message must be justified (and also validated as in the first round). See the ROUND-CHANGE section for more information.

The uniqueness of PRE-PREPARE is two-fold:

1. It is sent only by one node (the round&#39;s leader).

2. There&#39;s no need for a quorum for this message, validation against the Eth2 node is sufficient.

##### PREPARE

Upon receiving a valid PRE-PREPARE message, the validator node sets the timer to time t(r) and broadcasts the PREPARE message <PREPARE, λ, r, value> and waits until it receives a quorum of valid PREPARE messages.

Validation of PREPARE messages received from other nodes is achieved by checking that the message format is correct, and that a slashable event will not be caused if the data in the PREPARE message will be signed, i.e., this data hasn&#39;t been signed already in the past (there is no check against the Eth2 node, the validator node should have this information).

When a quorum of valid PREPARE messages is received, the validator node broadcasts the COMMIT message <COMMIT, λ, r, value> and waits until it receives a quorum of valid COMMIT messages.

If a validator node receives a quorum of valid PREPARE messages before receiving the matching PRE-PREPARE message, it can act upon it (i.e., broadcast the COMMIT message) without waiting for the PRE-PREPARE, because PRE-PREPARE is no longer required.

##### COMMIT

Upon receiving a valid PREPARE message, the validator node broadcasts the COMMIT message <COMMIT, λ, r, value> and waits until it receives a quorum of valid COMMIT messages.

Validation of COMMIT messages received from other nodes is achieved by checking that the message format is correct, and that a slashable event will not be caused if the data in the COMMIT message will be signed, i.e., this data hasn&#39;t been signed already in the past (there is no check against the Eth2 node, the validator node should have this information).

When a quorum of valid COMMIT messages is received, consensus has been reached and the validator node stops the timer and submits the information (signed duty - attestation/proposal) to the Eth2 node.

If a validator node receives a quorum of valid COMMIT messages before receiving the matching PRE-PREPARE and/or PREPARE message, it can act upon it (i.e., submit the information to the Eth2 node) without waiting for the missing messages.

##### ROUND-CHANGE

If the time t(r) is reached before consensus is reached, the timer expires and the node broadcasts a ROUND-CHANGE message in order to advance the round from r to r+1. The content of a ROUND-CHANGE message sent by a node (that’s current round is r) is <ROUND-CHANGE, λ, r+1, pr, pv>, where pr indicates the highest round in which a quorum of PREPARE messages has been received, and pv indicates the value which was included in those PREPARE messages. After broadcasting this message, the node sets the timer to expire at time t(r+1) and waits for a quorum of valid ROUND-CHANGE messages (Qrc). If the node has never received a quorum of PREPARE messages for the instance λ, then pr and pv will be ⊥ (null).
\*Note - a ROUND-CHANGE message is considered valid if its format is correct and if r+1 is higher than the current round the recipient node is in. In addition, if pr≠⊥, then pr<r+1 must be true for the message to be valid.
Important - while λ and r+1 must be identical across all messages in a Qrc, each pr and pv can be of different values (in such a set, the pr with the highest value is referred to as pr(max)).

Another event which triggers the node to broadcast a ROUND-CHANGE message is when receiving a set of f+1 valid ROUND-CHANGE messages (such a set is referred to as Frc) before the timer expires. In Frc, unlike Qrc, the r value in the set of f+1 messages can be different in each message, and the node that received the Frc will broadcast a ROUND-CHANGE message with the smallest r value among the r values of the Frc set (referred to as r(min)).

Before broadcasting the PRE-PREPARE message of a new round, the new leader (see &quot;Leader Choosing&quot; section) must first perform justification on the quorum of ROUND-CHANGE messages Qrc. This is done by verifying that a quorum of valid PREPARE messages was indeed received in round r=pr(max), and that pr(max) is the highest round in which a quorum of PREPARE messages has been received in this instance. If pr=⊥ in all of the ROUND-CHANGE messages that should be justified, they are automatically justified.

Following the justification of Qrc, the leader sets a new timer for the new round and broadcasts a new PRE-PREPARE message <PRE-PREPARE, λ, r (of the new round), inputValue>, in which inputValue=pv if pv≠⊥, and if pv=⊥ then inputValue is taken from the Eth2 node as in the first round. In case the Qrc contains messages with different pr and pv values, the pv used is the one with the highest related pr (pr(max)).

The PRE-PREPARE messages should be justified by the nodes when received (note that if r=1, i.e., no round change has yet occurred, PRE-PREPARE does not require justification). This justification is performed by justifying the quorum of ROUND-CHANGE messages in the same way that the leader justified it before broadcasting the PRE-PREPARE. After the node justifies the PRE-PREPARE message, it also checks that it is valid in the same fashion as in the first round. If the PRE-PREPARE is justified and valid, the round continues normally as in the first round.

### Diagrams

![Normal case](../github/resources/IBFTChart1.png)

![Round change](../github/resources/IBFTChart2.png)
