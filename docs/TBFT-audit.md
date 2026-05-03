# Adversarial review of Appendix B.3 — L_Bid-prepended TBFT

The central marketing claim of B.3 is bold: **"preserves baseline TBFT's safety and liveness exactly… no regression in any failure mode, every slot baseline TBFT completes is also completed here"** ([TBFT.md L633](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L633), [L794](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L794), [L829](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L829)). The whole argument for choosing B.3 over B.1 ("strictly better than B.1") rests on this.

I think this claim is **false**, in a way that's not subtle. There are at least three distinct concrete byzantine strategies that deadlock the new `L_Bid` layer in scenarios where baseline TBFT completes cleanly. Beyond that, several specifications are missing or hand-waved. Findings below, ordered by severity.

---

## CRITICAL — claim of "no liveness regression vs baseline" is false

### C1. Selective-bid-withholding deadlock at `L_Bid` blocks fall-through

The σ-eligibility predicate at `L_Bid` is: "received valid Phase-1 envelopes from all `n` bidders by `T_candidate_accept`, AND validated the bid-winner's `V` locally" ([L641](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L641), [L655](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L655)). Otherwise NR_LBid. Per-operator σ-or-NR exclusivity at `L_Bid` ([L727](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L727)).

The doc's fall-through argument ([L804](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L804)): "If byz withholds their own bid envelope (or selectively broadcasts to fragment honest reception), all honest who don't see all `n` bids go NR-side at `L_Bid` → fall through to baseline TBFT structure → slot completes at `L_0_baseline`."

**This silently assumes byz withholds from *all* honest.** Byz can do better — withhold from *some* honest:

> Setup: 4 ops {A, B, C, D}. A=L_0_baseline, B=L_1_baseline, C=neither, D=byzantine.
>
> D is also a bidder (every op is). D delivers their bid envelope to A and B but withholds from C (or releases late enough that C's `T_candidate_accept` rejects it; gossipsub re-flooding has the same byzantine-leader-at-cutoff edge described at [L50](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L50)).
>
> - A: saw_all_bids=true, validates V_{L_Bid}=V_D → **σ_LBid**.
> - B: saw_all_bids=true → **σ_LBid**.
> - C: saw 3 bids only (missing D's) → **NR_LBid**.
> - D (byz): does nothing.
>
> σ_LBid pool: {A, B} = 2 < qV=3.
> NR_LBid pool: {C} = 1 < qEnc=3.
> **Neither quorum reaches at `L_Bid`. Layer 1's chained encryption cannot be peeled. Slot misses.**

In baseline TBFT this same scenario completes at L_0: D is *not* a leader, their behavior is irrelevant in Phase 1, and σ_L0 pool = {A, B, C} = 3 trivially.

This isn't an edge case — it's the *standard* byzantine-leader-at-cutoff edge ([L207](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L207)) extended to non-leader bidders. Baseline immunizes itself with `f+2 = qV` exact-equality math; B.3's prepended layer breaks that math because the σ-eligibility predicate now depends on the receipt of *every* bidder's envelope, not just the rotation leader's.

The structural defect: B.3 turns *every bidder* into a Phase-1-broadcast critical path. Baseline restricts that role to L_0 and L_1. B.3 *enlarges* the byzantine leader-at-cutoff attack surface from {L_0, L_1} (2 ops) to all `n` bidders (4 ops at n=4) — and gives byz a new griefing primitive: be a non-rotation bidder, withhold selectively, deadlock the bid layer without touching the baseline rotation.

The doc's "stricter precondition" argument ([L794](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L794)) — "any operator who would σ at L_Bid would also σ at the corresponding rotation layer if needed" — is true for σ-side, but **misses the NR-side constraint**. Honest who σ at L_Bid are σ-committed and can't NR_LBid (cross-phase exclusivity); honest who σ-committed but were "wrong" to (in the sense that NR-quorum was needed) cannot retract. The σ-or-NR rule at `L_Bid` cuts both ways.

### C2. Bidder-equivocation deadlock — the equivocation surface widens from 2 ops (baseline) to n ops (B.3)

Baseline has a known deadlock under leader equivocation when only some honest detect it ([L257](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L257) — "single-V receiver interaction"). The same shape appears at L_Bid, but the equivocation surface is `n` bidders rather than `K=2` leaders:

> D (byz, non-rotation) signs two distinct envelopes for slot s: env₁=(V_D, bid=high) sent to A; env₂=(V_D, bid=low) sent to B and C. Same V_D, different bids → different `value_root`-or-bid binding, different σ^op signatures, but only one σ_V on V_D (legal — the byz refrains from EKM-bypass).
>
> - A: sees env₁ → argmax (over A's view of 4 bids) → V_D as L_Bid. saw_all_bids=true. σ_LBid on V_D.
> - B, C: see env₂ → argmax → some other op X (whoever has the highest bid that B/C see) as L_Bid. saw_all_bids=true. σ_LBid on V_X.
> - σ_LBid pool on V_D = {A, byz-cross-sign?} ≤ 2 < qV.
> - σ_LBid pool on V_X = {B, C} = 2 < qV.
> - NR_LBid pool: 0 honest (all σ-committed). At most 1 byz cross-sign. < qEnc.
>
> **Deadlock.** No fall-through.

In baseline TBFT this same byzantine has no power because they're not a rotation leader — their envelopes are simply ignored in Phase 1. The doc's "Equivocation → NR" rule ([L821](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L821)) requires honest to *detect* the equivocation, but a single-envelope receiver cannot detect locally — same caveat as [L257](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L257), now applicable to all `n` bidders rather than just `{L_0, L_1}`.

### C3. Honest-disagreement-on-V_{L_Bid}-validity deadlock

Per [L227–L231](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L227), the application-validity-divergence deadlock at baseline L_0 is a known liveness limit. B.3 imports the same problem at L_Bid — but with a *broader* trigger surface: the bid-winner is determined per-slot from the cluster's bids, so the validity check fires on a different V each slot, and any single divergence (one honest's relay state differs, one head-change between bid broadcast and validate) hits the same deadlock pattern.

The doc claims "the σ-eligibility predicate (`saw_all_bids AND V_{L_Bid} valid`) ensures every σ-signer has the same input set and computes the same argmax" ([L779](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L779)). True for σ-side — but the deadlock fires when σ-side is *too small* (NV honest can't reach NR-quorum because σ-committed honest are excluded from NR pool by exclusivity). Same mechanism as baseline's [L222–L231](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L222), now triggerable at L_Bid in addition to L_0_baseline.

The doc handles this in the trade-offs table ([L827](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L827)) with "Layer deadlocks (same threshold); fall through to next layer if NR-quorum reaches" — but the parenthetical caveat is what bites: when σ-side is non-trivial at L_Bid, NR-quorum *cannot* reach at L_Bid, and fall-through is blocked.

---

## HIGH — under-specified or broken sub-mechanisms

### H1. The bid-hiding sketch in the open-questions section is broken

[L848](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L848) proposes tlock-encrypting bids under `(slot, "phase1-cutoff")` so all bids reveal simultaneously, with each bidder including a `σ^IBE((slot, "phase1-cutoff"))` partial. The "caveat" admits a byzantine who withholds their own IBE partial can aggregate `qEnc` honest partials and decrypt locally before the cutoff.

This isn't a "partial mitigation." It's **no mitigation** against any rational byz adversary at f=1, because at qEnc=3 a byz only needs to wait until 3 honest broadcast their IBE partials — which they will, since they have no reason not to. The byz then has the full bid set in plaintext before crafting their own bid. Anti-grinding is the *entire purpose* of the construction; the construction provides zero anti-grinding.

The pointer to "drand-style external time-released randomness beacon" hand-waves an entirely separate cryptographic dependency without acknowledging that drand has its own setup, trust-model, and liveness assumptions that B.3 hasn't analyzed. If the bid-routing motivation requires anti-grinding to be meaningful (and for MEV, it does — otherwise the byz can always extract the maximum), this sub-bullet should be promoted from open question to "B.3's value proposition is conditional on a not-yet-specified external dependency."

### H2. Phase-1 fetch timing for non-rotation bidders is completely unspecified

Baseline TBFT's `T_1 < T_0` asymmetry ([L23](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L23)) is *load-bearing* for the safety/value trade-off: backup is fetched against a deeper-confirmed parent (re-org-resistant, see [L625](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L625)), primary is fetched late for max MEV. B.3 says "every operator broadcasts a Phase-1 bundle" but never says *when* non-rotation bidders fetch.

If all `n` bidders fetch at `T_0` (late, MEV-optimized), then `L_1_baseline` is no longer a safe-early-fetched backup — it's just another late-fetched candidate, and the head-change-resilience B.3 claims to inherit from baseline ([L833](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L833) "Liveness equivalence to baseline TBFT") is silently destroyed. Baseline's head-change-divergence mitigation ([L350–L368](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L350)) explicitly relies on the asymmetric timing.

If non-rotation bidders fetch at `T_1` (early), they're bidding stale low-MEV blocks and their bids are uninformative — defeating the routing premise.

If non-rotation bidders use a custom timing schedule, that's spec-net-new and Appendix C's "asymmetric fetch times don't generalize cleanly" caveat ([L884](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L884)) bites directly.

This is a **glaring spec hole.** The protocol can't be implemented as written without resolving it, and each resolution introduces a different liveness/value-capture trade-off.

### H3. Bidder-fetch correlation under shared relay infrastructure

[L855](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L855) motivates B.3 with "operators on different relays, different searcher pipelines, different orderflow." Empirically this is *false* for typical SSV clusters — operators commonly share relay configurations (Flashbots, BloXroute, etc.) for reliability reasons. If 4 operators query the same relay at roughly the same time, they get correlated/identical MEV bids, and the bid-routing is just noise.

A more honest framing: the variance B.3 is supposed to capture exists across *clusters* (which run different operators with different relay setups) but rarely within a *cluster*. The motivation needs empirical grounding. Without it, B.3 may pay K=3 spec-surface for an effectively-zero value-capture upside.

Also: 4 operators all hammering the same relay endpoint at `T_0` increases the chance of relay rate-limiting + non-byzantine fetch failures. Per H2, if even *one* bidder fails to broadcast (whatever the reason), `saw_all_bids=false` for everyone → universal NR_LBid → fall-through. This is the *good* case, but it means the optimistic L_Bid path fires only when *all `n` operators succeed at fetching*. With per-op failure rate p, this is `(1-p)^n` ≈ `1 - np` for small p. At p=2% per op: L_Bid path activates ~92% of slots. The bandwidth cost (n bid envelopes + K=3 onion) is paid 100% of slots.

### H4. The σ_LBid eligibility rule is honest-only — byz can σ on a different V to grief without violating any pigeonhole

The σ-eligibility predicate ([L655](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L655)) is a rule for honest behavior, not a cryptographic constraint. Byz can σ_LBid on whatever V they want. If 2 honest argmax to V_X (the cluster's bid-winner per their view) and byz σ's on V_Y (an honest non-winner's V), the σ_LBid pool fragments:

- σ_LBid on V_X: {2 honest} = 2 < qV.
- σ_LBid on V_Y: {byz} = 1 < qV.
- NR_LBid pool: {1 honest who NR'd} + maybe byz_cross_sign = ≤ 2 < qEnc.

Same deadlock as C1. Byz needs only to deviate from argmax, no other cooperation. In baseline TBFT the byz has analogous-but-narrower power — they can only deviate as L_0/L_1, not as a non-rotation operator.

---

## MEDIUM — design questions the doc downplays

### M1. Bandwidth-cost framing is too generous

[L833](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L833) reports "+~50% (3 layers vs 2; each operator emits one extra σ or NR partial; chained encryption at layer 2)" and Phase 1 "≈ n × baseline." That's pessimistic on Phase-2 (correctly accounting for chained-IBE expansion) but optimistic on Phase-1: n × baseline = 4× at n=4, and each Phase-1 envelope now has additional bid-and-op-id binding. The bandwidth row of the trade-offs table understates this asymmetry.

More importantly: the propagation budget `D` at the candidate-acceptance cutoff is structurally tighter for n=4 envelopes vs 2. Gossipsub mesh propagation does not scale as O(1) in message count — there's queueing, processing, and amplification overhead. The `T_candidate_accept = T_commit − (D + δ)` budget needs to absorb *every* bidder's envelope reaching every honest before *every* honest's local cutoff. This eats into the `Δ_2` window or pushes `T_commit` later, both of which compress the relay-cutoff budget further.

### M2. Equivocation-rule extension to "all bidders" doubles slashing-evidence surface

[L843](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L843) acknowledges: "Every operator is a bidder every slot, so the equivocation rule applies cluster-wide every slot." This is a 4× increase in equivocation-evidence-collection surface vs baseline (where only L_0 and L_1 can equivocate per slot). The "Evidence shape unchanged" claim doesn't address: (a) how evidence is collected and stored at scale, (b) whether existing SSV slashing-protection logs key on per-bidder envelopes (they don't, currently — they key on per-validator-share signings), (c) the false-positive surface for bidder equivocation given that the bid is now a separate equivocable field on top of value_root.

### M3. Tiebreaker on `op_id` plus byz strategy

[L671](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L671): "lower `op_id` wins" on bid ties. A byz with the lowest `op_id` in a cluster has structural advantage: bid `= max_observed_honest_bid` ties with no honest (since honest don't tie at all), but on equality byz wins. Combined with bid-hiding being broken (H1), byz can grind: observe honest bids, set their own bid to maxhonestbid, win the tiebreak, steer the cluster onto V_byz (which is the byz's own block, controllable at will). This is a meaningful steering attack even with no bid-lying detection.

The mitigation ("could be any other deterministic function instead") is hand-wavy; a strong tiebreaker (e.g., VRF over (slot, op_id)) prevents structural advantage but adds spec surface. The doc should either pick one or acknowledge the lowest-op_id byz disadvantage.

### M4. "+~50% Phase-2-3 bandwidth" doesn't account for the n bidder Phase-1 σ_V partials that go unused

[L771](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L771): "Phase-1 σ_V partials from bidders not in `{L_Bid, L_0_baseline, L_1_baseline}` are unused for reconstruction — their values are not the protocol's commit target this slot." So the cluster broadcasts up to `n − 3 = 1` extra σ_V partials per slot at n=4 that exist purely as evidence. Acceptable at n=4 but asymmetric — at larger `n`, this scales linearly while bandwidth budget doesn't.

The trade-offs table should explicitly call out "bidder σ_V partials retained for attribution but unused for reconstruction." Otherwise readers may assume σ_V economy.

### M5. Phase-3 walk pseudocode notation: `(key_LBid + key_L0)`

[L762](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L762): "decrypted with `(key_LBid + key_L0)`." IBE chained decryption is *sequential nesting*, not key addition. The notation is wrong in a way that suggests the author may not have fully fleshed out the Phase-3 cryptographic detail. Should be explicit: "outer-decrypt with key_L0 to recover `E_{nr_tag_LBid}(σ_i^V(...))`; inner-decrypt with key_LBid to recover `σ_i^V(...)`."

### M6. The "post-hoc bid-attribution evidence" promise is undelivered

[L808](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L808) and [L844](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L844) point to "same as B.1: who runs it, where evidence lives, what triggers slashing. Out of TBFT scope; shared with B.1."

The bid-attribution machinery is not "out of TBFT scope" — it's the *only* enforcement mechanism that makes byzantine bid-lying bounded. Without it, byz can lie about bids to capture the L_Bid layer and steer the cluster onto V_byz, with no consequence. The "liveness fault, attributable post-hoc" framing ([L483](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L483)) is contingent on attribution actually existing somewhere in the system. Punt-to-out-of-scope is acceptable for a design sketch, but at minimum the sketch should call out that **B.3's value proposition is contingent on an unspecified slashing infrastructure that doesn't exist in SSV today**.

### M7. The "no L-identity-divergence" property is technically true but unhelpfully framed

[L783](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L783): "No L-identity-divergence regression — the σ-eligibility predicate (saw all bids, not local argmax) is what makes this property hold."

True: under sub-partial-synchrony, σ-side honest are *guaranteed* to have computed the same L_Bid (same complete bid set → same argmax). But what this fails to advertise is that σ-side may be *empty* (e.g., if 2 honest didn't see all bids and the third honest also didn't), or σ-side may be *too small* to reach qV (the C1/C3 patterns). The property "all σ-signers agree on L_Bid" is preserved at the cost of "σ-pool may be too small to reach quorum, and NR-pool may be too small to fall through." The trade is real and not summarized in this property.

---

## LOW — spec/clarity issues

### L1. "Strictly better than B.1" is overclaimed

[L860](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L860): "If pursued, this is the right shape for bid-routing in TBFT — strictly better than B.1 (which sacrifices hedging across layers and adds a fragmentation regression at K=2)."

Per the issues above (C1, C2, C3, H4), B.3 has its own liveness regressions vs baseline TBFT. The comparison "B.3 vs B.1" should weigh:
- B.1's K=2-bid-fragmentation regression vs
- B.3's L_Bid selective-withholding deadlock + bidder-equivocation-deadlock + V_{L_Bid}-divergence-deadlock.

Without a direct attack-by-attack comparison, "strictly better" isn't substantiated. B.3 buys hedging-preservation *at the cost of* a wider deadlock surface at the prepended layer. Whether that trade is favorable depends on attack-frequency assumptions the doc doesn't articulate.

### L2. "K=3 chain depth is 2 — small, but non-zero spec surface" understates audit cost

[L895](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L895): chained encryption at K=3 is "small spec surface." But the safety argument for B.3 *requires* Pigeonhole 3 (cross-layer safety), which is the whole reason TBFTR introduces chained encryption at all ([TBFTR.md L248](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFTR.md#L248)). The spec-surface delta isn't just the chained ciphertext format — it's the full Pigeonhole 3 proof + the offline-aggregation adversary model. Auditing B.3 needs the auditor to verify all three pigeonholes, not just the K=2 ones baseline TBFT relies on.

### L3. Cross-phase exclusivity at L_Bid is asymmetric across operators

The "leader's Phase-1 σ counts as their σ-side commitment" rule ([L89](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L89)) was clean in baseline because *only* L_0 has Phase-1 σ on V_{L_0}. In B.3, every operator has Phase-1 σ on their own V_op_id, but the σ on V_{L_Bid} is a *different* signing event (unless op_id = L_Bid). The cross-phase exclusivity rule needs to be specified per (slot, layer, V), not per (slot, layer, operator). The doc gestures at this at [L729](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L729) but doesn't clearly state how EKM enforces it across the multi-V signings of [L849](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L849).

### L4. "Bidder unable to produce a candidate" is treated as acceptable degradation, but it's the failure mode

[L846](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L846): "Acceptable degradation; worth specifying explicitly." But this is the *common case* — relay timeouts, mempool gaps, software hiccups. Even at low individual failure rates, `1 - (1-p)^n` is meaningful. Combined with H3 (bidder correlation), this could mean L_Bid succeeds in only 50–80% of slots. The trade-offs table ([L827](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L827)) implies L_Bid is a near-certain optimistic path; reality may be very different.

### L5. Phase 1 bandwidth claim "≈ n × baseline" ignores the per-envelope bid binding

Each Phase-1 envelope now binds `(protocol_tag, message_kind, cluster_id, slot, op_id, value_root, bid)` ([L661–L669](https://github.com/ssvlabs/ssv/blob/obft/docs/TBFT.md#L661)) — extra fields, larger envelope, larger σ^op signature. The "n ×" approximation is fine to first order but should be honest about the constant factor.

---

## Summary

The headline claim ("preserves baseline TBFT's safety and liveness exactly… no regression in any failure mode") is **structurally false**. There are at least three concrete byzantine strategies (C1 selective-bid-withholding; C2 bidder-equivocation; H4 byz-σ-on-non-argmax-V) that deadlock the prepended L_Bid layer in scenarios where baseline TBFT completes. The deadlock blocks fall-through to baseline's K=2 layers — so the "additive layer with full baseline fallback" framing doesn't survive contact with selective-broadcast adversaries.

The mechanism: B.3 turns every operator into a Phase-1-broadcast critical path, multiplying the byzantine-leader-at-cutoff attack surface from {L_0, L_1} to all `n` bidders. The cluster-state predicate `saw_all_bids` was supposed to make this safe — and it does for σ-cluster-consistency (Pigeonhole 2 at L_Bid still holds) — but it fails the *liveness* requirement, because the predicate is fragmentable and σ-vs-NR exclusivity then prevents the σ-fragmented honest from contributing to fall-through.

Beyond that, several sub-mechanisms B.3 leans on are under-specified or broken:
- Bid-hiding scheme (H1): broken against any rational byz adversary at f=1.
- Non-rotation bidder fetch timing (H2): completely unspecified; every resolution breaks something.
- Bidder-fetch correlation under shared relays (H3): may make L_Bid value-capture upside near-zero in production.
- Post-hoc attribution (M6): "out of scope" but is the *only* mechanism that bounds byz bid-lying.

**Recommendation:** the headline "no regression in any failure mode" needs to be retracted or qualified. The honest claim would be something like:

> B.3 preserves baseline TBFT's safety profile but introduces a *new* liveness deadlock at the prepended L_Bid layer under three adversary strategies (selective bid withholding, bidder equivocation, σ-on-non-argmax). The deadlock blocks fall-through to baseline's K=2 structure. Whether this trade is favorable depends on whether the value-capture upside of bid routing exceeds the additional liveness-loss rate from the new attack surface — which depends on relay-failure rate, cluster-relay diversity, and byz-frequency assumptions the protocol doesn't quantify.

The "B.3 strictly better than B.1" claim should also come down or be reframed: B.1 trades hedging for a K=2 fragmentation regression; B.3 trades K=3 spec-surface and a wider Phase-1 attack surface for hedging preservation. They're different points on a Pareto frontier, not totally ordered.

The protocol may still be *worth doing* — bid-routing has real upside if the attribution layer is built and the relay-diversity assumption holds — but the case has to be made on quantitative grounds, not on a "strictly additive optimization, no regression" claim that the design itself doesn't support.