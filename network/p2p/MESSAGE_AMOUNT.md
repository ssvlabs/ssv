# Estimation of amount of messages

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
- Pre-consensus: [$\frac{(N+f)}{2}+1$, N]
- Post-consensus: [$\frac{(N+f)}{2}+1$, N]
- Consensus: $\infty$ in theory (actually, the maximum number of rounds is 12 what limitates the total number of messages)

Let's consider the consensus to have $0.95$ probability of sucess. Considering each as a Bernoulli trial, the geometric distribution becomes

$$
\begin{aligned}
P(X=k) = (1-0.95)^{k-1}*(0.95)
\end{aligned}
$$
with expected value given by $E(X) = 1/p$.

In a successful round, the interval of possibile messages received is
$$
[1 + \lfloor\frac{(N+f)}{2}\rfloor+1 + \lfloor\frac{(N+f)}{2}\rfloor + 1, 1 + N + N] = [3 + 2\times\lfloor\frac{N+f}{2}\rfloor, 2N + 1]
$$

In an unsuccessful round, the number of messages are
$$
[4 + 3\times\lfloor\frac{N+f}{2}\rfloor,3N+1]
$$


Let's take the maximum number of messages for each case. The expected number of messages in a round is
$$
E(messages\,per\,round) = 0.95 * (2N+1) + 0.05 * (3N+1)
$$

So the expected number of messages for the protocol is

$$
E(messages) = \frac{1}{0.95}\times (0.95 * (2N+1) + 0.05 * (3N+1))
$$

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
$$
E(messages\,per\,slot) = P(Attestation\,per\,slot) * E(messages|Attestation) +\newline
P(Aggregator) * E(messages|Aggreator) +\newline
P(Proposer) * E(messages|Proposer) +\newline
P(SyncCommittee) * E(messages|SyncCommittee) +\newline
P(SyncCommitteeAggregator) * E(messages|SyncCommitteeAggregator)\newline
$$

Using the highest probability for being an attestaion aggreator, we have (for one validator with 4 operators):
$$
E(messages\,per\,slot) = 0.6781
$$

## Expected messages in subnet

Suppose an operator belongs to a subnet. In this subnet, there are $V$ validators. Each validator has 4 operators assigned.

Note: Here, it doesn't matter if all validators assigned the same 4 operators or not. The total number of messages is determined by the number of validators and how operators eadch assinged. Of course, it impacts the processing time of an operator whether it's must answer to all messages or not. But it doesn't impact how many messages it receives.

Suppose we keep active only one validator (and 4 operators). The expected number of messages is still 0.6781.

If we activate one more validator, then, it becomes $0.6781\times2$, and so on.

## Expected messages for all subnets

Expanding the view, supposing an operator may belong to numerous subnets. The number of expected messages becomes $0.6781 \times V$ where V is the total number of validators in all subnets (supposing each validator has 4 operators assigned).

For example, if there were $10000$ validators, we would have
$$
10000 \times 0.6781 = 6781 \text{ messages per slot} = 565 \text{ messages per second}
$$



