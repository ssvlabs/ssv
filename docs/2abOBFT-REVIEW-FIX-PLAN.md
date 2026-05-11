# 2abOBFT Review-Fix + Slashing-Change Plan

**Status**: EXECUTED. Retained as a project artifact for review-trail context.

This document was the execution plan for the review-fix + slashing-change pass; all edits enumerated below have been applied. Validation steps in §Validation passed (tests green, no residual MUST-gossip language in docs or implementation). Per-execution cleanup follow-ups were addressed in the same commit.

**Scope**: Two coupled edits, executed as one pass.

1. **Reviewer concerns 1-4 + smaller items** raised on [docs/2abOBFT.md](2abOBFT.md). See "Background — Reviewer concerns" below.
2. **Slashing change** family-wide: remove the "MUST gossip slashing evidence on the wire" requirement (and the anti-amplification rate-limits that derive from it) from [OBFT.md](OBFT.md), [OBFTR.md](OBFTR.md), [2abOBFT.md](2abOBFT.md), and the associated [2abOBFT-design-notes.md](2abOBFT-design-notes.md). Replace with a MUST-log-locally requirement; log format is implementation-defined. Out-of-band aggregation (the planned manual-blacklist mechanism) consumes the logs.

This plan enumerates every concrete edit before any file is touched. Execute strictly in the order in §Execution order.

## Confirmed scope decisions

| # | Decision |
|---|---|
| 1 | Slashing change applies to **all OBFT-family docs**: OBFT.md, OBFTR.md, 2abOBFT.md, 2abOBFT-design-notes.md. OBFT-formal-verif.md has no MUST-gossip language (verified — only network-model gossipsub references); no change needed there. |
| 2 | Reviewer concern #1 ("treat verdict-equivocator as null"): **SHOULD-level recommended receiver behavior** in 2abOBFT.md, not MUST. Explicit acknowledgement that it shrinks but does not eliminate the verdict-equivocation surface. Stays as an open question #16 in design-notes for the full EKM-binding answer. |
| 3 | Log format: **implementation-defined**. Spec says "MUST log observed evidence per-rule"; schema and retention are not specified. |
| 4 | OBFT implementation must be re-verified for spec consistency after edits. (See §Implementation impact.) |

## Background — Reviewer concerns

| # | Concern | Disposition |
|---|---|---|
| 1 | Per-peer first-observed convergence + treat-equivocator-as-null parked as open question — elevate. | SHOULD-level rule in 2abOBFT.md §Phase 2a verdict-broadcast paragraph + acknowledgement in §Failure modes Class B residual entry. |
| 2 | Manual-blacklist deterrent is aspirational, not deployed — be explicit. | Clarification in 2abOBFT.md assumption-4 paragraph; cross-ref the same wording present in OBFT.md:64/118 (already explicit there). |
| 3 | Persistent Phase-2a state: "should persist" → MUST. | One word change in 2abOBFT.md:76 + framing reinforcement in §EKM coordination model paragraph. |
| 4 | Verdict-propagation-budget arithmetic mismatch ("1 BTT − ε_proc slack" vs actual ε_proc arrival-to-end). | Reword 2abOBFT.md:171 to split the two quantities. |
| smaller 1 | Δ_2a = 1 BTT broken-by-construction with late-broadcast schedule, not just risky. | Strengthen the warning in 2abOBFT.md:171. |
| smaller 2 | Deepest-layer NR commitment produces no on-wire emission — no NR-tag exists at K-1. | One-line annotation on convergence-rule row 4/5. |
| smaller 3 | Onion equivocation adequately covered by existing rules. | No change. |

## Background — Slashing change

**Motivation.** Every current evidence rule consumes data that is already on the wire for protocol-functional reasons (Phase-1 bundles, KindVerdict envelopes, KindCommit / KindOnion2b partials). The current "MUST gossip evidence + anti-amplification rate-limit" framing buys *cluster-wide convergence on observed evidence*, but cluster-wide convergence on evidence is not load-bearing for safety ([2abOBFT.md:576](2abOBFT.md#L576) explicitly: "Acting on the evidence … is a human-coordinated process"). Operator-side logging + out-of-band aggregation (the planned manual-blacklist mechanism's input) suffices for attribution.

**Rule-by-rule impact:**

| Rule | Pre-change | Post-change |
|---|---|---|
| 1 (σ + NR same layer) | Receiver observes both partials via natural onion gossip; logs the pair | No change to detection; just log |
| 2 (leader equivocation) | Two bundles via natural re-flood; logs the pair | No change to detection; just log |
| 3 (cross-onion σ on V vs V') | Two σ partials via natural onion gossip | No change; just log |
| 4 (fake encrypted-presence k>0) | Detected via local decryption walk in Phase 3 | No change; just log |
| 5 (fake plaintext σ at L_0) | Detection requires retained or auth-only-retained V; spec says MUST-gossip the pair for no-V receivers | **Detection now restricted to receivers with retained-or-auth-only V**. Bundle re-flood covers this case under partial synchrony. Receivers without V do not surface Rule 5; out-of-band log aggregation across operators recovers cluster-wide attribution. |
| 6a (verdict-vs-verdict, 2ab only) | Verdicts via natural gossip | No change; just log |
| 6b (verdict-vs-action, 2ab only) | Verdict + Phase-2b partial via natural gossip; cross-reference cluster verdict view | No change to detection; just log |

**Rate-limit rules become moot.** "MUST gossip evidence at most once per (slot, layer, operator_id)" disappears with the gossip itself. Per-rule first-fire dedup *within* an operator's log is implementation-discretion (the existing OBFT impl does this via `recordRule1`/`recordRule4`/`evidenceObserved` — see §Implementation impact).

**One nuance worth noting in §EKM coordination / §Slashing-protection scope:** under operator-log-only, the blacklist mechanism's input is operator logs rather than re-flooded evidence. This strengthens reviewer concern #2: the blacklist mechanism is now also responsible for the log-sharing channel. Worth stating explicitly in the §Implications-of-deterrent paragraph.

## Spec changes

### File: [docs/2abOBFT.md](2abOBFT.md)

| Edit | Line(s) | Description |
|---|---|---|
| E1 | 18 | (no change — bullet correctly attributes 2-1-byz-defect residual) |
| E2 | 70 | Split assumption-4 wording into two pieces: (a) **continuous per-validator fees + staker migration** = deployed today via SSV's existing fee model; (b) **manual-blacklist + `Byzantine ≡ Down` collapse** = planned, not deployed. Spec assumption #4 holds against the combination; until the blacklist ships, only (a) is in force. (Addresses concern #2.) |
| E3 | 76 | "Implementations should persist" → "Implementations **MUST** persist". Add one sentence: "This is a correctness requirement, not a performance optimization — a non-persisting honest operator produces byzantine-equivalent wire behavior under Rule 6a (self-equivocation on restart) or Rule 6b (verdict-vs-action mismatch from lost retention)." (Addresses concern #3.) |
| E4 | 102-103 | Update "Narrower residual scope" paragraph to note that the deterrent's (b)-half is aspirational; until blacklist lands, both 2-1-byz-defect and verdict-equivocation residuals are absorbed only by the (a)-half (per-slot fee equivalence + staker migration). Same framing as E2. |
| E5 | 163-165 | After "load-bearing assumption with a real attack surface": add a new bullet — **"Receivers SHOULD treat verdict-equivocator's contribution as null in convergence count once a second distinct KindVerdict from the same operator is observed before convergence-rule evaluation at Phase-2b start."** Add: "This rule shrinks the verdict-equivocation surface (re-flood normally completes within Δ_2a ≥ 2 BTT, so receivers observe both verdicts in time); it does not eliminate the surface, because a timing-optimized byzantine can engineer a per-peer asymmetry within ~1 BTT of slack." Add cross-ref to §Open questions #16 (full EKM-binding answer). (Addresses concern #1.) |
| E6 | 167 | Remove "Each receiver MUST gossip slashing evidence at most once per `(slot, layer, operator_id)` tuple (the same anti-amplification rule as Rules 5 and 6b)." Replace with: "Each receiver MUST log observed verdict-equivocation per-(slot, layer, operator_id) once for later out-of-band aggregation; log format is implementation-defined." (Slashing change.) |
| E7 | 171 | Rewrite the verdict-propagation-budget paragraph: split arrival-to-end slack (`= ε_proc`) from within-Δ_2a slack (`= Δ_2a − 1 BTT − ε_proc`); state plainly that the recommended `Δ_2a = 2 BTT` is the minimum coherent sizing because at `Δ_2a = 1 BTT` the late-broadcast schedule `T_verdict_max − ε_proc` resolves to before Phase-2a starts. (Addresses concern #4 + smaller #1.) |
| E8 | 192-199 | Convergence-rule table: add a footnote to row 4 and row 5: "At layer `k = K-1` (deepest layer), there is no nr_tag for the operator to sign; an NR commit at K-1 produces no on-wire emission (the layer slot is empty in the onion). Reconstruction at K-1 has no fall-through path." Cross-ref §Operator commitment states. (Addresses smaller #2.) |
| E9 | 365 | Reinforce: a non-persisting honest operator produces byzantine-equivalent wire behavior — this is a correctness failure, not latency. Strengthen the "Implementations may colocate" to "MUST persist; implementations may colocate". (Concern #3 follow-through.) |
| E10 | 554 | Update §Slashing evidence introductory paragraph: change "The protocol surfaces the evidence; the surviving operators verify it…" to "The protocol surfaces the evidence on the wire as part of normal protocol message flow (bundles, verdicts, partials); honest operators **MUST log observed evidence** per the rules below for later out-of-band aggregation. Log format is implementation-defined." |
| E11 | 562 (Rule 5) | Remove "Receivers MUST gossip the evidence so receivers without retained V eventually receive the attribution." Replace with: "Rule 5 detection requires the receiver to have retained-or-auth-only-retained V at L_0. Under partial synchrony, bundle re-flood delivers V to all honest receivers within the absorption window, so all honest who first-observe the σ partial within the window can evaluate Rule 5. Out-of-band log aggregation across operators recovers cluster-wide attribution for receivers whose V observation lagged." Delete the "Rate-limit (anti-amplification rule)" paragraph (564). |
| E12 | 568 (Rule 6a) | Delete the "Rate-limit (anti-amplification rule)" line. |
| E13 | 574 (Rule 6b) | Delete the "Rate-limit (anti-amplification rule)" line. Update "Honest receivers should aggregate observations across receivers before acting on Rule-6b evidence." → "Honest receivers MUST log Rule-6b observations; cluster-wide aggregation happens out-of-band before action. Acting on a single observer's Rule-6b detection is not advised due to the higher false-positive risk." |
| E14 | 586 (evidence-quality table) | Rule 5 row "Surface-ability" cell: change "Always when retained-V receivers gossip evidence" → "Conditional on receiver retaining V (covered by bundle re-flood under partial synchrony)". |

### File: [docs/OBFT.md](OBFT.md)

| Edit | Line(s) | Description |
|---|---|---|
| O1 | 546 | Update §Slashing evidence intro paragraph: same "surfaces on wire as part of normal flow; honest operators MUST log" framing as 2ab E10. |
| O2 | 554 | Update Rule 5 ("Fake plaintext σ at the cluster's plaintext layer") body: remove any implicit reliance on gossipping evidence. The current OBFT.md:554 doesn't have an explicit MUST-gossip in the rule body (the surface-ability cell at line 566 is where the gossip claim lives). Verify language clean. |
| O3 | 566 (evidence-quality table) | Rule 5 row: same change as 2ab E14. "Always when retained-V receivers gossip evidence (MUST-gossip rule, rate-limited per `(slot, layer, operator_id)`)" → "Conditional on receiver retaining V (covered by Phase-1 bundle re-flood under partial synchrony)". |

### File: [docs/OBFTR.md](OBFTR.md)

| Edit | Line(s) | Description |
|---|---|---|
| R1 | 574 | Same as O1 — §Slashing evidence intro paragraph update. |
| R2 | 594 (evidence-quality table) | Same as O3 — Rule 5 row "Surface-ability" cell update. |
| R3 | 561, 656 (and any other inline "Slashing evidence" cross-refs) | Verify no remaining "on-wire evidence … MUST gossip" framing; replace with "on-wire evidence (signed by offender's own keys) … honest operators log" framing. (Spot-check during execution.) |

### File: [docs/OBFT-formal-verif.md](OBFT-formal-verif.md)

**No changes.** Verified via grep — only network-model gossipsub references; no MUST-gossip-evidence claims. The formal pigeonhole proofs do not depend on evidence-gossip semantics.

### File: [docs/2abOBFT-design-notes.md](2abOBFT-design-notes.md)

| Edit | Line(s) | Description |
|---|---|---|
| D1 | 242 | Update Rule 6 implementation note: "should retain the evidence (verdict + action) but not propagate as slashable" → "should log the evidence; propagation is out-of-band". |
| D2 | 259 | Same character of edit — "globally observable" framing becomes "operator-locally observable; out-of-band aggregation". |
| D3 | 284 | Remove "Rate-limit (anti-amplification rule): same as OBFT's Rule 5 rate-limit"; replace with "Operators MUST log; log format implementation-defined." |
| D4 | 385 (open question #2) | Mark as **superseded** by the SHOULD-level treat-equivocator-as-null rule in spec (E5). The full EKM-binding answer remains open via question #16. |
| D5 | 413 (open question #16) | Add an annotation that the spec now includes a SHOULD-level treat-as-null rule (E5) as a partial close; #16 remains open for the full cryptographic-binding answer. |
| D6 | 453 | "Surface evidence via existing slashing-evidence gossip mechanism" → "Log evidence per-rule; out-of-band aggregation". |

## Implementation impact — [protocol/v2/obft/](../protocol/v2/obft/)

**Pre-existing state**: the OBFT implementation **does not implement on-wire gossip of evidence**. Evidence is accumulated in `Instance.evidence` and surfaced via the optional `EvidenceObserver` callback (one fire per `(Rule, OperatorID, Layer)` tuple). The current implementation comments explicitly acknowledge this divergence from the spec — see [instance.go:195-201](../protocol/v2/obft/instance.go#L195), [instance.go:349-358](../protocol/v2/obft/instance.go#L349), [evidence.go:16-17](../protocol/v2/obft/evidence.go#L16).

**The spec change aligns the spec with the existing implementation.** Code paths require only comment cleanup.

| File | Lines | Edit |
|---|---|---|
| [evidence.go](../protocol/v2/obft/evidence.go) | 12-18 (file header), 63-68 (Rule 5 comment) | Remove the "spec mandates MUST-gossip on the wire" framing. Replace with: "spec mandates MUST log observed evidence; this implementation's `EvidenceObserver` callback is the logging surface. Log format is caller-defined." |
| [instance.go](../protocol/v2/obft/instance.go) | 195-201 | Remove "ideally should be on-wire; logged-only is a deliberate scope choice — operators monitor logs out-of-band" — replace with: "per spec §Slashing evidence, operators MUST log observed evidence; this callback is the logging surface, called once per (Rule, OperatorID, Layer) tuple". |
| [instance.go](../protocol/v2/obft/instance.go) | 343-358 (Rule 5 deferred attribution comment) | Update the comment to reflect the new spec: "per spec §Slashing evidence Rule 5, detection requires retained-or-auth-only-retained V; under partial synchrony, bundle re-flood covers this case. l0SigmaUnknownV (σ verifies cryptographically but the receiver never observed any matching V) is not fired here to avoid the false-positive risk under leader equivocation + asymmetric propagation. Out-of-band log aggregation across operators recovers cluster-wide attribution for the unknownV case." (Removes the "on-wire gossip is not yet implemented" framing — that's no longer the spec.) |
| [instance.go](../protocol/v2/obft/instance.go) | 495-501 (EvidenceObserver type doc) | Same character of edit — "spec mandates MUST-gossip … impl substitutes out-of-band logging" → "spec mandates MUST-log; this callback is the logging surface". |

**No behavior changes required.** No new tests required.

## Test impact

Existing OBFT evidence tests assume the no-gossip false-positive avoidance model (e.g., [`TestObft_UnknownV_NoRule5_NotFiredAtFinalize`](../protocol/v2/obft/instance_test.go), [`TestObft_Rule5_NoFalsePositiveOnEquivocation`](../protocol/v2/obft/instance_test.go)). These remain valid under the new spec — they exercise the "no firing on unknownV at slot end" choice which is now spec-aligned rather than a temporary deviation.

**No test file changes required.** Verify by running `go test ./protocol/v2/obft/...` after the impl-comment edits.

## Validation steps

After all edits:

1. **Spec internal consistency.** Grep all four docs for residual "MUST gossip" / "MUST re-flood" / "anti-amplification" / "gossip the evidence" / "gossip slashing" — should return zero hits (modulo the slashing change explanation paragraph itself).
2. **Cross-reference consistency.** Grep 2abOBFT.md, 2abOBFT-design-notes.md for references to "rate-limit" → check each remaining reference still parses (some refer to verdict-spam-rate-limit which is now logging-rate-limit, others may be different concepts).
3. **Implementation consistency.** `grep -rn "MUST.gossip\|on-wire gossip\|gossip evidence" protocol/v2/obft/` — should be zero hits after impl-comment edits.
4. **Tests pass.** `go test ./protocol/v2/obft/...` — expect pass with no test changes.
5. **Build/lint clean.** `make lint` in repo root (per [CLAUDE.md](../CLAUDE.md)).

## Execution order

1. **Spec edits first** (read-only on implementation):
   1. 2abOBFT.md (E1-E14, in order from top of file to bottom)
   2. OBFT.md (O1-O3)
   3. OBFTR.md (R1-R3)
   4. 2abOBFT-design-notes.md (D1-D6)
2. **Implementation comment edits** ([evidence.go](../protocol/v2/obft/evidence.go), [instance.go](../protocol/v2/obft/instance.go)).
3. **Validation** (5 steps in §Validation).
4. **Cleanup tracking review**: present any cleanup notes accumulated during the edit pass (per [global preferences](~/.claude/CLAUDE.md) — "Code cleanup tracking").
5. **Hand off** — present diff summary to user; no commits unless explicitly asked.

## Out-of-scope / explicit non-goals

- **No EKM-binding of verdicts** (open question #16 in design-notes). Reviewer concern #1 is addressed by the SHOULD-level treat-as-null rule, not by elevating #16 to a decided design.
- **No new evidence rules**, **no schema changes**, **no wire-format changes**.
- **No changes to formal-verif proofs** (proofs don't depend on evidence-gossip semantics).
- **No new tests**. Existing tests stay valid.
- **No tracked-as-2-2-validity-split fix** (that's a separate design discussion; out of scope here).
- **No commit** without explicit user ask.

## Open issues for user to confirm before execution

None — all four scope decisions in §Confirmed scope decisions are settled. Awaiting "execute" / "proceed" before touching files.
