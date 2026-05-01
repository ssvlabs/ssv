# TBFT audit

Aggregated review of `docs/TBFT.md`, `docs/TBFT2.md`, and `docs/TBFT-comparison.md` against the implementation under `protocol/v2/tbft/` and `protocol/v2/ssv/runner/tbft/` (reviewed at commit `5400640`). The findings combine an external review, an adversarial design review, and a code-level verification pass.

## Summary

The cryptographic core is sound: positive and non-receipt quorums at the same layer are mutually exclusive, so at most one validator-signed value surfaces per TBFT instance. The 1-RTT vs QBFT 3-RTT win is real, and reusing SSV's existing threshold BLS share avoids a second DKG.

Beyond that, the protocol docs claim more than the design delivers. The two load-bearing issues:

- A Byzantine layer leader can grief its own layer **deterministically** by delivering the candidate to exactly `f+1` honest operators and withholding its own contribution. Neither positive quorum nor non-receipt quorum reaches `q`, and the layer is stuck with no fall-through to deeper layers. This invalidates the spec's "K = max(3, f+1) handles Byzantine leaders" framing and TBFT2's "n=4 cannot miss within bound" claim.
- Candidate authenticity is unenforced in the runner path: `ProcessCandidate` reaches `ObserveCandidate` without checking that the sender is the designated layer leader, and `ObserveCandidate` keeps the first observation. Any cluster member can race a forged candidate body for any layer.

The findings below are graded P0 (overstated correctness claims), P1 (spec or implementation gaps that should be fixed before deployment), P2 (operational / clarity), and P3 (smaller cleanups).

## What holds

- The pigeonhole safety argument at [TBFT.md:83–96](TBFT.md): honest can't sign both `σ_i(V)` and a non-receipt at the same layer, so positive quorum and non-receipt quorum cannot both reach `q = 2f+1`.
- Single-RTT decision path vs QBFT's three.
- Reusing SSV's threshold BLS share for both partial-sig signing and tag-bound IBE decryption — no second DKG.
- Dropping the "absent = ALL-value" rule. The reasoning at [TBFT.md:81](TBFT.md:81) is correct: the rule weakened the Byzantine bound while buying liveness the cluster wasn't entitled to under `3f+1`.

## P0 — Correctness/safety claims that overstate the design

### P0.1 Selective candidate delivery griefs the layer deterministically

Setup: `n = 3f+1`, `q = 2f+1`, Byzantine `L_k`.

- `L_k` ships `V_{L_k}` to exactly `f+1` honest operators just before the deadline; the remaining `f` honest get nothing. No equivocation needed — just selective delivery.
- Positive count at layer k: `(f+1) + byz_pos`. Reaching quorum requires `byz_pos ≥ f`. Byzantine refuses → positive = `f+1 < q`.
- Non-receipt count at layer k: `f + byz_nr`. Reaching quorum requires `byz_nr ≥ f+1`. Impossible (Byzantine has only `f`).
- Both quorums fail. The implementation returns `ErrNoQuorum` at [instance.go:298](../protocol/v2/tbft/instance.go) and there is no fall-through to layer k+1 (the layer-(k+1) decryption key requires a quorum on the layer-k no-quorum tag, which is exactly the non-receipt quorum that just failed).

This is a **deterministic** in-bound miss, not the probabilistic "marginal network" caveat at [TBFT.md:199–209](TBFT.md). The `K = max(3, f+1)` framing at [TBFT.md:19](TBFT.md) and [TBFT-comparison.md:55](TBFT-comparison.md) ("at least one honest leader in top-K") is not load-bearing here: it guarantees an honest _successor_ leader, but a higher-priority Byzantine leader can grief its own layer regardless, and the cluster is stuck at that layer.

The narrower "Byzantine vote-flip band" point in the doc deserves an aside: the caveat at [TBFT.md:199](TBFT.md:199) describes the band where Byzantine vote choice can flip the outcome as `f ≤ x ≤ f+1`, with `x` honest who didn't receive `V_{L_k}` by the deadline. The band where Byzantine choice can prevent _any_ quorum (slot missed at the layer with no fall-through) is wider: `x ∈ {1, …, f}`. Any non-zero `x` lets Byzantine refuse to push positive over quorum, and the non-receipt side can't reach quorum until `x ≥ f+1`. The selective-delivery attack is a Byzantine _leader_ deliberately landing `x = f`, the worst point in that band.

**Action**

- Add a dedicated subsection to [TBFT.md:199](TBFT.md) describing this as a deterministic Byzantine-leader grief, separate from the network-marginality caveat.
- State explicitly that `K = f+1` does not save the slot when a higher-priority Byzantine leader griefs first.
- Tighten the deadline-tuning condition: the relevant tail is `P(x ≥ 1) ≪ 1`, i.e. P99 (or higher) propagation, not P95.

### P0.2 TBFT2 at n=4 is also griefable

Same attack at `n=4, f=1, q=3`, Byzantine `L_p`:

- `L_p` delivers `V_p` to 2 of 3 honest operators → positive = 2.
- The remaining honest emits non-receipt → NR = 1.
- Byzantine refuses both → positive = 2, NR = 1. Both `< 3`. Stuck.
- Layer 2 (backup) cannot unlock — its decryption key requires 3 partial sigs on `tag_no_p`, but only 1 is available.
- Slot missed.

The footnote at [TBFT-comparison.md:75](TBFT-comparison.md) ("TBFT2 _cannot_ miss within-bound at n=4"), the "strictly the right choice over TBFT" claim at [TBFT-comparison.md:83](TBFT-comparison.md), and the recommendation table at [TBFT-comparison.md:101](TBFT-comparison.md) are wrong as written. Byzantine resilience at n=4 TBFT2 equals TBFT's, not better.

The right summary is that TBFT2 at n=4 trades simplicity (single tag, two layers, no `K`) for the same Byzantine-leader miss exposure as TBFT, not for a cleaner safety profile.

**Action**

- Remove "TBFT2 has no in-bound miss scenarios" from [TBFT-comparison.md:75–85](TBFT-comparison.md).
- Replace footnote ¹ at [TBFT-comparison.md:71](TBFT-comparison.md) with language acknowledging the Byzantine-primary selective-delivery grief.
- Update the recommendation table at [TBFT-comparison.md:101](TBFT-comparison.md) to say TBFT2's win at n=4 is on bandwidth and simplicity, not on Byzantine resilience.

## P1 — Spec/implementation issues

### P1.1 Tag indexing is internally inconsistent

[TBFT.md:45–46](TBFT.md) defines `tag_k` as the _encryption_ tag of layer k (with `tag_1 = ⊥` plaintext, `tag_k = ("slot", N, "layer", k−1, "no-quorum")` for `k > 1`). The Phase-3 algorithm at [TBFT.md:63–66](TBFT.md) then aggregates "non-receipt-attestation partials for `tag_k`" and uses them to unlock layer k+1. These are two different objects:

- The _encryption_ tag of layer k = the tag k's ciphertext is locked under.
- The _non-receipt_ tag at layer k = the tag operators sign when they didn't receive `V_{L_k}`, which when aggregated unlocks layer k+1.

The two are related by an off-by-one (`enc_tag_{k+1} = nr_tag_k`) but the spec reuses the symbol `tag_k`. With `tag_1 = ⊥` you literally can't sign non-receipts on it, so the doc as written has no path to unlock layer 2.

The implementation has the cleaner model. [tag.go:46–62](../protocol/v2/tbft/tag.go) defines `NoQuorumTag(clusterID, height, layer)` — the message signed for non-receipt at `layer`, used to unlock `layer+1`. [tag.go:64–74](../protocol/v2/tbft/tag.go) defines `LayerTag(layer) = NoQuorumTag(layer-1)` — the encryption tag of the ciphertext at `layer`. Layer 0 returns nil (plaintext).

**Action**

- Rewrite [TBFT.md:33–71](TBFT.md) using 0-based indexing matching the code.
- Use distinct symbols: `enc_tag_k` (encryption tag of layer k) and `nr_tag_k` (the tag honest sign for non-receipt at layer k). Relate them as `enc_tag_{k+1} = nr_tag_k`, with `enc_tag_0 = ⊥`.

### P1.2 Candidate authenticity is not enforced — implementation bug

The doc gap and the implementation bug coincide. [TBFT.md:26–31](TBFT.md) says leaders gossip candidates without requiring receivers to authenticate sender == designated layer leader. Tracing the runner path:

- [validator.go:254](../protocol/v2/ssv/validator/validator.go) extracts `senderID` from the outer `SignedSSVMessage`.
- [proposer_tbft.go:117–138](../protocol/v2/ssv/runner/proposer_tbft.go) only rate-limits before dispatch; there is no leader/sender check.
- [controller.go:278–289](../protocol/v2/ssv/runner/tbft/controller.go) `ProcessCandidate(cb *CandidateBroadcast)` doesn't even take `senderID` as a parameter — it just calls `instance.ObserveCandidate(layer, value)`.
- [instance.go:150–164](../protocol/v2/tbft/instance.go) `ObserveCandidate` semantics: "first observation for a layer wins; later observations are silently ignored" — assuming leader honesty without enforcement.

Result: any cluster member, not just `L_k`, can race a forged candidate body for any layer; whatever arrives first is what every honest operator signs. The inconsistency-fault detector at [instance.go:200](../protocol/v2/tbft/instance.go) won't catch this — it covers `σ + NR` from the same operator, not forged candidates from non-leaders.

**Action**

- In `ProcessTBFTEnvelopeMsg` ([proposer_tbft.go:117](../protocol/v2/ssv/runner/proposer_tbft.go)) before dispatch, require:
  - For `KindCandidate`: `senderID == cfg.Layers[env.Candidate.Layer].Leader` AND `senderID == env.Candidate.OperatorID`.
  - For `KindOnion` and `KindNonReceipt`: `senderID == env.{Onion,NonReceipt}.OperatorID` (no operator should be able to broadcast another operator's signed envelope).
- Replace the "first observation wins" semantics in `ObserveCandidate` ([instance.go:155](../protocol/v2/tbft/instance.go)) with the equivocation-to-non-receipt rule the comment at [instance.go:319–323](../protocol/v2/tbft/instance.go) already promises: a second observation from the same leader at the same layer triggers local "treat as non-receipt" and surfaces the conflict for fault attribution.
- Tests for: candidate from a non-leader, candidate where `senderID != cb.OperatorID`, two distinct candidates from the same leader at the same layer.

### P1.3 Application validity is an unstated precondition

[TBFT.md:119–121](TBFT.md) lists "Validity (output ∈ proposed values) | Yes" with no elaboration. The implementation comment at [instance.go:150–152](../protocol/v2/tbft/instance.go) actually says "Caller must have already validated `value` against the application-specific rules" — which is a runner-level invariant, not a stated protocol property.

For SSV's proposer duty, the application-level rules include slot, proposer index, fork/domain, parent root, relay metadata, doppelganger and slashing-protection checks, and value encoding. If even one honest operator skips a check before signing, the cryptographic safety guarantee no longer implies _output validity_ — the cluster might still produce a single signature, but on something the application would consider invalid.

**Action**

- Convert the table tick to a stated precondition: "Validity holds iff every honest operator validates each candidate against application-level rules before including a positive partial signature in their onion."
- Add a "Preconditions on the host application" section enumerating the required checks for SSV's proposer duty.

### P1.4 "At most one full threshold sig per slot" is too broad

[TBFT.md:94–96](TBFT.md) states "at most one full threshold signature is ever produced per slot." This is true within one TBFT instance and assumes:

- Single instance per slot — no parallel signing path.
- Domain separation between TBFT and any other path the share signs against.
- Slashing protection gates candidate signing.

The QBFT proposer path was just removed (commit `4c2f17d58`), so the concurrent-path concern is hypothetical today, but the doc-level claim should be tightened: "within one TBFT instance, at most one full threshold signature is ever produced; a host that runs a parallel signing path loses this guarantee unless slashing protection gates both paths and they share a domain-separating tag."

A related operational note: each operator's share signs `K` distinct values per slot inside the onion (one per layer). The cluster's safety property collapses these to a single output, but the per-operator signing log shows multiple block sigs at the same slot. EKM needs to handle this without flagging it as a violation. Today the EKM record is taken at submit time at [proposer_tbft.go:287–295](../protocol/v2/ssv/runner/proposer_tbft.go); for the "one-sig-per-slot" claim to hold operationally, slashing protection should gate _candidate signing_, not just submission.

**Action**

- Tighten [TBFT.md:94–96](TBFT.md) to scope the claim to "within one instance" and list the domain-separation and slashing-protection preconditions.
- Document the per-share multi-block-signing pattern explicitly so EKM behavior is not an unstated assumption.

### P1.5 Cross-onion partial-sig equivocation by operators is not in inconsistency-slashing

[TBFT.md:203](TBFT.md) defines slashing for `σ_i(V_{L_k}) + NR-on-tag_k` from the same operator. Two adjacent cases are missing:

- **Cross-onion partial-sig equivocation.** Operator `i` broadcasts onion A with `σ_i(V)` at layer k and onion B with `σ_i(V')` at the same layer. Both partials are valid against `i`'s share pubkey. The aggregator at [instance.go:343–389](../protocol/v2/tbft/instance.go) groups by value, so `i` contributes to two groups. Detectable from the partial sigs alone (two distinct partials from the same identity at the same `(slot, layer)`). Should be slashable on the same logic as the σ + NR case.
- **Hidden equivocation in unopened layers.** If layer 0 succeeds, the layer-2+ ciphertext is never decrypted, so any equivocation at deep layers is concealed. An operator can equivocate freely at deep layers, knowing evidence only surfaces in the (rare) execution path that opens those layers.

**Action**

- Extend the inconsistency-fault detector at [instance.go:200](../protocol/v2/tbft/instance.go) to cover same-operator multiple distinct partials at the same layer.
- Note in the doc that fault attribution at deep layers is path-conditional.

## P2 — Operational and clarity

### P2.1 Equivocation detection is overstated

[TBFT.md:122–123](TBFT.md) lists "Equivocation detection | Implicit (each operator commits all `K` partial sigs in one signed onion)". Actual behavior:

- Equivocation by `L_k` causes split signature groups during reconstruction at [instance.go:343–389](../protocol/v2/tbft/instance.go); the largest group wins; if no group reaches `q`, the layer is stuck and `ErrNoQuorum`.
- The implementation comment at [instance.go:319–323](../protocol/v2/tbft/instance.go) explicitly punts to the caller: "honest operators are expected to detect equivocation during Phase 1 ... and treat the layer as non-receipt." But there is no such detector wired up — `ObserveCandidate` keeps the first observation silently (P1.2 above).

Combined with P0.1: an honest leader on a healthy network is fine, but adversarial leaders have multiple grief vectors (selective delivery, real equivocation) and the protocol detects neither today.

**Action**

- Rewrite the table row honestly: "Equivocation by a leader produces split signature groups; if no group reaches quorum and no non-receipt quorum exists, the layer is stuck. Honest operators that detect multiple distinct candidate broadcasts from the same leader during Phase 1 are expected to emit a non-receipt at that layer instead of signing — this converts equivocation into a clean fall-through. Detection is the host application's responsibility; the protocol itself does not enforce it."
- Wire up the detector or document the host's required behavior. (P1.2's candidate-acceptance fix is the natural place for this.)

### P2.2 No final-certificate gossip — "lone reconstructor failed to submit" failure mode

[envelope.go:44–53](../protocol/v2/tbft/wire/envelope.go) defines only `KindOnion`, `KindNonReceipt`, `KindCandidate`. No certificate kind. [TBFT.md:73](TBFT.md) and [TBFT.md:123](TBFT.md) confirm operators may have different views; each who reconstructs submits independently at [proposer_tbft.go:299](../protocol/v2/ssv/runner/proposer_tbft.go).

If only one operator's local view crossed quorum, and that operator's beacon / relay path fails after reconstruction, the slot is missed even though the cluster _had_ enough sigs to construct a full signature.

**Action (deferred design)**

- Add `KindCertificate(slot, V, S)` so any operator with the certificate can submit. Wire-format change with replay/cache implications; queue as a follow-up.

### P2.3 Timing claims need an end-to-end adversarial budget

[TBFT.md:140–148](TBFT.md) gives `Δ_1 ≈ 1s`, `Δ_2 ≈ 500ms`, `T_d ≈ slot_start + 3s`, leaving roughly 500ms for reconstruction and relay submission inside the 4s relay cutoff in the _healthy_ case. Under congested gossip ([TBFT-comparison.md:19](TBFT-comparison.md) assumes 500 ms RTT), both `Δ_1` and `Δ_2` need to grow; finalize slips past 4s and the slot is missed at the relay regardless of consensus correctness. The "~600 ms" TBFT number in scenario 2 of the comparison doc is consensus-phase only and obscures this. [TBFT.md:217–219](TBFT.md) acknowledges clock skew "must be bounded and known" but never bounds it.

**Action**

- Add an end-to-end budget with tail percentiles: gossip propagation, EKM signing, beacon-node submit, relay submission, and an explicit clock-skew bound `δ`.
- State the deadline rule: `T_d − T_arrival > D + δ` where `D` is the propagation P99 (or P999) — sharper than P95 because the relevant tail is `P(x ≥ 1) ≪ 1`, not `P(x ≥ f+1) ≪ 1` (see P0.1).

### P2.4 Other operational items

- **Worst-of-K beacon-fetch latency** for `Δ_1`. K leaders fetch in parallel from K distinct beacons; `Δ_1` must accommodate the slowest of K independent block-fetch RTTs, not the typical one. Worth a sentence.
- **Head-change handling during Phase 1** is specified for TBFT2's `V_b` at [TBFT2.md:40](TBFT2.md) but not for TBFT's any-of-K candidates. Same problem applies — if the head changes during the fetch window, all candidates are stale.
- **TBFT2 "deterministic backup"** at [TBFT2.md:157](TBFT2.md) glosses over Ethereum execution-payload non-determinism (mempool selection, fee ordering, builder logic). Either commit to a canonical empty/near-empty block as `V_b` (with the MEV-zero cost on miss) or drop the suggestion.
- **TBFT2 dual-leader-Byzantine probabilities under random rotation** are non-trivial at `f ≥ 2`: roughly 4.8% (n=7), 6.7% (n=10), 7.7% (n=13). VRF or sub-quorum rotation reduces but doesn't eliminate. The comparison doc's recommendation to use TBFT (K=f+1) at n≥7 rests on the right reasoning, subject to P0.2's correction at n=4.

## P3 — Smaller items

- **`T_d` is named "deadline" but the protocol finalizes after it** ([TBFT.md:20](TBFT.md)). It's actually a view-fix point. Renaming would prevent downstream timing-budget mistakes.
- **Comparison-doc scope drift**: [TBFT-comparison.md:9–15](TBFT-comparison.md) says "from start of consensus to a validator-signed value"; [TBFT-comparison.md:111](TBFT-comparison.md) calls the numbers "consensus-phase-only." Pick one phrase and apply it consistently.
- **`K=3` floor for n=4** over-provisions; K=2 already guarantees ≥1 honest leader. The comparison doc effectively argues this at [TBFT-comparison.md:84](TBFT-comparison.md) when noting TBFT2 = TBFT(K=2) for n=4. Either justify the floor as defense-in-depth or drop it.
- **Multiple submitters / beacon de-dup** ([TBFT.md:73](TBFT.md)). Every operator that crosses quorum will submit; the beacon node de-dupes. Worth stating so a reader doesn't assume one designated submitter.

## Recommended sequencing

### A. Now — implementation fix (~1 PR)

P1.2 closes the verified candidate-authenticity bug:

- Add sender/leader checks in `ProcessTBFTEnvelopeMsg` ([proposer_tbft.go:117](../protocol/v2/ssv/runner/proposer_tbft.go)) before dispatch.
- Replace `ObserveCandidate`'s first-wins semantics ([instance.go:155](../protocol/v2/tbft/instance.go)) with the equivocation-to-non-receipt rule the comment at [instance.go:319–323](../protocol/v2/tbft/instance.go) already promises.
- Extend the inconsistency-fault detector ([instance.go:200](../protocol/v2/tbft/instance.go)) to cover cross-onion same-operator partials (P1.5).
- Tests: non-leader candidate, mismatched `senderID`, two distinct candidates from same leader same layer, two distinct partials from same operator same layer.

### B. Same week — doc rewrites (~1 PR against `docs/TBFT*.md`)

Order: correctness first (P0.1, P0.2, P1.3, P1.4), then clarity (P1.1, P2.1, P2.3, P3 items).

Add a **Threat model** section to `TBFT.md` separating:

- Safety (cryptographic; pigeonhole on positive vs non-receipt quorums).
- Liveness (`3f+1` trust bound; bounded propagation; bounded clock skew `δ`; `T_d − T_arrival > D + δ` with `D` = propagation P99).
- Leader equivocation (host-detected; protocol does not enforce).
- Application validity (host precondition; enumerated for the proposer duty).

### C. Backlog (file as separate issues)

- **Final-certificate gossip** (P2.2). Wire-format change.
- **End-to-end timing telemetry** (P2.3). Needs production data on gossip propagation, EKM signing, beacon submit, relay submission tails.
- **Real selective-delivery mitigation** beyond doc-honesty (P0.1, P0.2). Options: (a) two-phase TBFT with a short post-deadline window where operators that didn't see `V` but observe peers' positive sigs can switch from non-receipt to positive; (b) leader-bound delivery acks aggregated by some honest threshold; (c) accept the limitation and amortize misses across slots via K. Each has bandwidth/latency cost; none are obviously right.

## Deployment recommendation

Do not deploy TBFT or TBFT2 to mainnet until at least:

1. The implementation fix in section A lands (P1.2).
2. The TBFT2 n=4 doc claim is corrected (P0.2).

The current `tbft-d` branch's protocol claims overstate Byzantine resilience in a way that is likely to leak into operator expectations: operators will read "n=4 TBFT2 cannot miss within bound" and configure capacity assuming that, when in fact a single Byzantine primary can grief by selective delivery. The cryptographic core is solid; the issues are at the protocol-spec / runner-glue layer.
