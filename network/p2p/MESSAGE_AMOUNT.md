# Estimation of amount of messages

To play with the below formulas, follow this [google sheet link](https://docs.google.com/spreadsheets/d/1TpXnVFzF4eGiQarXuPBrOIU4tJXhzOHav9PkGve3_Qc/edit#gid=0).

## Expected messages for duty

Consider a subnet with only 4 operators that works for a single validator.

## Probability of having a duty

The number of validators in the Ethereum network was considered to be 683334 (value at 23th July of 2023).

### Attestation

One attestation per epoch.

- $P(Attestation\,per\,epoch) = 1$
- $P(Attestation\,per\,slot) = 1/32$

### Attestation Aggregator

There are 16 aggregators per attestation committee. Committee size ranges from 128 to 2048.

- $P(Aggregator | Attestator) \in [\frac{C(15,2048)}{C(16,2048)},\frac{C(15,128)}{C(16,128)}] = [\frac{1}{16\times(2048-15)},\frac{1}{16\times(128-15)}] = [3.074 \times 10^{-05},0.000553]$


### Proposer

- $P(Proposer) = \frac{1}{683334} = 1.46 \times 10^{-6}$

### Sync Committee

512 validators selected for 256 epochs.

- $P(Sync Committee) = C(511,683334) / C(512,683334) = 2.86 \times 10^{-9}$

### Sync Committee Aggregator

The 512 validators are divided across 4 independent subnets (each with 128). Each has 16 aggregators.

- $P(Sync Commitee Aggregator | Sync Commitee) = \frac{C(15,128)}{C(16,128)} = \frac{1}{16\times(128-15)} = 0.000553$

The duties are non-excluvive. A validator may have the 5 duties in a single slot.

## Number of messages exchanged for duty

Suppose there are N operators running a validator.

Each duty may has 3 steps: pre-consensus, consensus, post-consensus.

In one operator perspective, the interval of possible number of messages received are:
- Pre-consensus: $[\frac{(N+f)}{2}+1, N]$
- Post-consensus: $[\frac{(N+f)}{2}+1, N]$
- Consensus: $\infty$ in theory (actually, the maximum number of rounds is 12 what limitates the total number of messages)

Let's consider the consensus to have $0.95$ probability of sucess. Considering each as a Bernoulli trial, the geometric distribution becomes
$$P(X=k) = (1-0.95)^{k-1}*(0.95)$$

with expected value given by $E(X) = 1/p$.

In a successful round, the interval of possibile messages received is
$$[1 + \lfloor\frac{(N+f)}{2}\rfloor+1 + \lfloor\frac{(N+f)}{2}\rfloor + 1, 1 + N + N] = [3 + 2\times\lfloor\frac{N+f}{2}\rfloor, 2N + 1]$$

In an unsuccessful round, the number of messages are
$$[4 + 3\times\lfloor\frac{N+f}{2}\rfloor,3N+1]$$


Let's take the maximum number of messages for each case. The expected number of messages in a round is
$$E(messages\,per\,round) = 0.95 * (2N+1) + 0.05 * (3N+1)$$

So the expected number of messages for the protocol is
$$E(messages) = \frac{1}{0.95}\times (0.95 * (2N+1) + 0.05 * (3N+1))$$

Also, once consensus is reached, decided message may be received. It node can send f+1 decided messages with different committees sizes. Thus, we have up to:
- Decided: $N \times (f+1)$

Every duty must do consensus (with decided messages) and post-consenus, while some duties doesn't require pre-consensus. Thus, we have:
- Without pre-consensus: $\frac{1}{0.95}\times (0.95 * (2N+1) + 0.05 * (3N+1)) + N\times(f+1) + N$
- With pre-consensus: $\frac{1}{0.95}\times (0.95 * (2N+1) + 0.05 * (3N+1)) + N\times(f+1) + 2N$

Regarding pre-consensus for each duty, we have:
- Attestation: no pre-consensus
- Attestation Aggregation: with pre-consensus
- Proposer: with pre-consensus
- Sync Committee: no pre-consensus
- Sync Committee Aggregator: with pre-consensus

## Number of expected messages per slot

The final number of expected messages per slot becomes
$$E(messages\,per\,slot) = P(Attestation\,per\,slot) * E(messages|Attestation) +\\ P(Aggregator) * E(messages|Aggreator) +\\ P(Proposer) * E(messages|Proposer) +\\ P(SyncCommittee) * E(messages|SyncCommittee) +\\ P(SyncCommitteeAggregator) * E(messages|SyncCommitteeAggregator)$$

To get the highest estimation, we set the aggreator attestation to its higher probability (lowest committee size) and set each consensus step to have their maximum number of messages. Then, we have (for one validator with 4 operators):
$$E(messages\,per\,slot) = 0.6781$$

If we set each consensus step to their minimum number of messages, we would have:
$$E(messages\,per\,slot) = 0.4543$$


## Expected messages in subnet

Suppose an operator belongs to a subnet. In this subnet, there are $V$ validators. Each validator has 4 operators assigned.

Note: Here, it doesn't matter if all validators assigned the same 4 operators or not. The total number of messages is determined by the number of validators and how operators eadch assinged. Of course, it impacts the processing time of an operator whether it's must answer to all messages or not. But it doesn't impact how many messages it receives.

Suppose we keep active only one validator (and 4 operators). The expected number of messages is $E(messages\,per\,slot)$.

If we activate one more validator, then, it becomes $E(messages\,per\,slot)\times2$, and so on.

## Expected messages for all subnets

Expanding the view, supposing an operator may belong to numerous subnets. The number of expected messages becomes $E(messages\,per\,slot) \times V$ where V is the total number of validators in all subnets (supposing each validator has 4 operators assigned).

For example, if there were $10000$ validators, we would have with our highest estimation:
$$10000 \times 0.6781 = 6781 \text{ messages per slot} = 565 \text{ messages per second}$$

And with the lowest estimation:
$$10000 \times 0.4543 = 4543 \text{ messages per slot} = 378 \text{ messages per second}$$

## Duty weight on expected number of messages


| Duty | Weight |
| ---- | ------ |
| Attestation | 0.99927 |
| Aggregate Attestation | 0.00066 |
| Proposer | 0.00005529 |
| Sync Committee| 0.00000011 |
| Sync Committee Aggregatio| 0.0000000000610 |

## Number of expected messages by number of operators

The table below shows how the expected number of messages grows as the number of operators hired by validators grows. The expected number of messages was computed as the average between the higher and lower estimation.

| $f$ | $N = (3f+1)$ | $E(messages\,per\,slot)$ | $E(messages)$ for 10000 validators per second|
| ---- | ----- | ---- | ---- |
1 | 4 | 0.5661944351375915 | 471.8286959479929 |
2 | 7 | 1.0632497473032632 | 886.0414560860527 |
3 | 10 | 1.6541112918036796 | 1378.4260765030665 |
4 | 13 | 2.338779068638841 | 1948.982557199034 |
5 | 16 | 3.1172530778087464 | 2597.7108981739552 |
6 | 19 | 3.9895333193133973 | 3324.611099427831 |
7 | 22 | 4.955619793152793 | 4129.683160960661 |
8 | 25 | 6.015512499326932 | 5012.927082772443 |
9 | 28 | 7.169211437835817 | 5974.342864863181 |
10 | 31 | 8.416716608679447 | 7013.9305072328725 |