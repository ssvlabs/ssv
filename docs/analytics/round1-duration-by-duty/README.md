# Round-1 QBFT Duration Across Duty Types

How long round-1 QBFT consensus takes — from the leader's proposal to the decided message — for all four SSV duty types, and how much of the proposer's tail is intra-cluster drift. Extends the [proposer](../proposer-decision-timing/README.md), [attester](../attester-decision-timing/README.md), and [operator-drift](../operator-qbft-start-drift/README.md) analyses.

**Window:** Ethereum **mainnet**, epochs `465250`–`472000`, 30 sampled epochs. Proposer 24,984 duties; aggregator 2,746; sync-contribution 6,897; attester 110,244.

## TL;DR

- **Round-1 *duration* is tight for proposer, aggregator, and sync-contribution; only the attester has a fat tail.** Aggregator (max **626ms**) and sync-contribution (max **701ms**) hug the proposer's curve — in fact a touch tighter — while the attester trails to ~**3.3s**.
- **That difference is structural, not incidental.** The committee (attester) runner is the only one where each operator validates the proposal against its *own* node-derived vote and does so on the *currently-arriving* head; proposer/aggregator/sync validate a self-contained value (structural-only check) built on *settled* data, so operator readiness clusters tightly.
- **The proposer's *extra* tail over aggregator/sync is intra-cluster drift, and it separates cleanly.** The Exporter records per-operator prepare times, so removing the prepare-phase readiness spread drops the proposer's bulk (p99 **191ms → 109ms**) right onto the aggregator/sync band — the intrinsic ~30–110ms QBFT latency all four share.

## The comparison

![Round-1 duration tail by duty type](./assets/round1-duration-by-duty.png)

Round-1 duration = decided − round-1 proposal (Exporter receive-times), for round-1-decided duties:

| duty type | n | p50 | p90 | p99 | p99.9 | max |
|---|--:|--:|--:|--:|--:|--:|
| proposer (observed) | 24,984 | 59 | 102 | 191 | 796 | 1,148 |
| **proposer (drift-removed)** | 24,984 | 31 | 71 | **109** | 625 | 1,095 |
| aggregator | 2,746 | 47 | 89 | 128 | 335 | 626 |
| sync-contribution | 6,897 | 55 | 86 | 127 | 344 | 701 |
| attester (committee) | 110,244 | 44 | 267 | **712** | 2,231 | 4,010* |

\* attester's genuine max is ~3,293ms; the raw 4,010ms is the epoch-boundary Exporter artifact documented in the [attester analysis](../attester-decision-timing/README.md).

The medians are all ~50ms — a healthy round-1 is a couple of gossip hops regardless of duty. The divergence is entirely in the tail.

## Why aggregator/sync match the proposer, not the attester

Two things drove the attester's fat tail, and **both are absent** for proposer, aggregator, and sync-contribution:

- **Value-check semantics.** The committee runner validates the proposed `BeaconVote` by comparing its source/target epochs against the operator's *own* node-derived vote ([`value_check.go`](../../../protocol/v2/ssv/value_check.go), `voteChecker`), so an operator can only participate once its beacon node has determined the head. Proposer, aggregator, and sync-contribution all use *structural-only* checks (`proposerChecker` / `aggregatorChecker` / `syncCommitteeContributionChecker`) — a non-leader accepts the leader's self-contained value the moment it arrives.
- **Settled vs arriving data.** The committee attests to the *current* slot's head, which is still propagating (`round1HeadStart = slotDuration/3`, ~2.3s start). Proposer builds on the *settled* parent; aggregator and sync-contribution aggregate attestations / sync-messages that arrived at 4s and are pulled at ~8s (`round1HeadStart = slotDuration/3*2`, [`timer.go`](../../../protocol/v2/qbft/roundtimer/timer.go)). Waiting on an arriving block is inherently more variable than building on settled data.

So aggregator/sync decide *late* (~8.1s into slot, vs proposer's ~1.4s and attester's ~2.4s), but *how long consensus takes* once proposed is tight. Round changes are correspondingly low: aggregator 0.18% (≈ proposer's 0.17%), sync-contribution 0.82%, vs attester 1.09%.

## Isolating the drift

In the proposer's slowest round-1s the **prepare spread is ~1,145ms while the commit spread is ~18ms**: the leader prepares immediately, then the quorum waits ~1s for the *marginal operator's prepare* (its block-fetch readiness — the same clock/fetch drift measured in the [drift analysis](../operator-qbft-start-drift/README.md)), and commits fire fast once 2f+1 prepares exist. So the tail lives in the prepare phase.

Removing it per duty — `drift-removed = observed − (2f+1-th prepare − fastest non-leader prepare)` — collapses the proposer's bulk onto the aggregator/sync band (dashed line; p50 59→31, p99 191→109). Since aggregator/sync still carry their own small drift, the fully drift-removed proposer is even a shade tighter — all three converge on the intrinsic ~30–110ms QBFT latency, exactly as expected.

## Method & caveats

- **Duration is Exporter receive-times** of the round-1 proposal and the aggregated decided message; the decided is gated by the marginal (2f+1-th) operator to reach quorum.
- **The drift removal isolates single-straggler drift** (the common case). A small residual tail remains (drift-removed p99.9 625ms, max 1,095ms) from duties where even the *second*-fastest operator was slow — multiple operators or a whole-cluster stall, which is not pairwise drift.
- **Aggregator is sampled** across the validator population (~3–4% are selected per duty); sync-contribution is targeted at each epoch's sync-committee validators. Both are per-validator duties, unlike the committee-level attester.

## Reproduce

```
# aggregator + sync-contribution round-1 timing (needs an ssv-scout workspace + vnet)
cd scripts
SSV_SCOUT_DIR=/path/to/ssv-scout OUT_DIR=./data python collect.py 465250 472000
# drift-removed proposer duration (reuses the proposer analysis raw traces)
PROP_DATA=../../proposer-decision-timing/scripts/data python drift.py
# comparison table + chart (reuses proposer + attester datasets)
python compare.py
```
