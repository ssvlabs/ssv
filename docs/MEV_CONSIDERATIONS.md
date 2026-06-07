# MEV considerations

## TL;DR

To get the most out of MEV opportunities, configure `timing games on the PBS layer` — either mev-boost v1.11+ launched with `-config <path>` (and optionally `-watch-config` for hot reload), or commit-boost. With PBS-side timing games configured, SSV's `ProposerDelay` should stay at its default value of `0` — operators relying on `ProposerDelay` can't also use the multi-BN bid scoring described in [Multi-BN setup](#multi-bn-setup).

If your PBS does not support timing games (mev-boost < v1.11, mev-boost without `-config <path>`, or any other PBS lacking the feature), SSV's `ProposerDelay` is still available — see [Appendix A](#appendix-a--legacy-proposerdelay-approach). PBS-side timing games are preferred because the PBS polls each relay multiple times within a precise slot-relative auction window — yielding higher-value bids than a single `getHeader` call after an SSV-side `ProposerDelay` sleep.

**Do NOT apply both**: `timing games on the PBS layer` configuration + SSV's `ProposerDelay / ProposalSoftTimeout` - only one of these is supposed to run at any given time. Here are the recommended configuration steps, to avoid any sort of undesirable downtime during transition:
- configure SSV node first to remove/unset any of `ProposerDelay / ProposalSoftTimeout`
- if you run multiple Beacon nodes, set `ProposalSoftDeadline = your PBS late_in_slot_time_ms + ~50ms BN→SSV transport` - see [Multi-BN setup](#multi-bn-setup) for details, single-BN operators can skip that section entirely
- restart SSV node to apply
- set/update mev/commit-boost configuration settings to enable `timing games on the PBS layer` - see [PBS configuration settings](#configuration-knobs) for details
- it is desirable for all SSV nodes in the same cluster to run the same/similar configuration (very large differences may lead to missed duties)

## Definitions and typical values

The variables below name the stages of the SSV proposer-duty timeline. The values are typical/average for a healthy mainnet SSV cluster — real-world variance is significant, and operators should baseline their own latencies (see [Tuning guidance](#tuning-guidance--measurement-methodology)) before treating them as hard numbers.

| Variable | Typical | Description |
|---|---|---|
| `RANDAO` | ~50ms | Pre-consensus phase: SSV operators build the RANDAO signature used in the block-fetch request. |
| `(auction window)` | varies | PBS-side relay polling. Configurable; see [PBS-side timing games](#pbs-side-timing-games-recommended). |
| `MEVBoostRelayTimeout` | ~200ms | *Legacy path only:* mev-boost's single getHeader call when SSV asks for a block. Replaced by `(auction window)` in PBS-timing-games setups. |
| `QBFT` | ~2350ms worst case | QBFT consensus over the blinded block. Worst-case decomposes into `QBFTRound1Time` (~2000ms round-1 timer, fires if round 1 fails) + `QBFTRoundChange` (~100ms ROUND-CHANGE handshake) + `QBFTRound2Time` (~250ms successful round 2). |
| `PostConsensusSigning` | ~50ms | Operators reconstruct the validator BLS signature from partial signatures. |
| `BlockSubmission` | ~100ms | Leader submits the signed blinded block to the BN; relay reveals the payload; block propagates. |

The Ethereum slot-propagation deadline is **4000ms** after slot start.

## SSV proposer-duty flow background

The proposer-duty flow runs the stages above in sequence. For an SSV cluster to function reliably, the following must hold even in the worst case where round 1 fails and round 2 runs as fallback:

```
RANDAO + (auction window) + QBFT + PostConsensusSigning + BlockSubmission < 4000ms
```

You must budget for the worst case: in the common case round 1 succeeds quickly and round 2 never runs, but the budget reserved by the equation cannot be reclaimed. If the equation doesn't hold, the validator risks missing its proposal slot whenever round 1 fails.

Where `(auction window)` sits in the slot is what determines MEV capture — bid value grows as the slot ages, so later auction windows yield higher-value bids on average, subject to staying within this deadline.

## PBS-side timing games (recommended)

The PBS layer implements "timing games" — proactively polling relays at intervals defined in its own config, decoupling *when* the auction happens from *when* SSV asks for the block. SSV asks once and receives whatever bid the PBS has selected by its slot-relative cutoff.

Preferred over `ProposerDelay` because:
- The PBS polls each relay multiple times within the auction window (`target_first_request_ms` + `frequency_get_header_ms`), capturing higher-value bids than a single `getHeader` per relay. `ProposerDelay` + single `getHeader` gets one bid per relay at one point in time; PBS-side timing games sample several and keep the best.
- The auction cutoff is slot-relative (`late_in_slot_time_ms`), so QBFT round 1 starts at a predictable point in the slot. With `ProposerDelay`, QBFT starts at `ProposerDelay + variable relay-response time`, which makes the post-auction budget harder to size.
- The PBS handles auction-timing risk internally. If all polls time out or no relay responds, the PBS falls back to a local block (vanilla), not a missed slot.
- Configuration is concentrated in the PBS rather than split across SSV-side and PBS-side knobs.

### Configuration knobs

Both mev-boost and commit-boost expose the same five knobs:

- `timeout_get_header_ms` — per-relay-request timeout for a single `getHeader` call.
- `late_in_slot_time_ms` — slot-relative hard cutoff. The PBS returns to the caller no later than this point in the slot.
- `enable_timing_games` (per-relay) — opt in to the multi-poll behavior for this relay. Defaults to `false`; must be set per-relay.
- `target_first_request_ms` — when the first poll for this relay fires, measured from slot start.
- `frequency_get_header_ms` — interval between subsequent polls.

The effective per-request deadline is:
```
max_timeout_ms = min(timeout_get_header_ms, late_in_slot_time_ms - ms_into_slot)
slot-relative cutoff = ms_into_slot + max_timeout_ms
```
When the PBS receives the request early in the slot, `timeout_get_header_ms` tends to bind; when asked later, `late_in_slot_time_ms - ms_into_slot` binds.

### PBS-specific notes

- **commit-boost** validates `timeout_get_header_ms < late_in_slot_time_ms` at config load — refuses to start otherwise. Set `timeout_get_header_ms` just below `late_in_slot_time_ms`.
- **mev-boost (v1.11+)** has the same knobs and budget math but does not enforce that inequality — values may be equal. Requires `-config <path>` to enable the YAML-based timing-games config; `-watch-config` enables hot reload.
- Default `late_in_slot_time_ms`: mev-boost `2000ms`, commit-boost `3000ms`. Both are aggressive.
- mev-boost selects the most-recently-received bid per relay, then compares across relays for the highest value.

Upstream references:
- mev-boost: [github.com/flashbots/mev-boost/blob/main/docs/timing-games.md](https://github.com/flashbots/mev-boost/blob/main/docs/timing-games.md)
- commit-boost: [commit-boost.github.io/commit-boost-client](https://commit-boost.github.io/commit-boost-client/)

## Configuration examples

Two scenarios shown for both PBSes. The numbers are starting points for a healthy mainnet cluster — operators should validate against their own measured latencies (see [Tuning guidance](#tuning-guidance--measurement-methodology)).

### Example A — bid-sample equivalent of legacy `ProposerDelay ≈ 1000ms` (recommended starting point)

Lands the last relay poll at ~1000ms, matching when legacy `ProposerDelay = 1000ms` would have queried the relays. Useful as a migration baseline — same bid quality, but the header arrives at SSV at ~1100ms (1050ms PBS cutoff + ~50ms BN→SSV) instead of legacy's ~1300–2000ms (depending on relay response speed), leaving more slot budget for QBFT and submission.

The polling pattern (`target_first_request_ms = 700`, `frequency_get_header_ms = 150`) fires polls at 700ms, 850ms, and 1000ms.

**commit-boost** (TOML):
```toml
[pbs]
late_in_slot_time_ms = 1050
timeout_get_header_ms = 1030        # must be < late_in_slot_time_ms in commit-boost
timeout_get_payload_ms = 4000

[[relays]]
url = "https://<relay-pubkey>@relay-1.example"
enable_timing_games = true
target_first_request_ms = 700       # polls at 700ms, 850ms, 1000ms
frequency_get_header_ms = 150

[[relays]]
url = "https://<relay-pubkey>@relay-2.example"
enable_timing_games = true
target_first_request_ms = 700
frequency_get_header_ms = 150
```

**mev-boost** (YAML):
```yaml
timeout_get_header_ms: 1050
late_in_slot_time_ms: 1050          # mev-boost permits equality
relays:
  - url: https://<relay-pubkey>@relay-1.example
    enable_timing_games: true
    target_first_request_ms: 700
    frequency_get_header_ms: 150
  - url: https://<relay-pubkey>@relay-2.example
    enable_timing_games: true
    target_first_request_ms: 700
    frequency_get_header_ms: 150
```

**SSV-side** (multi-BN setups only — see [Multi-BN setup](#multi-bn-setup); single-BN operators skip this):
```yaml
eth2:
  ProposalSoftDeadline: 1100ms   # = PBS late_in_slot_time_ms (1050ms) + ~50ms BN→SSV transport
```

### Example B — aggressive: PBS-side cutoff at 1800ms (round 1 must succeed)

Pushes the PBS-side cutoff to `1800ms` — past the ~1450ms threshold where round-2 QBFT fallback may no longer fit within the slot for typical clusters. This accepts "round 1 must succeed" in exchange for capturing more intra-slot bid growth (clusters with measurably faster QBFT + submission may still leave room for round 2). Last relay poll at ~1600ms; header at SSV by ~1850ms.

The polling pattern (`target_first_request_ms = 1000`, `frequency_get_header_ms = 200`) fires polls at 1000ms, 1200ms, 1400ms, 1600ms — four chances with ~200ms RTT margin.

Trade-off vs Example A: bid-sample time shifts ~600ms later, capturing more intra-slot bid growth, but the remaining slot budget for QBFT and submission shrinks from ~2900ms to ~2150ms — below the ~2500ms typically needed for the worst-case 2-round QBFT scenario. Example B accepts that round 1 must succeed; if round 1 fails, the slot may be missed (whether it's actually missed depends on your cluster's QBFT + submission latencies). Use only after baselining your stack's round-1 success rate.

**commit-boost** (TOML):
```toml
[pbs]
late_in_slot_time_ms = 1800
timeout_get_header_ms = 1780        # must be < late_in_slot_time_ms in commit-boost
timeout_get_payload_ms = 4000

[[relays]]
url = "https://<relay-pubkey>@relay-1.example"
enable_timing_games = true
target_first_request_ms = 1000      # polls at 1000ms, 1200ms, 1400ms, 1600ms
frequency_get_header_ms = 200

[[relays]]
url = "https://<relay-pubkey>@relay-2.example"
enable_timing_games = true
target_first_request_ms = 1000
frequency_get_header_ms = 200
```

**mev-boost** (YAML):
```yaml
timeout_get_header_ms: 1800
late_in_slot_time_ms: 1800          # mev-boost permits equality
relays:
  - url: https://<relay-pubkey>@relay-1.example
    enable_timing_games: true
    target_first_request_ms: 1000
    frequency_get_header_ms: 200
  - url: https://<relay-pubkey>@relay-2.example
    enable_timing_games: true
    target_first_request_ms: 1000
    frequency_get_header_ms: 200
```

**SSV-side** (multi-BN setups only — 1850ms triggers the safe-max startup warning since it exceeds the ~1450ms threshold; see [Multi-BN setup](#multi-bn-setup); single-BN operators skip this):
```yaml
eth2:
  ProposalSoftDeadline: 1850ms   # = PBS late_in_slot_time_ms (1800ms) + ~50ms BN→SSV transport
```

## Tuning guidance & measurement methodology

The example configs are starting points. Production tuning requires measuring your own stack — relay RTTs, QBFT consensus times, and submission latencies vary enough between operators that a single recommended value isn't optimal for everyone.

### Where the auction window should land

Bid value grows through the slot, so the auction cutoff should be as late as possible, subject to:

- **Round-2 fallback should fit:** `QBFT + PostConsensusSigning + BlockSubmission < 4000ms − late_in_slot_time_ms − ~50ms` (the ~50ms covers BN→SSV transport between the PBS cutoff and SSV receiving the header). Using the typical values from [Definitions](#definitions-and-typical-values), the post-cutoff budget needed is ~2500ms, giving a strict bound of `late_in_slot_time_ms ≲ ~1450ms`. **Recommended:** stay at `late_in_slot_time_ms ≲ ~1400ms` to keep a 50ms buffer for latency variance — this also matches SSV's startup-warning threshold (`SafeMaxProposalSoftDeadline = 1450ms` SSV-side, which equals `~1400ms` PBS-side plus the `~50ms` BN→SSV transport).
- **Cutoffs above ~1400ms** consume the variance buffer; SSV emits a startup warning. **Cutoffs above ~1450ms** are past the strict bound and accept that round 1 must succeed — if round 1 fails, the slot may be missed (depending on your cluster's QBFT + submission latencies). Example B (1800ms) sits in this regime.
- **Round-1-only variance buffer:** even in the round-1-must-succeed regime, cutoffs much beyond ~3000ms tighten the slot enough that occasional latency spikes risk missing the deadline even when round 1 succeeds.

### What to measure

Useful signals to baseline before tuning, by data source:

**On the SSV side** — metrics on Grafana (if export is enabled) and structured logs:
- **RANDAO completion time** — pre-consensus duration.
- **QBFT round-1 completion distribution** — consensus duration.
- `"got beacon block proposal"` log with `took` duration.
- `"received proposal"` debug log with `score`, `latency`, `blinded`, `pending` fields — emitted per BN response in multi-BN setups.
- `"successfully finished duty processing"` log with pre-consensus, consensus, and post-consensus splits.

**On the PBS side** — PBS logs:
- BN → PBS RTT — typically same machine, well under 10ms.
- Per-relay RTT distribution (p50/p95/p99) — logged per `getHeader` call.

**End-to-end** — submission round-trip from the signed block leaving SSV through the relay payload-reveal step (visible from PBS and relay logs).

## Multi-BN setup

> Single-BN operators can skip this section — SSV bypasses parallel fetch entirely and calls the single BN directly regardless of which knobs are set below.

With multiple Beacon nodes, SSV races them in parallel for the block proposal. The recommended action is to set `ProposalSoftDeadline`:

```yaml
eth2:
  ProposalSoftDeadline: <your PBS late_in_slot_time_ms + ~50ms BN→SSV transport>
```

This makes SSV wait for all BN responses up to that slot-relative deadline and return the highest-scored bid. Valid range `[1000ms, 3600ms]`; values above ~1450ms emit a startup warning — for typical clusters, the worst-case 2-round QBFT scenario may no longer fit within the slot, so round 1 effectively has to succeed.

### Default behavior (if you don't set `ProposalSoftDeadline`)

SSV returns as soon as one BN delivers a blinded (MEV) block — treating the first blinded response as the chosen MEV bid. If no blinded response arrives by the default slot-relative deadline (1450ms — the largest safest deadline for typical clusters; see [Tuning guidance](#tuning-guidance--measurement-methodology)), SSV returns the best non-blinded response collected so far, waiting for the first valid response if nothing usable arrived.

This default is faster but doesn't compare bid *values* across BNs — the first BN to return blinded wins regardless of bid quality. Fine for multi-BN setups run primarily for redundancy.

### Legacy approach

Setting `ProposerDelay` or `ProposalSoftTimeout` selects legacy block-fetch behavior (preserved bit-for-bit) — see [Appendix A](#appendix-a--legacy-proposerdelay-approach). SSV logs a startup warning suggesting migration.

### Interaction

The approaches are mutually exclusive. Selection at startup:

```
if ProposerDelay > 0 || ProposalSoftTimeout is set:
    -> legacy approach (see Appendix A)
elif ProposalSoftDeadline is set:
    -> new approach (waits for all BN responses, picks highest-scored)
else:
    -> new approach default (returns first blinded response)
```

Setting `ProposalSoftDeadline` together with either legacy knob (`ProposerDelay` or `ProposalSoftTimeout`) is rejected at startup with a clear error — pick one approach.

## Appendix A — Legacy `ProposerDelay` approach

`ProposerDelay` remains supported. It is the right tool when:
- Your PBS does not support timing games (mev-boost < v1.11, or any PBS without the feature).
- You are running mev-boost without `-config <path>` and don't want to introduce a YAML config file.
- Operator constraints prevent PBS-side configuration changes.

To configure, set in the SSV config file (or via the `PROPOSER_DELAY` environment variable):
```yaml
ProposerDelay: 300ms
```

With `ProposerDelay` active, the slot-budget equation becomes:
```
RANDAO + ProposerDelay + MEVBoostRelayTimeout + QBFT + PostConsensusSigning + BlockSubmission < 4000ms
```

Using the typical values from [Definitions](#definitions-and-typical-values), `ProposerDelay ≤ 4000ms − (50 + 200 + 2350 + 50 + 100) = 1250ms` is the theoretical maximum. In practice, latency variance can easily add several hundred ms — we consider **~700ms** the maximum reasonable value for `ProposerDelay` on Ethereum mainnet, leaving ~550ms of headroom for variance.

We recommend starting with a small value such as 300ms and increasing gradually while monitoring miss rate.

**The SSV node refuses to start if `ProposerDelay` is set higher than 1000ms without explicit confirmation.** This 1000ms hard stop sits above the ~700ms recommended ceiling: values in the 700–1000ms range start without complaint, but you should only push past ~700ms after baselining your cluster's latencies.

If you attempt to use a `ProposerDelay` value higher than 1000ms, the node exits with an error message. If you understand the risks and want to proceed anyway, set the `AllowDangerousProposerDelay` flag:

```yaml
ProposerDelay: 2000ms
AllowDangerousProposerDelay: true
```

Or via environment variables:
```bash
PROPOSER_DELAY=2000ms ALLOW_DANGEROUS_PROPOSER_DELAY=true ./bin/ssvnode start-node
```

**Warning:** `ProposerDelay` values higher than 1000ms significantly increase the risk of missed block proposals, which can result in penalties and lost rewards.
