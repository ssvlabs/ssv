# MEV considerations

## TL;DR

To get the most out of MEV opportunities, configure **timing games on the PBS layer** — either mev-boost v1.11+ launched with `-config <path>` (and optionally `-watch-config` for hot reload), or commit-boost. With PBS-side timing games configured, SSV's `ProposerDelay` should stay at its default value of `0`.

If your PBS does not support timing games (mev-boost < v1.11, mev-boost without `-config <path>`, or any other PBS lacking the feature), SSV's `ProposerDelay` is still available — see [Appendix A](#appendix-a--path-0-proposerdelay-legacy-approach). PBS-side timing games are preferred because they don't consume SSV's slot budget for the auction wait.

If you run **multiple Beacon nodes** and want SSV to cross-compare bids across them rather than taking the first BN's response, opt into the MEV-optimized fetch path by setting `ProposalSoftDeadline` on the SSV side (see [Configuration paths](#configuration-paths)). Single-BN setups don't need this — they bypass the parallel-fetch logic entirely.

## Definitions and typical values

The variables below name the stages of the SSV proposer-duty timeline. The values are typical/average for a healthy mainnet SSV cluster — real-world variance is significant, and operators should baseline their own latencies (see [Tuning guidance](#tuning-guidance--measurement-methodology)) before treating them as hard numbers.

| Variable | Typical | Description |
|---|---|---|
| `RANDAO` | ~100ms | Pre-consensus phase: SSV operators build the RANDAO signature used in the block-fetch request. |
| `(auction window)` | varies | PBS-side relay polling. Configurable; see [PBS-side timing games](#pbs-side-timing-games-recommended). |
| `MEVBoostRelayTimeout` | ~200ms | *Legacy path only:* mev-boost's single getHeader call when SSV asks for a block. Replaced by `(auction window)` in PBS-timing-games setups. |
| `QBFT` | ~2500ms worst case | QBFT consensus over the blinded block. Worst-case decomposes into `QBFTRound1Time` (~2000ms round-1 timer, fires if round 1 fails) + `QBFTRoundChange` (~150ms ROUND-CHANGE handshake) + `QBFTRound2Time` (~350ms successful round 2). The round timer is currently round-relative rather than slot-relative ([#2429](https://github.com/ssvlabs/ssv/issues/2429)); round-2 budget must be reserved even when round 1 typically succeeds. |
| `PostConsensusSigning` | ~150ms | Operators reconstruct the validator BLS signature from partial signatures. |
| `BlockSubmission` | ~200ms | Leader submits the signed blinded block to the BN; relay reveals the payload; block propagates. |

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
- The SSV node doesn't sit idle during the auction wait — its slot clock doesn't advance, so QBFT round 1 isn't squeezed.
- The PBS handles auction-timing risk internally. If all polls time out or no relay responds, the PBS falls back to a local block (vanilla), not a missed slot.
- Configuration is concentrated in the PBS rather than coordinated across SSV-side and PBS-side knobs.

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

**SSV-side** (optional; recommended for multi-BN setups to enable cross-BN bid scoring via Path 2 — see [Configuration paths](#configuration-paths)):
```yaml
eth2:
  ProposalSoftDeadline: 1100ms   # = PBS late_in_slot_time_ms (1050ms) + ~50ms BN→SSV transport
```

### Example B — aggressive: PBS-side cutoff at 1800ms (round 1 must succeed)

Pushes the PBS-side cutoff to `1800ms` — well past the ~1100ms threshold where round-2 QBFT fallback stops fitting within the slot. This explicitly accepts "round 1 must succeed" in exchange for capturing more intra-slot bid growth. Last relay poll at ~1600ms; header at SSV by ~1850ms.

The polling pattern (`target_first_request_ms = 1000`, `frequency_get_header_ms = 200`) fires polls at 1000ms, 1200ms, 1400ms, 1600ms — four chances with ~200ms RTT margin.

Trade-off vs Example A: bid-sample time shifts ~600ms later, capturing more intra-slot bid growth, but the remaining slot budget for QBFT and submission shrinks from ~2900ms to ~2150ms — below the ~2850ms required for the worst-case 2-round QBFT scenario. Example B accepts that round 1 must succeed; if round 1 fails, the slot is missed. Use only after baselining your stack's round-1 success rate.

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

**SSV-side** (recommended for multi-BN setups; 1850ms triggers a startup warning since it exceeds the ~1100ms safe-max for the worst-case 2-round QBFT scenario — see [Configuration paths](#configuration-paths)):
```yaml
eth2:
  ProposalSoftDeadline: 1850ms   # = PBS late_in_slot_time_ms (1800ms) + ~50ms BN→SSV transport
```

## Tuning guidance & measurement methodology

The example configs are starting points. Production tuning requires measuring your own stack — relay RTTs, QBFT consensus times, and submission latencies vary enough between operators that a single recommended value isn't optimal for everyone.

### Where the auction window should land

Bid value grows through the slot, so the auction cutoff should be as late as possible, subject to:

- **Round-2 fallback must fit:** `QBFT + PostConsensusSigning + BlockSubmission < 4000ms − late_in_slot_time_ms − ~50ms` (the ~50ms covers BN→SSV transport between the PBS cutoff and SSV receiving the header). Using the typical values from [Definitions](#definitions-and-typical-values), the post-cutoff budget required is ~2850ms, resolving to `late_in_slot_time_ms ≲ ~1100ms`. Above this threshold, a round-2 fallback can no longer complete within the slot deadline. For Path-2 operators, match `ProposalSoftDeadline` to `late_in_slot_time_ms + ~50ms`.
- **Cutoffs above ~1100ms** accept that round 1 must succeed — if round 1 fails, the slot is missed. Example B (1800ms) sits in this regime.
- **Round-1-only variance buffer:** even in the round-1-must-succeed regime, cutoffs much beyond ~2500ms tighten the slot enough that occasional latency spikes risk missing the deadline even when round 1 succeeds.

### What to measure first

- **RANDAO completion time** — `measurements.PreConsensusTime()` in `protocol/v2/ssv/runner/`. Visible on Grafana charts if exported.
- **BN → PBS RTT** — typically same machine, well under 10ms. Visible in PBS logs.
- **Per-relay RTT distribution (p50/p95/p99)** — PBSes log this on every `getHeader` call.
- **QBFT round-1 completion distribution** — `measurements.ConsensusTime()` in `protocol/v2/ssv/runner/`. Visible on Grafana charts if exported.
- **Submission round-trip** — from `SubmitBeaconBlock` in `beacon/goclient/proposer.go` through the relay payload-reveal step.

### SSV telemetry

Relevant logs and metrics:

- `"got beacon block proposal"` log with `took` duration, in `protocol/v2/ssv/runner/proposer.go`.
- `"received proposal"` debug log with `score`, `latency`, `blinded`, `pending`, in `beacon/goclient/proposer.go`. Emitted per BN response in multi-BN setups.
- `"successfully finished duty processing"` log with pre-consensus, consensus, and post-consensus splits.

### Mainnet vs testnet

Testnet relays (Hoodi, Holesky, Sepolia) typically run reference or synthetic builders; their bid distributions don't reflect mainnet economics. Use testnet for plumbing validation (reliability, config parsing, no missed slots). For MEV-uplift quantification, use mainnet validator data + relay-data APIs.

### Iteration discipline

- Start with PBS defaults; tighten `late_in_slot_time_ms` toward later values gradually.
- Change one knob per iteration window.
- Monitor miss rate alongside bid-value distribution; back off if miss rate degrades.
- Allow enough observation time — proposals are sparse (roughly one per validator per month on mainnet), so small validator sets need long windows for statistical signal.

## Configuration paths

SSV chooses one of three multi-BN block-header fetch strategies at startup based on your config. Single-BN setups bypass the parallel-fetch logic entirely and aren't affected.

### Path selection algorithm

```
if ProposerDelay > 0 || ProposalSoftTimeout is set:
    -> Path 0 (legacy)
elif ProposalSoftDeadline is set:
    -> Path 2 (MEV-optimized)
else:
    -> Path 1 (safe, default)
```

Setting `ProposalSoftDeadline` together with either legacy knob is rejected at startup — pick one.

### Path 1 — Safe (default)

Multi-BN parallel fetch with **early-exit on the first blinded response**. If no blinded response is received by the slot-relative `ProposalSoftDeadline` (default 1000ms), returns the best non-blinded response collected so far, or falls through to waiting for the first valid response.

### Path 0 — Legacy

Preserves the original `ProposerDelay` / `ProposalSoftTimeout` behavior bit-for-bit. Selected automatically when either legacy knob is set. SSV logs a startup warning suggesting migration to the new model. See [Appendix A](#appendix-a--path-0-proposerdelay-legacy-approach).

### Path 2 — MEV-optimized (opt-in)

Same as Path 1 but **without** the early-exit on the first blinded response — SSV waits for all multi-BN responses until the slot-relative `ProposalSoftDeadline`, then returns the highest-scored bid across all BNs. Useful only when multiple BNs may produce meaningfully different bids worth cross-comparing.

To enable, set `ProposalSoftDeadline` (`eth2:` block in YAML, or `WITH_PROPOSAL_SOFT_DEADLINE` env var) to match your PBS `late_in_slot_time_ms + ~50ms BN→SSV transport`. Valid range `[1000ms, 3600ms]`; values above ~1100ms emit a startup warning because the worst-case 2-round QBFT scenario can no longer fit within the slot (round 1 must succeed for the slot).

## Appendix A — Path 0 (`ProposerDelay`, legacy approach)

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

Using the typical values from [Definitions](#definitions-and-typical-values), `ProposerDelay ≤ 4000ms − (100 + 200 + 2500 + 150 + 200) = 850ms` is the theoretical maximum. In practice, latency variance can easily add several hundred ms — we consider **~700ms** the maximum reasonable value for `ProposerDelay` on Ethereum mainnet, leaving ~150ms of headroom for variance.

The safety guard at startup is the looser 1000ms cap ([Appendix B](#appendix-b--safety-limits)): values between ~700ms and 1000ms are permitted without `AllowDangerousProposerDelay` but should be considered risky and not recommended.

We recommend starting with a small value such as 300ms and increasing gradually while monitoring miss rate.

## Appendix B — Safety limits

**The SSV node refuses to start if `ProposerDelay` is set higher than 1000ms without explicit confirmation.**

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
