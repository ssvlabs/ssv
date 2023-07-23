# Estimation of amount of messages sent

In this document, we try to estimate the number of message an operator creates for each validator it's responsible to.

To play with the below formulas, follow this [google sheet link](https://docs.google.com/spreadsheets/d/1TpXnVFzF4eGiQarXuPBrOIU4tJXhzOHav9PkGve3_Qc/edit#gid=891452709).

## Probability of having a duty

Check this [file](MESSAGE_RECEIVED_ESTIMATION.md#probability-of-having-a-duty) for the discussion on the probability of having each duty.


## Number of messages sent for duty per operator

Suppose there are $N$ operators running a validator, out of which $f$ may fail.

Each duty may have 3 steps: pre-consensus, consensus, post-consensus.

In one operator perspective, the number of messages sent are:
- Pre-consensus: $1$
- Post-consensus: $1$
- Consensus: $\infty$ in theory (actually, the maximum number of rounds is 12 what limitates the total number of messages)

Let's consider the consensus to have $0.95$ probability of sucess. Considering each as a Bernoulli trial, the geometric distribution (on the probability of the $k$-th round having success) becomes
$$P(X=k) = (1-0.95)^{k-1}*(0.95)$$

with expected value given by $E(X) = 1/p = 1/0.95$.

In a successful round, the expected amount of messages sent is
$$E(messages|round) = 1/N + 1 + 1$$

where $1/N$ is the proabability of being the proposer. The probability of sending a Round-Change in a succesfull round is considred to be almost zero.

In an unsuccessful round, the expected number of messages sent is
$$E(messages|round) = 1$$


Then, the expected number of messages in a round is
$$E(messages\,per\,round) = 0.95 * (2 + 1/N) + 0.05 * (1)$$

So the expected number of messages for the protocol is
$$E(messages) = \frac{1}{0.95}\times (0.95 * (2 + 1/N) + 0.05 * (1))$$

Also, once consensus is reached, decided message may be received. Each node can send f+1 decided messages with different committee sizes. Thus, we have:
- Decided: $[1,(f+1)]$

Every duty must do consensus (with decided messages) and post-consenus, while some duties doesn't require pre-consensus. Thus, we have:
- Without pre-consensus: $\frac{1}{0.95}\times (0.95 * (2 + 1/N) + 0.05 * (1)) + (f+1) + 1$
- With pre-consensus: $\frac{1}{0.95}\times (0.95 * (2 + 1/N) + 0.05 * (1)) + (f+1) + 2$

Regarding pre-consensus for each duty, we have:
- Attestation: no pre-consensus
- Attestation Aggregation: with pre-consensus
- Proposer: with pre-consensus
- Sync Committee: no pre-consensus
- Sync Committee Aggregator: with pre-consensus

## Number of expected messages sent per slot per operator

The final number of expected messages sent per slot becomes
$$E(messages\,per\,slot) = P(Attestation\,per\,slot) * E(messages|Attestation) +\\ P(Aggregator) * E(messages|Aggreator) +\\ P(Proposer) * E(messages|Proposer) +\\ P(SyncCommittee) * E(messages|SyncCommittee) +\\ P(SyncCommitteeAggregator) * E(messages|SyncCommitteeAggregator)$$

The expected number of message sent by operator (per validator) per slot is:
$$E(messages\,per\,slot) = 0.1344$$


## Expected messages for numerous validators

If an operator is responsible for $V$ validators, the expected number of messages is per slot
$$V \times E(messages\,per\,slot)$$

E.g. for 10000 validators, an operator sends $1344$ messages per slot (112 per second).


## Duty weight on expected number of messages


| Duty | Weight |
| ---- | ------ |
| Attestation | 0.9999 |
| Aggregate Attestation | 0.00068 |
| Proposer | 0.0000575 |
| Sync Committee| 0.0000000915 |
| Sync Committee Aggregatio| 0.0000000000624 |

## Number of expected messages by number of operators

The table below shows how the expected number of messages sent relates to the number of operators responsible by each validator.

| $N$ | $E(messages\,per\,slot)$ | $E(messages)$ for 10000 validators per second|
| ----- | ---- | ---- |
| 4 | 0.1344 | 112 |
| 7 | 0.1311 | 109 |
| 10 | 0.1298 | 108 |
| 13 | 0.1290 | 107 |
| 16 | 0.1286 | 107 |
| 19 | 0.1283 | 106 |