# Istanbul Byzantine Fault Tolerance (IBFT)

**Blox SSV - Secret Shared Validator -** is governed by the Istanbul Byzantine Fault Tolerance (IBFT) as the consensus algorithm.
The IBFT ensures a single and deterministic defined order of transactions in the distributed digital ledger.

There are different working algorithms:

- **Proof Of Work (PoW):**  the first algorithm to build a decentralized ledger. It is an expensive algorithm that requires
  massive computing efforts and energy consumption from a hardware - the validator node - to deter malicious uses of
  computing power such as spammers or massive service attacks.

- **Proof of Stake (PoS):**  considered as an enhanced algorithm over the PoW, the Proof of Stake optimize significantly the hardware and power consumption. Blocks are created via various combination of random selection and wealth or age (i.e. stake). The PoS attributes mining power to the proportion of coins held by a miner. This way, a node is limited to mine a percentage of transactions proportionally to his/her stake. Whereas in PoW-based systems an amount of cryptocurrency is created as rewards for miners, the PoS systems usually uses transaction fees as a reward - i.e. Gas fees in Ethereum network.

- **Proof of Authority (PoA):**  known possibly as the best solution, PoA is a consensus mechanism in which a limited number of entities - validators - are allowed to validate transactions. This vetting is based upon someone's identity as a node owner and his/her wealth stake - making malicious behaviour very damaging to their reputation and often leading to penalties. Unlike the PoW mechanism, here there is no competition between validators. The consensus requires almost no computing power, and therefore almost no electricity for its operation.

- **Byzantine Fault Tolerance (BFT):** conceptually, the BFT is the characteristic which defines a system that tolerates the class of failures that belong to the [Byzantine General's Problem](https://medium.com/loom-network/understanding-blockchain-fundamentals-part-1-byzantine-fault-tolerance-245f46fe8419). It is considered as the most difficult class of *failure modes*. In simple words, the algorithm guarantees that the number of traitors do not exceed  one third of the generals. In the absence of BFT< a peer is able to transmit and post false transactions effectively nullifying the blockchain's reliability. Worst than that, given the decentralized nature of the blockchain, no central authority could take it over and repair the damage.
- ---

## Istanbul Byzantine Fault Tolerance (IBFT)

IBFT - implementation of BFT - is a consensus mechanism that ensures a single, agreed-upon ordering of transactions in the blockchain.

IBFT uses a pool of validation nodes - _Validators_ - operating on an Ethereum network - ETH 2.0 - for `Attestation` and `Proposal`.

At a very high level point-of-view we can be expressed in a **3 phase protocol**:

1. `PRE-PREPARE`: a leader is selected from the validators group to propose a value to agree upon.
1. `PREPARE`: upon receiving a valid pre-prepare, other validators broadcasts the prepare message. 
1. `COMMIT`: upon receiving 2/3 votes, validators broadcasts a commit message.

_Istanbul BFT is inspired by Castro-Liskov 99 [paper](http://pmg.csail.mit.edu/papers/osdi99.pdf)_


### IBFT Benefits :
- **Immediate Block Finality:** The only one block producer can propose a block. Therefore, the chain does not suffer from forks or orphans. Single block is guaranteed on a protocol level.
- **Reduced Computations:** since every block producer have a chance to add block deterministically, the efforts required to compute and propose a block is reduced significantly, using minimum CPU power and network propagation.
- **High Data Integrity and Fault Tolerance:** IBFT uses a group of validators to ensure the integrity of each block being proposed. A super-majority (~66%) of these validators are required to sign the block prior to insertion to the chain, making block forgery very difficult. The "leadership" of the group also rotates over time - ensuring that a faulty node connot exert long term influence over the chain.
- **Operationally Flexible:** the group of validators can be modified in time, ensuring the group contains only full-trusted nodes.


### Terminology
- **`VALIDATOR`**: block validation participant - a.k.a. node.
- **`PROPOSER`**: a block validation participant - node - chosen to propose a block in a consensus round.
- **`ROUND`**: a consensus turn. Starts with a proposer creating a block proposal and ends with a block attestation - or a round change.
- **`PROPOSAL`**: a new block generation with an undergoing consensus processing.
- **`ATTESTATION`**: votes by validators which confirm the validity of a block.
- **`SEQUENCE`**: a sequence number of a proposal. It shall be greater than all previous sequence numbers.
- **`COMMITTEE`**: group of nodes - validators - responsible for a consensus voting to assure a valid block generated.
- **`SIGNATURES`**: a technical signature from a node to attest messages broadcast for a given block.

### Consensus States

Istanbul BFT is a [state machine replication](https://en.wikipedia.org/wiki/State_machine_replication) algorithm.
Each validator maintains a state machine replica in order to reach block consensus.

#### States:
- **`NEW ROUND`**: proposer sends new block proposal or attestation. Validators waits for PRE-PREPARE message
- **`PRE-PREPARED`**: validator received the PRE-PREPARE message and broadcasts PREPARE message. Waits for 2/3 of PREPARE or COMMIT messages
- **`PREPARED`**: validator received 2/3 of PREPARE messages and broadcasts COMMIT messages.
- **`COMMITTED`**: validator received 2/3 of COMMIT messages and inserts the proposed block or attestation into the blockchain
- **`ROUND CHANGE`**: validator is waiting for 2/3 of ROUND CHANGE messages on the same proposed round number

---
**Documentation References:**
- [Understanding Blockchain Fundamentals](https://medium.com/loom-network/understanding-blockchain-fundamentals-part-1-byzantine-fault-tolerance-245f46fe8419)
- [PoS explained by Binance](https://academy.binance.com/en/articles/proof-of-stake-explained)
- [PoA explained by Binance](https://academy.binance.com/en/articles/proof-of-authority-explained)
- [BFT explained by Binance](https://academy.binance.com/en/articles/byzantine-fault-tolerance-explained)
- [IBFT Paper](http://pmg.csail.mit.edu/papers/osdi99.pdf)
- [Ethereum Repo IBFT Issue](https://github.com/ethereum/EIPs/issues/650)
- [IBFT Benefits by Consensys](https://media.consensys.net/scaling-consensus-for-enterprise-explaining-the-ibft-algorithm-ba86182ea668)
- [Distributed Ledger Definition](https://searchcio.techtarget.com/definition/distributed-ledger)
- [Distributed Ledger by Wikipedia](https://en.wikipedia.org/wiki/Distributed_ledger)
- [Distributed Ledger Technology](https://tradeix.com/distributed-ledger-technology)







