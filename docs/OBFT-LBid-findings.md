# OBFT L_Bid appendix — findings & remediation analysis

Working analysis for [Appendix B — L_Bid mini-consensus extension](OBFT.md) (`docs/OBFT.md`). **Status: roadmap executed — all 7 items below have been applied to `docs/OBFT.md` (Appendix B + touched main-spec sections) and reviewed (uncommitted).** This document captures the validated findings, their impact against the two end-goals, the fixes (now landed), and the safety argument for each.

Line references are against `docs/OBFT.md` at the time of writing (commit `0911dc631`); they will drift as the spec is edited — section names are the durable locator.

## End-goals for L_Bid

- **Goal A — Safety parity (hard, 100%).** L_Bid must preserve OBFT's safety exactly: Pigeonholes 1/2/3 hold at `K' = K+1` layers; at most one `V` reconstructs cluster-wide. Every proposed change below carries an explicit safety argument.
- **Goal B — All-honest liveness parity (priority).** In the all-honest case L_Bid must not miss any slot that bare OBFT would output. Adversarial-byz liveness is a secondary goal ("match as much as possible"), pursued only where it does not threaten Goal A.

Throughout: `n = 3f+1`, `qV = qEnc = 2f+1`; running example `n=4, f=1, qV=qEnc=3`. "Witness" = the `σ_L^V` re-inclusion section of `KindCommit` (`OBFT.md:266`, §Phase 2 / Wire format).

## Summary

| # | Finding | Verdict | Goal A (safety) | Goal B (all-honest) | Adversarial liveness |
|---|---|---|---|---|---|
| F1 | L_Bid σ-pool harvest strands the promoted winner's leader witness | Correct (refined) | Neutral (fix is safe) | Indirect (see F3/B) | **Improves at f=1** — closes no-equiv 2-1-byz-defect; partial at f≥2 (§Generalization) |
| F2 | Genuinely-new cost is equivocation *promotion* (K/n), not "every slot" | Correct | Neutral | Neutral | Reframes; attestation is liveness-load-bearing |
| F3 | All-honest liveness is clean except aggressive sizing | Conclusion correct; a sub-claim was wrong (2-2 floor *is* an all-honest floor — corrected) | Neutral | **Key** — parity at `Δ_verdict ≥ 1·BTT` | n/a |
| F4 | `qBid = K−f` is near-vacuous at recommended `K=f+1` | Premise confirmed; severity sizing-dependent | Neutral | Neutral (value, not liveness) | Neutral |
| F5 | Framing: attestation "optional"; "essentially free" | Fair (mostly cross-refs) | Neutral | Neutral | Documentation |
| F6 | **[Independent]** Recommended-`K` contradiction (`OBFT.md:644` vs five other sites) | Confirmed | Neutral | Neutral | Documentation |
| F7 | **[Independent, deep-dive]** Verdict-binding semantics inconsistent (Rule 8 vs fall-through) | Confirmed | Neutral | **Bug** — honest ops emit false slashing evidence | Changes byz-defect deterrent |

**The 469 main-spec inconsistency** (an independent issue beyond the original review) is the analytical backbone of F1 and is documented there. **F1's implementation status** (the σ-pool is layer-keyed in code → F1 is an impl change) and **F7** (verdict-binding semantics) come from a codebase + design deep-dive (this revision); see also the [fall-through-on-equivocation deep-dive](#deep-dive--can-l_bid-fall-through-on-equivocation-goal-b-secondary).

## Key result — all-honest liveness parity (Goal B)

This is the priority goal, so it is stated up front; the per-finding sections below support it.

> **All-honest parity verdict (post-plan).** Once the two Goal-B items land (item 1: `Δ_verdict ≥ 1·BTT`; item 2: non-binding verdicts), the all-honest case matches bare OBFT on **both** axes:
> - **Safety — identical, unconditional.** Pigeonholes 1/2/3 hold at `K'=K+1`; F1 and F7 preserve them (F1's bound proven for all `n=3f+1`; F7 binds no partials). Under all-honest there is no byzantine to stress it.
> - **Liveness — parity, conditional on item 1.** L_Bid's all-honest miss set ⊆ bare OBFT's: the only all-honest miss is the *shared, inherited* L_0 2-2 validity floor. So **never worse** — and occasionally *better* (when a re-org 2-2-splits `V_{L_0}` but a valid alternative wins the bid, L_Bid routes to it and outputs where bare OBFT deadlocks at L_0). Without item 1, aggressive sizing's partial-propagation deadlock is an all-honest miss bare OBFT lacks → strictly worse.
>
> F1 and the residual-catalog work (F2) are **adversarial-only** — they do not bear on the all-honest case. F7 additionally restores all-honest *behavioral* parity (no spurious Rule-8 slashing evidence on a benign verdict-split), which bare OBFT, having no verdicts, never produces.

**Claim. In the pure all-honest case (0 byzantine), with `Δ_verdict ≥ 1·BTT` (standard or conservative sizing), L_Bid never misses a slot that bare OBFT would output.** The only all-honest miss is the shared 2-2 validity-divergence floor, which L_Bid does not worsen.

**Argument.** All-honest, every operator commits σ-or-NR at L_Bid (three-state, no silent honest). Two branches:

1. **`verdict_quorum` forms on `V_X`.** `verdict_pool[V_X] ≥ qV` (`OBFT.md:1166`) with all verdicts from honest operators means **≥ qV honest verdicted `V_X`**. To verdict `V_X` an operator must have `V_X ∈ bid_set` — i.e. retained-and-host-validated `V_X` by `T_verdict` (`OBFT.md:1151`–`OBFT.md:1153`). Retention holds to slot end, and host-validity is *locked* at first-observation (cannot flip; `OBFT.md:1151`). So at Phase 2 those ≥ qV honest each satisfy the L_Bid σ predicate (`verdict_quorum ∧ retain V_X ∧ valid`; `OBFT.md:1185`) → **≥ qV plaintext σ on `V_X` → σ-quorum → success.** (An honest operator that received `V_X` only in `(T_verdict, T_commit]` verdicted null but still σ's at Phase 2 — the permitted "null verdict + Phase-2 σ" case, `OBFT.md:1239` — so it can only add to the pool.)
2. **No `verdict_quorum`.** Every honest emits NR at L_Bid → NR-pool = n ≥ qEnc → clean fall-through to L_0, which is bare OBFT's L_0 (unchanged). Bare OBFT L_0 then outputs in the all-honest case **unless `V_{L_0}` is itself 2-2 validity-split** (bare OBFT's own floor — see F3).

So all-honest L_Bid is **success-or-clean-fall-through at its own layer**. Any residual all-honest miss is then bare OBFT's **inherited** L_0 2-2 validity floor (reached whenever `V_{L_0}` is 2-2-split — including the common case where the bid winner *is* the L_0 candidate). L_Bid falls through to that floor but does not add it, so Goal-B **parity** holds. ∎

**The single all-honest regression** is aggressive sizing's partial verdict propagation (`Δ_verdict < 1·BTT`; `OBFT.md:1277`): all-honest mesh jitter can split the verdict pool 2-2 so neither σ-quorum nor NR-quorum forms, and the no-fall-through gate (`OBFT.md:1199`) turns that into a miss bare OBFT would not have. **Therefore Goal B requires excluding aggressive sizing (floor `Δ_verdict ≥ 1·BTT`).** This costs nothing on MEV-fetch at default RefloodDelay (all sizings shift = 0; `OBFT.md:1073`), and standard sizing is free even at RefloodDelay=0 (`OBFT.md:1079`).

**Recommended default for L_Bid deployments targeting Goal B: standard sizing (`Δ_minicon = 1.5·BTT`, `Δ_verdict = 1·BTT`).**

Note the interaction with F4: at the base spec's recommended `K=f+1`, `qBid=1` makes verdict splits frequent → frequent fall-through → L_Bid rarely engages → all-honest parity is met *trivially* (the cluster mostly runs bare OBFT L_0). All-honest parity is non-trivial only at higher `K`, where the argument above is what secures it.

## Findings

### F1 — L_Bid σ-pool harvest strands the promoted winner's leader witness

*Goals touched: Safety (neutral — fix proven safe) · All-honest liveness (indirect) · Adversarial liveness (**improves**).*
*Source: review item #1. Includes the independent `OBFT.md:469` inconsistency.*

**Claim.** The 2-1-byz-defect residual counts `σ-pool[V_X] = 2 (majority)` (`OBFT.md:1275`) and treats candidate-withholding as deadlocking. But the byzantine rotation leader's own Phase-1 partial `σ_B^V(V_X)` is a valid, value-bound partial that is unavoidably leaked, and counting it makes `σ-pool[V_X] = 3 = qV` → success. So the **candidate-withholding-without-equivocation** variant need not deadlock.

**Verdict: correct in substance, with a sharpened root cause.**

**Why the partial is unavoidable.** To form `verdict_quorum` on its own candidate `V_X`, the byz needs qV verdicts; with only `f` byz verdicts it must induce **≥ qV − f honest** to verdict `V_X` (at n=4: 2 honest). Inducing an honest operator to verdict `V_X` requires delivering `V_X`'s Phase-1 bundle to it — which deposits `σ_B^V(V_X)` into that operator's retained bundle, hence into its `KindCommit` witness section. The byz cannot retract a Phase-1 partial it already broadcast. So whenever `verdict_quorum` forms on a byz candidate, ≥ qV−f honest σ on `V_X` **and** carry its witness.

**Why the partial is reusable at L_Bid.** σ partials are `σ_i^V(V)` — signatures on the *value*, value-bound, with layer-separation living only in the IBE wrapper (`OBFT.md:218`, `OBFT.md:1177`). The reconstructed `S` is a beacon-chain block signature on `V_X`; it cannot be layer-domain-separated. So `σ_B^V(V_X)` is a valid partial for reconstructing `V_X` at *any* layer, including L_Bid.

**Root cause — the harvest is layer-keyed, and the appendix never re-specifies it for L_Bid.** Bare OBFT's σ-pool is explicitly keyed by layer: `sigs[k] = {σ_{L_k}^V from witness sections at layer k} ∪ {layer-k onion contents}` (`OBFT.md:306`–`OBFT.md:307`). `V_X` originates at rotation layer `h` (the byz's rotation layer), so its witness is tagged `h` and, under the inherited harvest, lands in the *sealed* rotation-`h` pool — not L_Bid. The appendix's L_Bid Phase-3 step (`OBFT.md:1195`) just says "σ-pool plaintext on `V_X`" and does **not** specify that the winner's witness (tagged at its rotation layer) is harvested into the L_Bid pool. So `σ-pool = 2` is internally consistent with the appendix *as written* — the defect is the harvest keying, not the arithmetic.

**Independent finding — this contradicts the main spec.** `OBFT.md:469` already commits to the exact principle for bare OBFT: *"A byzantine leader's `σ_L^V` partial reaches the σ-pool via honest peers' witness sections … even when the byz suppresses its own gossip; the per-V dedup cap is enforced at aggregation regardless of receipt path."* The appendix's deadlock assumes the byz *can* suppress its leaked partial's effect at L_Bid. That is an internal inconsistency, not merely a missed optimization.

**Two recovery paths (the doc accounts for neither):**

1. **Witness-counting (timing-robust, primary).** Harvest the L_Bid winner `V_X`'s `σ^V` witness into the L_Bid σ-pool by `value_root`, regardless of the witness's layer tag. Then `σ-pool[V_X] = (honest σ-emitters) + (leader witness) ≥ (qV−f) + 1 = qV`. No timing dependence — the witness travels in the same `KindCommit`s as the honest σ-emissions.
2. **Peer-reflood-V (not viable at L_Bid).** At bare-OBFT L_0 a V-holder *early-emits* on bundle observation (well before `T_commit`), giving V-drop receivers a window to harvest `V` + witness and σ before their own `T_commit` fallback (`OBFT.md:241`–`OBFT.md:256`). **That window does not exist at L_Bid:** L_Bid σ-eligibility is gated on `verdict_quorum`, observable only near `T_commit` (after the `Δ_verdict` propagation window), so every operator's L_Bid commitment lands at ~`T_commit` with no intervening harvest gap — and single-emission OBFT has no late σ-upgrade (an NR-committed operator is EKM-locked). A V-drop receiver could harvest a peer's onion only ~`1·BTT` into Phase 2, *after* it already emitted. So peer-reflood-V is **not a usable recovery at L_Bid** (analysis: open-question Q1, resolved). **Witness-counting (path 1) is therefore the only timing-robust recovery.**

**Proposed fix.** In the appendix's Phase-3 walk, specify the L_Bid σ-pool harvest explicitly:

> L_Bid σ-pool on `V_X` = `{σ_j^V(V_X) from L_Bid onion σ-side entries}` ∪ `{σ_{L_h}^V(V_X) from any KindCommit witness section, matched by value_root(V_X), BLS-verified against the originating rotation leader's pubshare}`, deduplicated per operator.

This is value-keyed (not layer-keyed) **for the unique L_Bid winner only**, which is well-defined because L_Bid has exactly one winner and `σ^V(V_X)` is value-bound. Optionally also state that peer-reflood-V applies at L_Bid (defense-in-depth).

**Safety argument (Goal A) — the fix preserves Pigeonholes.**

- *Validity.* Only BLS-verified `σ^V(V_X)` partials count (`OBFT.md:268`); forgeries are rejected at verify time.
- *Bounded contribution.* One leader per value → the witness adds **≤ 1** partial to `V_X`'s pool; dedup-per-operator (`OBFT.md:309`) collapses byte-identical copies.
- *L_Bid σ-reconstruct XOR fall-through (mutual exclusivity), general `n = 3f+1`.* Let `e_h` = honest σ-emitters on `V_X` at L_Bid, `r_h` = honest NR-emitters; honest commit exactly once so `e_h + r_h = 2f+1`. The witness contributes ≤ 1 partial (one leader per value); the ≤ `f` byz may each NR. If `σ-pool = e_h + 1 ≥ qV = 2f+1` then `e_h ≥ 2f`, so `r_h ≤ 1` and `NR-pool ≤ r_h + f ≤ f+1 < qEnc = 2f+1` (for `f ≥ 1`). Conversely `NR-quorum (≥ 2f+1)` forces `r_h ≥ f+1`, so `e_h ≤ f` and `σ-pool ≤ e_h + 1 ≤ f+1 < qV`. The two cannot co-occur, so L_Bid either reconstructs `V_X` (walk halts at `OBFT.md:1195`, rotation layers stay sealed) or falls through — never both.
- *Across layers.* The witness partial may be counted at both L_Bid and its origin rotation layer `h`, but reconstruction halts at the first σ-quorum layer (L_Bid is outermost; `OBFT.md:325`), so rotation-`h` is never evaluated when L_Bid reconstructs. Pigeonhole 2 depends on what operators *sign* (`OBFT.md:268`); the byz signed only `V_X`, so no second value reaches qV. ∎

**Residual after fix.** The **equivocation** variant of 2-1-byz-defect (`OBFT.md:1272`, distinct-V or bid-metadata to the minority) is *not* closed by F1: detected equivocation forces honest to NR, so the witness alone (≤ 1 < qV) cannot reconstruct. That residual is real and **slashable** (base leader-equivocation / Rule 7 / Rule 8). So the corrected catalog should read: candidate-withholding-without-equivocation **closes** under the fix; the equivocation variant **remains** (slashable, deterred via assumption 4). (Claim-strength: "closes" is warranted for the no-equivocation variant — zero residual under the fix; "remains" for the equivocation variant.)

**Scope limit (what witness-counting does *not* close).** The fix adds the leader's partial to the pool only when the leader broadcast it but did not σ-emit at L_Bid, and only reaches qV when enough honest σ-emit alongside it. That holds for candidate-withholding-without-equivocation: the byz casts one (non-equivocated) verdict, so **all** honest compute the same `verdict_quorum` and ≥ qV−f of them σ-emit → `+1` witness = qV. It does **not** reliably close **verdict-equivocation**: there the byz fragments *which* honest see the quorum (`OBFT.md:1276`), so fewer than qV−f honest σ-emit; and when `V_X` is honest-led that leader σ-emits anyway, so its witness dedups (`OBFT.md:309`) and adds no net partial. Verdict-equivocation therefore remains a residual — **slashable** via Rule 8 (`OBFT.md:1239`).

**Implementation status (resolved — F1 is an impl change, not spec-only).** The σ-pool is strictly layer-keyed in code: `sigmaPool map[int]map[[32]byte]map[OperatorID]Signature` (`protocol/v2/obft/twoab/instance.go:168`), written by `addToSigmaPool(layer, vRoot, op, partial)` (`instance.go:934`); `tryReconstructLayer(layer, …)` checks the quorum **per layer** (`phase3.go:110`); and `harvestOneWitness` files a witness only into *its tagged layer's* pool, scoped to that layer's configured leader (`leaderID := i.cfg.Layers[w.Layer].Leader`, `phase2a.go:1078`–`1104`). No cross-layer dedup (same-operator partials coalesce only *within* a layer). L_Bid itself is **spec-only** (no implementation). Consequence: because L_Bid has no configured rotation leader, the winner `V_X`'s witness — tagged at its rotation layer — would never reach an L_Bid pool under the existing harvest. So when L_Bid is implemented, the fix is an explicit, localized addition mirroring `addToSigmaPool` but targeting the L_Bid index once `verdict_quorum` fixes `V_X`: `addToSigmaPool(L_Bid_index, ValueRoot(V_X), rotationLeaderID, leaderSigma)`. The data structure already supports it; safety holds (within-L_Bid same-operator coalescing prevents double-count; halt-at-first-σ-quorum with L_Bid outermost prevents double-output).

### F2 — The genuinely-new liveness cost is equivocation *promotion* (K/n), not "every slot"

*Goals touched: Safety (neutral) · All-honest liveness (neutral) · Adversarial liveness (reframes the true cost).*
*Source: review item #2.*

**Claim (2a).** Verdict-equivocation is harmless standalone: if honest agree on the bid winner, qV honest verdicts on `V_X` already form `verdict_quorum`, and the byz can only *add* to a pool (Pigeonhole on verdicts, `OBFT.md:1170`), never subtract honest verdicts. So `OBFT.md:1276`'s "some see quorum, some don't" requires a pre-existing honest verdict split — i.e. bid divergence, which requires the byz to be a winning rotation leader. The "available to any byz operator every slot" framing (`OBFT.md:1282`) conflates *action available* with *deadlock-causing*.

**Verdict: correct.** Verdict-equivocation is a rider on bid divergence, not a standalone deadlock. The byz amplifying an existing honest split into a false quorum still needs that split to exist.

**Claim (2b).** The real new mechanism is **promotion via bid inflation**. In bare OBFT a byz *deep-layer* leader that equivocates is harmless: the honest L_0 σ-quorums first and the deep layer stays encrypted (`OBFT.md:1199` walk halts at first σ-quorum). Under L_Bid, the same byz can inflate `bid_value` to promote its equivocated candidate to the **outermost gating layer** L_Bid, then equivocate with tight timing → L_Bid deadlocks → the honest L_0 below stays sealed under `nr_tag_LBid` → **miss**. This raises the equivocation-miss surface from **1/n** (byz must be the L_0 leader) to **K/n** (byz any rotation leader + bid lie).

**Verdict: correct, and the sharpest point in the review.** The outermost-gate design (`OBFT.md:1217`–`OBFT.md:1224`) is exactly what lets a deep-layer byz "reach up" and block the whole onion. The 1/n → K/n escalation is the concrete no-fall-through cost.

**Attestation is liveness-load-bearing.** Relay/builder attestation (`OBFT.md:1330`) caps `bid_value` at what a recognized relay committed to, so the byz cannot promote a low-real-value candidate by inflation — it can only reach L_Bid when it genuinely holds the top bid. That bounds *promotion frequency*, i.e. it is a **liveness** mechanism, not only the MEV-honesty mechanism the doc files it as (`OBFT.md:986`–`OBFT.md:996`). Without attestation (institutional-honesty deployments), the byz gets the full K/n promotion surface.

**Proposed fixes (documentation, no protocol change).**
- In §Liveness / New residual failure modes, restate verdict-equivocation as conditional on bid divergence (not standalone), and present equivocation-promotion as the genuinely-new residual with frequency K/n.
- Add the attestation→promotion-frequency coupling to §Liveness (currently only in §bid-value honesty), cross-referencing `OBFT.md:1330`.

**Safety (Goal A).** No change — these are documentation corrections.

**Open question.** Is there a design lever (short of relay-attestation) that lets L_Bid *fall through* on detected equivocation instead of deadlocking? This trades against the no-fall-through gate (`OBFT.md:1217`–`OBFT.md:1224`) and bid-routing priority; worth a dedicated analysis if adversarial-liveness is to be pushed further (secondary goal).

### F3 — All-honest liveness is clean except aggressive sizing

*Goals touched: Safety (neutral) · All-honest liveness (**this is the Goal-B finding**) · Adversarial (n/a).*
*Source: review item #3. Backbone of the Key Result above.*

**Verdict: conclusion correct; a sub-claim I made was wrong (corrected below).** At standard/conservative, all-honest L_Bid is success-or-clean-fall-through at its own layer (see Key Result). The only **L_Bid-added** all-honest miss is aggressive partial verdict propagation (`OBFT.md:1277`), Class A, eliminated at `Δ_verdict ≥ 1·BTT`. The 2-2 validity floor is *also* an all-honest miss, but it is **shared/inherited** from bare OBFT (not L_Bid-added).

**Correction (this sub-claim was wrong).** I previously argued the 2-2 validity floor "requires byz alignment, not an all-honest floor." That conflated two operator-count models. `OBFT.md:1278`'s "byz aligns to extend the split" is the **standard** n=4 model (3 honest + 1 byz: 3 honest can only split 2-1, so the byz's NR makes it 2-2 *on the pools*). In the **pure all-honest** model (4 honest, 0 byz) the split is 2-2 *directly*: at L_0, `σ-pool = 2`, `NR-pool = 2`, both `< qV = qEnc = 3` → deadlock → **miss, with no byz**. So the 2-2 validity floor **is** a genuine all-honest floor. What L_Bid changes is only that its *own* layer falls through on 2-2 validity (verdict-gating forces unanimous NR) instead of deadlocking; the floor then lands at L_0 exactly as in bare OBFT — **shared, not L_Bid-added**. Goal-B parity holds, but the floor is real (the review was right here; my earlier wording was not).

**Proposed action (Goal B).** Make `Δ_verdict ≥ 1·BTT` the normative floor for deployments targeting all-honest parity (drop aggressive, or mark it "non-parity / telemetry-gated only"). State the Key Result explicitly in §Liveness: *all-honest L_Bid matches bare OBFT (success or clean fall-through) at `Δ_verdict ≥ 1·BTT`; the only all-honest regression is aggressive sizing.*

**Safety (Goal A).** No change.

### F4 — `qBid = K−f` is near-vacuous at the recommended `K = f+1`

*Goals touched: Safety (neutral) · All-honest liveness (neutral — value, not liveness) · Adversarial (neutral).*
*Source: review item #4.*

**Verdict: premise confirmed; liveness sub-claim is sizing-dependent.** `K=f+1` (=2 at n=4) is the recommended default in five places (`OBFT.md:11`, `OBFT.md:36`, `OBFT.md:691`, `OBFT.md:698`, `OBFT.md:830`). There `qBid = K−f = 1` (`OBFT.md:1031`, `OBFT.md:1153`): an operator predicts from a single observed bid over a 2-candidate universe, while the residual catalog is worked at K=n=4 (`qBid=3`). The cost/value tension is real: bid-routing value scales with K, but the residuals are taken on regardless, and the value is weakest exactly at the recommended K.

**Caveat.** "Maximizes verdict-splits → frequent fall-through" is the tight-`Δ_select` worst case; with conservative/standard `Δ_select` settling most operators see both bids before verdicting and converge. The *structural* thinness (a single bid suffices to predict) stands; realized split rate is sizing-gated.

**Goal-B note.** `qBid=1` does **not** threaten all-honest liveness — splits cause *fall-through*, not miss. It is a *value* regression (less realized bid-routing). At low K, Goal B is met trivially because L_Bid barely engages.

**Proposed action.** Pick one and state it: (a) work the residual catalog at K=2 as well as K=n; or (b) document that L_Bid presumes a higher-K deployment to be worthwhile; or (c) reconsider flooring `qBid` at `K` for small K. Recommend (b), with a note pointing at (a) for the catalog. **This is more than a roadmap note:** the appendix's "When to use it" (`OBFT.md:1000`) currently sells the MEV upside and the residual cost *without* stating the upside is near-vacuous at the recommended `K=f+1` — so it is actively misleading a deployer who takes the default. The `K > f+1` caveat must land **there**, not only in this plan.

**Safety (Goal A).** No change (qBid governs bid-routing eligibility, not threshold reconstruction).

### F5 — Framing: attestation "optional"; "essentially free"

*Goals touched: documentation only.*
*Source: review item #5.*

**(5a)** Bid-value honesty is a genuinely new trust assumption (`OBFT.md:986`) and — per F2 — attestation is also liveness-load-bearing. The doc already says attestation is "recommended for SSV proposer duty" (`OBFT.md:992`); the gap is a missing *liveness* cross-reference, not absent guidance. **Action: strengthen the cross-ref (point §Liveness at attestation), don't re-argue.**

**(5b)** "Zero MEV-fetch cost under default RefloodDelay" (`OBFT.md:1296`) is a true *budget* statement, but it reuses the reflood buffer as mini-consensus headroom — "free" assumes the buffer is idle (healthy mesh). Under degraded mesh the cost is paid as **L_Bid success rate** (more fall-through to vanilla L_0), which is already documented as the success-rate gradient (`OBFT.md:1059`–`OBFT.md:1063`). The only issue is the "essentially free" headline (`OBFT.md:1328`) not cross-referencing it. **Action: cross-ref the success-rate gradient at the "free" headline; no new caveat.**

**Safety (Goal A).** No change.

### F6 — [Independent] Recommended-`K` contradiction

*Goals touched: documentation only.*

`OBFT.md:644` calls **`K = n = 4`** the "recommended OBFT default for proposer duty," directly contradicting the **`K = f+1` (=2)** default stated in five other places (`OBFT.md:11`, `OBFT.md:36`, `OBFT.md:691`, `OBFT.md:698`, `OBFT.md:830`). Likely stale text in the §Failure modes backup-cascade paragraph (where K=n maximizes fall-through depth, which may have leaked into a "default" phrasing). This is exactly the ambiguity F4 probes, so it should be resolved in the same pass.

**Action.** Reconcile `OBFT.md:644` to the `K=f+1` default (or, if K=n is genuinely intended for proposer duty, fix the other five sites — but the five-vs-one weight and the F4 analysis both favor `K=f+1`).

### F7 — [Independent, from deep-dive] Verdict-binding semantics are inconsistent (Rule 8 vs the fall-through rule)

*Goals touched: Safety (neutral) · All-honest liveness (**bug — honest operators generate false slashing evidence**) · Adversarial (changes the byz-defect deterrent).*

**The inconsistency.** Rule 8 (`OBFT.md:1239`) makes "broadcast `KindBidVerdict(σV(V_X))` **and** emit Phase-2 NR on `nr_tag_LBid`" **self-contained slashable**. But the L_Bid commitment rule (`OBFT.md:1185`) says an operator σ's only if it *observes* `verdict_quorum`, **else → NR**. So an operator that verdicts `V_X` but does not observe `verdict_quorum` (the cluster didn't converge) takes the honest fall-through (NR) — which is exactly the Rule-8 pattern. **Honest operators in a benign verdict-split emit self-incriminating evidence a byzantine can harvest and submit.**

**All-honest reachability (Goal B).** 4 honest, a near-tie or bid-view split → 2 verdict `V_X`, 2 verdict `V_Y` → no value reaches `qV` verdicts → all NR → clean fall-through *for liveness*, but all four now carry `verdict + NR` on the wire = Rule-8 evidence. It is a tail event (needs bid-view divergence; more likely at tight `Δ_select` and at `qBid=1`, F4) — but honest behavior must never produce slashable evidence.

**Root cause.** The spec is ambiguous whether a verdict is (a) a non-binding *prediction* or (b) a *commitment to σ*. Rule 8's verdict-vs-action arm assumes (b); the fall-through rule assumes (a). They cannot both hold: under (b), an all-honest verdict-split where each side is locked to σ on its value fragments `σ-pool` `2/2` with neither side able to NR → **all-honest deadlock** (strictly worse for Goal B); under (a), the fall-through is fine but Rule 8's verdict-vs-action arm is wrong.

**Proposed fix (required for Goal B).** Make verdicts **non-binding predictions**: `OBFT.md:1185` (verdict_quorum-gated σ, else NR) is the single commitment rule. **Narrow Rule 8 to its double-verdict arm only** (two distinct `KindBidVerdict` envelopes = slashable); drop the verdict-vs-action arm. By symmetry with the already-permitted reverse pattern (null-verdict→σ, `OBFT.md:1239`), verdict-`V_X`→NR must also be permitted.

**Cost — negligible, and independent of F1.** The dropped arm only ever caught an *irrational* byzantine. A byz that verdicts `V_X` to pad `verdict_quorum` and then wants to deadlock gets the **identical** deadlock from the *silent* variant (verdict `V_X` + Phase-2 silence): same `σ-pool < qV`, `NR-pool < qEnc`, but **no** verdict-vs-action evidence (silence emits no message) and a verdict that still looks honest. The NR-emit variant the arm catches is strictly dominated for the byz, so a rational byz never picks it. Dropping the arm therefore loses deterrence only against *irrational* byz — at **every** `f`, with or without F1. (This supersedes the earlier "F1 closes it anyway" rationale, which held only at f=1; F1's closure is a separate *liveness* benefit, not the justification for narrowing Rule 8. So F1 and F7 are **independent** — neither gates the other.)

**Safety (Goal A).** Unaffected — verdicts bind no threshold partials (`OBFT.md:1230`); this changes only slashing-rule scope and the σ/NR commitment wording.

## Deep-dive — can L_Bid fall through on equivocation? (Goal-B-secondary)

Answers the prior pass's open question. **Conclusion: not safely, at the running `f=1, n=4` config — the hard core is Pigeonhole 1's algebraic limit. Manage it with F1 + relay-attestation + slashing, not with added protocol machinery.**

**The L_Bid deadlock is always the σ-locked-split shape, never 1-1-1.** `verdict_quorum` admits at most one value cluster-wide: for two values to each reach `qV = 2f+1` verdicts across any honest views, each needs ≥ `f+1` honest verdicts (≤ `f` byz can't bridge alone), but honest total is `2f+1 < 2(f+1)`. So honest σ at L_Bid concentrates on the single winner `V_X` (or NR) — there is no multi-value σ-fragmentation. (Contrast bare-OBFT L_0, where a leader hands 3 honest 3 distinct values → 1-1-1.)

**The residual decomposes into two halves:**

1. **Hidden equivocation — closed by F1.** The byz delivers `V_X` cleanly to ≥ `qV−f` honest who σ on it without observing `V_X'`; F1's witness tops the pool to `qV` → success. The tight timing the byz needs to avoid detection is exactly what lets the σ-side form.
2. **Revealed equivocation + passive byz — irreducible at f=1 n=4.** The byz reveals `V_X'` to enough honest that the σ-side fragments below `qV` (those honest NR on the equivocation) while staying silent so the NR-side also falls short. At f=1 n=4 this bottoms out at 1 honest σ-locked on `V_X` + 1 byz passive ⇒ `σ-pool = 1 + witness = 2 < qV` and `NR-pool = 2 < qEnc`. This is **Pigeonhole 1's algebraic limit** — the same wall bare OBFT and 2abOBFT both hit at L_0 (1-1-1 equivocation; validity-divergence + passive byz, `2abOBFT.md:17`–`18`).

**Why a 2abOBFT-style no-lock/defer state (`KindNoValue`) does not rescue half 2 at f=1 n=4:**

- Its recovery is toward **σ-quorum** — V-drop operators stay harvest-eligible and *upgrade* to σ on the majority value (`OBFT.md:908`). But the promotion attack makes operators *observe* the equivocation, and the equivocation rule forces NR — they cannot upgrade to σ on `V_X`.
- It cannot rescue toward **NR-quorum** either: the value-holder σ-locks (the head-start that defines the family; `2abOBFT.md:16`), so the no-value cohort is only `{2 honest} = 2 < qEnc`. Un-σ-locking it to join NR would let one operator feed both σ- and NR-pools → breaks Pigeonhole 1 → **violates Goal A**.
- The only safe way to make half 2 fall through is to have value-holders **defer σ entirely** (lazy commit) — which discards the eager-σ head-start/fast-path that *is* OBFT (it becomes a confirm-then-commit protocol, i.e. QBFT-shaped).

**Recommendation (Goal-B-secondary): do not add a defer state to L_Bid.** It cannot close the residual at the running config and re-imports the wire-kind + dynamic-Phase-2b cascade OBFT deliberately sheds (`OBFT.md:853`, `OBFT.md:917`). Manage the residual with F1 (closes half 1), relay-attestation (bounds *frequency* — the byz reaches L_Bid only with a genuine top bid, F2), and slashing + assumption 4 (deterrence). The L_Bid-specific amplification — the no-fall-through gate seals the *whole onion* rather than one layer — is the deliberate bid-routing-priority trade-off (`OBFT.md:1217`–`OBFT.md:1224`); F1 narrows how often the cluster reaches the sealed state. (At larger `n`/`f` the cohort algebra loosens, so a defer state could recover more — but the running and recommended config is `f=1, n=4`, where it cannot.)

## Generalization to n ≥ 7 (f ≥ 2)

The catalog and the Key Result are worked at `f=1, n=4` (the recommended SSV config). Generalizing to `n = 3f+1`, `qV = qEnc = 2f+1`:

**Goal A (safety) generalizes cleanly.** F1's safety argument is already stated for all `n = 3f+1`: `σ-pool ≥ qV ⇒ NR-pool ≤ f+1 < qEnc` for `f ≥ 1`. The witness adds ≤ 1 partial (one leader per value) regardless of `f`, and the byz's witness-in-σ-pool + Phase-2-NR double-contribution does not break Pigeonhole 1 (the bound accounts for it). **Safety parity holds at all `n`.**

**Goal B (all-honest liveness) generalizes cleanly.** The Key Result is `f`-agnostic: `verdict_quorum` on `V_X` (`2f+1` verdicts, all honest) ⇒ `2f+1` honest hold and σ on `V_X` ⇒ `σ-pool = 2f+1 = qV` → success; else unanimous NR → fall-through. **All-honest parity holds at all `n`, at `Δ_verdict ≥ 1·BTT`.**

**F1 closes the candidate-withholding 2-1-byz-defect *only at f=1* (Q2/Q3 — the key adversarial limitation).** To form `verdict_quorum` the byz must induce ≥ `qV − f = f+1` honest to verdict (hence σ on) `V_X` and withholds from the rest, so the minimum honest σ-pool is `f+1`, deficit `f`. The single witness reduces the deficit to `f − 1`:

| f (n) | honest σ on V_X (`f+1`) | + witness | qV (`2f+1`) | F1 closes? |
|---|---|---|---|---|
| 1 (4) | 2 | 3 | 3 | **Yes** (deficit 1→0) |
| 2 (7) | 3 | 4 | 5 | No (deficit 2→1) |
| 3 (10) | 4 | 5 | 7 | No (deficit 3→2) |
| 4 (13) | 5 | 6 | 9 | No (deficit 4→3) |

The witness contributes exactly one partial (one leader per value), and peer-reflood-V has no usable window at L_Bid (F1 / path 2), so **F1 moves a slot from deadlock to success only when the deficit is 1 — i.e. only at `f=1`.** At `f ≥ 2` F1 narrows the σ-pool margin but the candidate-withholding deadlock persists as a no-fall-through miss.

**No-fall-through exposure reduction (Q3) — concentrated at f=1.** At `f=1`, F1 eliminates the *entire candidate-withholding row* of the 2-1-byz-defect table (`OBFT.md:1273`): both Phase-2 actions (NR-emit, silent) → success, since the witness completes `qV` independent of the byz's Phase-2 move. That removes the only **"fully behavioral"** (no cryptographic evidence) no-fall-through trigger; every remaining Class-B deadlock then carries cryptographic equivocation evidence (base leader-equivocation / Rule 7 / Rule 8), so the whole Class-B residual sits in the well-deterred (assumption-4) category. At `f ≥ 2`, F1 moves no slot out of the deadlock set, so this reduction does not generalize.

**Adversarial deadlock zones *widen* with f.** The all-honest aggressive-sizing partial-propagation deadlock (`OBFT.md:1277`) occurs for any verdict-pool split with σ-side `s ∈ {f+1, …, 2f}` (both pools below `2f+1`) — a single split (`s=2`) at `f=1`, but `f` distinct splits at general `f`. The validity-divergence deadlock (`OBFT.md:1278`) likewise covers a wider band of honest valid/invalid splits as `f` grows. Both reinforce the `Δ_verdict ≥ 1·BTT` floor (Goal B) more strongly at larger clusters. Trigger frequency also rises: candidate-withholding/equivocation needs the byz to be a rotation leader, and `P(≥1 of f byz is a leader) = 1 − C(n−f, K)/C(n, K) > K/n` (`= 1` at `K=n`).

**Net for n ≥ 7 (caveat to carry forward).** Safety **parity** and all-honest-liveness **parity** generalize cleanly: L_Bid adds no safety break and no all-honest miss at any `n = 3f+1` (at `Δ_verdict ≥ 1·BTT`). What degrades at `n ≥ 7` is **L_Bid's adversarial liveness specifically**: F1 no longer closes the candidate-withholding deadlock (deficit `f > 1`), and — unlike bare-OBFT L_0 — there is no peer-reflood-V fallback. The two widening zones above (aggressive-sizing and validity-divergence) are **shared family properties** — bare OBFT widens identically, so they are *not* an L_Bid-vs-bare-OBFT gap, and the aggressive one is excluded at the Goal-B floor `Δ_verdict ≥ 1·BTT` anyway. **Precise statement: safety and all-honest *parity* hold at all `n`; only L_Bid's *adversarial* liveness degrades at `n ≥ 7`** (the shared floors widen for the whole family, L_Bid included, but L_Bid does not worsen them). Deployments at `n ≥ 7` treating adversarial-byz tolerance as a hard requirement should weight this (it compounds bare OBFT's own `K ≥ f+2` guidance).

## Safety parity summary (Goal A)

The appendix's safety basis (`OBFT.md:1226`–`OBFT.md:1232`) is sound and unchanged by these findings: the mini-consensus binds no threshold partials (verdicts and bid-metadata contribute 0 to σ/NR pools, `OBFT.md:1230`); `nr_tag_LBid` is a deeper chained gate under Pigeonhole 3's induction (`OBFT.md:1232`).

The only proposed change that touches the σ/NR threshold surface is **F1's witness-harvest fix**, proven safe above (validity, bounded contribution, L_Bid σ-reconstruct-XOR-fall-through, halt-at-first-quorum). **F7** changes only slashing-rule scope and the σ/NR commitment *wording* (verdicts bind no partials), so it is safety-neutral. All other proposed changes are documentation. **Goal A is preserved under the full remediation set.**

## Proposed remediation roadmap (mapped to goals)

Priority order:

1. **[Goal B, high] Adopt `Δ_verdict ≥ 1·BTT` as the parity floor** and state the Key Result in §Liveness (F3). Zero MEV-fetch cost at default RefloodDelay. This alone secures all-honest liveness parity.
2. **[Goal B, high] Make verdicts non-binding; narrow Rule 8 to the double-verdict arm** (F7). Required so all-honest verdict-splits fall through without honest operators emitting false slashing evidence. Spec wording + slashing-rule scope; safety-neutral. **Independent of F1** — justified by the rational-byz argument (F7/Cost), not F1's closure; they can land separately.
3. **[Goal A-consistency + adversarial, high — impl change] Specify the L_Bid σ-pool harvest** (F1): value_root-keyed witness for the winner, with the safety paragraph. Resolves the `OBFT.md:469` inconsistency and closes the no-equivocation 2-1-byz-defect **at f=1** (partial mitigation at f≥2 — see §Generalization). This is an *implementation* change (the codebase σ-pool is layer-keyed), to be carried when L_Bid is built.
4. **[Docs, medium] Correct the residual catalog** (F2 + deep-dive): verdict-equivocation conditional on bid divergence; equivocation-promotion (K/n) as the genuinely-new residual; the σ-locked-split = Pigeonhole-1 limit at the gate; attestation→liveness-frequency coupling; explicitly *don't* add a defer state.
5. **[Docs, trivial — ship now] Resolve the `K` default contradiction** (F6): reconcile `OBFT.md:644` to the `K=f+1` default. One-line factual fix — **don't gate it on F4's analysis** (unbundled).
6. **[Doc-correctness, medium] Add the `K > f+1` caveat to "When to use it"** (F4): `OBFT.md:1000` currently sells L_Bid's MEV upside without noting it is near-vacuous at the recommended `K=f+1`. Pick a stance on `qBid` at small K and state it *there*, not only in this plan.
7. **[Docs, low] Cross-ref tightenings** (F5): attestation in §Liveness; success-rate gradient at the "free" headline.

## Open questions for deeper analysis

All five prior-pass open questions are now resolved (this revision):

- ~~**Impl harvest keying** (F1)~~ **Resolved:** σ-pool is layer-keyed (`instance.go:168`); F1 is an impl change. See F1 / Implementation status.
- ~~**Equivocation-promotion mitigation** (F2)~~ **Resolved:** [deep-dive](#deep-dive--can-l_bid-fall-through-on-equivocation-goal-b-secondary) — not safely achievable at f=1 n=4; manage via F1 + attestation + slashing.
- ~~**Peer-reflood-V timing at L_Bid** (F1)~~ **Resolved:** no usable window — `verdict_quorum` is a ~`T_commit` event (no early-emit gap) and OBFT has no late σ-upgrade. See F1 / path 2. Witness-counting is the only timing-robust recovery.
- ~~**n ≥ 7 generalization**~~ **Resolved:** see [§Generalization](#generalization-to-n--7-f--2) — Goal A/B generalize cleanly; F1 closes only at f=1; deadlock zones widen with f.
- ~~**Quantify no-fall-through exposure reduction** (F1)~~ **Resolved:** at f=1 F1 eliminates the entire candidate-withholding row (→ success), removing the only fully-behavioral trigger; at f≥2 it moves no slot out of the deadlock set. See §Generalization.

Remaining / next:

- **Draft the appendix edits** from the roadmap (the original next step).
- **(Optional) f ≥ 2 defer-state analysis** — whether a 2abOBFT-style no-lock state recovers any L_Bid deadlock at larger `n` (the deep-dive's hedged case); only relevant if `n ≥ 7` adversarial liveness becomes a hard requirement.
