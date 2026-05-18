# MEV considerations

## TL;DR

To get the most out of MEV opportunities, configure **timing games on the PBS layer** — either mev-boost v1.11+ launched with `-config <path>` (and optionally `-watch-config` for hot reload), or commit-boost. With PBS-side timing games configured, SSV's `ProposerDelay` should stay at its default value of `0`.

If your PBS does not support timing games (mev-boost < v1.11, mev-boost without `-config`, or any other PBS lacking the feature), the SSV-side `ProposerDelay` configuration is still available — see Appendix A below. PBS-side timing games are the preferred path because they don't consume SSV's slot budget for the auction wait.

## SSV proposer-duty flow background

To understand how MEV configuration interacts with SSV, here is the proposer-duty flow:
- SSV nodes participate in the pre-consensus phase to build a RANDAO signature that will be used when requesting the block from the Beacon node (call it `RANDAOTime`).
- The current round Leader requests the blinded block header from the Beacon node, which proxies the request to the PBS layer (mev-boost or commit-boost). The PBS in turn queries one or more relays.
- The PBS returns the chosen block header, and the SSV cluster goes through the QBFT consensus phase to sign it as Validator (call it `QBFTTime`).
- QBFT may require multiple rounds in case of round-leader faults. Each round can take up to `RoundTimeout` (currently 2000ms on the SSV protocol level), so QBFT is allowed at most 2 rounds before the Ethereum block-propagation deadline of 4000ms after slot start.
- Once QBFT completes, the Operator submits the signed block to the Beacon node for propagation (call it `BlockSubmissionTime`).
- A small additional overhead (`MiscellaneousTime`) covers the glue code wiring this together.

For an SSV cluster to function reliably, the following must hold:
```
RANDAOTime + (auction window) + QBFTTime + BlockSubmissionTime + MiscellaneousTime < 4000ms
```
If this doesn't hold, the validator misses its proposal slot (the block must propagate within 4000ms after slot start).

For QBFT to complete in a single round — the common and desirable case — the following tighter constraint also has to hold:
```
RANDAOTime + (auction window) + QBFTTime + MiscellaneousTime < 2000ms
```

Where the "auction window" sits in the slot is what determines MEV capture — bid value grows as the slot ages, so later auction windows yield higher-value bids on average, subject to staying within these deadlines.

## PBS-side timing games (recommended)

The PBS layer implements "timing games" — proactively polling relays at intervals defined in its own config, decoupling *when* the auction happens from *when* SSV asks for the block. SSV asks once and receives whatever bid the PBS has selected by its slot-relative cutoff.

This is preferred over `ProposerDelay` because:
- The SSV node doesn't sit idle during the auction wait — its slot clock doesn't advance, so QBFT round 1 isn't squeezed.
- The PBS layer handles auction-timing risk internally. If all polls time out or no relay responds, the PBS falls back to a local block (vanilla), not a missed slot.
- Configuration is concentrated in the PBS, rather than coordinated across SSV-side and PBS-side knobs.

### Configuration knobs

Both mev-boost and commit-boost expose the same five knobs with identical names:

- `timeout_get_header_ms` — per-relay-request timeout for a single `getHeader` call.
- `late_in_slot_time_ms` — slot-relative hard cutoff. The PBS returns to the caller no later than this point in the slot.
- `enable_timing_games` (per-relay) — opt in to the multi-poll behavior for this relay. Defaults to `false` in both PBSes; must be set per-relay.
- `target_first_request_ms` — when the first poll for this relay fires, measured from slot start.
- `frequency_get_header_ms` — interval between subsequent polls for this relay.

The effective per-request deadline is:
```
max_timeout_ms = min(timeout_get_header_ms, late_in_slot_time_ms - ms_into_slot)
slot-relative cutoff = ms_into_slot + max_timeout_ms
```
When the PBS receives the request early in the slot, the per-request `timeout_get_header_ms` tends to bind. When asked later, `late_in_slot_time_ms - ms_into_slot` binds, and the slot-relative cutoff equals `late_in_slot_time_ms`.

### PBS-specific notes

- **commit-boost** validates `timeout_get_header_ms < late_in_slot_time_ms` at config load — refuses to start otherwise. Set `timeout_get_header_ms` just below `late_in_slot_time_ms` so the slot-relative cutoff binds for any realistic ask time.
- **mev-boost (v1.11+)** has the same knobs and the same budget math, but does not enforce that strict inequality — values may be equal. mev-boost requires `-config <path>` to enable the YAML-based timing-games config; `-watch-config` enables hot reload.
- Default `late_in_slot_time_ms`: mev-boost `2000ms`, commit-boost `3000ms`. Both are aggressive defaults assuming a well-tuned QBFT and BN setup.
- mev-boost selects the most-recently-received bid per relay, then compares across relays for the highest value.

Upstream references:
- mev-boost timing-games doc: [github.com/flashbots/mev-boost/blob/main/docs/timing-games.md](https://github.com/flashbots/mev-boost/blob/main/docs/timing-games.md)
- commit-boost docs: [commit-boost.github.io/commit-boost-client](https://commit-boost.github.io/commit-boost-client/)

## Configuration examples

Two scenarios, each shown for both PBSes. The starting numbers below are reasonable defaults for a healthy mainnet cluster — operators should validate them against their own measured latencies before adopting (see [Tuning guidance](#tuning-guidance--measurement-methodology)).

### Example A — "block header at SSV by ~1500ms" (recommended safe default)

Targets a PBS-side cutoff of `1450ms`; with ~50ms overhead between PBS → BN → SSV, the header arrives at SSV by ~1500ms. (This assumes BN and PBS are co-located with SSV; a remote BN adds network RTT and the 50ms allowance should be widened accordingly.) That leaves headroom for QBFT round 1 to complete before the 2000ms round-1 deadline in a healthy cluster, while still positioning the auction window late enough to capture a meaningful fraction of intra-slot bid growth.

The relay polling pattern (`target_first_request_ms = 700`, `frequency_get_header_ms = 200`) fires polls at 700ms, 900ms, 1100ms, and 1300ms — four chances per relay, with ~150ms RTT margin for the last poll to complete before the cutoff.

**commit-boost** (TOML):
```toml
[pbs]
late_in_slot_time_ms = 1450
timeout_get_header_ms = 1430        # must be < late_in_slot_time_ms in commit-boost
timeout_get_payload_ms = 4000

[[relays]]
url = "https://<relay-pubkey>@relay-1.example"
enable_timing_games = true
target_first_request_ms = 700       # polls at 700ms, 900ms, 1100ms, 1300ms
frequency_get_header_ms = 200

[[relays]]
url = "https://<relay-pubkey>@relay-2.example"
enable_timing_games = true
target_first_request_ms = 700
frequency_get_header_ms = 200
```

**mev-boost** (YAML):
```yaml
timeout_get_header_ms: 1450
late_in_slot_time_ms: 1450          # mev-boost permits equality
relays:
  - url: https://<relay-pubkey>@relay-1.example
    enable_timing_games: true
    target_first_request_ms: 700
    frequency_get_header_ms: 200
  - url: https://<relay-pubkey>@relay-2.example
    enable_timing_games: true
    target_first_request_ms: 700
    frequency_get_header_ms: 200
```

### Example B — "close equivalent of `ProposerDelay = 1000ms`"

`ProposerDelay = 1000ms` on the legacy path causes SSV to wait 1000ms before asking the PBS, which then queries each relay once at t≈1000ms. The PBS-timing-games equivalent below lands the last poll at ~1000ms — same auction-window position — without burning SSV's slot budget. QBFT round 1 starts as soon as the header arrives (around 1050ms) instead of after a 1000ms idle wait.

This is more conservative than Example A; useful for operators migrating from a legacy `ProposerDelay = 1000ms` configuration who want to swap to timing games with minimal behavioral change.

**commit-boost**:
```toml
[pbs]
late_in_slot_time_ms = 1050
timeout_get_header_ms = 1030
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

**mev-boost**:
```yaml
timeout_get_header_ms: 1050
late_in_slot_time_ms: 1050
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

## Tuning guidance & measurement methodology

The example configs are starting points. Tuning these knobs in production requires measuring your own stack — relay RTTs, QBFT consensus times, and submission latencies vary enough between operators that a single recommended value won't be optimal for everyone.

### Where the auction window should land

Bid value grows through the slot: more transaction order flow becomes available, more arbitrage opportunities resolve, and builders accumulate higher-quality bundles. So the auction cutoff should be as late as possible, subject to:
- `QBFT + submission < 4000ms − cutoff`. The block must propagate by 4000ms after slot start.
- Ideally, round 1 completes within the 2000ms round-1 deadline. Cutoffs much beyond ~1500ms push round 1 past its deadline and force round-2 leadership every slot.

Example A's 1500ms is the recommended starting point. Example B's ~1050ms is more conservative — useful while you learn your stack's behavior under timing games.

### What to measure first

Before changing knobs, baseline these values:

- **RANDAO completion time** — how long pre-consensus takes. Visible via `measurements.PreConsensusTime()`.
- **BN → PBS RTT** — typically same machine, well under 10ms.
- **Per-relay RTT distribution (p50/p95/p99)** — PBSes log this.
- **QBFT round-1 completion distribution** — via `measurements.ConsensusTime()`.
- **Submission round-trip** — includes the relay payload-reveal step.

### SSV telemetry

Relevant logs and metrics already emitted by SSV:

- `"got beacon block proposal"` log with `took` duration, in `protocol/v2/ssv/runner/proposer.go`.
- `"received proposal"` debug log with `score`, `latency`, `blinded`, and `pending`, in `beacon/goclient/proposer.go`. Emitted per BN response in multi-BN setups.
- `"successfully finished duty processing"` log with pre-consensus, consensus, and post-consensus splits.

For multi-BN setups, per-BN scoring visibility comes from the parallel-fetch path in `beacon/goclient/proposer.go`.

### Mainnet ground truth

For quantifying MEV capture on mainnet, the relay data APIs are authoritative:

- `/relay/v1/data/bidtraces/proposer_payload_delivered?proposer_pubkey=<pk>` — what bid was delivered to your validator, with timestamps.
- `/relay/v1/data/bidtraces/builder_blocks_received?slot=<n>` — every bid the relay saw for a given slot.

A useful capture-efficiency metric: `delivered_value / max_bid_at_T`, where T is your auction cutoff time. This lets you compare different configurations on equal footing.

### Mainnet vs testnet

Testnet relays (Hoodi, Holesky, Sepolia) typically run reference or synthetic builders, and their bid distributions don't reflect mainnet economics. Use testnet for end-to-end plumbing validation only — proposer reliability, correct config parsing, no missed slots. For MEV-uplift quantification, use mainnet validator data + relay-data APIs.

### Iteration discipline

- Start with PBS defaults; tighten `late_in_slot_time_ms` toward later values gradually.
- Change one knob per iteration window.
- Monitor miss rate alongside bid-value distribution; back off if miss rate degrades.
- Allow enough observation time — proposals are sparse (roughly one per validator per month on mainnet), so small validator sets need long windows for statistical signal.

### Multi-BN caveat

The parallel-fetch logic in `beacon/goclient/proposer.go` exits as soon as one BN returns a blinded block, even if a slower BN would have returned a higher-scoring bid. With timing-games-capable PBSes on multiple BNs, the fastest BN's bid effectively wins regardless of score. Worth knowing if you're running redundant BN setups and expecting cross-BN bid scoring to matter.

## Interaction with `ProposerDelay`

When PBS-side timing games are configured, set `ProposerDelay = 0` (the default). Stacking is redundant — both mechanisms position the auction window in the slot, but only one should do so. Setting both also triggers SSV's `proposalSoftTimeout -= proposerDelay` reduction in `beacon/goclient/options.go`, which can shrink the multi-BN scoring window unnecessarily.

## Appendix A — `ProposerDelay` (legacy approach)

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
RANDAOTime + ProposerDelay + MEVBoostRelayTimeout + QBFTTime + BlockSubmissionTime + MiscellaneousTime < 4000ms
```

Plugging in realistic numbers:
```
RANDAOTime           ≈ 100ms
MEVBoostRelayTimeout ≈ 200ms
QBFTTime             ≈ 350ms
MiscellaneousTime    ≈ 150ms
BlockSubmissionTime  ≈ 1000ms
ProposerDelay        = 4000ms − (sum above) ≈ 2200ms
```
The 2200ms figure is the absolute slot-deadline budget. The tighter QBFT round-1 deadline matters more in practice:
```
RANDAOTime + ProposerDelay + MEVBoostRelayTimeout + QBFTTime + MiscellaneousTime < 2000ms
```
which gives `ProposerDelay ≤ ~1200ms` for round 1 to complete on time.

**Note:** the `MEVBoostRelayTimeout ≈ 200ms` figure above assumes the legacy single-shot PBS behavior, where mev-boost queries each relay once at the moment SSV asks. A timing-games-capable PBS uses a much larger budget here, in which case the SSV-side `ProposerDelay` lever isn't useful — see the PBS-side timing games section above.

We consider **~1200ms** the maximum reasonable value for `ProposerDelay` on Ethereum mainnet. Going beyond risks missed block proposals.

We recommend starting with a small value such as 300ms and increasing gradually while monitoring miss rate.

## Appendix B — Safety limits

**The SSV node refuses to start if `ProposerDelay` is set higher than 1000ms without explicit confirmation.**

If you attempt to use a `ProposerDelay` value higher than 1000ms, the node exits with an error message. If you understand the risks and want to proceed anyway, you must also set the `AllowDangerousProposerDelay` flag:

```yaml
ProposerDelay: 2000ms
AllowDangerousProposerDelay: true
```

Or via environment variables:
```bash
PROPOSER_DELAY=2000ms ALLOW_DANGEROUS_PROPOSER_DELAY=true ./bin/ssvnode start-node
```

**Warning:** `ProposerDelay` values higher than 1000ms significantly increase the risk of missed block proposals, which can result in penalties and lost rewards.
