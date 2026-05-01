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

**Closed by accepting the path-conditional limit:**

- **P1.5 second bullet** (hidden equivocation in unopened layers) — only relevant at K ≥ 3 (TBFTR). Doesn't affect safety under σ+NR exclusion; deep-layer cross-signers may escape attribution but cannot break safety. Engineering decision: accept the limit. Documented in [TBFTR.md](TBFTR.md) caveat 3.

**Closed by spec additions:**

- **P2.2** (final-certificate gossip) — `KindCertificate(slot, V, S)` envelope kind specified in [TBFT.md](TBFT.md) Phase 3 and [TBFTR.md](TBFTR.md) Phase 3, broadcast after successful reconstruction. Implementation tracked in TASKS.md.
- **P2.4 worst-of-K beacon-fetch** — note added to [TBFTR.md](TBFTR.md) "Timing budget" subsection (only TBFTR has K ≥ 3 parallel fetchers). Implementation tracked in TASKS.md.
- **P2.4 head-change handling** — TBFT.md (n=4) already had `L_b` refresh rule; TBFTR.md now has analogous "Head-change handling" subsection for top-K leaders. Implementation tracked in TASKS.md.

**Framework specified, numbers TBD with telemetry:**

- **P2.3** (end-to-end timing budget) — [TBFT.md](TBFT.md) and [TBFTR.md](TBFTR.md) "Application: SSV Ethereum proposer duty" sections include a "Timing budget" subsection with the per-leg structure. Concrete numbers must come from production telemetry; tracked in TASKS.md as a follow-up.

## What still holds (reaffirmed)

- The pigeonhole safety argument in TBFT.md / TBFTR.md "Why it's safe" holds: at any layer, σ-quorum (`qV = 2f+1`) and NR-quorum (`qEnc = f+1`) cannot both be reached, made structural by the σ+NR exclusion rule at aggregation. The slashing rule is for attribution, not load-bearing for safety. See TBFT.md "Why it's safe" and caveat 1.
- Single-RTT decision path (1.5-RTT in TBFTR with Δ_2b) vs QBFT's three.
- V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair is a new separate DKG at threshold `qEnc = f+1`, run once at cluster init.

## P3 findings (smaller cleanups)

- **Comparison-doc scope drift** — addressed in the TBFT-comparison.md update. Closed.
- **`K=3` floor for n=4** — addressed in the TBFT-comparison.md update. Closed.
- **Multiple submitters / beacon de-dup** — addressed in TBFT.md Phase 3 ("Multiple operators may reconstruct and submit independently; the downstream system de-duplicates"). Closed.

## Sequencing

### Done in this round

- **Spec rewrite, cluster-size split.** TBFT.md is now the n=4 protocol (K=2, primary + backup); TBFTR.md is the n≥7 protocol (K = max(3, f+1), with V plaintext in onions and Phase 2a/2b split). Both share leader-authenticated candidates, threshold separation, distinct tag symbols, σ+NR exclusion rule, validity preconditions, three-rule slashing model.
- **TBFT-comparison.md** updated for apples-to-apples comparison: QBFT vs TBFT (n=4) and QBFT vs TBFTR (n=7+).
- **TBFT2.md** deleted (subsumed into TBFT.md).
- **Final-certificate gossip** (`KindCertificate`) specified in both protocol docs.
- **Worst-of-K beacon-fetch** and **head-change handling** documented in TBFTR.md.
- **Timing budget framework** added to both protocol docs (concrete numbers TBD with telemetry).
- **Path-conditional detection limit** at deep TBFTR layers acknowledged as accepted-attribution-only.

### Implementation alignment (next)

See [TASKS.md](TASKS.md) for the breakdown of changes in `protocol/v2/tbft/` and `protocol/v2/ssv/runner/tbft/`. Priority shape:

- Protocol-correctness gaps (leader-auth with leader-σ-V-in-Phase-1, equivocation-to-NR rule, sender check).
- σ+NR exclusion at aggregation.
- Final-certificate gossip implementation (new envelope kind).
- Threshold-separation gaps (separate IBE DKG at `qEnc = f+1`; per-keypair signers and aggregation thresholds).
- TBFTR-specific (V plaintext in onions, Phase 2a/2b composition, head-change refresh) for n≥7 cluster deployments.
- Slashable-evidence collection (three rules from caveat 2).
- Tests.

### Backlog

- End-to-end timing budget with production data (P2.3 — framework specified, numbers TBD).

## Deployment recommendation

Before mainnet: (a) the implementation work in [TASKS.md](TASKS.md) lands for the cluster size in question (TBFT for n=4, TBFTR for n≥7), (b) the inputs feeding the deadline-tuning rule (TBFT/TBFTR caveat on deadline coordination) are measured against the cluster's gossip-propagation tail. P0.1/P0.2 are now closed at all SSV cluster sizes once the implementation matches the spec.
