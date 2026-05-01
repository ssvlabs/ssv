# TBFT audit (residual)

Aggregated review of `docs/TBFT.md`, `docs/TBFT2.md`, and `docs/TBFT-comparison.md` against the implementation under `protocol/v2/tbft/` and `protocol/v2/ssv/runner/tbft/`. The original audit listed several findings; this document is the residual after the spec-rewrite landing in the same change.

## Status of original findings

**Closed by the spec rewrite** (see [TBFT.md](TBFT.md)):

- **P1.1** (tag indexing) — TBFT.md now uses 0-based indexing with distinct `enc_tag_k` / `nr_tag_k` symbols, matching the implementation.
- **P1.2** (candidate authenticity) — TBFT.md adds leader-authenticated candidates and the equivocation-to-non-receipt rule. Implementation work tracked in [TASKS.md](TASKS.md).
- **P1.3** (validity precondition) — TBFT.md adds an explicit "Preconditions on the host application" section enumerating SSV's validation rules.
- **P1.4** (one-sig-per-instance scope) — TBFT.md caveat 8 tightens the claim with the listed preconditions and documents the per-share multi-block-signing pattern.
- **P1.5 first bullet** (cross-onion partial-sig equivocation) — TBFT.md caveat 2 lists this as the third inconsistency-slashing rule. Implementation work in [TASKS.md](TASKS.md).
- **P2.1** (equivocation detection overstated) — TBFT.md Properties summary now describes the actual mechanism (leader-signed candidates → equivocation-to-NR rule → slashable evidence).

**Closed at n=4 (and narrowed at n ≥ 7) by the leader-publishes-σ-on-V Phase-1 mechanism:**

- **P0.2** — selective-delivery grief in TBFT2 at n=4. Closed mechanically.
- **P0.1** — selective-delivery grief in TBFT. Closed at n=4; residual `[f+1, 2f-1]` window at n ≥ 7.

**Partially addressed** (residual below):

- **P1.5 second bullet** — hidden equivocation in unopened layers.

**Open** (no spec-rewrite action; tracked here):

- **P2.2** — no final-certificate gossip.
- **P2.3** — end-to-end adversarial timing budget.
- **P2.4** — operational items (some addressed by the TBFT2 / TBFT-comparison rewrites).
- **P3** — smaller cleanups (some closed below).

## What still holds (reaffirmed)

- The pigeonhole safety argument in TBFT.md "Why it's safe" still holds, now in the form: at any layer, σ-quorum (`qV = 2f+1`) and NR-quorum (`qEnc = f+1`) cannot both be reached given honest non-cross-signing plus byzantine being deterred from σ+NR cross-signing by slashing. The slashing assumption is now **load-bearing** under threshold separation; see TBFT.md "Why it's safe" and caveat 2.
- Single-RTT decision path vs QBFT's three.
- V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair is a new separate DKG at threshold `qEnc = f+1`, run once at cluster init.

## Residual P0 findings

### P0.1 Selective-delivery grief in TBFT — closed at n=4, residual at n ≥ 7

Closed at n=4 by the leader-publishes-σ-on-V Phase-1 mechanism (TBFT.md "Phase 1 — Candidate broadcast" + caveat 1 algebra). The leader's forced threshold partial plus the f+1 honest partials in Phase 2 sum to qV exactly at f=1, leaving no grief window.

Doc-honesty actions completed in TBFT.md caveat 1:

- Framed correctly as a *deterministic byzantine-leader grief* (not a probabilistic "marginal network" caveat).
- Corrected the "K = max(3, f+1) saves the slot" framing — K guarantees an honest *successor* leader, not that the cluster recovers when a higher-priority byzantine leader griefs its own layer.
- Tightened deadline-tuning condition to P99/P999 (not P95).
- Added the algebra showing the new grief window `k ∈ [f+1, 2f-1]` (size `f-1`), which is empty at n=4 and narrows by one at every cluster size compared to the pre-leader-σ design.

Residual at n ≥ 7: a small grief window remains (1 point at n=7, 2 at n=10, 3 at n=13). The protocol-level fix that closes this residual at all cluster sizes is documented in [TBFTR.md](TBFTR.md) as the deferred-NR composition.

### P0.2 Selective-delivery grief in TBFT2 at n=4 — closed

The same leader-publishes-σ-on-V mechanism applied to TBFT2's Phase 1A (`L_b` broadcasting `V_b`) and Phase 1B (`L_p` broadcasting `V_p`) closes P0.2 at n=4 by the same algebra as P0.1 (TBFT2.md caveat 1 walk-through). Both TBFT2 leaders are forced to publish their σ-on-V along with the candidate; receivers reject malformed bundles; the cluster reaches qV = 3 at n=4 in any non-griefable `k` configuration.

[TBFT2.md](TBFT2.md) and [TBFT-comparison.md](TBFT-comparison.md) updated:

- TBFT2.md Phase 1A/1B describe the new bundle shape with both signatures.
- TBFT2.md caveat 1 walks the closed-at-n=4 algebra and notes residual at n ≥ 7 if TBFT2 is run there (it shouldn't be — see comparison recommendation table).
- TBFT-comparison.md scenario 6b at n=4 now succeeds for both TBFT and TBFT2; recommendation reasoning updated to reflect that TBFT2 wins on bandwidth/simplicity at n=4 *and* has equivalent byz resilience to TBFT (both clean at n=4).

P0.2 considered closed for the n=4 cluster size TBFT2 is targeted at.

## Residual P1 findings

### P1.5 second bullet — hidden equivocation in unopened layers

If layer 0 succeeds, layers 1+ are never decrypted, so an operator's σ+NR cross-signing at deep layers escapes detection in that execution path. Under threshold separation (`qEnc = f+1`), σ+NR cross-signing is now load-bearing for safety — undetected cross-signing at deep layers partially erodes the safety guarantee.

TBFT.md caveat 2 acknowledges this as the *path-conditional detection limit* and lists two mitigation options:

- (a) Post-protocol gossip of all-layer σ partials so deep layers can be retroactively verified for slashing material — adds a wire-format change and a post-slot gossip round.
- (b) Accept that path-conditional escape is rare relative to attacker payoff (deep layers rarely matter when upper layers succeed) — engineering choice.

No protocol-level resolution chosen; left as an engineering decision. Tracked in [TASKS.md](TASKS.md) as a follow-up.

## Open P2 findings

### P2.2 — no final-certificate gossip

[envelope.go](../protocol/v2/tbft/wire/envelope.go) defines `KindOnion`, `KindNonReceipt`, `KindCandidate`. No certificate kind. Operators may have different views; each who reconstructs submits independently to the beacon node, which de-duplicates. If only one operator's local view crossed quorum and that operator's beacon/relay path fails after reconstruction, the slot is missed even though the cluster *had* enough sigs to reconstruct.

**Action (deferred design)**: add `KindCertificate(slot, V, S)` so any operator with the certificate can submit. Wire-format change with replay/cache implications. Tracked in [TASKS.md](TASKS.md).

### P2.3 — end-to-end adversarial timing budget

TBFT.md caveat 6 states the deadline rule (`T_d − T_arrival > D + δ`, `D = P99/P999`) but doesn't commit a full end-to-end budget across pre-consensus, gossip, EKM signing, beacon submit, relay submission. Production telemetry is needed to commit numbers.

**Action**: collect production tail data for each leg; commit a budget; update timing tables in TBFT.md application section. Tracked in [TASKS.md](TASKS.md).

### P2.4 — other operational items (mostly still open)

- **Worst-of-K beacon-fetch latency for `Δ_1`.** The K leaders fetch in parallel from K distinct beacons; `Δ_1` must accommodate the slowest of K independent block-fetch RTTs. Worth a sentence in TBFT.md application section; not yet added.
- **Head-change handling during Phase 1** for TBFT's any-of-K candidates (analogous to TBFT2's `V_b` handling). Not yet specified.
- **TBFT2 "deterministic backup"** non-determinism — addressed in TBFT2.md update. Closed.
- **TBFT2 dual-leader-byzantine probabilities** — addressed in TBFT-comparison.md update. Closed.

## Open P3 findings (smaller cleanups)

- **`T_d` is named "deadline" but the protocol finalizes after it.** TBFT.md adds clarification ("`T_d` is a *view-fix point*"), but the symbol name itself is unchanged. Renaming would prevent downstream timing-budget mistakes.
- **Comparison-doc scope drift** — addressed in the TBFT-comparison.md update. Closed.
- **`K=3` floor for n=4** — addressed in the TBFT-comparison.md update. Closed.
- **Multiple submitters / beacon de-dup** — addressed in TBFT.md Phase 3 ("Multiple operators may reconstruct and submit independently; the downstream system de-duplicates"). Closed.

## Sequencing

### Done in this round

- **Spec rewrite** — TBFT.md restructured around leader-authenticated candidates, threshold separation, distinct tag symbols, validity preconditions, three-rule slashing model.
- **TBFT2.md and TBFT-comparison.md** updated for P0.2 doc-honesty and threshold separation alignment.
- **TBFTR.md** created and extended with the deferred-NR composition that targets P0.1/P0.2 protocol-level resolution.

### Implementation alignment (next)

See [TASKS.md](TASKS.md) for the breakdown of changes in `protocol/v2/tbft/` and `protocol/v2/ssv/runner/tbft/`. Priority shape:

- Protocol-correctness gaps (leader-auth, equivocation rule, sender check).
- Threshold-separation gaps (separate IBE DKG at `qEnc = f+1`; per-keypair signers and aggregation thresholds).
- Slashable-evidence collection (three rules from caveat 2).
- Tests.

### Backlog

- Final-certificate gossip (P2.2).
- End-to-end timing budget with production data (P2.3).
- Worst-of-K beacon-fetch and head-change handling (P2.4).
- Path-conditional detection mitigation choice (P1.5 second bullet).
- TBFTR composition for genuine P0.1/P0.2 closure.

## Deployment recommendation

Before mainnet: (a) the implementation work in [TASKS.md](TASKS.md) lands (especially the candidate-authenticity, equivocation-to-NR, and threshold-separation gaps), (b) the inputs feeding the deadline-tuning rule (TBFT.md caveat 1) are measured against the cluster's gossip-propagation tail, (c) operators understand that selective-delivery grief by a byzantine layer leader is a known liveness gap until TBFTR ships.
