# TBFT audit (residual)

Aggregated review of `docs/TBFT.md`, `docs/TBFTR.md`, and `docs/TBFT-comparison.md` against the implementation under `protocol/v2/tbft/` and `protocol/v2/ssv/runner/tbft/`. The original audit listed several findings; this document is the residual after the spec rewrite landing in the same change.

The spec is now split by cluster size: **TBFT** is the n=4 protocol; **TBFTR** is the n≥7 protocol with the V-plaintext / Phase-2-split additions that close P0.1 at f≥2.

## Status of original findings

**Closed by the spec rewrite + cluster-size specialization:**

- **P0.1** (byzantine-leader selective-delivery grief) — Closed at all SSV cluster sizes:
  - At n=4 by [TBFT.md](TBFT.md) leader-publishes-σ-on-V mechanism (the leader's forced threshold partial + f+1 = 2 honest partials sum to qV = 3 exactly).
  - At n≥7 by [TBFTR.md](TBFTR.md) Phase-2 split: V plaintext in Phase-2a onions provides a recovery channel for missing-V honest, who then sign late σ in Phase-2b. σ count reaches qV = 2f+1.
- **P0.2** (TBFT2 at n=4) — subsumed by P0.1 closure at n=4. TBFT2.md has been deleted; [TBFT.md](TBFT.md) is now the n=4 spec.
- **P1.1** (tag indexing) — TBFT.md and TBFTR.md use 0-based indexing with distinct `enc_tag_k` / `nr_tag_k` symbols, matching the implementation.
- **P1.2** (candidate authenticity) — Both specs add leader-authenticated candidates and the equivocation-to-non-receipt rule. Implementation work tracked in [TASKS.md](TASKS.md).
- **P1.3** (validity precondition) — Both specs add an explicit "Preconditions on the host application" section enumerating SSV's validation rules.
- **P1.4** (one-sig-per-instance scope) — Both specs tighten the claim with the listed preconditions and document the per-share multi-block-signing pattern.
- **P1.5 first bullet** (cross-onion partial-sig equivocation) — Both specs list this as the third inconsistency-slashing rule. Implementation work in [TASKS.md](TASKS.md).
- **P2.1** (equivocation detection overstated) — Both specs' Properties summaries describe the actual mechanism (leader-signed candidates → equivocation-to-NR rule → slashable evidence).

**Partially addressed** (residual below):

- **P1.5 second bullet** — hidden equivocation in unopened layers (attribution-only concern; doesn't affect safety under σ+NR exclusion rule).

**Open** (no spec-rewrite action; tracked here):

- **P2.2** — no final-certificate gossip.
- **P2.3** — end-to-end adversarial timing budget.
- **P2.4** — operational items (some addressed by the TBFT-comparison rewrite).
- **P3** — smaller cleanups (some closed below).

## What still holds (reaffirmed)

- The pigeonhole safety argument in TBFT.md / TBFTR.md "Why it's safe" holds: at any layer, σ-quorum (`qV = 2f+1`) and NR-quorum (`qEnc = f+1`) cannot both be reached, made structural by the σ+NR exclusion rule at aggregation. The slashing rule is for attribution, not load-bearing for safety. See TBFT.md "Why it's safe" and caveat 1.
- Single-RTT decision path (1.5-RTT in TBFTR with Δ_2b) vs QBFT's three.
- V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair is a new separate DKG at threshold `qEnc = f+1`, run once at cluster init.

## Residual P1 findings

### P1.5 second bullet — hidden equivocation in unopened layers

If layer 0 succeeds, layers 1+ are never decrypted, so an operator's σ+NR cross-signing at deep layers escapes detection in that execution path. Under the σ+NR exclusion rule (TBFT.md "Why it's safe"), this **doesn't affect safety** — the exclusion rule applies wherever aggregation actually happens (at opened layers), and at unopened layers no aggregation occurs to be subverted. The remaining concern is purely *attribution*: undetected deep-layer cross-signers escape slashing.

TBFT.md caveat 2 acknowledges this as the *path-conditional detection limit* and lists two mitigation options:

- (a) Post-protocol gossip of all-layer σ partials so deep layers can be retroactively verified for slashing material — adds a wire-format change and a post-slot gossip round.
- (b) Accept that path-conditional escape is rare relative to attacker payoff and let unattributed faults remain unattributed (deep layers rarely matter when upper layers succeed) — engineering choice.

No protocol-level resolution chosen; left as an engineering decision for attribution coverage. Tracked in [TASKS.md](TASKS.md) as a follow-up.

## Open P2 findings

### P2.2 — no final-certificate gossip

[envelope.go](../protocol/v2/tbft/wire/envelope.go) defines `KindOnion`, `KindNonReceipt`, `KindCandidate`. No certificate kind. Operators may have different views; each who reconstructs submits independently to the beacon node, which de-duplicates. If only one operator's local view crossed quorum and that operator's beacon/relay path fails after reconstruction, the slot is missed even though the cluster *had* enough sigs to reconstruct.

**Action (deferred design)**: add `KindCertificate(slot, V, S)` so any operator with the certificate can submit. Wire-format change with replay/cache implications. Tracked in [TASKS.md](TASKS.md).

### P2.3 — end-to-end adversarial timing budget

TBFT.md caveat 6 states the deadline rule (`T_d − T_arrival > D + δ`, `D = P99/P999`) but doesn't commit a full end-to-end budget across pre-consensus, gossip, EKM signing, beacon submit, relay submission. Production telemetry is needed to commit numbers.

**Action**: collect production tail data for each leg; commit a budget; update timing tables in TBFT.md application section. Tracked in [TASKS.md](TASKS.md).

### P2.4 — other operational items (mostly still open)

- **Worst-of-K beacon-fetch latency for `Δ_1`.** The K leaders fetch in parallel from K distinct beacons; `Δ_1` must accommodate the slowest of K independent block-fetch RTTs. Worth a sentence in TBFT.md application section; not yet added.
- **Head-change handling during Phase 1** for TBFT's any-of-K candidates. Not yet specified.

## Open P3 findings (smaller cleanups)

- **`T_d` is named "deadline" but the protocol finalizes after it.** TBFT.md adds clarification ("`T_d` is a *view-fix point*"), but the symbol name itself is unchanged. Renaming would prevent downstream timing-budget mistakes.
- **Comparison-doc scope drift** — addressed in the TBFT-comparison.md update. Closed.
- **`K=3` floor for n=4** — addressed in the TBFT-comparison.md update. Closed.
- **Multiple submitters / beacon de-dup** — addressed in TBFT.md Phase 3 ("Multiple operators may reconstruct and submit independently; the downstream system de-duplicates"). Closed.

## Sequencing

### Done in this round

- **Spec rewrite, cluster-size split.** TBFT.md is now the n=4 protocol (K=2, primary + backup); TBFTR.md is the n≥7 protocol (K = max(3, f+1), with V plaintext in onions and Phase 2a/2b split). Both share leader-authenticated candidates, threshold separation, distinct tag symbols, σ+NR exclusion rule, validity preconditions, three-rule slashing model.
- **TBFT-comparison.md** updated for apples-to-apples comparison: QBFT vs TBFT (n=4) and QBFT vs TBFTR (n=7+).
- **TBFT2.md** deleted (subsumed into TBFT.md).

### Implementation alignment (next)

See [TASKS.md](TASKS.md) for the breakdown of changes in `protocol/v2/tbft/` and `protocol/v2/ssv/runner/tbft/`. Priority shape:

- Protocol-correctness gaps (leader-auth with leader-σ-V-in-Phase-1, equivocation-to-NR rule, sender check).
- Threshold-separation gaps (separate IBE DKG at `qEnc = f+1`; per-keypair signers and aggregation thresholds).
- σ+NR exclusion at aggregation (T6).
- TBFTR-specific (V plaintext in onions, Phase 2a/2b composition) for n≥7 cluster deployments.
- Slashable-evidence collection (three rules from caveat 2).
- Tests.

### Backlog

- Final-certificate gossip (P2.2).
- End-to-end timing budget with production data (P2.3).
- Worst-of-K beacon-fetch and head-change handling (P2.4).
- Path-conditional detection mitigation choice (P1.5 second bullet).

## Deployment recommendation

Before mainnet: (a) the implementation work in [TASKS.md](TASKS.md) lands for the cluster size in question (TBFT for n=4, TBFTR for n≥7), (b) the inputs feeding the deadline-tuning rule (TBFT/TBFTR caveat on deadline coordination) are measured against the cluster's gossip-propagation tail. P0.1/P0.2 are now closed at all SSV cluster sizes once the implementation matches the spec.
