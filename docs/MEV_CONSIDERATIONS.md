# MEV considerations

## TL;DR

To get the most out of MEV opportunities, configure **timing games on the PBS layer** — either mev-boost v1.11+ launched with `-config <path>` (and optionally `-watch-config` for hot reload), or commit-boost. With PBS-side timing games configured, SSV's `ProposerDelay` should stay at its default value of `0`.

If your PBS does not support timing games (mev-boost < v1.11, mev-boost without `-config`, or any other PBS lacking the feature), the SSV-side `ProposerDelay` configuration is still available — see Appendix A below. PBS-side timing games are the preferred path because they don't consume SSV's slot budget for the auction wait.

## SSV proposer-duty flow background

To understand how MEV configuration interacts with SSV, here is the proposer-duty flow:
- SSV nodes participate in the pre-consensus phase to build a RANDAO signature that will be used when requesting the block from the Beacon node (`RANDAOTime`).
- The current round Leader requests the blinded block header from the Beacon node, which proxies the request to the PBS layer (mev-boost or commit-boost). The PBS in turn queries one or more relays (the *auction window*).
- The PBS returns the chosen block header, and the SSV cluster runs QBFT consensus to sign it (`QBFTRound1Time`; if round 1 faults, `QBFTRound2Time` for the fallback round). Each round has a 2000ms timer, currently measured from round start rather than slot start (see [#2429](https://github.com/ssvlabs/ssv/issues/2429)).
- After consensus, operators reconstruct the validator BLS signature from partial signatures (`PostConsensusSigningTime`).
- The leader submits the signed blinded block to the Beacon node; the relay reveals the actual execution payload, which propagates through the network (`BlockSubmissionTime`).

For an SSV cluster to function reliably, the following must hold:
```
RANDAOTime + (auction window) + QBFTRound1Time + QBFTRound2Time + PostConsensusSigningTime + BlockSubmissionTime < 4000ms
```
In the typical case where round 1 succeeds, `QBFTRound2Time = 0` and the constraint has comfortable slack. If round 1 times out, round 2 starts; in the worst case where both rounds consume their full timer, the slot deadline is at risk. If the equation doesn't hold, the validator misses its proposal slot (the block must propagate within 4000ms after slot start).

Where the auction window sits in the slot is what determines MEV capture — bid value grows as the slot ages, so later auction windows yield higher-value bids on average, subject to staying within this deadline.

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

### Example A — bid-sample equivalent of legacy `ProposerDelay ≈ 1000ms` (recommended starting point)

Lands the last relay poll at ~1000ms, matching when legacy `ProposerDelay = 1000ms` would have queried the relays. Useful as a migration baseline: the relay bids you'll see are sampled at the same moment in the slot.

This is **not** the same as legacy `ProposerDelay = 1000ms` in terms of when the header arrives at SSV — legacy would deliver the header to SSV anywhere from ~1500ms to ~2000ms (after mev-boost's `getHeaderTimeout` runs its course), whereas this configuration delivers it at ~1050ms. PBS-timing-games is strictly better at the same bid-sample time: same bid quality, more slot budget left for QBFT and submission.

The relay polling pattern (`target_first_request_ms = 700`, `frequency_get_header_ms = 150`) fires polls at 700ms, 850ms, and 1000ms — three chances per relay, with the last poll landing at the target bid-sample time.

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

### Example B — aggressive: fully use SSV's ~1800ms header-fetch buffer

SSV's `proposalSoftTimeout` (default 1800ms, defined in `beacon/goclient/options.go`) sets the wall-clock budget SSV allocates for collecting block-header responses from BNs. This example targets that full budget: PBS-side cutoff at `1800ms`, last relay poll at ~1600ms, header at SSV by ~1850ms.

The polling pattern (`target_first_request_ms = 1000`, `frequency_get_header_ms = 200`) fires polls at 1000ms, 1200ms, 1400ms, and 1600ms — four chances per relay, with ~200ms RTT margin to the cutoff.

Trade-off vs Example A: bid-sample time shifts ~600ms later in the slot, capturing meaningfully more intra-slot bid growth, but the remaining slot budget for QBFT and submission shrinks from ~2950ms (Example A) to ~2150ms. Workable for healthy clusters but leaves less buffer for latency variance — use only after baselining your stack's QBFT and submission timings.

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

## Tuning guidance & measurement methodology

The example configs are starting points. Tuning these knobs in production requires measuring your own stack — relay RTTs, QBFT consensus times, and submission latencies vary enough between operators that a single recommended value won't be optimal for everyone.

### Where the auction window should land

Bid value grows through the slot: more transaction order flow becomes available, more arbitrage opportunities resolve, and builders accumulate higher-quality bundles. So the auction cutoff should be as late as possible, subject to:
- `QBFT + post-consensus signing + submission < 4000ms − late_in_slot_time_ms − ~70ms` (the ~70ms covers BN→SSV transport and pre-QBFT blinding, both of which happen after the PBS cutoff and before QBFT can start). The block must propagate by 4000ms after slot start.
- A safety margin for variance in QBFT consensus, signing, and submission latencies. An unlucky combination of slower-than-typical components can add several hundred ms to the budget; cutoffs much beyond ~2000ms tighten the slot enough that occasional spikes risk missing the deadline.

Example A's ~1050ms cutoff is the recommended starting point — equivalent to legacy `ProposerDelay = 1000ms` in terms of when relay bids are sampled. Example B's 1800ms cutoff is the aggressive upper end — fully uses SSV's allocated header-fetch budget for maximum MEV capture, at the cost of less variance margin.

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
RANDAOTime + ProposerDelay + MEVBoostRelayTimeout + QBFTRound1Time + QBFTRound2Time + PostConsensusSigningTime + BlockSubmissionTime < 4000ms
```

Plugging in realistic numbers (typical case where round 1 succeeds):
```
RANDAOTime               ≈ 100ms
MEVBoostRelayTimeout     ≈ 200ms
QBFTRound1Time           ≈ 350ms
QBFTRound2Time           ≈ 0ms    (typically not needed)
PostConsensusSigningTime ≈ 150ms
BlockSubmissionTime      ≈ 200ms
ProposerDelay            = 4000ms − (sum above) ≈ 3000ms
```

**Note:** the `MEVBoostRelayTimeout ≈ 200ms` figure above assumes the legacy single-shot PBS behavior, where mev-boost queries each relay once at the moment SSV asks. A timing-games-capable PBS uses a much larger budget here, in which case the SSV-side `ProposerDelay` lever isn't useful — see the PBS-side timing games section above.

The 3000ms figure is the theoretical maximum assuming median latencies for every component. In practice, QBFT consensus, BN submission, and relay payload-reveal latencies all have meaningful variance — an unlucky combination can easily add several hundred ms. We consider **~1200ms** the maximum reasonable value for `ProposerDelay` on Ethereum mainnet; the ~1800ms of headroom is buffer against this variance. Going beyond risks missed block proposals.

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
