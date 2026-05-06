# 2abOBFT — Two-Phase Witness BFT for Distributed Validators

A single-round agreement protocol for SSV clusters that produces one collective threshold-signed value per slot against a hard deadline. 2abOBFT separates the cluster's σ-eligibility *observation* from its cryptographic σ-*commitment* via a two-window Phase-2 split: Phase 2a is verdict broadcast (op-identity-signed claims about local σ-eligibility, no threshold partials), and Phase 2b is the binding σ-or-NR commit driven by Phase-2a's converged cluster-view.

The "2ab" in the name reflects this split — the protocol's defining feature relative to OBFT-family ancestors. Cryptographic safety is identical to single-Phase-2 protocols (chained IBE + EKM-enforced per-operator commitments + qV = qEnc = 2f+1); the split buys *liveness* — equivocation σ-locked split recovery, h_V=1 selective-delivery deadlock recovery, validity-divergence recovery within the f-bound, mesh-flakiness mitigation — at +1 RTT cost vs single-Phase-2 designs.

2abOBFT operates with K configurable layers (`max(2, f+1) ≤ K ≤ n`), each layer with its own deterministically-derived leader, falling through within a single Phase-3 reconstruction walk (sequential local decryption, no per-layer RTT). The running example throughout is `n = 4, f = 1, K = 4` for SSV Ethereum proposer duty; algebra generalizes to higher cluster sizes.

## When to use it

**Suited for:**
- SSV proposer duty under healthy-network partial synchrony (`D` ≈ 100ms cluster gossipsub P99/P999) where the +1 RTT vs single-Phase-2 designs fits the slot budget.
- Deployments operating under realistic adversarial conditions: small clusters, transient operators, weak governance, high-stake-to-grief-value ratios. The witness phase closes the σ-locked split equivocation, h_V=1 selective-delivery, and within-window validity-divergence patterns that single-Phase-2 designs leave as Class B / Class A failures.
- High-D networks (`D` ≈ 300–500ms) where multi-round protocols don't fit a 4s relay cutoff but a single round with the Phase-2 split still does.

**Not suited for:**
- Deployments where every millisecond of submission headroom is critical and the Class B grief patterns 2abOBFT closes are not relevant (e.g., low-stake testnet clusters with cooperative byzantines). [OBFT](OBFT.md) saves 100-300ms but exposes the closed-here failure modes.
- General-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. 2abOBFT (like the rest of the OBFT family) gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.
- Scenarios requiring host-validity-divergence recovery in 2-2 splits at f=1 n=4 (still Class A; the witness phase narrows the divergence window but cannot eliminate it). [QBFT](#a3--comparison-with-bare-obft-and-qbft) is the appropriate choice when validity is meaningfully unstable across the consensus window.
- Sustained partition tails beyond the absorption window (`Δ_2a + (D + δ)` ≈ 450ms at Config A recommended). Multi-round extensions are a future direction.

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n = 3f+1` (the BFT-tight setting; matches SSV's deployment configurations `n ∈ {4, 7, 10, 13}`). Running example: `n = 4, f = 1`. The threshold formula `qV = qEnc = 2f+1` below equals `n − f` exactly at this setting; this equality is what makes the bare Pigeonhole arguments in [§Safety](#safety-cryptographic-honest-majority) hold. (At `n > 3f+1`, the convergence rule's verdict-pool tie-break + `nr_eligibility_quorum` override would need to do the safety work — see [§Phase 2b convergence rule](#phase-2b--σ-or-nr-commit-t_commit--Δ_2a-t_commit--Δ_2a--Δ_2b); current spec does not exercise this case.)
- **Two threshold BLS keypairs** from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) as the threshold-signing scheme for no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (IBE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the threshold — see [Safety](#safety). At `n = 4`, `qEnc = 3`.
- A **leader-authentication signature scheme** (operator-identity key) for candidate broadcasts, verdict envelopes, and Phase-2b onion auth. Distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- **K layers** (`max(2, f+1) ≤ K ≤ n`, configurable; **K ≥ f+2 strongly recommended** — see below) with deterministically-derived leaders: layer 0 with **primary leader** `L_0`, layer 1 with **backup leader** `L_1`, ..., layer `K-1` with `L_{K-1}`. Leaders must be distinct (`L_i ≠ L_j` for `i ≠ j`). For `K = n`, every cluster member is a leader at exactly one layer per slot.

  **Two distinct K bounds:**
  - **`K ≥ f+1` is the BFT-liveness minimum** — pigeonhole over the f-byz bound guarantees at least one honest leader. At K < f+1, all leaders could be byzantine and no σ-quorum reaches at any layer.
  - **`K ≥ f+2` is the late-leader-resilience minimum** — pigeonhole guarantees ≥ 2 honest leaders, so a single late-broadcasting honest leader doesn't foreclose the slot via the deepest-layer NR-lock pathology.

  Concrete minimums by f: at `f = 1`, BFT-min `K = 2` but **late-leader-resilient `K = 3` recommended**, with **`K = n = 4` as the 2abOBFT default** for SSV proposer duty (every cluster member leads exactly one layer; maximum honest-leader probability via pigeonhole). At `f = 2, n = 7`, BFT-min `K = 3` but resilient `K = 4` recommended; at `f = 3, n = 10`, resilient `K = 5`.

- **Single agreement round per slot.** R is fixed at 1: one Phase 1 → Phase 2a → Phase 2b → Phase 3 sequence per slot, no retry, no re-flood across rounds. The slot's reconstruction deadline is the only deadline. Operators who do not reach σ-emit-eligibility by Phase-2a end commit NR at Phase 2b per the convergence rule (see [§Phase 2b](#phase-2b--σ-or-nr-commit-t_commit--Δ_2a-t_commit--Δ_2a--Δ_2b)). For deployments needing the wider partial-synchrony absorption of multi-round retry, see [OBFTR(R≥2)](OBFTR.md) (which currently runs without the Phase-2 split, but the split composes orthogonally with R-round retry — that's a future direction, not specified here).

- **Per-layer leader-fetch deadlines** `T_{K-1} < T_{K-2} < ... < T_1 < T_0`, plus a single cluster deadline `T_commit`. (`T_commit` is the Phase-1-broadcast cutoff; verdict broadcast and σ/NR commit happen after.) The asymmetric fetch times let primary leaders fetch high-MEV values late (`T_0` close to `T_commit`) while deeper layers' leaders fetch safe early values from deeper-confirmed parents (`T_{K-1}` well before `T_0`).

- **Three distinct deadlines** (do not conflate):

  - **Leader broadcast deadline** `T_broadcast_max = T_commit − 2(D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. Each layer's leader must finish broadcasting by this time so that under worst-case propagation, all honest first-observe by `T_commit − (D + δ)`. Per-layer fetch windows fit: `T_k + Δ_1 ≤ T_broadcast_max` for each leader `L_k`.
  - **Receiver acceptance horizon** `T_accept_max = T_commit + Δ_2a − (D + δ)`. Receivers accept Phase-1 bundles whose first-observation time is in `[slot_start, T_accept_max]`. A bundle first-observed past `T_accept_max` is auth-only-retained (usable for verifying re-flooded V's during Phase-2a, but cannot drive a Phase-2a verdict from the receiving operator; see [§Phase 1 / Late-bundle behavior](#phase-1--candidate-broadcast)).
  - **Verdict broadcast horizon** `T_verdict_max = T_commit + Δ_2a − (D + δ)`. Operators must emit their Phase-2a verdict envelope by this time so it propagates to all honest peers before Phase-2a end (`T_commit + Δ_2a`). Coincides with `T_accept_max` by construction.

- **Phase-window minimums:**

  - **`Δ_2a ≥ D + δ`** so verdict envelopes and re-flooded Phase-1 bundles propagate before Phase-2a end. **Recommended: `Δ_2a ≥ 2(D + δ)`** to absorb mesh-jitter and accommodate late-arriving bundles within Phase-2a's window.
  - **`Δ_2b ≥ D + δ`** so Phase-2b σ partials propagate before Phase 3.
  - **`Δ_3 ≥ (D + δ) + ε_3`** where `ε_3` ≈ 100ms is local processing time. Phase 3 must absorb (a) end-of-Phase-2b NR-partial propagation and (b) reconstruction processing.

- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

## Assumptions and implications

2abOBFT's claims hold conditional on six explicit assumptions. They are the same six as [OBFT](OBFT.md) except that assumption 5 simplifies (no Phase-1 σ_V to coordinate with later signings). The rest of the spec assumes them and refers back to this section rather than re-deriving the trade-offs each time.

### Assumed

1. **Standard BFT trust bound at the tight setting.** `n = 3f + 1`, up to `f` operators may be byzantine. At `n = 4`, `f = 1`. SSV deploys at the BFT-tight bound (`n ∈ {4, 7, 10, 13}` for `f ∈ {1, 2, 3, 4}`); the threshold formula `qV = qEnc = 2f+1` below requires this tightness to give `2f+1 = n − f`, which is what the bare Pigeonhole arguments depend on. Honest operators run protocol-conformant software (correct convergence-rule enforcement, correct EKM rule enforcement, correct host application, correct gossipsub behavior). Byzantine operators may deviate arbitrarily within their f-bound.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `D` (cluster gossipsub P99/P999) and clock skew `δ`. Safety is unconditional on timing; only liveness depends on this. **2abOBFT's effective absorption window is `Δ_2a + (D + δ)`** — the Phase-1-broadcast-to-receiver-first-observation tolerance for late bundles to still drive a Phase-2a verdict. Real propagation that exceeds this window is Class A "sustained partition" — out of scope by definition.

3. **Host validity is best-effort unanimous at decision time.** 2abOBFT consumes the host application's `valid` / `not-valid` verdict on `V_{L_k}` at Phase-2a verdict-broadcast time and at Phase-2b sign time. Operators may transiently diverge (e.g., a head change observed by some but not others). The host's job is to make divergence rare via per-operator stabilization. **Phase-2a's window narrows the divergence window structurally** — operators re-evaluate at Phase-2a verdict-broadcast time (not at Phase-1 acceptance time as in OBFT-family) and the convergence rule routes verdict-divergent operators through NR fall-through to a deeper layer's leader. Validity-divergence is *recovered within the f-bound* in 2abOBFT (e.g., 3-of-4 verdict σV vs 1-of-4 NV at f=1 n=4 succeeds at L_0 or L_1) but still cannot cross 2-2 splits at f=1 n=4 (insufficient cluster majority on either side; slot-misses cleanly).

4. **Persistent operator set with rational-byzantine deterrent.** 2abOBFT operates within a stable SSV cluster running protocol instances over many slots. The deterrent is the same one that already disciplines an offline operator under SSV's network-wide threat model: per-validator operator fees flow continuously to all cluster operators regardless of per-slot contribution (the remaining `n − f` honest carry the work at zero ops cost to the silent/byzantine), and stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters, collapsing the silent/byzantine operator's fee accrual to zero. SSV is already designed for the operator-down case ("the cluster and stakers deal with it"); the rational-byzantine claim is that a byzantine operator gains nothing an offline operator wouldn't already get, and has reputation (persistent across slots) to lose.

   **Asymmetry — Byzantine vs Down — and what restores equivalence.** With QBFT, `Byzantine ≡ Down` automatically: round-change rotates past silent or malformed PROPOSE/PREPARE/COMMIT, so the worst a byzantine can do per-slot is silently going offline. With OBFT-family, byzantine is *significantly worse on latency than Down* — equivocation σ-locked splits, h_V=1 selective-delivery, fake-encrypted-presence, behavioral σ-refusal, and (2abOBFT-specific) verdict-vs-verdict and verdict-vs-action equivocation can engineer per-slot grief above what equivalent offline behavior would produce. **2abOBFT structurally closes most of these — equivocation σ-locked splits and validity-divergence are recovered in-protocol via the convergence rule** — leaving 2-1-byz-defect and non-leader verdict-equivocation as the residual surfaces. The expected mitigation for the residuals is **manual blacklisting**: the cluster's surviving `n − f` operators agree out-of-band on the misbehaving operator's identity, push a config-file update to their nodes treating that operator's messages as silent for subsequent slots, and the byzantine's effective contribution becomes identical to offline — restoring the `Byzantine ≡ Down` guarantee. **The blacklist mechanism is a planned OBFT-family protocol extension** — current OBFT/OBFTR/2abOBFT do not specify it; until added, the byzantine's per-slot grief surface above offline behavior is bounded only by stakers eventually migrating validators away from the cluster.

   The on-wire byzantine-fault evidence ([§Slashing evidence](#slashing-evidence)) informs both staker migration decisions and (once the extension lands) the cluster operators' blacklist trigger. Rule 6a (verdict-vs-verdict) is decisively blacklistable on a single observed envelope-pair; Rule 6b (verdict-vs-action) is behavioral-pattern quality — boundary-conditional surface-ability, not unambiguous-from-single-message — so the deterrent's effective strength on Rule 6b depends on receiver convergence on the cluster's verdict pool. Filing a stake-slashing transaction via the SSV contract is a complementary punitive action for cryptographically-self-contained evidence, but is not the primary deterrent. See [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the full evidence-quality discussion.

5. **Coordinated EKM across both keypair shares + persistent Phase-2a state.** The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log keyed on `(slot, layer, side, value_root)`. **2abOBFT's EKM is the simplest in the OBFT family**: one signing event per (slot, layer) per operator at Phase-2b — no cross-round atomicity, no Phase-1 σ_V to coordinate with later Phase-2 σ, no persistent partial-sig cache.

   **However, 2abOBFT requires a separate per-slot persistent state** beyond the EKM log: each operator must durably record (a) any `KindVerdict` envelope they have broadcast at Phase-2a, and (b) the retained `(V, σ_L^op)` tuples per `(slot, layer, leader_id)`. This state is needed because Phase-2a verdicts are **not** EKM-logged (they are op-identity-signed, not threshold partials), but they bind the operator's behavior at Phase-2b: an operator restarting mid-slot whose pre-crash verdict is on the wire but whose retained-V state is lost cannot replay the verdict-broadcast decision (re-broadcasting would be self-equivocation under Rule 6a) and may be forced into an action that mismatches the on-wire verdict (Rule 6b false-positive against the honest operator).

   The persistent-state requirement is operator-internal — the EKM log alone is insufficient. Implementations should persist verdict envelopes and retained Phase-1 bundles per `(slot, layer)` to a durable store keyed on slot, with retention until reconstruction halts or the slot is declared missed. See [§EKM coordination model](#ekm-coordination-model).

6. **Independent V-keypair and IBE-keypair DKGs over the same operator set at the same threshold `qV = qEnc = 2f+1`.** Both DKGs are run once at cluster init with all `n` operators participating; the shared operator set and threshold makes [Pigeonhole 1](#pigeonhole-1--σ-vs-nr-at-the-same-layer)'s algebra work (`h_σ + h_NR ≤ 2f+1` across the cross-keypair signing log).

### Implications of safety being honest-majority cryptographic, not 100% cryptographic

2abOBFT's safety holds against an offline-aggregating byzantine *given the protocol rules are correctly enforced by honest operators*. Enforcement happens at three independent layers per operator — the **convergence-rule layer** (operator software computing Phase-2b commits from Phase-2a observations), the **EKM** (slashing-protection log that rejects bad signing requests at Phase-2b), and the **gossipsub layer** (operator software re-flooding bundles, verdicts, and onions). Together they ensure single-σ-V per (slot, layer) per operator and σ-XOR-NR per layer; these rules reduce the safety proof to pigeonhole arguments on the cluster-wide signed-message set.

An operator whose software actively misbehaves (maliciously compromised, or with compounding bugs across the convergence-rule + EKM layers that produce byzantine-equivalent signing behavior) consumes f-budget directly. With `f = 1` byz at `n = 4`, one such operator is tolerated; two violate the trust bound and Pigeonhole arguments can fail. Single-layer bugs (convergence-only or EKM-only) do **not** produce byzantine behavior on their own — the other layer catches them. This is the same trust posture as QBFT.

"Cryptographic safety" in this spec means: against a partially-synchronous byzantine network adversary plus up to `f` byzantine operators, honest operators following the protocol cannot produce two contradictory outputs. It does not mean: safety against arbitrary bugs in honest operator software.

### Implications of validity-divergence partial recovery (assumption 3)

2abOBFT recovers validity-divergence at f=1 n=4 within 3-of-4 majorities (recovers when honest majority agrees on validity). The 2-2 split at f=1 n=4 with all honest still slot-misses cleanly — no protocol can decide between two equally-supported sides without breaking BFT bound symmetry, and 2abOBFT's convergence rule routes such splits to NR-quorum fall-through, which at K = n with re-org affecting all layers may also miss. This is a OBFT-family inherited algebraic limit, not a 2abOBFT-specific gap. The host's stabilization workflow (re-evaluating against current head at Phase-2a verdict time, narrowing the divergence window to events landing inside the verdict-broadcast window) is the design's path to keeping 2-2 splits rare.

### Implications of equivocation recovery (assumption 4)

Unlike OBFT where equivocation σ-locked split patterns slot-miss, 2abOBFT structurally recovers most equivocation patterns at f=1 n=4 via the σ-eligibility-quorum-short rule: when honest verdicts split across multiple V's, no V reaches `verdict_pool[V] ≥ qV`, all honest rule-flip to NR, NR-pool reaches qEnc, slot falls through to the next layer.

The exception: **2-1 equivocation with byz-defect**. When byzantine leader delivers V to majority + V' to minority and verdict-claims σV(V) at Phase-2a but defects to NR at Phase-2b (slashable Rule 6b evidence), the slot misses at L_0 with no fall-through. Bare OBFT succeeds at L_0 in this case via cryptographic Phase-1 σ_V lock — 2abOBFT's regression here is the cost of removing Phase-1 σ_V to gain validity-divergence recovery and 1-1-1 equivocation recovery. Per-slot, this is a real cost; across many slots, the rational-byzantine deterrent (assumption 4) absorbs it. See [§Liveness / Equivocation patterns](#equivocation-σ-locked-split) for the full case analysis.

### Implications of the rational-byzantine deterrent (assumption 4)

The deterrent affects *liveness only*, not safety. Pigeonholes 1, 2, 3 hold cryptographically against any byzantine within the f-bound regardless of whether the byzantine is rational.

**The deterrent mechanism: SSV's existing offline-operator economics.** Per-validator operator fees on SSV are paid continuously to all cluster operators regardless of per-slot contribution — a byzantine that engineers slot-miss earns the same per-slot fee as an operator who is silently online or completely offline. Operations cost is ~zero (the other operators do the work). Stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters; once enough stakers migrate, the cluster's fee inflow drops to zero across all operators including the byzantine. This is the same mechanism that disciplines a permanently-offline operator. The byzantine's gain per slot is bounded above by what an offline operator would get; their loss (reputation, future cluster invitations, validator migration before the offending slot's fees materialize at scale) is real and persistent across slots.

**Byzantine ≡ Down in QBFT, significantly worse on latency than Down in OBFT-family — manual blacklist is the equalizer.** QBFT's round-change makes any byzantine deviation functionally indistinguishable from operator silence. OBFT-family has no round-change escape valve; **2abOBFT structurally closes most byzantine grief surfaces in-protocol** (equivocation σ-locked splits, validity-divergence, h_V=1 selective-delivery — all recovered via the convergence rule) but leaves residual surfaces (2-1-byz-defect, non-leader verdict-equivocation) where byz can engineer per-slot grief above what equivalent offline behavior would produce. The expected operational response for residual surfaces is a **manual blacklist**: the surviving `n − f` operators, on observing sufficient evidence of byzantine behavior, push a config-file update treating the byzantine operator's messages as silent for subsequent slots. The protocol must support this — message-level dropping/discarding by operator identity, plus duty-scheduling that excludes the blacklisted operator's leader rotation — as a planned protocol extension. **Current OBFT/OBFTR/2abOBFT do not specify the blacklist mechanism**; once added, the byzantine's residual grief surface above offline behavior is bounded by detection latency + cluster governance reaction time.

**Sketch of the planned blacklist mechanism.** Each operator attaches a 2-byte (16-bit) **blacklist bitfield** to their first message in each slot — Phase-1 bundle for layer leaders, Phase-2a verdict envelope for non-leaders. Each bit indicates "I locally consider this operator blacklisted"; 16 bits accommodates SSV's largest cluster size (`n ≤ 13`). The bitfield is covered by the carrying message's operator-identity-key auth envelope, so the signal is attributable per `(operator, slot)`. Receivers maintain a per-`(slot, target)` ACK count; once an operator observes **`f+1` ACKs** on any target, they treat the target as silent for the slot's duty — leader rotation skips the target's layer (the K-layer fall-through advances to the next layer), the target's verdict/σ/NR contributions are ignored, and any Phase-1 bundle from the target is dropped.

The `f+1` threshold is the BFT-liveness minimum (any `f+1` distinct ACKs contains at least one honest agreement by pigeonhole at the f-byz bound); 2f+1 would be the full BFT-quorum strength but slower to activate. The threshold is a deployment tuning knob between activation latency and false-positive resistance against byz bit-flipping to falsely flag honest operators. Blacklist state persists in each operator's local store across slots, so a target blacklisted in slot N stays blacklisted in N+1, N+2, ... unless explicitly rehabilitated.

**Within-slot timing.** Because the bitfield piggybacks on first-broadcast, blacklist convergence happens *during* the slot. The byz can still grief the slot in which they are first being added to others' blacklists; effective enforcement is from the *following* slot onward, bounding the byz's per-slot latency-grief above offline behavior to the detection-and-convergence window. For 2abOBFT specifically, the residual blacklist scope is narrow (2-1-byz-defect, non-leader verdict-equivocation); most byz patterns recover in-protocol via the convergence rule without needing the blacklist.

The protocol surfaces seven rules (counting 6a and 6b separately) of byzantine fault evidence on the wire (signed by the offender's own keys, verifiable in isolation by any observer); the surviving operators consume this evidence to drive blacklist decisions. The deterrent's effective strength depends on three deployment-level factors:

1. **Evidence quality.** Six of the seven rules leave self-contained cryptographic proofs that can drive a single-observation blacklist decision confidently. Rule 6b (verdict-vs-action equivocation) alone is *behavioral-pattern quality* — receivers must cross-reference cluster verdict view to distinguish honest revision from byzantine equivocation. Rule 6a (verdict-vs-verdict equivocation) is decisively blacklistable on a single observed envelope-pair. False-positive risk is correspondingly higher only for Rule 6b.
2. **Coordination responsiveness.** Active operators with clear governance push the blacklist update faster than passive ones; small clusters with one or two honest operators take longer to converge than larger clusters.
3. **Byzantine's stake.** A byzantine that values continued cluster fee accrual avoids actions that would get them blacklisted. A byzantine already exiting (no future fee accrual to lose) is not deterred per-slot.

Where any of these is weak (small clusters, transient operators, weak governance, byzantine on their way out, high-MEV slots where per-slot grief value spikes), the deterrent is correspondingly weaker — but for 2abOBFT, the Class B grief patterns the protocol structurally closes (equivocation, validity-divergence, h_V=1) remain closed regardless. The deterrent's residual scope is the 2-1-byz-defect and non-leader verdict-equivocation patterns; the protocol itself is the primary defense, the deterrent is the residual safety net.

Filing a stake-slashing transaction via the SSV contract is a complementary punitive action available where the evidence is cryptographically self-contained, but it is not the primary deterrent — the primary deterrent is the economic equivalence to going offline plus the eventual `Byzantine ≡ Down` collapse via blacklisting.

## Protocol

2abOBFT runs **a single agreement round** per slot: Phase 1 → Phase 2a → Phase 2b → Phase 3. Phase 1 is a fresh broadcast (no re-flood across rounds, since there is only one round). The slot's hard deadline (`T_round_end`) is the cluster's reconstruction cutoff; a slot that does not reach σ-quorum at any layer by `T_round_end` is missed.

### Phase 1 — Candidate broadcast

Phase 1 has K per-layer windows (driven by the asymmetric fetch times): `[T_{K-1}, T_{K-1} + Δ_1]` for the deepest backup, then progressively `[T_{K-2}, T_{K-2} + Δ_1]`, ..., ending at `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see [§Preconditions on the host application](#preconditions-on-the-host-application)).
2. Signs `V_{L_k}` with the **operator-identity key** — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a structured envelope binding `(protocol_tag = "2abOBFT-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates 2abOBFT Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, [OBFT](OBFT.md), [OBFTR](OBFTR.md), other 2abOBFT message kinds). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. **No threshold partial signature is produced at Phase 1** — the leader's σ-side commitment happens at Phase 2b, uniformly with all other operators.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^{op}(envelope))` to peers via gossipsub.

**Why no Phase-1 σ_V.** [OBFT](OBFT.md) and [OBFTR](OBFTR.md) include `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle, giving the cluster a "head start" of one real threshold partial as soon as Phase 1 succeeds. 2abOBFT removes it because the Phase-1 σ_V locks the leader's σ-commitment irrevocably at fetch time — when host validity flips post-fetch (e.g., a re-org changes parent_root), the leader cannot retract, and the cluster is structurally blocked from converging on NR-quorum at that layer (the leader's σ_V counts as σ-side per cross-phase exclusivity, capping non-leader honest NR contributions at 2f < qEnc). The Phase-1 σ_V is the structural obstacle to validity-divergence recovery in single-Phase-2 OBFT-family designs ([docs/OBFT.md / Implications of validity-divergence not being recovered](OBFT.md#implications-of-validity-divergence-not-being-recovered-assumption-3)). 2abOBFT trades the Phase-1 head-start for late-binding flexibility — the leader's σ commitment happens at Phase 2b based on their host's verdict at that time, which the convergence rule can route through NR-quorum fall-through if validity has diverged.

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify the leader-auth signature against the leader's known pubkey, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ^{op}` signs, and check the first-observation timestamp against the receiver acceptance window. Bundles failing cryptographic-auth are silently dropped.
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. **The host validity check is run again at Phase-2a verdict-broadcast time** (potentially with a more recent head snapshot — see [§Receiver-side validity stabilization](#head-change-handling)).

Bundles passing cryptographic-auth checks are classified by first-observation timestamp:

- **First-observation ≤ T_accept_max**: bundle is accepted as a Phase-1 candidate. The operator may issue a `KindVerdict` envelope at Phase-2a based on their host's validity verdict on `V_{L_k}`.
- **First-observation > T_accept_max**: bundle is **auth-only retained**. The cryptographic-auth checks have passed at this point, so the leader's auth signature is retained for the slot. Auth-only retention does *not* allow the operator to issue a `KindVerdict` based on this bundle (which would re-open timing-fragmentation grief surfaces). Auth-only-retained bundles can be used to verify the leader-auth on a peer-re-flooded V at deeper layers' Phase-2a (a defense against fake-V-injection by a byzantine operator).

**Retention bounds.** Retention state for accepted Phase-1 bundles is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **two distinct `(V, σ_{L_k}^{op})` tuples** — the first auth-valid bundle observed, plus, if a second auth-valid bundle with a *distinct* `value_root` is later observed, that one (sufficient for both Phase-2a verdict on the chosen V *and* leader-equivocation evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Retention lifetime: until the operator's local end of Phase 3; slot state is then cleared. This caps memory at `O(K · n)` bundles per slot in the worst case (every leader equivocates).

**Bundle propagation.** Honest receivers re-flood every bundle via standard gossipsub on first observation. Re-flood propagation continues into Phase 2a (the "bundle re-flood" activity below) — late bundles arriving in `(T_commit, T_accept_max]` are absorbed into Phase-2a's verdict-eligibility window for receivers who first-observe them in time.

**Equivocation handling at Phase 1.** If the receiver retains 2 distinct `(V, σ^{op})` tuples at the same `(slot, layer, leader_id)`, that is leader equivocation. The pair is self-contained slashable evidence (Rule 2; see [§Slashing evidence](#slashing-evidence)), gossipped for out-of-band slashing. The receiver's local Phase-2a verdict for this layer is `NR-due-to-equivocation` regardless of host validity verdicts on either V — single-σ-V exclusivity at Phase-2b prevents the operator from σ-emitting on either side once equivocation is observed.

### Phase 2a — Bundle re-flood + verdict broadcast `[T_commit, T_commit + Δ_2a]`

Two activities run in parallel during Phase 2a:

#### Activity 1 — Bundle re-flood

Standard gossipsub re-flood of any retained Phase-1 bundles. Honest receivers forward bundles to peers on first observation. By Phase-2a end (under partial synchrony with `Δ_2a ≥ 2(D + δ)`), bundles broadcast at the leader's `T_broadcast_max` deadline have propagated to all honest receivers within Phase-2a's effective acceptance window. Late-arriving bundles past `T_accept_max` enter auth-only retention.

This is the primary bundle-distribution path. The Phase-2a window's purpose for bundle re-flood is identical to OBFT's Phase-2 widening — late re-flood absorption — except that 2abOBFT's split structure means late receivers can issue Phase-2a verdicts on the recovered V (at the cost of the verdict propagating before `T_verdict_max = T_commit + Δ_2a − (D + δ)`).

#### Activity 2 — Verdict broadcast

Each operator `i`, for each layer `k ∈ {0, ..., K-1}`, computes their local verdict at this layer and broadcasts a single `KindVerdict` envelope:

1. **Compute local verdict at layer `k`**:
   - If `i` retained ≥ 2 distinct V's (equivocation observed) → `verdict = NR` (operationally `NR-due-to-equivocation`; wire form is unchanged from `NR`). `value_root = null`.
   - If `i` retained 1 V and host returns `valid` at this moment → `verdict = σV`. `value_root = hash(V_{L_k})`.
   - If `i` retained 1 V and host returns `not-valid` at this moment → `verdict = NV`. `value_root = null`.
   - If `i` retained 0 V's → `verdict = NR`. `value_root = null`.

2. **Construct envelope**: `(protocol_tag = "2abOBFT-v1", message_kind = "phase2a-verdict", cluster_id, slot, operator_id i, layer k, verdict, value_root)`.

3. **Sign with operator-identity key**: `σ_i^{op}(envelope)`.

4. **Gossip via gossipsub.**

The verdict envelope is **op-identity-signed, not threshold-signed**. EKM/slashing-protection is *not* consulted for Phase-2a verdicts — they are non-binding cryptographically. The operator's binding commitment happens at Phase-2b sign time (where EKM enforces single-σ-V and σ-XOR-NR per layer).

**Verdict equivocation slashable.** If `i` is observed broadcasting two distinct `KindVerdict` envelopes for the same `(slot, layer)`, the pair is self-contained cryptographic slashable evidence (**Rule 6a**; see [§Slashing evidence](#slashing-evidence)). The pair is unambiguous from a single observer's view — both envelopes are op-identity-signed by `i`. Honest receivers count `i`'s **first-observed** verdict for convergence purposes; subsequent verdicts are dropped from convergence input but recorded as slashable.

**First-observed convergence is per-peer, not cluster-wide.** Different peers may first-observe different verdicts from the same equivocating operator (byzantine `i` can engineer per-peer first-observed by injecting different verdicts into different peers' meshes near `T_verdict_max`). Re-flood eventually delivers both verdicts to all peers, but the "subsequent" verdict does not flip the convergence count — it only flips the slashing-evidence record. **This is a load-bearing assumption with a real attack surface**: see [§Failure modes / Class B / Non-leader verdict-equivocation under marginal h_V](#failure-modes) for the regression this enables.

**Verdict-spam rate-limit.** A byzantine operator can broadcast many distinct verdict envelopes per `(slot, layer)`. Honest receivers retain the first-observed for convergence purposes; subsequent envelopes are processed for slashing evidence but otherwise dropped. Each receiver MUST gossip slashing evidence at most once per `(slot, layer, operator_id)` tuple (the same anti-amplification rule as Rules 5 and 6b). Per-receiver memory remains bounded by gossipsub message-id deduplication and the per-(slot, layer) retention cap.

**Verdict broadcast timing.** Operators should broadcast their verdict as late as possible within `[T_commit, T_verdict_max]` to maximize the time available for late-arriving bundles to drive the verdict. A practical schedule: each operator broadcasts at `T_verdict_max − ε_proc` where `ε_proc` is the operator's local processing budget for verdict construction + signing. Earlier broadcast risks issuing a verdict that doesn't reflect a late-arriving bundle (forcing the operator into an honest verdict-vs-action revision at Phase-2b end).

**Verdict propagation budget.** Verdicts broadcast at `T_verdict_max − ε_proc` propagate to all honest peers within `D + δ` under partial synchrony, reaching them by `T_verdict_max + (D + δ) − ε_proc = T_commit + Δ_2a − ε_proc`. With recommended `Δ_2a = 2(D + δ)`, this leaves `D + δ − ε_proc` of slack between verdict arrival and Phase-2a end (`T_commit + Δ_2a`) — enough margin for one full propagation cycle's variance. At minimum sizing `Δ_2a = D + δ`, slack collapses to `−ε_proc` (the verdict barely makes it; processing-delay variance can push it past Phase-2a end for some peers). **Recommendation: never use minimum Δ_2a sizing in production**; the recommended `2(D + δ)` is what makes the verdict propagation budget viable.

**One verdict per operator per (slot, layer).** The operator must commit to their verdict before broadcasting (no "tentative" verdicts that get overwritten — the second verdict is equivocation). To handle late-arriving bundles cleanly, the operator should *delay* their verdict broadcast as long as possible, not issue early-and-revise.

**Late-bundle path within Phase-2a.** A Phase-1 bundle re-flooded into Phase-2a (first-observed past `T_commit` but ≤ `T_accept_max`) lets the receiving operator issue a `σV` verdict instead of `NR` (no-V) — provided the operator's verdict broadcast happens before `T_verdict_max`. If the bundle arrives later than `T_verdict_max`, the operator's verdict will already be NR (no V at verdict time); the bundle becomes auth-only retained.

### Phase 2b — σ-or-NR commit `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]`

At the start of Phase 2b (`T_commit + Δ_2a`), each operator `i` computes its **convergence decision** per layer based on observed Phase-2a verdicts and its own local state.

#### Convergence rule

For each layer `k`:

- Let `V_local` be `i`'s retained V at layer `k` (if any single retained V — equivocation observed → no V_local).
- Let `verdict_pool[V] = { distinct ops j : j broadcast a first-observed KindVerdict(j, slot, k, σV, hash(V)) }`. (Counted per-distinct-operator; verdict equivocation collapses to first-observed.)
- Let `nr_pool = { distinct ops j : j broadcast a first-observed KindVerdict(j, slot, k, NR | NV, _) }`.
- Let `σ_eligibility_quorum = ∃V : |verdict_pool[V]| ≥ qV`.
- Let `nr_eligibility_quorum = |nr_pool| ≥ qEnc`.

`i`'s commit at layer `k` (evaluated **in order** — first matching row wins):

| # | Condition | Commit |
|---|---|---|
| 1 | Equivocation observed (`i` retained ≥ 2 distinct V's at this layer) | `NR` (NR-due-to-equivocation; `V_local` undefined) |
| 2 | `nr_eligibility_quorum` reached | `NR` (regardless of `i`'s own verdict; honest defers to cluster NR-side decision) |
| 3 | `σ_eligibility_quorum` reached on V AND `i` has `V_local = V` AND host re-validates V as valid at Phase-2b sign time | `σ` on V |
| 4 | `σ_eligibility_quorum` reached on V AND `i` does not have `V_local = V` (or host re-validation says NV) | `NR` |
| 5 | Neither quorum reached | `NR` (verdict-quorum short — cluster did not converge on V; honest defaults to NR) |

**Tie-break note.** At `n = 3f+1` (the SSV BFT bound), at most one V can have `|verdict_pool[V]| ≥ qV` cluster-wide because `Σ_V |verdict_pool[V]| ≤ n = 3f+1 < 2 · qV = 4f+2`. So no σ-side tie-break is needed at the recommended SSV configurations. (At `n > 3f+1`, multiple V's could conceivably reach qV in pathological verdict patterns; the rule there is "lowest `value_root` lexicographic" as a deterministic tiebreak.)

**NR-vs-σ tightness at n = 3f+1.** Symmetrically, the row 2 (`nr_eligibility_quorum` reached) → NR override never fires *ambiguously* at `n = 3f+1` either: if `nr_eligibility_quorum` (≥ qEnc = 2f+1) reached, then `verdict_pool[V] ≤ n − qEnc = f < qV`, so no V can simultaneously have σ-eligibility-quorum. Row 2 is "tight" — at the BFT bound, both quorums cannot simultaneously reach, so the override is structural rather than disambiguating between two reached quorums. At `n ≥ 4f+2`, both quorums could reach simultaneously and row 2's "NR overrides σ" rule becomes load-bearing for the convergence direction.

**Why the rule "nr_eligibility_quorum overrides everything".** If qEnc operators (a quorum) verdict-claimed `NR/NV`, the cluster is collectively saying "this layer's V is not viable". Honest who *would* have σ'd defer to the cluster decision and NR-emit. Symmetry preserves Pigeonhole 1 (σ-quorum and NR-quorum cannot both reach).

**Why the rule "σ_eligibility_quorum requires V_local"**: an operator without `V_local = V` cannot compute `σ_i^V(V)` (no V to sign over). The rule degrades them to NR — consistent with their inability to participate in σ-pool cluster-wide.

**Why the rule "host re-validates"**: the operator runs the host's validity check again at Phase-2b sign time. If state has shifted post-Phase-2a (e.g., further re-org), the operator re-evaluates and may fall into the "host says NV" branch even though σ-eligibility-quorum was reached. NR is the safe action — the cluster's σ-pool may still reach qV from other operators who continue to validate.

#### Phase-2b emission

Each operator emits per their commit. The emission is wrapped in a single auth envelope `KindOnion2b` signed by `i`'s operator-identity key, binding `(protocol_tag = "2abOBFT-v1", message_kind = "phase2b-onion", cluster_id, slot, operator_id i, per-layer commits)`.

Per-layer commit content:

- **σ on V at layer k**: a single Phase-2b σ partial.
  - At layer 0: plaintext `σ_i^V(V_{L_0})`.
  - At layer `k > 0`: chained-IBE-encrypted `E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( σ_i^V(V_{L_k}) ) ... )`. The chain depth at layer `k` is `k`, applied innermost-first when constructing.
- **NR/NV at layer k**: a Phase-2b NR partial `σ_i^{IBE}(nr_tag_k)` (only for `k ∈ {0, ..., K-2}`; the last layer has no NR tag).
- **No commit at layer k** (e.g., insufficient information): the layer slot is empty in the operator's onion. This is distinct from σ or NR — the operator is contributing nothing at this layer. (In practice, the convergence rule resolves every operator to σ or NR per layer; no-commit only fires at the deepest layer K-1 if equivocation is observed and there is no NR tag for force-NR.)

EKM/slashing-protection is consulted at Phase-2b sign time:

- `Sign σ on V at (slot, layer)` (V-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "σ", value_root(V))`.
- `Sign NR on nr_tag_k at (slot, layer)` (IBE-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "NR", null)`.

**Single signing event per (slot, layer) per operator.** This is the cleanest point in the OBFT family — no Phase-1 σ_V to coordinate with later Phase-2 σ, no cross-round atomicity, no persistent partial-sig cache. Standard transactional sign-and-log spans the operator's V-share and IBE-share.

**Per-operator commitment is exclusive across phases.** An operator who emitted `σ_i^V(V)` at layer `k` on any V has σ-side committed at this layer; they may **not** subsequently broadcast NR/NV on `nr_tag_k`. They may also **not** σ on a different `V'` at the same `(slot, layer)`. EKM enforces this cryptographically via the slashing-protection log keyed on `(slot, layer)`. Across layers, commitments are **independent**.

A byzantine operator that publishes both σ and NR on the same `(slot, layer)` is publicly attributable (Rule 1; see [§Slashing evidence](#slashing-evidence)). Under `qEnc = qV`, cross-signing has no safety impact.

### Phase 3 — Local decryption and reconstruction `[T_commit + Δ_2a + Δ_2b, T_round_end]`

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches on some V (output produced), or NR-quorum reaches (advance to next layer), or neither (slot misses).

```
L_C = 0                                        # current frontier; advances per loop iteration
sigs[k] for k in [0, K)                        # σ pools, indexed by layer (initially empty)
nrs[k]  for k in [0, K)                        # NR pools, indexed by nr_tag_k (initially empty)

for k in [0, K):
    # Aggregate σ pool at layer k from received Phase-2b onion contents.
    sigs[k] = {σ_j^V(V) from received layer-k onion contents on any value V}
              # decrypted via accumulated NR-decryption keys at layers 0..k-1, if k > 0
              # deduplicated per operator
              # Per Pigeonhole 2, at most one V can have qV partials cluster-wide,
              # so partition sigs[k] by V and check each.

    nrs[k]  = {σ_j^{IBE}(nr_tag_k) partials, deduplicated per operator}

    # Reconstruction attempt:
    if exists V such that |{partials on V in sigs[k]}| ≥ qV:
        S = reconstruct V signature from those partials
        output (V, S); halt

    # Advance attempt: NR-quorum unlocks next layer.
    if |nrs[k]| ≥ qEnc:
        decryption_key_k = aggregate(nrs[k])       # threshold sig on nr_tag_k
        # decryption_key_k unlocks the layer-k+1 chained ciphertext layer
        L_C = k + 1
        # Continue the walk with the next layer.
    else:
        # NR-quorum did not reach at L_k. No path forward; slot misses.
        break    # exit the layer-walk

if L_C == K and no σ-quorum reached:
    # Walked all layers; no output. Slot misses.
    pass

# End of reconstruction. If output produced, halt; else slot misses.
```

`T_round_end` for the deadline rule is the cutoff by which the operator must have received all Phase-2b onions and NR partials they intend to count. Practically, `T_round_end = T_commit + Δ_2a + Δ_2b + Δ_3` where `Δ_3 ≥ (D + δ) + ε_3`.

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

### Treatment of missing onions

A participant that hasn't received `j`'s Phase-2b onion at decryption time treats `j` as not having contributed at that layer: no σ partial, no NR partial. Standard threshold cryptography — only signed messages count. Within 2abOBFT's absorption window (`Δ_2a + (D + δ)`), gossipsub propagation is expected to deliver all honest broadcasts to all honest receivers before `T_round_end`.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within `T_round_end`. If more than `f` operators are offline or byzantine combined, neither σ nor NR quorums reach their thresholds and the slot is missed.

### Operator commitment states

Three states per layer:

| State | Trigger |
|---|---|
| `σ` | Phase-2b σ partial emitted on a specific V |
| `NR` | Phase-2b NR partial emitted on `nr_tag_k` (operationally NR-silent or NR-due-to-equivocation or NV — wire-identical) |
| `uncommitted` | Default at Phase 1 / Phase 2a — operator has not yet emitted Phase-2b. Once Phase 2b ends, every operator must be in `σ` or `NR` per the convergence rule. |

There is no `Defer` state. Phase-2a's window IS the deferral mechanism: every operator's σ/NR decision is deferred until after the Phase-2a observation phase resolves cluster-wide σ-eligibility. By Phase-2a end, the convergence rule resolves every operator to either `σ` or `NR` at Phase-2b sign time.

**NR and NV are operationally interchangeable for the protocol.** Both materialize on the wire as a single message kind: a partial `σ_i^{IBE}(nr_tag_k)` from the IBE keypair on the layer's NR tag. The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to throughout as "NR-pool" or "no-σ pool" for short). The distinction is **local-only diagnostic** (an operator may log *why* it didn't sign σ — for telemetry — but the cluster-wide message and counting logic are identical).

### Slot structure

2abOBFT runs a single agreement round per slot with Phase 2 split into Phase 2a (verdict broadcast) and Phase 2b (σ-or-NR commit). The slot proceeds as follows:

1. **Phase 1** `[slot_start, T_commit]`: K leaders broadcast their Phase-1 bundles per their per-layer fetch windows (`[T_{K-1}, T_{K-1} + Δ_1]`, ..., `[T_0, T_0 + Δ_1]`), with `T_0 + Δ_1 ≤ T_broadcast_max = T_commit − 2(D + δ)`. **No σ_V partial in Phase-1 bundles** (Variant C). Receivers accept bundles first-observed in `[slot_start, T_accept_max]` where `T_accept_max = T_commit + Δ_2a − (D + δ)`.
2. **Phase 2a** `[T_commit, T_commit + Δ_2a]`: each operator broadcasts a per-layer verdict envelope (`KindVerdict`) reflecting their σ-eligibility per layer based on observed Phase-1 bundles and host validity verdicts. Bundle re-flood absorbs late-arriving Phase-1 bundles within the window. Operators emit verdicts at the latest-safe time (around `T_commit + Δ_2a − (D + δ)`) to maximize observed peer state.
3. **Phase 2b** `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]`: each operator computes per-layer convergence decisions from the observed Phase-2a verdict pool (per the convergence rule) and emits σ-or-NR partials per layer. EKM enforces single-σ-V per (slot, layer) per operator at sign time.
4. **Phase 3** `[T_commit + Δ_2a + Δ_2b, T_round_end]`: each operator runs the K-layer reconstruction walk. If σ-quorum reaches on some V at any layer, output the V; halt. If NR-quorum reaches up to some layer `L_C < K`, advance L_C and continue the walk. If neither σ-quorum nor NR-quorum advance unlock at any layer, the slot misses.

**Slot timing**: `T_round_end = T_commit + Δ_2a + Δ_2b + Δ_3`. Phase 1 fetch occupies `[slot_start, T_commit]`. Total consensus budget (Phase 2a + Phase 2b + Phase 3) is `Δ_2a + Δ_2b + Δ_3 ≈ 2(D + δ) + (D + δ) + 100ms` at recommended sizing, ≈ 850ms at Config A.

## Preconditions on the host application

2abOBFT is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at Phase-1 acceptance, Phase-2a verdict-broadcast, and Phase-2b sign time. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be broadcast.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` at three points: Phase-1 acceptance (drives bundle retention / auth-only fallback), Phase-2a verdict-broadcast time (drives `KindVerdict` content), Phase-2b sign time (drives the final σ/NR commitment per the convergence rule's "host re-validates" branch). Each check is independent; a `not-valid` verdict at any point steers the operator's commitment toward NR/NV.

The protocol-level checks (cryptographic auth, envelope re-derivation, timing cutoffs) are protocol-internal and do not depend on the application.

For SSV's Ethereum proposer duty, application-level checks include slot match, proposer index match, fork/domain match, parent root match against the operator's view of the head, relay metadata validity, doppelganger / slashing-protection checks, and block encoding well-formedness. See the Application section for details. The protocol body does not enumerate or interpret these — they're host concerns.

### Slashing-protection scope

Each operator's V-signing share signs *up to K* values per slot (potentially one per layer they validated and σ-committed to at Phase 2b). Constraints per (slot, layer):

- **Each operator commits σ-or-NR exclusively per (slot, layer).** Honest who include σ on any V at layer `k` may not subsequently broadcast NR/NV on `nr_tag_k` AND may not σ on a different V' at the same layer (single-σ-V per operator per layer); honest who broadcast NR/NV may not subsequently include σ at L_k. EKM enforces this cross-side + single-V exclusivity by coordinating across the operator's V-signing and IBE-signing shares: an NR/NV partial on `nr_tag_k` is rejected if the same operator has previously signed σ at L_k, and vice versa; a σ partial on V' at L_k is rejected if the same operator has previously signed σ on V ≠ V' at L_k.
- **Every operator signs each layer's V_{L_k} they consider valid** (host returns `valid` AND convergence rule resolves to σ on V) at Phase-2b. Pigeonhole 1 and 2 below rely on these rules.

The gating points for EKM are **Phase-2b candidate signing** (V-share for σ) and **Phase-2b no-σ signing** (IBE-share for NR/NV). Phase-2a verdict broadcasts are op-identity-only — they do not pass through the EKM.

### EKM coordination model

The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log. Production EKMs (Web3Signer, Dirk, etc.) typically expose per-key signing checks; 2abOBFT requires a coordination layer above them.

**Log schema.** One row per signing event: `(slot, layer, side, value_root)` where `side ∈ {"σ", "NR"}`; `value_root` is set on σ-side entries, null on NR-side. No round dimension (single-round protocol). No phase dimension (Phase-2a verdicts don't log to EKM; only Phase-2b signs do).

**Per-request checks.**

- **Sign σ on V at (slot, layer)** (V-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "σ", value_root(V))`. The single-σ-V rule means a second signing attempt with `V' ≠ V` at the same `(slot, layer)` is rejected even though the side matches — the existing row already contains `value_root(V)`.
- **Sign NR on `nr_tag_k` at (slot, layer)** (IBE-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "NR", null)`.

**Simplifications relative to other OBFT-family protocols.** 2abOBFT's EKM is the simplest in the family:

- **No cross-round atomicity.** Single round; no rounds to span.
- **No persistent partial-sig cache.** No re-emission across rounds.
- **No deterministic re-signing fallback.** Single signing event per (slot, layer) per operator.
- **No Phase-1 σ_V to coordinate with later Phase-2 σ.** [OBFT](OBFT.md) and [OBFTR](OBFTR.md) both log a Phase-1 σ_V row for the leader and require dedup-with-Phase-2-σ at Phase 3. 2abOBFT's leader signs σ once at Phase 2b like everyone else.

**Beyond the EKM log: persistent Phase-2a state.** Per assumption 5, each operator must durably record per-slot:

- **Phase-2a verdict envelopes broadcast** by this operator (one per `(slot, layer)`). On restart, the operator must consult this record before issuing a verdict to avoid self-equivocation (Rule 6a).
- **Retained Phase-1 bundles** per `(slot, layer, leader_id)` — the auth-valid `(V, σ_L^op)` tuples (up to 2 per key for equivocation evidence). On restart, the operator must reload retained-V state to compute Phase-2b commits consistent with their pre-crash verdict.

This state lives outside the EKM slashing-protection log (the log only covers σ/NR threshold-partial signings at Phase 2b). Implementations may colocate it with the EKM database for transactional consistency, or store it in a separate per-slot durable file.

**Without persistent Phase-2a state**, an honest operator restarting mid-slot risks: (a) re-broadcasting a different verdict than their pre-crash one, producing self-equivocation that other peers slash under Rule 6a; (b) emitting a Phase-2b action that mismatches the on-wire verdict, producing Rule 6b false-positive evidence against an honest restarter. Either outcome is byzantine-equivalent on the wire — equivalent to a buggy operator under the "honest-majority cryptographic safety" framing (which assumes honest software conforms to protocol rules including persistent-state requirements). See [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic).

**Implementation paths.** (a) A custom EKM that maintains the unified log natively across both keypair shares. (b) A coordinator component in the SSV operator process that wraps two standard EKMs (one per keypair share) and enforces the cross-keypair rules in the operator's slashing-protection database — a single transactional log, two keypair-specific signing-request handlers reading and writing it. The coordinator is **simpler** than the OBFTR(R≥2)-equivalent or even OBFT's: it requires only the unified log to be transactionally consistent across both shares for the **single Phase-2b signing event per (slot, layer)**.

**EKM correctness is defense-in-depth for safety, not the sole enforcement.** 2abOBFT's safety holds when honest operators correctly implement the protocol rules — single-σ-V per (slot, layer) per operator, σ-XOR-NR per layer. The **convergence-rule layer** (operator software computing Phase-2b commits from Phase-2a observations) is the primary enforcement point — it determines when σ vs NR is requested from the EKM in the first place. The **EKM** is a catch-net rejecting signing requests that violate the slashing-protection invariants. For a single honest operator to produce safety-violating behavior (e.g., σ on V and σ on V' at the same `(slot, layer)`), **both layers must be buggy in compounding ways**: the convergence-rule layer must request the second σ AND the EKM must fail to reject it.

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model.

### Trust model

- **Byzantine bound `f`** with cluster size `n = 3f+1` (the BFT-tight setting; see [§Assumed / Standard BFT trust bound at the tight setting](#assumed)): up to `f` operators may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, etc.). Exactly `2f+1` honest.
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `D` (propagation P99/P999) and clock skew `δ`. Three distinct cutoffs operationalize this bound: `T_broadcast_max = T_commit − 2(D + δ)` (leader broadcast deadline), `T_accept_max = T_commit + Δ_2a − (D + δ)` (receiver acceptance horizon), `T_verdict_max = T_commit + Δ_2a − (D + δ)` (verdict broadcast horizon, coincident with `T_accept_max`). Phase 3's reconstruction deadline is `T_round_end = T_commit + Δ_2a + Δ_2b + Δ_3`.

  **2abOBFT's effective absorption window** = `T_accept_max − T_broadcast_max = Δ_2a + (D + δ)`:
  - At `Δ_2a = D + δ` (BFT-minimum): `2(D + δ)` ≈ 300ms at Config A.
  - At `Δ_2a = 2(D + δ)` (recommended): `3(D + δ)` ≈ 450ms at Config A.

  Real propagation > absorption window is Class A "sustained partition" — out of envelope by definition.

### Safety (cryptographic, honest-majority)

**Claim:** at most one full `V` signature is ever produced per 2abOBFT instance per slot — across any layer, on any value, across any combination of σ sources — cluster-wide, against an offline-aggregating byzantine within the f-bound, regardless of which honest aggregation rules are followed.

The proof rests on three pigeonhole arguments. All three are properties of cluster-wide signed messages, enforced cryptographically by [EKM coordination](#ekm-coordination-model) rules at the operator level — no honest-aggregation rule required.

The pool definitions used in the arguments below:

- **σ-pool on V at L_k**: `{σ_i^V(V) Phase-2b partials at L_k}`, deduplicated per operator.
- **NR-pool at L_k**: `{σ_i^{IBE}(nr_tag_k) Phase-2b partials}`, deduplicated per operator.

#### Pigeonhole 1 — σ vs NR at the same layer

σ-quorum on `V` (any V) at `L_k` and NR-quorum on `nr_tag_k` cannot both reach.

- σ-quorum on V: `h_σ + byz_σ ≥ qV = 2f+1` (where h_σ counts honest with Phase-2b σ partials on V at L_k, deduplicated per operator).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Cross-side exclusivity (per "Slashing-protection scope"): `h_σ + h_NR ≤ n − f = 2f+1` (equality at `n = 3f+1`). Each honest commits σ-or-NR per layer at most once at Phase 2b, EKM-enforced.
- **Leader-counting.** Unlike OBFT/OBFTR, 2abOBFT has no Phase-1 σ_V — every operator (including the layer's leader) emits σ-or-NR uniformly at Phase-2b under the convergence rule. There is no leader-specific σ-side pre-commitment. Byzantine leaders that equivocate at Phase 1 contribute to `byz_σ_V` per V at most once each (deduplication).
- Byzantine cross-signing: `byz_σ + byz_NR ≤ 2f` (each byz contributes at most 1 to each pool).
- If both quorums reached: `h_σ + h_NR ≥ 4f+2 − 2f = 2f+2`. But `h_σ + h_NR ≤ 2f+1`. Contradiction. ∎

#### Pigeonhole 2 — two σ-quorums on different values at the same layer

Two distinct `V`'s cannot both reach σ-quorum at the same layer.

- `(h_σ_V + h_σ_V') + (byz_σ_V + byz_σ_V') ≥ 2 · qV = 2(2f+1)`.
- Honest sign at most one V per layer (single-σ-V exclusivity, EKM-enforced): `h_σ_V + h_σ_V' ≤ 2f+1`.
- Byzantine can sign multiple V's: `byz_σ_V + byz_σ_V' ≤ 2f`.
- Bound: `(2f+1) + 2f = 4f+1 < 4f+2`. Contradiction. ∎

This is the key safety constraint: regardless of which V's honest σ-commit on under equivocation, at most one V can reach qV cluster-wide. The convergence rule's σ-eligibility-quorum check at Phase-2a end is what makes this "honest commit on at most one V" actually hold per-operator: when verdict-quorum is short, all honest rule-flip to NR (no σ partials emitted), so per-operator h_σ contribution is zero on every V.

#### Pigeonhole 3 — cross-layer safety under chained encryption

Two distinct V signatures (V_k and V_{k+m} for any `m ≥ 1`) cannot both be reconstructed cluster-wide. Proof by induction on `m`, applying Pigeonhole 1 at every L_j with `j ∈ [k, k+m−1]`.

- *Decryption requirement.* V_{k+m} σ partials at L_{k+m} are encrypted under `nr_tag_k ∧ nr_tag_{k+1} ∧ … ∧ nr_tag_{k+m−1}`. Decryption requires NR-quorum on every `nr_tag_j` for `j ∈ [k, k+m−1]` (chained-IBE oracle).
- *Inductive step.* For each such `j`, Pigeonhole 1 applied at L_j gives: σ-quorum at L_j ⇒ NR-quorum at L_j does not reach. Therefore if V_k σ-quorum reaches at L_k, NR-quorum at L_k fails, the chain at L_k stays sealed, and V_{k+m}'s σ partials are inaccessible.
- *Symmetric direction.* If V_{k+m} reconstructs, NR-quorum at L_k must have reached, so by Pigeonhole 1 σ-quorum at L_k did not reach, so V_k does not reconstruct. ∎

Applied to every pair of layers, at most one V signature reconstructs cluster-wide across all K layers.

#### Verdict envelopes do not affect safety

Verdict envelopes are op-identity-signed claims, not threshold partials. They influence the *liveness convergence* (which Phase-2b emission an operator chooses) but contribute zero partials to either σ-pool or NR-pool. Safety holds regardless of how operators verdict-claim.

A byzantine that verdict-claims σV but Phase-2b NR-emits (or vice versa) commits **verdict-vs-action equivocation** (Rule 6b) — slashable evidence (the verdict envelope contradicts the Phase-2b σ/NR partial), but does not violate Pigeonhole 1/2/3. Verdict-vs-verdict equivocation (Rule 6a) is similarly slashable but does not threaten safety either — verdicts are op-identity-signed claims, not threshold partials.

### Liveness (synchrony-conditional)

2abOBFT's liveness is **partial-synchrony-conditional within `T_round_end`**. The protocol absorbs network-induced failures via the late-bundle re-flood window in Phase 2a. Equivocation σ-locked split, h_V=1 selective-delivery, and validity-divergence within the f-bound are recovered structurally via the convergence rule's σ-eligibility-quorum-short → NR-pool fall-through path.

Running example: `f = 1, n = 4, K = 4`. Honest A, B, C; byzantine D.

#### Healthy path

All 4 operators receive `V_{L_0}` via gossipsub within `D + δ`.

- Phase-2a: all 4 verdict `σV(V_{L_0})`. `verdict_pool[V_{L_0}] = 4 ≥ qV`.
- Phase-2b: all 4 σ-emit. σ-pool = 4. **Slot succeeds at L_0.**

#### Marginal-receive cases

##### h_V_honest = 3 (3 of 4 operators received V on time; 1 didn't)

A, B, D have V; C does not.
- verdict_pool[V] = A + B + (D if cooperative) = 2 or 3. nr_pool = C = 1.
- If D σV-cooperates: verdict_pool[V] = 3 ≥ qV. A, B, D σ-emit; C does not have V → C NR. σ-pool = 3 = qV. **Slot succeeds at L_0.**
- If D NR or silent: verdict_pool[V] = 2 < qV. Per rule, A and B → NR; C → NR. NR-pool actual = 3 ≥ qEnc. **Fall through to L_1**, succeeds there.

##### h_V_honest = 2 (2 of 4 operators received V; 2 did not)

A, B have V (A is leader); C does not. D byz arbitrary. The byz strategy space is **wider** than uniform "σV/NR/silent" because D may verdict-equivocate per-peer.

- **D σV-cooperates uniformly (verdicts σV(V) to all + Phase-2b σ on V)**: verdict_pool[V] = 3 ≥ qV at every peer's view. σ-pool = 3 = qV. **Slot succeeds at L_0.**
- **D NR-uniformly (verdicts NR to all + Phase-2b NR)**: verdict_pool[V] = 2 < qV; nr_pool = 2 < qEnc at every peer. Per rule 5, A and B → NR (verdict-quorum short); C → NR. NR-pool actual = 3 ≥ qEnc. **Fall through to L_1.**
- **D silent (no verdict, no Phase-2b)**: verdict_pool[V] = 2; nr_pool = 1; same outcome as D-NR-uniform. **Fall through to L_1.**
- **D verdict-equivocates + Phase-2b defects** (Class B regression — see [§Failure modes](#failure-modes)): D issues σV(V) first-observed by A, NR first-observed by B and C; D defects to σ on V at Phase-2b. Per-peer first-observed:
  - A's view: verdict_pool[V] = {A, B, D} = 3 ≥ qV; rule 3 fires; A σ-emits.
  - B's view: verdict_pool[V] = {A, B} = 2; nr_pool = {C, D} = 2; rule 5; B NRs.
  - C's view: same as B; C NRs.
  - D defects to σ on V.
  - σ-pool actual = {A, D} = 2 < qV; NR-pool actual = {B, C} = 2 < qEnc. **Slot misses at L_0 with no fall-through.** Cryptographic Rule-6a evidence (D's two distinct KindVerdict envelopes) is on the wire from any peer who eventually receives both via re-flood; D is slashable.

The verdict-equivocation case is a **second Class B regression vs single-Phase-2 protocols** alongside 2-1-byz-defect, and a more general one — it triggers on every slot where h_V_honest = 2 (network jitter + byz mesh manipulation puts honest at this boundary, not only byz-leader slots). Bare OBFT here would fall to silent-leader-NR cleanly when D is silent (slot misses too — bare OBFT also fails this scenario at f=1 n=4 per its [Class B mesh-flakiness analysis](OBFT.md)), but does **not** have the verdict-equivocation surface (no verdicts to equivocate on). 2abOBFT trades the "no verdict surface" for the convergence-rule recovery of equivocation σ-locked splits, h_V=1, and validity-divergence majority — and pays this regression as a side-effect.

##### h_V_honest = 1 (only one honest has V)

A has V; B, C don't.
- verdict_pool[V] = 1 + maybe-byz < qV. nr_pool = 2 + maybe-byz.
- All sub-cases: A → NR (verdict-quorum short), B → NR, C → NR. NR-pool actual ≥ 3 = qEnc. **Fall through to L_1.**

This is the h_V=1 "Class B byzantine selective-delivery deadlock" case from OBFT — recovered structurally in 2abOBFT.

#### Equivocation σ-locked split

Byzantine D = leader equivocates at L_0.

##### 1-1-1 split

D delivers V_a to A, V_b to B, V_c to C near end of Phase 1.

- **If re-flood completes within Phase-2a** (`Δ_2a ≥ D + δ` from byz's late delivery): A retains V_a + V_b + V_c → equivocation observed → A's verdict NR. Same for B, C. NR-pool actual = 3 ≥ qEnc. **Fall through to L_1.**
- **If re-flood does not complete** (byz times deliveries past T_accept_max for each honest): A retains V_a only; B retains V_b only; C retains V_c only. Each honest issues σV verdict on their respective V. `verdict_pool[V_a] = 1`, `verdict_pool[V_b] = 1`, `verdict_pool[V_c] = 1` (all < qV). All honest go NR per rule. NR-pool = 3 ≥ qEnc. **Fall through.**

In both sub-cases, the slot recovers via L_1 fall-through. OBFT base 1-1-1 slot-misses at L_0 with no fall-through; 2abOBFT structurally fixes this.

##### 2-1 split

D delivers V to {A, B}, V' to {C}.

- **D cooperates (verdict σV(V) + Phase-2b σ on V)**: verdict_pool[V] = 3 ≥ qV. A, B, D σ-emit. C does not have V_local = V → NR. σ-pool = 3 = qV. **Slot succeeds at L_0.**
- **D silent (no verdict, no Phase-2b emit)**: verdict_pool[V] = 2 < qV. A, B → NR (verdict-quorum short); C → NR. NR-pool actual = 3 ≥ qEnc. **Fall through to L_1.** (Bare OBFT would have succeeded at L_0 here via Phase-1 σ_V lock; 2abOBFT pays one extra layer of latency.)
- **D defects (verdict σV(V) + Phase-2b NR-emit)**: verdict_pool[V] = 3 ≥ qV. A, B σ-emit on V; D defects to NR. σ-pool actual = 2 < qV; NR-pool = 1 (C) + 1 (D defected) = 2 < qEnc. **Slot misses at L_0 with no fall-through.** Rule-6b evidence (verdict-vs-action mismatch) is on the wire. **Strictly worse than OBFT base, which would have succeeded via Phase-1 σ_V cryptographic lock.**

The 2-1-byz-defect regression vs OBFT is real. The cost of removing Phase-1 σ_V to gain validity-divergence recovery and 1-1-1 equivocation recovery. Across many slots, the rational-byzantine deterrent (assumption 4) absorbs the byzantine-defect-grief cost.

#### Validity-divergence within the f-bound

A re-org during Phase-1-to-Phase-2a window splits honest verdicts: some operators say V valid (parent_root matches their pre-reorg head), some say invalid (parent_root mismatches their post-reorg head).

##### 3-σ vs 1-NV at f=1 n=4

- 3 ops verdict σV(V); 1 op verdict NV. verdict_pool[V] = 3 ≥ qV. nr_pool = 1.
- 3 σV-side ops with V_local σ-emit. NV op (had V but host says NV at re-evaluation) → NR per rule.
- σ-pool = 3 = qV. **Slot succeeds at L_0.**

##### 2-σ vs 2-NV at f=1 n=4 (the boundary case)

- 2 ops verdict σV; 2 ops verdict NV. verdict_pool[V] = 2 < qV. nr_pool = 2 < qEnc. Neither met.
- Per rule, σV-side honest → NR (verdict-quorum short); NV-side honest → NR. NR-pool actual = 4 ≥ qEnc. **Fall through to L_1.**

If L_1 has the same parent_root issue (e.g., same re-org affects all backup leaders' V's), the same divergence pattern repeats at L_1 and slot may miss across all layers. This is the residual Class A risk; in practice, backup leaders' asymmetric fetch from deeper-confirmed parents (`T_{K-1} < ... < T_0`) makes them re-org-resistant by construction, so L_1's V is unlikely to share the divergence.

#### Late deepest-layer leader broadcast

A deepest-layer leader L_{K-1} whose Phase-1 bundle's first cluster-observation arrives in `(T_commit, T_accept_max]` (slightly past `T_broadcast_max`, but within Phase-2a's absorption window) is recoverable in 2abOBFT: the late bundle re-floods to honest receivers in Phase-2a, they verdict σV at Phase-2a, verdict-pool reaches qV, σ-emit at Phase-2b. **Slot succeeds at L_{K-1}** where bare OBFT would have lost the bundle past the receiver acceptance window.

#### Mesh-flakiness coordinated with byz σ-refusal

A mesh-flaky honest operator with poor gossipsub visibility can fail to observe peer Phase-2a verdicts within their convergence window. Combined with byzantine refusing to verdict-broadcast, the mesh-flaky honest may compute convergence based on partial cluster-view.

Variant C's convergence rule degrades gracefully: if mesh-flaky honest sees verdict_pool[V] = qV (cluster-wide convergence reached and propagated to them despite flakiness), they σ-emit. If not, they NR-emit per rule, joining NR-pool. Fall-through happens unless the cluster also fails to reach NR-quorum (unlikely if the mesh-flaky honest is the only flaky one).

The recommended `Δ_2a ≥ 2(D + δ)` absorbs typical mesh-jitter (one full `D + δ` of additional slack on top of P99 propagation). For wider mesh outliers, deployment-level mesh-diversity remains relevant.

### Liveness comparison: 2abOBFT vs bare OBFT, OBFTR, QBFT

A side-by-side comparison of 2abOBFT's recovery scope against bare OBFT, OBFTR(R≥2), and QBFT — covering healthy path, byzantine-leader patterns, multi-leader silent, validity-divergence, sustained partition, and the residual surfaces unique to 2abOBFT (2-1-byz-defect, verdict-equivocation) — is in [§Appendix A.3 — Comparison with bare OBFT and QBFT](#a3--comparison-with-bare-obft-and-qbft). Apples-to-apples framing across protocols, the four-bucket recovery taxonomy (absorbable-by-waiting / OBFT-family-only / 2abOBFT-only / 2abOBFT-regressions), and per-failure-class outcomes are discussed there.

The summary takeaway: at apples-to-apples T, pure all-honest network failures recover identically across all four protocols. The structural distinctions are at the byzantine-equivocation and validity-divergence axes, where 2abOBFT closes most of bare-OBFT/OBFTR's adversarial-byz exposure (σ-locked equivocation, h_V=1, validity-divergence-majority, mesh-flakiness) at the cost of two narrower regressions (2-1-byz-defect, verdict-equivocation) — both slashable, both R-invariant.

### Equivocation handling

See [§Phase 1 / Equivocation handling at Phase 1](#phase-1--candidate-broadcast) for the operational rule. Summary: when an honest receiver detects leader equivocation (two distinct auth-valid Phase-1 bundles for the same `(slot, layer, leader_id)`), they:

1. Retain both bundles as slashable evidence (Rule 2).
2. Issue a Phase-2a verdict of `NR-due-to-equivocation` (wire form: `NR`).
3. Per the convergence rule at Phase-2a end, the equivocation-observed branch overrides any other criteria — operator commits NR at Phase 2b.
4. Gossip the equivocation evidence (the pair of conflicting Phase-1 bundles) for out-of-band slashing.

The leader is required to sign exactly one Phase-1 bundle per `(slot, layer)`; any second bundle with a different `value_root` is a protocol violation regardless of intent.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_k` is a slashable cross-signer. The dual partials are publicly attributable (Rule 1). Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it cryptographically — the EKM-enforced cross-side exclusivity is what gives Pigeonhole 1 its load-bearing constraint, and a byzantine cross-signer simply consumes their f-bound contribution to one of σ-pool or NR-pool, not both effectively). Detection is purely for **attribution** and out-of-band punishment.

### Slashing evidence

Six rules surface byzantine fault evidence for *attribution and punishment*. Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to inform the surviving operators (and stakers) that a byzantine fault has occurred so the cluster can act on it — see [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the offline-equivalence model. The protocol surfaces the evidence; the surviving operators verify it and decide whether to (a) blacklist the byzantine via cluster-wide config update (restoring `Byzantine ≡ Down`; planned protocol extension), (b) file a stake-slashing transaction via the SSV contract where the evidence is cryptographically self-contained, and/or (c) propagate the signal to stakers and to future cluster formation.

- **Rule 1 — Self-contradiction (σ + NR/NV at the same layer).** If operator `i` emitted `σ_i^V(V)` at some layer AND `σ_i^{IBE}(nr_tag_k)` at the same layer, the dual partials are slashable evidence. **Cryptographic, self-contained — immediate detection.**
- **Rule 2 — Leader equivocation.** Two distinct auth-valid Phase-1 bundles `(V, σ^{op})` and `(V', σ^{op})` from the same leader at the same `(slot, layer)`. **Cryptographic, self-contained — immediate detection.**
- **Rule 3 — Cross-onion partial-sig equivocation.** Operator `i` emitting `σ_i^V(V)` and `σ_i^V(V')` for different `V` at the same layer (in Phase-2b onion). **Cryptographic, self-contained — immediate detection** (single-σ-V exclusivity is EKM-enforced, so any dual-V observation is unambiguous byzantine fault).
- **Rule 4 — Fake encrypted-presence (post-decryption garbage at k > 0).** Operator `i` broadcasting an auth-signed `KindOnion2b` with an encrypted partial at layer `k > 0` that, after NR-quorum unlocks decryption, decrypts to garbage (not a valid σ partial on any leader-known V at L_k, or fails to decrypt entirely). **Cryptographic but delayed** — detection is conditional on NR-quorum reaching at all prior layers (so the chained encryption can be unlocked). When the slot misses cleanly at L_0 with no NR-quorum, this evidence stays sealed for that slot.
- **Rule 5 — Fake plaintext σ at L_0.** Operator `i` broadcasting an auth-signed `KindOnion2b` with a plaintext σ partial at L_0 that does not verify against any retained leader-broadcast V_{L_0} (where the receiver has retained at least one such V). **Cryptographic, self-contained — immediate detection** for receivers with retained V; no decryption-unlocking dependency. Receivers MUST gossip the evidence so receivers without retained V eventually receive the attribution.

  **Rate-limit (anti-amplification rule).** Each receiver MUST gossip evidence at most once per `(slot, layer, operator_id)` tuple to cap amplification.

- **Rule 6a — Verdict-vs-verdict equivocation.** Operator `i` broadcast two distinct `KindVerdict` envelopes for the same `(slot, layer)` (different `verdict` field, different `value_root`, or both). The pair is signed by `i`'s op-identity key. **Cryptographic, self-contained — immediate detection.** Same evidence quality as Rules 1-3 (single message-pair conclusively demonstrates the byzantine action). Receivers MAY act on a single observed pair; cluster-wide consensus on the evidence is not required.

  **Rate-limit (anti-amplification rule)**: each receiver MUST gossip evidence at most once per `(slot, layer, operator_id)` tuple.

- **Rule 6b — Verdict-vs-action equivocation.** Operator `i` broadcast `KindVerdict(σV, hash(V))` in Phase-2a but emitted Phase-2b NR/NV (i.e., a `σ_i^{IBE}(nr_tag_k)` partial), or broadcast `KindVerdict(NR or NV)` but emitted Phase-2b σ on V. The verdict envelope and the Phase-2b partial together form slashable evidence — both are signed by `i`'s keys (op-identity for verdict, V/IBE share for partial). **Cryptographic but boundary-conditional** — receivers must cross-reference cluster verdict view to distinguish honest revision (e.g., σV verdict at Phase-2a, then bundle equivocation observed mid-Phase-2a, NR action per convergence rule row 1) from byzantine equivocation. Honest revision is *permitted*; byzantine equivocation is slashable. The distinguishing condition is whether the cluster's verdict pool *would have* converged on the verdict side: σV verdict + NR action where σ-eligibility-quorum on V was reached cluster-wide is byzantine; σV verdict + NR action where cluster's σ-eligibility-quorum was short is honest revision (rule 5 fired) or honest equivocation revision (rule 1 fired).

  **False-positive risk** is higher than Rules 1-5 and 6a because at the boundary of receiver convergence (when receivers' Phase-2a verdict views differ slightly), Rule-6b attribution is gossipsub-pattern-quality rather than self-contained. Honest receivers should aggregate observations across receivers before acting on Rule-6b evidence.

  **Rate-limit (anti-amplification rule)**: each receiver MUST gossip evidence at most once per `(slot, layer, operator_id)` tuple.

Each piece of evidence is verifiable in isolation (signed by the offending operator's own keys) — any observer with the published partials and (where applicable) cluster verdict view can independently confirm the byzantine action. **Acting on the evidence (slashing transaction, cluster removal) is a human-coordinated process**, not an automated protocol step; honest operators judge whether the evidence is compelling and decide whether to act.

**Evidence quality and surface-ability vary by rule:**

| Rule | Detection timing | Surface-ability | False-positive risk |
|---|---|---|---|
| 1. Self-contradiction (σ + NR/NV) | Immediate (dual partials on the wire) | Always — public partials | Very low |
| 2. Leader equivocation | Immediate (two auth-valid bundles, same leader) | Always — public bundles | Very low |
| 3. Cross-onion partial-sig equivocation | Immediate (two σ partials on different V) | Always — public partials | Very low |
| 4. Fake encrypted-presence (k > 0) | Delayed (post-decryption) | **Best-effort, conditional on slot progressing past prior layers' NR-quorum** | Very low when surfaced |
| 5. Fake plaintext σ at L_0 | Immediate (partial vs retained V check) | Always when retained-V receivers gossip evidence | Very low |
| 6a. Verdict-vs-verdict equivocation | Immediate (two distinct KindVerdict envelopes from same op) | Always — public envelopes | Very low |
| 6b. Verdict-vs-action equivocation | Boundary-conditional (cross-reference cluster verdict view) | Best-effort, conditional on receiver convergence on cluster verdicts | Higher — gossipsub-pattern quality |

### Failure modes

The slot misses (no V signature is produced) under any of the following.

- **[Class A]** **Sustained partition (real propagation > absorption window)** — violates assumption 2 (partial synchrony) under 2abOBFT's framing (absorption = `Δ_2a + (D + δ)`, ≈ 450ms at Config A recommended). Slot misses cleanly. No safety violation.
- **[Class A]** **More than `f` faults** — violates assumption 1 (BFT trust bound). Slot misses regardless of protocol structure.
- **[Class A]** **Validity-divergence at the 2-2 boundary at f=1 n=4** — re-org lands inside Phase-1-to-Phase-2a window and produces a 2-σ vs 2-NV honest split. Both σ-eligibility and NR-eligibility quorums short of threshold; cluster falls through to L_1 (per rule). If L_1 also exhibits the same divergence (same re-org affects both layers' parent_roots), slot misses cleanly. **In practice, backup leaders fetch from deeper-confirmed parents and rarely share L_0's re-org exposure.**
- **[Class A]** **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates in non-recoverable patterns at every layer). At K ≥ f+1, byzantines alone (within f-bound) cannot cause this — pigeonhole guarantees ≥ 1 honest leader. So this typically requires either >f faults (Class A) or coincident non-byzantine independent failures.
- **[Class B — cryptographically slashable]** **Equivocation 2-1 split with byz-defect** — byz delivers V to majority, V' to minority (byz must be leader to control bundle delivery); verdict-claims σV(V) at Phase-2a; defects to NR at Phase-2b. σ-pool short by 1; NR-pool short by 1. Slot misses at L_0. **Strictly worse than OBFT base in this specific case** (where Phase-1 σ_V cryptographically locks byz's σ partial). Triggerable only on byz-leader slots (1/K of slots at K=n=4 with uniform leader rotation). Slashable Rule-6b evidence on the wire (verdict-vs-action mismatch).

- **[Class B — cryptographically slashable]** **Non-leader verdict-equivocation under marginal h_V** — when fewer than `qV` honest receive V on time (e.g., `h_V_honest = 2` at f=1 n=4 due to gossipsub propagation tail or byz mesh manipulation), any byzantine operator (not necessarily leader) can verdict-equivocate to engineer per-peer convergence divergence. Concretely at f=1 n=4 with leader honest, h_V_honest = 2: byz issues `σV(V)` first-observed by one σ-eligible honest, `NR` first-observed by the others; byz defects to σ on V at Phase-2b. The σ-eligible honest who observed byz's σV verdict converges on σ-eligibility met (3 ≥ qV) and σ-emits; the other honest converge on neither met and NR-emit. Cluster-wide pools: σ-pool = 2 (the σ-eligible honest + byz's defect); NR-pool = 2 (the other 2 honest); both short. **Slot misses at L_0 with no fall-through.** Cryptographic Rule-6a evidence (byz's two distinct verdict envelopes) is on the wire after re-flood; byz is slashable.

  **This is a wider attack surface than 2-1-byz-defect** — it triggers on any slot where marginal h_V puts the cluster at the convergence boundary, not only on byz-leader slots. The trigger condition (h_V_honest = 2 at f=1 n=4) sits inside the absorption-window envelope under partial synchrony but is plausible under realistic network conditions. The deeper structural issue: the convergence rule's "first-observed wins" semantics implicitly assumes peer-convergence on the verdict view, which a byzantine can break by per-peer verdict injection. The rational-byzantine deterrent (assumption 4) is the practical defense; per-slot, this case slot-misses cleanly with cryptographic evidence (Rule 6a) on the wire.

  **Mitigation options at implementation time** (not in current spec; see [Appendix C / Open questions](#open-questions--decisions-to-make-at-implementation-time) #16):
  - EKM-bind verdicts so byz cannot issue more than one verdict per `(slot, layer)` from their own EKM. Byzantine bypass of own EKM is still possible (byz controls their software), but cluster-wide receiver behavior on observing two verdicts can include "treat verdict-equivocator's verdict as null" — which would route this scenario through NR-quorum fall-through. Closes both this and the 2-1-byz-defect regression at the cost of an EKM "verdict-void" operation gated on equivocation evidence (to preserve honest verdict revision when bundle equivocation is observed mid-Phase-2a).
  - Accept the regression: rely on rational-byzantine deterrent across slots.

### Class B — Recovered patterns (vs single-Phase-2 protocols)

These are slot-miss outcomes in [OBFT](OBFT.md) and [OBFTR(R≥2)](OBFTR.md) that 2abOBFT structurally recovers:

- **Equivocation 1-1-1 split**: recovered via NR-quorum fall-through (verdict-pool short on every V; all honest go NR).
- **Equivocation 1-1-NR-C, 1-NR-NR**: same recovery via NR-quorum fall-through.
- **h_V=1 selective-delivery deadlock**: recovered (verdict-pool short → all NR → fall-through).
- **Late deepest-layer leader broadcast**: recovered (Phase-2a re-flood absorbs late bundle).
- **Validity-divergence majority within f-bound**: recovered (3-of-4 or wider splits at f=1 n=4 reach σ-quorum or NR-quorum cleanly).
- **Mesh-flakiness coordinated with byz σ-refusal**: mitigated (Phase-2a window absorbs jitter; mesh-flaky honest still routed through NR fall-through if they don't recover in time).

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

2abOBFT uses **`K-1` IBE tags per slot** (the K-1 NR tags; the deepest layer has no NR tag). Each tag is a standard threshold-IBE tag using the cluster's IBE keypair. The chained encryption at each layer-transition is implemented as a single IBE ciphertext under `nr_tag_k`, nested across layers.

The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init. Long-lived, no per-slot rotation. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

**Chained encryption cost.** At layer K-1 (deepest), each Phase-2b σ partial is wrapped in `K-1` levels of IBE encryption. Per-onion size grows as `O(K)` ciphertext bytes (`K-1` levels × ~32-64 bytes ciphertext per level). At K=2 the chain has 1 level; at K=4 it has 3 levels. Concrete sizes: ~1 KB per Phase-2b onion at K=2, ~3 KB at K=4. Within practical SSV bandwidth budgets.

## Properties summary

| Property | 2abOBFT |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1` + chained IBE + EKM-enforced per-operator commitments (single-σ-V per (slot, layer), σ-XOR-NR per layer), holds against offline-aggregating byzantine within the f-bound. Honest-majority cryptographic, not 100% cryptographic. Same trust posture as QBFT. |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition (assumption 3) |
| Termination (output guaranteed) | Conditional: terminates within `T_round_end` if real propagation between leader broadcast and any honest first-observation ≤ absorption window `Δ_2a + (D + δ)` (≈ 450ms at Config A recommended) and ≤ f operators byzantine/offline. |
| Equivocation detection | Yes — leaders sign Phase-1 envelopes; conflicting signed candidates form self-contained slashable evidence (Rule 2) |
| Equivocation recovery | **Structural for 1-1-1, all-equivocation-NR, h_V=1 patterns** via convergence-rule fall-through. **2-1-byz-defect regresses** vs single-Phase-2 protocols (slot misses at L_0; Rule-6 evidence). |
| Validity-divergence recovery | **Majority recovers** (e.g., 3-of-4 σV vs 1 NV at f=1 n=4 reaches σ-quorum at L_0). 2-2 split at f=1 n=4 still slot-misses cleanly (no majority). |
| Byzantine-leader-grief resistance | Substantial. h_V=1 (both withhold-then-fake-σ and selective Phase-1 delivery variants), 1-1-1 equivocation split, mesh-flakiness, late-deepest-layer-broadcast — all closed structurally via Phase-2a observation. 2-1-byz-defect remains a Class B residual. |
| Mesh-flakiness tolerance | Good — Phase-2a window absorbs typical mesh-jitter (recommended `Δ_2a ≥ 2(D + δ)` accommodates one full propagation cycle of variance). Wider outliers fall back through NR-quorum to L_1. |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide. Same as OBFT-family. |
| Built-in leader fallback | Yes (K-layer fall-through within Phase 3's reconstruction walk; K configurable, K = n recommended for proposer duty) |
| Round-change recovery | No — single-round design. Late re-flood within Phase-2a's absorption window is the only within-slot partition-recovery mechanism. |
| Partial-synchrony absorption window | `Δ_2a + (D + δ)` (single round) — ≈ 450ms at Config A recommended. |
| Healthy-path latency (post-`T_commit`) | ~850ms at Config A recommended (Δ_2a=Δ_2b=2(D+δ)=300ms each + Δ_3=250ms); ~550ms at minimum sizing (Δ_2a=Δ_2b=D+δ=150ms each + Δ_3=250ms) |
| Slot budget cost vs single-Phase-2 ([OBFT](OBFT.md)) | +300ms at recommended sizing (extra Phase 2a window of 300ms vs single Phase 2); ±0ms at minimum sizing (both have Phase 2 window summing to D+δ-equivalent) |
| EKM complexity | Lowest in the OBFT family — single signing event per (slot, layer) per operator, no Phase-1 σ_V to coordinate, no cross-round atomicity, no persistent partial-sig cache. |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block, the recommended 2abOBFT configuration is **`K = 4` (= n)** — every cluster member is a leader at exactly one layer; pigeonhole guarantees ≥ 3 honest leaders at f=1, providing maximum K-layer fall-through depth within the single round. `K = 3 = f+2` is also viable at slightly lower onion bandwidth (~3KB savings per Phase-2b onion, same timing). `K = 2` (BFT-min at f=1) is **not recommended** — exposes the late-deepest-layer-leader-broadcast Class A failure mode at K=2 (no L_2 to fall through to).

| 2abOBFT concept | SSV mapping |
|---|---|
| `n` participants | 4 |
| `f` byzantine bound | 1 |
| `K` layers | **4 (recommended; `= n`, max fall-through depth)** or 3 (`= f+2`, smaller bandwidth) |
| `R` rounds | 1 (fixed; 2abOBFT is single-round) |
| Slot | Ethereum slot for which the cluster is proposer |
| V-signing keypair | the validator's split BLS key (already exists in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| `L_0` (primary leader) | designated MEV proposer for the slot |
| `V_{L_0}` | MEV-optimized block fetched late from the relay |
| `L_1, ..., L_{K-1}` (backup leaders) | separately designated operators, distinct from `L_0` and from each other |
| `V_{L_k}` for k ≥ 1 | safe early-fetched blocks from vanilla beacon-node payloads, refreshed on head changes |
| `T_commit` | view-fix deadline — anchor: `slot_start + 1.5s` for the configurations below |
| `T_broadcast_max` | leader broadcast deadline — `T_commit − 2(D + δ)`; per-layer fetch windows fit within `[0, T_broadcast_max]` |
| `T_accept_max` | receiver acceptance horizon — `T_commit + Δ_2a − (D + δ)`; bundles first-observed past this are auth-only-retained |
| `T_verdict_max` | verdict broadcast horizon — coincident with `T_accept_max` |
| `T_round_end` | reconstruction deadline — `T_commit + Δ_2a + Δ_2b + Δ_3` |

### Timing budget — concrete configurations

The slot's hard relay-submission deadline is `slot_start + 4.0s`; a minimum `T_submit ≈ 250ms` is reserved for relay submission. The consensus deadline is `T_round_end = slot_start + 4.0s − T_submit ≤ slot_start + 3.75s`.

Common parameters: **D = 100ms (cluster gossipsub P99/P999), δ = 50ms, n = 4, f = 1**.

#### 2abOBFT(n=4, K=4) recommended sizing

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch (effective) | 1200ms | slot_start + 1.20s | `T_broadcast_max = T_commit − 2(D+δ) = 1.20s` |
| Phase-1 propagation slack | 300ms | slot_start + 1.50s = T_commit | Bundles broadcast at deadline propagate to all honest within `D + δ` |
| Phase 2a | 300ms | slot_start + 1.80s | `Δ_2a = 2(D+δ)`; verdict broadcast horizon = `T_commit + Δ_2a − (D+δ) = 1.65s`; absorbs late bundles arriving up to 1.65s |
| Phase 2b | 300ms | slot_start + 2.10s | `Δ_2b = 2(D+δ)`; σ/NR partials propagate to peers before Phase 3 |
| Phase 3 | 250ms | slot_start + 2.35s | `Δ_3 = (D+δ) + ε_3 = 250ms`; absorbs end-of-Phase-2b NR-partial propagation + reconstruction |
| Submission | 1650ms | slot_start + 4.00s | 6.6× the 250ms minimum — comfortable headroom |

#### 2abOBFT(n=4, K=4) minimum sizing

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch (effective) | 1200ms | slot_start + 1.20s | Same |
| Phase-1 propagation slack | 300ms | slot_start + 1.50s | Same |
| Phase 2a | 150ms | slot_start + 1.65s | `Δ_2a = D+δ` (BFT-minimum); narrower late-bundle absorption |
| Phase 2b | 150ms | slot_start + 1.80s | `Δ_2b = D+δ` |
| Phase 3 | 250ms | slot_start + 2.05s | Same |
| Submission | 1950ms | slot_start + 4.00s | 7.8× the 250ms minimum |

**Recommended sizing trades 300ms of submission headroom for mesh-jitter absorption**. Production telemetry should drive the choice.

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_{L_k}` includes a `parent_root`-vs-current-head check. The host's head-tracking and validation logic is internal; the protocol consumes the verdict.

**Leader pre-broadcast fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before broadcast** — internal to the leader's local fetch state. The leader broadcasts the bundle on the final `V_{L_k}` they commit to after the loop terminates. **No σ_V partial is signed at Phase 1**; the leader's σ commitment happens at Phase 2b alongside everyone else's.

**Receiver-side validity stabilization.** Each operator runs the host's validity check at three points:

1. **Phase-1 acceptance**: drives bundle retention / auth-only fallback. A `not-valid` verdict at this point causes the operator to retain the bundle (auth-valid) but plan for a NV verdict at Phase-2a.
2. **Phase-2a verdict-broadcast time**: drives `KindVerdict` content. The operator runs validity check against their *current* head (potentially changed since Phase-1 acceptance) and issues σV / NV / NR accordingly.
3. **Phase-2b sign time**: the convergence rule's "host re-validates" branch. If the cluster has reached σ-eligibility-quorum on V but the operator's current host says NV, the operator falls into the NR-side per rule.

This per-operator workflow narrows the divergence window to events landing inside `[Phase-1 acceptance, Phase-2b sign time]` — typical span at recommended sizing is 600-800ms. Re-orgs landing within this span and producing honest verdict splits are the residual validity-divergence exposure (Class A at 2-2 boundary; Class B-equivalent at majority splits — recovered).

**Backup-leader re-org resistance.** Fetching `V_{L_k}` for k ≥ 1 from a deeper-confirmed parent (the asymmetric `T_{K-1} < ... < T_1 < T_0` schedule already accommodates this) reduces the likelihood that the backup's parent becomes orphaned. Backups are structurally re-org-resistant by construction.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. Same as [OBFT](OBFT.md) and [OBFTR](OBFTR.md).

2. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. Three distinct deadlines (do not conflate):

   - **`T_broadcast_max = T_commit − 2(D + δ)`**: leader broadcast deadline.
   - **`T_accept_max = T_commit + Δ_2a − (D + δ)`**: receiver acceptance horizon.
   - **`T_verdict_max = T_commit + Δ_2a − (D + δ)`**: verdict broadcast horizon (coincident with T_accept_max).

   Phase-window minimums: `Δ_2a ≥ D + δ`, `Δ_2b ≥ D + δ`, `Δ_3 ≥ (D + δ) + ε_3`. Recommended: `Δ_2a = Δ_2b = 2(D + δ)` for jitter absorption.

3. **Choosing K (layer count).** K is per-duty. `K = 2` (BFT-min at f=1) is not recommended — exposes the late-deepest-layer-leader-broadcast at K=2 (no L_2 to fall through to). `K = 3..n` provides multiple fall-through layers within Phase 3's single reconstruction walk. **Recommended for 2abOBFT proposer duty: `K = n = 4`** (maximum fall-through depth at f=1).

4. **R is fixed at 1.** 2abOBFT is single-round by design. Multi-round extension (combining Phase 2a/2b split with R-round retry) is an open future direction — composes cleanly but is not specified here.

5. **Tag construction and replay.** Each NR tag must uniquely bind `(slot, cluster, layer)` to prevent replay across slots/layers/clusters. Structure: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`.

6. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one 2abOBFT instance and assumes:
   - Single 2abOBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between 2abOBFT (`protocol_tag = "2abOBFT-v1"`) and any other path that signs against the V-signing share ([OBFT](OBFT.md), [OBFTR](OBFTR.md), QBFT, etc.).
   - Slashing protection gates **Phase-2b candidate signing** (V-share) and **Phase-2b no-σ signing** (IBE-share).

7. **2-1 equivocation byz-defect remains a Class B regression vs single-Phase-2 protocols.** Documented in [§Liveness / Equivocation 2-1 split](#2-1-split). Reputation deterrent (assumption 4) is the practical defense.

8. **Verdict envelope size.** ~100-200 bytes per verdict per layer per operator. Per slot: K × n verdicts × 200 bytes ≈ 3.2 KB at K=n=4. Within budget.

## Where this came from

2abOBFT extends bare [OBFT](OBFT.md) with the Phase 2a/2b observation-then-commit split: Phase-2a is a bundle re-flood + verdict broadcast window where operators announce their σ-eligibility without binding any threshold partial, and Phase-2b is the σ-or-NR commit window where each operator emits exactly one threshold partial based on the cluster-wide Phase-2a verdict pool.

The load-bearing design choice is **removing the Phase-1 σ_V**. A naive Phase-2a/2b split that keeps the leader's Phase-1 σ_V (signed at fetch time, on the wire from Phase 1) structurally locks the leader's σ-side commitment irrespective of subsequent verdict changes. This blocks validity-divergence recovery at the f-bound boundary (e.g., 2-σ vs 2-NV at f=1 n=4: leader is σ-locked on V_pre-reorg; non-leader honest cannot reach NR-quorum with only 2 NV-side honest contributing). 2abOBFT removes the Phase-1 σ_V entirely — leader broadcasts only `(V, σ^op)` at Phase 1, and the leader's σ commitment happens at Phase 2b alongside everyone else's.

Trade-off: 2-1 equivocation patterns where bare OBFT's Phase-1 σ_V cryptographically locks byz's σ partial on the wire — succeeding at L_0 even if byz subsequently goes silent — regress in 2abOBFT (the byz can defect from σV verdict to NR action; slashable Rule 6b but the slot misses at L_0). A second regression on the same theme: non-leader verdict-equivocation under marginal h_V — byz verdict-equivocation injects per-peer convergence divergence, leading to under-quorum splits at L_0. Across many slots the rational-byzantine deterrent absorbs both costs; per-slot, both are real regressions. See [§Liveness / Equivocation 2-1 split](#2-1-split) and [§Failure modes / Non-leader verdict-equivocation under marginal h_V](#failure-modes).

The relationship across the OBFT family:

| Protocol | R | K | Phase-2 split | Phase-1 σ_V | Role |
|---|---|---|---|---|---|
| [OBFT](OBFT.md) | 1 | configurable | no | yes | minimum machinery for K-layer fall-through |
| [OBFTR](OBFTR.md) | configurable (typically 2) | configurable | no | yes | OBFT + R-round retry for `(D, R · D]` partition coverage |
| **2abOBFT** | 1 | configurable | yes (Phase 2a/2b) | **no** | **OBFT + Phase 2a/2b + no Phase-1 σ_V for full validity-divergence recovery** |

## Appendix A — Protocol comparisons

### A.1 — Comparison with [OBFT](OBFT.md)

OBFT is the closest sibling — same single-round structure, same K-layer fall-through, same chained IBE, same six EKM-coordinated keypairs setup. The differences are concentrated at Phase-1 / Phase-2 commitment timing.

| Aspect | [OBFT](OBFT.md) | 2abOBFT |
|---|---|---|
| Phase-1 bundle | `(V, σ_L^V, σ_L^{op})` — leader signs threshold σ_V at Phase 1 | `(V, σ_L^{op})` — auth envelope only |
| Phase-2 structure | Single Phase 2 (single `KindCommit` at `T_commit`) | Two windows: Phase 2a (verdict broadcast) + Phase 2b (σ/NR commit) |
| Operator commitment states | σ, NR, NV (3 states) | σ, NR, NV (3 states) |
| σ-commit timing | Phase 1 (leader) or Phase 2 (others, at `T_commit`) | Phase 2b (uniform across all operators, after Phase-2a observation) |
| Convergence mechanism | Per-operator local view (each operator commits at `T_commit` based on retained V's) | Cluster-wide verdict observation in Phase-2a, σ-quorum-eligibility check at Phase-2a end |
| Healthy-path latency (post-`T_commit`, recommended sizing) | ~250ms | ~550ms (+300ms for Phase-2a window) |
| Marginal h_V_honest=2 + byz silent | Slot misses (σ-pool short, NR-pool short) | Falls through to L_1 ✓ |
| Equivocation 1-1-1 split | Slot misses | Falls through to L_1 ✓ |
| Equivocation 2-1, byz cooperates | Succeeds at L_0 | Succeeds at L_0 (tie) |
| Equivocation 2-1, byz silent | Succeeds at L_0 (Phase-1 σ_V locked) | Falls through to L_1 (one extra layer) |
| Equivocation 2-1, byz defects | Succeeds at L_0 (Phase-1 σ_V locked) | **Slot misses (regression)** — Rule 6b evidence |
| Non-leader verdict-equivocation at marginal h_V | n/a (no verdicts in OBFT) | **Slot misses (regression)** — Rule 6a evidence |
| h_V=1 selective-delivery deadlock | Partially closed (withhold-then-fake-σ variant by Defer removal); selective Phase-1 delivery still slot-misses (algebraic limit at f=1, n=4) | Falls through to L_1 ✓ |
| Validity-divergence at majority | Slot misses (Class A) | Recovered ✓ |
| Validity-divergence at 2-2 boundary | Slot misses (Class A) | Slot misses (Class A — same algebraic limit) |
| Late deepest-layer leader broadcast | Class A | Recovered (Phase-2a re-flood absorbs) ✓ |
| Mesh-flakiness | Class B | Mitigated ✓ |
| Submission headroom (Config A recommended) | ~3.4s | ~2.85s |
| Bandwidth (healthy, n=4, K=4) | ~27 KB | ~30 KB (+3 KB for verdicts) |
| EKM complexity | Phase-1 σ_V + Phase-2 σ + NR coordination | Phase-2b σ XOR NR only — simplest in the family |
| Slashing-evidence rules | 5 | 7 (Rules 1-5 inherited + Rule 6a verdict-vs-verdict cryptographic + Rule 6b verdict-vs-action gossipsub-pattern-quality) |

**Migration**: cluster running OBFT can adopt 2abOBFT by (1) extending the wire format with `KindVerdict`, (2) replacing the single-Phase-2 commit with the Phase-2a/Phase-2b split, (3) modifying the Phase-1 bundle schema to drop σ_V, (4) updating the protocol-tag to `2abOBFT-v1` for envelope domain separation. EKM coordination simplifies (one fewer signing event per (slot, layer)).

### A.2 — Comparison with [OBFTR(R≥2)](OBFTR.md)

OBFTR(R≥2) is the multi-round extension of OBFT with cross-round acceptance widening for wider partition absorption. Comparing 2abOBFT to OBFTR(R≥2):

- **2abOBFT covers more failure modes within R=1** than OBFTR(R≥2) does — Phase-2 split closes equivocation 1-1-1, h_V=1, validity-divergence-majority, mesh-flakiness, late-deepest-layer-broadcast that OBFTR(R≥2) leaves uncovered.
- **OBFTR(R≥2) covers wider partition tails** within `(D, R · D]` than 2abOBFT does — multi-round retry extends the absorption envelope, while 2abOBFT is bounded by `Δ_2a + (D + δ)` (single round).

The two design directions are orthogonal — Phase 2a/2b split + R-round retry composes cleanly. Combined design ("2abOBFT + R") is the most-recovery point in the family but not yet specified.

### A.3 — Comparison with bare OBFT and QBFT

QBFT is SSV's existing consensus protocol; bare [OBFT](OBFT.md) is the spec-simplest OBFT-family ancestor of 2abOBFT (single Phase 2 emitting a single KindCommit at T_commit, no Phase 2a/2b split). Two structural differences matter for the comparison:

- **QBFT vs OBFT family**: QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus partial-sig collection); the OBFT family (bare OBFT, 2abOBFT, OBFTR) fuses them by embedding partial signatures inside the consensus phases.
- **2abOBFT vs bare OBFT**: 2abOBFT splits Phase 2 into Phase 2a (op-identity-signed verdict broadcast — claim about σ-eligibility, no threshold partial bound) + Phase 2b (σ-or-NR commit with threshold partial bound). Bare OBFT binds at T_commit during a single Phase 2.

Throughout this section, "apples-to-apples T" means comparing all three protocols at equal *total slot budget*. Each protocol *uses* the budget differently — QBFT for rounds, 2abOBFT for phases, bare OBFT for a single phase — but the comparison is structurally stable when measured at equal T.

#### Recovery scope at apples-to-apples T

The failure modes factor into four buckets.

**Bucket 1 — Absorbable by waiting** (recovers identically across all three protocols, given sufficient T):

- Network jitter, propagation tails, late re-flood
- Mesh-flakiness with all-honest operators
- Honest-leader temporary silence
- Transient operator outage
- Network partition that resolves within absorption budget

The relevant variable is total absorption window. **None of the protocols' structural differences provide additional recovery here** — sufficient time + the trust bound is the common minimum. Pure all-honest network failures fall in this bucket; bare OBFT, 2abOBFT, and QBFT cover them identically at equal T.

**Bucket 2 — Structural advantages of OBFT family over QBFT** (regardless of T):

- **Multi-leader-silent recovery within budget**: K-layer parallel fall-through within Phase 3's local decryption walk recovers K-1 silent leaders in a single Phase-3 cycle. QBFT's serial round-change at RT per round = K rounds × RT exceeds typical slot budgets at K-1 ≥ 3 (e.g., 4 × 2s = 8s vs SSV's 4s relay cutoff).
- **Cryptographic safety primitive against offline aggregator** (assumes EKM correctness): σ-or-NR exclusivity is honest-majority-cryptographic via threshold-signature pigeonhole. QBFT's safety relies on certificate-construction correctness in operator software — same trust level (honest-majority correct code), different mechanism. See [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic).

**Bucket 3 — Structural advantages of 2abOBFT over bare OBFT** (regardless of T):

These are patterns where bare OBFT's σ-emit-during-Phase-2 cross-phase exclusivity blocks fall-through, and 2abOBFT's deferred binding at Phase 2b lets the convergence rule route σ-eligible honest through NR-pool fall-through:

- **Mesh-flakiness coordinated with byz σ-refusal at byz-leader slots**: no Phase-1 σ_V from byz leader → bare OBFT's σ-pool is starved when 1 honest mesh-flakes incorrectly to NR; 2abOBFT routes to NR fall-through via convergence rule.
- **σ-locked equivocation 1-1-1 split**: bare OBFT's σ partials split below qV; 2abOBFT's verdict pool short → all NR → fall-through.
- **h_V=1 selective-delivery deadlock**: same algebraic shape — bare OBFT's σ-locked-on-V honest can't switch sides; 2abOBFT's deferred binding allows convergence-rule fall-through.
- **Validity-divergence majority recovery (3-of-4 at f=1 n=4)**: bare OBFT's leader Phase-1 σ_V locks σ-side onto stale V at re-org; 2abOBFT's leader doesn't pre-lock (Variant C in [Appendix C / Variants considered](#variants-considered)) — Phase-2b binds against post-observation stabilized verdict pool.
- **Late deepest-layer-leader broadcast within Phase-2a**: Phase 2a re-flood absorbs cleanly; bare OBFT needs widened Δ_2 with its own absorption-vs-validity-divergence-window trade-off.

These all involve adversarial-byz patterns or application validity-divergence — **pure all-honest network failures fall in Bucket 1, not here.** Extending Δ_2 in bare OBFT (matching 2abOBFT's commit deadline) does not close these patterns; the cross-phase exclusivity is structural, not time-based.

**Bucket 4 — 2abOBFT regressions vs bare OBFT and/or QBFT** (regardless of T):

These are patterns 2abOBFT introduces or fails to recover, where bare OBFT and/or QBFT do recover:

- **2-1-byz-defect equivocation**: byz leader equivocates V/V', verdict-claims σV(V) with majority, defects to NR partial at Phase-2b binding step. Bare OBFT ✓ — Phase-1 σ_V cryptographically locks byz's σ on V; byz can't defect at later signing. QBFT ✓ — round-2 fresh V. 2abOBFT ✗ — Variant C's Phase-1 σ_V removal exposes the defection surface; rational-byzantine deterrent (Rule 6b) is the only protocol-level defense.
- **Verdict-equivocation surface**: byz broadcasts conflicting verdicts to fragment the Phase-2a convergence pool. Bare OBFT has no separable verdict surface (n/a); QBFT has no separable verdict surface (n/a — PREPARE carries the partial directly); 2abOBFT introduces this surface to enable Bucket-3 recoveries — it pays the verdict-equivocation surface as the structural cost. Reputation deterrent (Rule 6a) is the protocol-level defense.
- **Validity-divergence 2-2 split**: bare OBFT ✗ at L_0 (σ-locked leader, no fall-through). 2abOBFT falls through to L_1 via convergence rule, but if the head-change affects deeper layers' fetches the same way, L_1, L_2, ... also split — slot-misses unless a deeper layer happens to have been fetched at a different head. QBFT ✓ if head moves between round-1 timeout and round-2 leader's refetch.

#### Failure-mode coverage at apples-to-apples T

| Failure class | bare OBFT | 2abOBFT | QBFT |
|---|---|---|---|
| Healthy path | ✓ | ✓ | ✓ |
| Bucket 1 (all-honest network failures within absorption) | ✓ | ✓ | ✓ |
| Multi-leader silent K-1 ≥ 3 (Bucket 2) | ✓ in-round | ✓ in-round | ✗ exceeds budget at typical RT |
| Mesh-flakiness + byz σ-refusal (Bucket 3) | ✗ | ✓ | ✓ |
| σ-locked equivocation 1-1-1 (Bucket 3) | ✗ | ✓ | ✓ |
| h_V=1 selective-delivery (Bucket 3) | ✗ | ✓ | ✓ |
| Validity-divergence majority 3-of-4 (Bucket 3) | ✗ (leader σ_V locks σ-side) | ✓ | ✓ |
| Late deepest-layer broadcast (Bucket 3) | mitigated by K ≥ f+2 | ✓ | recoverable if round 2 fits |
| 2-1-byz-defect equivocation (Bucket 4) | ✓ (Phase-1 σ_V crypto-locks σ-pool) | ✗ regression | ✓ (round 2 fresh V) |
| Verdict-equivocation surface (Bucket 4) | n/a (no surface) | ✗ regression | n/a (no surface) |
| Validity-divergence 2-2 split (Bucket 4) | ✗ at L_0 (no fall-through) | falls through to deeper layers (may also split) | recoverable if head moves during round-1 timeout |
| Sustained partition > absorption | ✗ | ✗ | ✗ |
| > f operators offline / byz | ✗ | ✗ | ✗ |

#### Production T allocation and cost dimensions

At SSV proposer-duty default budget (~4s relay cutoff with D = 100ms, δ = 50ms), each protocol allocates the budget differently:

| Aspect | bare OBFT | 2abOBFT | QBFT (RT=2s, current SSV) |
|---|---|---|---|
| Consensus budget | ~600ms | ~850ms (recommended) | ~3s (2 rounds at RT=2s) |
| Submission headroom | ~3.4s | ~3.15s | ~1s |
| Healthy-path latency | ~600ms | ~850ms | ~750ms |
| Bandwidth (n=4, K=4 healthy) | ~27 KB | ~30 KB | ~14 KB |
| Cryptographic primitives | BLS + threshold IBE/SWE | BLS + threshold IBE/SWE | BLS threshold |
| Production maturity | spec only | spec only | SSV runs this today |

#### Apples-to-apples vs production-T framing

The production-T allocation reflects current deployment choices, which are **not** strictly apples-to-apples for failure-mode coverage — QBFT uses ~3s for consensus + ~1s for submission, while 2abOBFT uses ~850ms for consensus + ~3.15s submission headroom. Comparing failure-mode coverage at production-T can read as "QBFT recovers more failure modes than 2abOBFT" but conflates two different effects:

- **Time-conditional recoveries**: at small T, QBFT fits only 1 round and loses most of its multi-round-conditional recoveries (mesh-flakiness with byz leader, σ-locked equivocation, h_V=1, validity-majority — Bucket-3-equivalents). 2abOBFT recovers all of these at small T via single-round convergence rule. **At T = 600ms apples-to-apples, 2abOBFT has a strictly larger deadlock-free set than QBFT.** At larger T, QBFT's round-2 access scales recovery and matches 2abOBFT on most Bucket-1 + Bucket-3 patterns.
- **Structural recoveries**: Bucket 2 (OBFT-family-only — multi-leader silent, crypto safety primitive) and Bucket 4 (2abOBFT regressions — 2-1-byz-defect, verdict-equivocation, validity-2-2-refetch) are independent of T. They reflect protocol structure, not budget.

If you give 2abOBFT a 3s consensus budget (extending Δ_2a / Δ_2b), the recovery scope does not grow — single-round protocols don't add recoveries with more time, only wider absorption windows. If you compress QBFT to a 600ms consensus budget (1 round only), QBFT's recovery scope shrinks to single-round failures only, losing all time-conditional Bucket-3-equivalent recoveries.

The bucket structure makes the protocol-vs-protocol comparison stable across T choices. The production-T table reflects deployment-cost trade-offs (latency, submission headroom, primitive maturity) on top of the structural recovery scope.

#### Composability note

Multi-round 2abOBFT (Phase 2a/2b composed with R-round retry — see [§Where this came from](#where-this-came-from)) is a future direction that would close Bucket-4 partially: round-change with σ-lock abandonment recovers 2-1-byz-defect and validity-2-2-refetch; verdict-equivocation surface remains. The combined "2abOBFT + R" design point would have a deadlock-free set roughly comparable to QBFT's in adversarial-byz scenarios, with Bucket-2 OBFT-family advantages (multi-leader silent in-round) preserved. Not yet specified.

## Appendix B — L_Bid mini-consensus extension

This appendix specifies an opportunistic bid-routing extension to 2abOBFT. **L_Bid** is a bid-determined top layer prepended to 2abOBFT's rotation-determined K layers (yielding a `K' = K + 1` configuration). Unlike the analogous extension for [bare OBFT](OBFT.md#appendix-b--l_bid-mini-consensus-extension) or [OBFTR](OBFTR.md#appendix-b--l_bid-mini-consensus-extension), **2abOBFT does not need a separate mini-consensus phase** — the existing Phase-2a verdict broadcast already provides the cluster-wide convergence mechanism. L_Bid integrates as **another verdict-bound layer** in Phase 2a/2b alongside the rotation layers.

The integration closes the C1 (selective bid-withholding), C2 (bidder equivocation), and C3 (validity-divergence majority) deadlock surfaces of the naive bid-routing sketch (no mini-consensus, σ-eligibility predicated on each operator's locally-observed bid set), at **no additional slot-budget cost** over bare 2abOBFT (Phase 2a was already paying the RTT). It introduces the same residual L_Bid surfaces (2-1-byz-defect, verdict-equivocation) that 2abOBFT's rotation layers already expose, now with broader trigger surface (byz is always a bidder, not just byz-leader-only).

### When to use it

**Suited for**: deployments where MEV bid-routing upside justifies the additional adversarial-byz residual surface at L_Bid. For SSV proposer duty: high-MEV slots where bid-routed block value-capture exceeds slot-loss-rate cost from L_Bid-specific failure modes (which are the same shape as 2abOBFT's rotation-layer Class B regressions, just at higher trigger frequency).

**Not suited for**: low-MEV deployments where the +bandwidth and additional adversarial surface is not justified by routing upside.

### Setting

Adds to 2abOBFT's setting:

- **K' = K + 1 layers**: L_Bid (top, bid-determined) + 2abOBFT's rotation-determined L_0, ..., L_{K-1}.
- **Bid envelopes**: every operator broadcasts a bid envelope at Phase 1 (alongside rotation leaders' Phase-1 bundles).
- **Phase 2a covers L_Bid**: each operator's Phase-2a verdict envelope includes a verdict for L_Bid (in addition to verdicts for L_0, ..., L_{K-1}). No separate mini-consensus window — Phase 2a *is* the mini-consensus.
- **Phase 2b covers L_Bid**: σ-or-NR commit at L_Bid driven by 2abOBFT's standard convergence rule applied to the L_Bid verdict pool.

`qV = qEnc = 2f+1` and the BLS+IBE keypair structure are unchanged from 2abOBFT base.

### Wire kinds

In addition to 2abOBFT's wire kinds (`Phase1Bundle`, `KindVerdict`, `KindOnion2b`, `KindNR2b`, `KindCertificate`):

- **`KindBid`** (new): operator `i`'s bid envelope. Payload `(protocol_tag = "2abOBFT-LBid-v1", message_kind = "bid-envelope", cluster_id, slot, operator_id i, bid_value, V_i, relay_attestation)`, signed by `i`'s operator-identity key.

`KindVerdict` envelopes are extended to carry per-layer verdicts for **all K' layers** (L_Bid + L_0..L_{K-1}). The wire format is unchanged in shape — verdicts are already per-layer in 2abOBFT base; the addition is just another layer index in the verdict envelope's payload.

There is **no new mini-consensus message kind** — `KindVerdict` covers L_Bid the same way it covers rotation layers.

### Per-layer windows and deadlines

**Slot timeline is identical to bare 2abOBFT** (no new phases):

| Phase | Window | Activity |
|---|---|---|
| Phase 1 fetch | `[slot_start, T_broadcast_max]` | Operators fetch V_i; rotation leaders prepare Phase-1 bundles; all operators prepare bid envelopes |
| Phase 1 broadcast | `[T_broadcast_max, T_commit]` | Rotation leaders broadcast Phase-1 bundles; all operators broadcast `KindBid`. Propagation slack `D + δ`. |
| Phase 2a | `[T_commit, T_commit + Δ_2a]` | Bundle re-flood + bid-envelope re-flood + per-layer verdict broadcast (incl. L_Bid). |
| Phase 2b | `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]` | σ-or-NR commit at all K' layers. |
| Phase 3 | `[T_commit + Δ_2a + Δ_2b, T_round_end]` | K'-layer reconstruction walk. |

Sizing identical to bare 2abOBFT. **No additional slot-budget cost** for the L_Bid integration — Phase 2a was already paying the RTT.

### Protocol

#### Phase 1 — Bid + rotation leader broadcast

Each operator `i`:
1. Fetches V_i (relay or vanilla) with bid_value.
2. Constructs `KindBid` envelope; signs with operator-identity key; gossips.

In parallel, each rotation leader L_k for k ∈ {0, ..., K-1} broadcasts their Phase-1 bundle as in bare 2abOBFT (no σ_V partial — Variant C). **Coherence rule**: rotation leader's Phase-1 bundle V matches their bid envelope V_i.

Receivers retain bid envelopes per `(slot, operator_id)`; bid equivocation is slashable Rule 7 (see [§Slashing-evidence rules](#slashing-evidence-rules-1)).

#### Phase 2a — Bundle re-flood + per-layer verdict broadcast (incl. L_Bid)

Same as bare 2abOBFT, with verdicts now covering K' layers. Each operator computes per-layer verdicts:

- **L_Bid**:
  - Compute `bid_set_i` = received-and-validated bid envelopes by `T_commit + Δ_2a − (D + δ)`.
  - If `|bid_set_i| ≥ n − f` AND optional parent-root filter passes: `predicted_LBid_i = argmax over bid_value` (op_id tiebreak). Verdict: `σV(predicted_LBid_i)`.
  - Else: verdict `NULL` (insufficient visibility).
  - If host returns `not-valid` on `predicted_LBid_i`: verdict `NV` (operationally NR-side).
- **L_0, ..., L_{K-1}** (rotation layers): same as bare 2abOBFT — `σV(V_{L_k})` if operator has V_{L_k} retained and host validates; `NV` if host returns not-valid; `NULL`/`NR` if no V_{L_k} retained.

Each operator broadcasts a single `KindVerdict` envelope per slot containing per-layer verdicts. Verdict equivocation (two distinct verdicts from same operator) is slashable Rule 6 (2abOBFT base's existing rule, generalized to L_Bid).

#### Phase 2b — Convergence rule + σ-or-NR commit at K' layers

For each layer including L_Bid, apply 2abOBFT's standard convergence rule:

- `verdict_pool[V] = | { distinct ops broadcasting first-observed σV verdict on hash(V) } |` for each V.
- `nr_pool = | { distinct ops broadcasting first-observed NR/NV verdict } |`.
- If `∃V : verdict_pool[V] ≥ qV`: cluster σ-eligibility quorum reached on V; operators who have V locally + host re-validates valid σ-emit on V; others NR.
- Else: cluster NR-side; all operators NR.

L_Bid σ partials are **plaintext** (top of onion); deeper-layer σ partials are chained-IBE-encrypted under `nr_tag_LBid ∧ nr_tag_0 ∧ ... ∧ nr_tag_{k-1}` (mirrors OBFT's chained encryption with nr_tag_LBid added at the outer wrap).

EKM enforces single-σ-V per (slot, layer) per operator at Phase-2b sign time, including for L_Bid.

#### Phase 3 — Reconstruction walk

K'-layer walk starting from L_Bid. If σ-quorum reaches at any layer, reconstruct and halt. Standard 2abOBFT reconstruction logic.

### Safety

Identical to bare 2abOBFT. The L_Bid integration is purely a layer addition — verdicts and σ commits at L_Bid follow the same convergence-rule and EKM semantics as the rotation layers. Pigeonholes 1, 2, 3 hold unchanged at K' layers.

### Slashing-evidence rules

Inherits 2abOBFT's 6 rules unchanged. One additional rule for the new bid-envelope wire kind:

- **Rule 7 — Bid equivocation**: two distinct `KindBid` envelopes from same operator at same slot. Self-contained slashable evidence — both envelopes signed by `i`'s operator-identity key.

2abOBFT's existing Rule 6 (verdict equivocation / verdict-vs-action equivocation) extends to cover L_Bid verdicts naturally — the rule already applies to any per-layer verdict-vs-action mismatch.

### Liveness

#### Recovery scope at L_Bid

The Phase-2a/2b convergence rule closes C1/C2/C3 at L_Bid:

- **C1 — Selective bid-withholding**: byz withholds → some honest verdict NULL → verdict-pool fragments → no V reaches qV → all NR_LBid → fall-through to L_0. **Closed.**
- **C2 — Bidder equivocation**: byz sends conflicting bids → diverging predicted L_Bids → verdict-pool fragments → no V reaches qV → all NR_LBid → fall-through. **Closed.**
- **C3 — Validity-divergence majority on V_LBid (3-of-4 at f=1 n=4)**: 3 honest verdict σV(V_X), 1 NV. `verdict_pool[V_X] = 3 = qV` → cluster σ-binds. **Closed for 3-of-4 majority.** 2-2 split remains hard algebraic limit (same as 2abOBFT base's rotation-layer 2-2 splits).

#### Recovery scope at rotation layers L_0, ..., L_{K-1}

Identical to bare 2abOBFT. Convergence rule, NR-pool fall-through, validity-divergence-majority recovery — all apply uniformly at K' layers.

#### Residual failure modes at L_Bid

The same Class B residuals 2abOBFT exposes at the rotation layers also apply at L_Bid, **with broader trigger surface**:

- **2-1-byz-defect at L_Bid**: byz bid-equivocates + verdict-claims σV on majority-V at Phase 2a + defects to NR partial at Phase 2b. σ-pool < qV; NR-pool < qEnc; deadlock at L_Bid; chained encryption to L_0 stays sealed; slot misses. Slashable Rule 6.
- **Verdict-equivocation at L_Bid**: byz issues different verdicts to different peers; per-peer convergence diverges; deadlock at L_Bid. Same shape as 2abOBFT base's Class B regression at rotation layers ([§Failure modes](#failure-modes)). Slashable Rule 6.
- **2-2 validity split at L_Bid**: hard algebraic limit (same as 2abOBFT base).

**Trigger frequency comparison**:
- 2abOBFT rotation-layer 2-1-byz-defect: triggers when byz is a rotation leader (typically 1/n slots at uniform rotation).
- 2abOBFT + L_Bid 2-1-byz-defect: triggers any slot where byz can bid-equivocate, which is **every slot** byz is a bidder (which is every slot under SSV's all-operators-bid model). With relay-anchoring, byz can still equivocate by querying the relay multiple times for distinct attestations.
- Net: L_Bid extension increases per-slot 2-1-byz-defect trigger frequency by ~n× at f=1 n=4 (every byz slot vs every byz-as-rotation-leader slot).

### Best/worst time-to-completion

**Identical to bare 2abOBFT** — the L_Bid integration adds no phases and no RTT cost. Measured from `T_commit`:

| Scenario | Time | Mechanism |
|---|---|---|
| Healthy completion at L_Bid (or any layer) | ~700-850ms | Phase 2a + Phase 2b + Phase 3 |
| L_Bid fails (C1/C2 patterns) → fall-through to L_0 | ~700-850ms | Convergence rule routes to NR-pool; in-Phase-3 walk to L_0 |
| Multi-leader silent K-1 ≥ 3 fall-through | ~700-850ms | K'-layer walk in Phase 3 (sequential local decryption) |
| L_Bid 2-1-byz-defect / verdict-equivocation | slot misses | Deadlock at L_Bid; same as 2abOBFT base's Class B regression |
| 2-2 validity split | slot misses | Hard algebraic limit |

Best ≈ 450ms (skip Phase 2b minimum if L_Bid σ-quorum visible early, then reconstruct); worst (success) ≈ 850ms; ~2× spread (same as bare 2abOBFT).

### Comparison with bare 2abOBFT

| Aspect | Bare 2abOBFT | 2abOBFT + L_Bid |
|---|---|---|
| Slot structure | Phase 1 → Phase 2a → Phase 2b → Phase 3 | **Same** (no new phases) |
| Layers | K (rotation-determined) | K' = K + 1 (L_Bid + K rotation-determined) |
| Wire kinds | Phase1Bundle, KindVerdict, KindOnion2b, KindNR2b, KindCertificate | + KindBid (only one new wire kind — verdicts already cover per-layer in base) |
| Slashing-evidence rules | 6 (Rules 1-6) | 7 (+ Rule 7 bid equivocation; Rule 6 covers L_Bid verdict-equivocation naturally) |
| Healthy-path latency | ~700-850ms | **Same** |
| Best-case latency | ~450ms | **Same** |
| Worst-case latency (success) | ~850ms | **Same** |
| Time-to-completion spread | ~2× | **Same** |
| Bandwidth (n=4, K=4 healthy) | ~30 KB | ~33 KB (+n bid envelopes; verdict envelope grows by 1 layer-entry; +1 chained encryption layer in onion) |
| Submission headroom (4s cutoff) | ~3.15s | ~3.15s (no significant change) |
| Cryptographic primitives | BLS threshold + threshold IBE/SWE | Same |
| **Safety** | Cryptographic via Pigeonholes 1, 2, 3 | **Same** |
| Rotation-layer (L_0/.../L_{K-1}) liveness | 2abOBFT base recovery scope | **Same** (rotation layers unchanged) |
| L_Bid liveness — C1 selective bid-withholding | n/a (naive sketch deadlocks) | **Closed** (verdict-quorum-short → fall-through) |
| L_Bid liveness — C2 bidder equivocation | n/a (naive sketch deadlocks) | **Closed** (verdict-quorum-short → fall-through) |
| L_Bid liveness — C3 validity-majority (3-of-4) | n/a (naive sketch deadlocks) | **Closed** (verdict-quorum reaches on majority) |
| L_Bid liveness — 2-1-byz-defect | n/a | **Open**: same shape as rotation-layer Class B regression; slashable Rule 6 |
| L_Bid liveness — verdict-equivocation | n/a | **Open**: same shape as rotation-layer Class B regression; slashable Rule 6 |
| L_Bid liveness — 2-2 validity split | n/a | **Open**: hard algebraic limit |
| Bid-routing value capture | n/a | Highest-bid block on healthy path |
| Adversarial-byz trigger frequency at L_Bid | n/a | Higher than rotation-layer surfaces — byz is always a bidder, vs rotation-leader-only |

**Net trade vs bare 2abOBFT**: pays additional bandwidth (~3 KB) and extends 2abOBFT's existing Class B residuals (2-1-byz-defect, verdict-equivocation) to L_Bid with broader trigger frequency, in exchange for bid-routing value capture on the healthy path. **Latency, safety, and rotation-layer liveness are unchanged.** The trade is structurally cleaner than the OBFT and OBFTR L_Bid extensions, which add a separate mini-consensus phase costing +1 RTT — 2abOBFT's existing Phase 2a absorbs the L_Bid convergence at no additional latency cost.

**Comparison with the naive bid-routing sketch (no mini-consensus)**: closes C1, C2, and C3 deadlocks via the cluster-wide convergence rule (same mechanism as the rotation layers). The L_Bid residuals (2-1-byz-defect, verdict-equivocation) match 2abOBFT's existing rotation-layer Class B regressions in algebraic shape — they don't introduce structurally new failure modes, just expose the existing modes at a higher per-slot trigger frequency.

**Comparison with OBFT + L_Bid mini-consensus and OBFTR + L_Bid mini-consensus**: 2abOBFT + L_Bid is the cleanest composition of the three — no separate mini-consensus phase, no additional latency, identical recovery profile to bare 2abOBFT modulo the L_Bid-specific surfaces. OBFT and OBFTR pay +Δ_minicon (~300ms) for the same C1/C2/C3 closure plus the same residuals; 2abOBFT gets it for free because Phase 2a is already in the protocol.

## Appendix C — Design plan and discussion notes

**Non-load-bearing reference material.** This appendix preserves the design-plan discussion accumulated during development of 2abOBFT — variants considered, detailed scenario walkthroughs, edge cases, open implementation questions, and the build-phase plan. **It is not part of the canonical spec.** The spec is in §Setting through §Properties summary above; where this appendix and the spec differ, the spec is authoritative. The content here is preserved as historical reference and as a tracking record of discussions and ideas that may apply to future implementation.

### Status

- **Variant chosen**: "Variant C" below — no Phase-1 leader σ_V; verdict broadcasts in Phase-2a; σ/NR commit in Phase-2b. Justification follows.
- **Scope**: SSV proposer duty at `n = 4, f = 1, K = 4` as the running example; algebra generalizes to higher `n`/`f`.
- **Relationship to existing code**: bare OBFT (without Phase 2a/2b) is implemented in [protocol/v2/obft](protocol/v2/obft/). 2abOBFT extends it by adding the Phase-2a observation phase; several 2abOBFT pieces are drop-in additions on top of the existing bare-OBFT state machine.

### What changes vs bare OBFT

| Component | OBFT | OBFT + Phase 2a/2b (this) |
|---|---|---|
| Phase-1 bundle | `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` | `(V_{L_k}, σ_{L_k}^{op}(envelope))` — **no σ^V** |
| Phase-2 structure | Single Phase 2 (single `KindCommit` at `T_commit`) | Two windows: Phase 2a (verdict broadcast, no σ partials) + Phase 2b (σ-or-NR commit) |
| Operator commitment states | σ / NR / NV (3 states) | σ / NR / NV (3 states) |
| σ-commit timing | Phase-1 (leader) or Phase-2 at `T_commit` (others) | Phase-2b only (uniform across all operators) |
| Convergence mechanism | Per-operator local view at `T_commit` based on retained V's | Cluster-wide verdict observation in Phase-2a, σ-quorum-eligibility check at Phase-2a end |
| EKM coordination | Single signing event per (slot, layer) per operator (V-share + IBE-share) | Same — single signing event per (slot, layer) per operator at Phase-2b; verdict envelope is op-identity-signed (not threshold) and does not consume EKM slashing-protection |
| Equivocation σ-locked split recovery | None — slot-miss class | Recovered structurally — σ-quorum-eligibility short → all honest go NR → fall-through |
| h_V=1 selective-delivery deadlock | Partially closed (withhold-then-fake-σ via Defer removal); selective Phase-1 delivery still slot-misses | Recovered structurally |
| Validity-divergence recovery | Out-of-scope (Class A) | In-scope at f=1 n=4 (recovered by NR-quorum fall-through); structural at higher n/f |
| Slot timing | `T_commit + Δ_2 + Δ_3` ≈ 250ms post-T_commit (Config A) | `T_commit + Δ_2a + Δ_2b + Δ_3` ≈ 550ms post-T_commit (+300ms for Phase-2a window) |
| Wire kinds | `Phase1Bundle`, `KindCommit`, `KindCertificate` | + `KindVerdict` (Phase-2a, op-identity-signed verdict envelope); Phase-2b uses its own commit message |
| Slashing-evidence rules | 5 rules | 5 rules + 1 (verdict-vs-action equivocation) |
| Late-deepest-layer leader broadcast (Class A) | Mitigated by K ≥ f+2; class-A residual | Closed structurally — late bundle observed in Phase-2a is σ-emittable in Phase-2b |
| Mesh-flakiness coordinated with byz σ-refusal (Class B) | Slot-miss surface | Mitigated — deferred commit lets brief flakiness recover |

### Variants considered

Three coherent design points emerge for adding a Phase 2a/2b split to OBFT, differing on whether Phase-2a carries σ partials and whether Phase-1 keeps the leader's σ_V:

#### Variant A — Phase-1 σ_V kept, Phase-2a onion carries σ

Leader signs Phase-1 σ_V (same as bare OBFT). Phase-2a onion carries `V_{L_k}` plaintext and `C_k(σ_i^V(V_{L_k}))` (σ partial wrapped in chained IBE). Operators σ-commit in Phase-2a if they have V; Phase-2b is for late-comers who recover V from a peer's onion (gated by an `f+1`-distinct-Phase-2a-σ-signers witness threshold).

- **Pros**: narrowest spec gap to fill on top of bare OBFT.
- **Cons**: keeps Phase-1 σ_V — leader is σ-locked at Phase-1; **does not recover validity-divergence** (the leader's Phase-1 σ_V on stale V remains the structural blocker). At `f=1 n=4` the witness threshold `f+1 = 2` adds no widening over a leaner construction. Equivocation σ-locked splits where honest σ-commit on different V's at Phase-2a still fail.

#### Variant B — Phase-1 σ_V kept, Phase-2a observation-only

Leader signs Phase-1 σ_V (same as OBFT base). Phase-2a is bundle re-flood + verdict broadcast, no σ partials. Phase-2b: each operator σ-or-NR-emits based on Phase-2a verdict observations. Cross-phase exclusivity: leader is σ-locked from Phase-1; non-leaders commit at Phase-2b.

- **Pros**: keeps the leader's Phase-1 σ_V head-start (one σ partial cluster-wide as soon as Phase-1 succeeds), helping marginal-receive cases reach qV at L_0.
- **Cons**: leader's Phase-1 σ_V still locks them on stale V at validity-divergence. At `f=1 n=4` with all-honest 2-σ vs 2-NV split (leader on σ-side), σ-pool = 2 < qV = 3; non-leader σ-eligible can rule-flip to NR, contributing to NR-pool = 3 = qEnc → fall-through. **At higher `n`/`f` (e.g., `f=2 n=7` with 4-3 validity split)**: σ-pool eligible = 4 < qV = 5, all non-leader σ-eligibles rule-flip to NR, NR-pool = 1+3 = 4 < qEnc = 5 — **slot misses; validity-divergence not recovered**. Variant B is f=1-n=4-tolerable but doesn't generalize.

#### Variant C — No Phase-1 σ_V (chosen)

Leader broadcasts only `(V_{L_k}, σ_{L_k}^{op}(envelope))` — no σ^V partial in Phase-1. Phase-2a is bundle re-flood + verdict broadcast. Phase-2b: every operator (including the leader) σ-or-NR-emits based on Phase-2a verdict observations.

- **Pros**: leader is not locked at Phase-1; their verdict is re-evaluated at Phase-2b time. Validity-divergence recovers at all `n`/`f` because the leader can join the NR-pool when their verdict flips. Equivocation σ-locked split, h_V=1 selective-delivery, late-deepest-layer-leader-broadcast — all recover structurally. Mesh-flakiness mitigates because Phase-2a's window absorbs brief observability outages.
- **Cons**: marginal-receive cases (`h_V` = 2 at f=1 n=4) lose the Phase-1-σ_V head-start. They no longer σ-quorum at L_0 directly; instead they fall through to L_1. At K = n = 4 this costs one local-decryption iteration in Phase 3 — **no extra RTT** since Phase 3 walks layers via local decryption, not RTT-per-layer. Slot still succeeds.

**Why Variant C was chosen.** OBFT's strongest claim about Phase 2a/2b — "Recovers the Class A validity-divergence deadlock" ([docs/OBFT.md / Where this came from](OBFT.md#where-this-came-from)) — is only true under Variant C. The parenthetical hint at [docs/OBFT.md / Where this came from](OBFT.md#where-this-came-from) ("without a Phase-1 σ_L^V, the leader doesn't pre-lock; Phase-2b's σ-emit is on the post-observation stabilized V") points at exactly Variant C. The marginal-`h_V` cost is acceptable because L_0 → L_1 fall-through is cheap (no extra RTT), and the recovery scope is genuinely wider.
### Liveness analysis

OBFT's recovery scope ([docs/OBFT.md / Liveness](OBFT.md#liveness-synchrony-conditional)) extends in three places. We walk through each scenario class.

Running example: `f = 1, n = 4, K = 4`. Honest A, B, C; byzantine D (when present). Leader at L_0 unless stated otherwise.

#### Healthy path

All 4 operators receive `V_{L_0}` via gossipsub within `D + δ`.

- Phase-2a: all 4 operators verdict-claim `σV` on V_{L_0}. `verdict_pool[V_{L_0}] = 4`, `nr_pool = 0`.
- Phase-2a end: σ-eligibility-quorum reached on V (4 ≥ qV = 3). All 4 operators σ-emit at Phase-2b.
- σ-pool actual = 4 (assuming byz cooperates) or 3 (if byz defects to NR — but EKM blocks defection if byz already σ-claimed cross-claim slashing applies; the σ-pool from honest is still 3 ≥ qV).
- Slot succeeds at L_0 in 3 RTTs (Phase 1 + Phase 2a + Phase 2b, with Phase 3's local decryption adding ε_3). At Config A: ~700ms total to certificate gossip start.

#### Marginal-receive cases

##### h_V = 3 (3 of 4 operators received V on time; 1 didn't)

Suppose A, B, D have V; C does not (e.g., C's gossipsub mesh delivered V late, past T_accept_max).

- Phase-2a verdicts: A_σV, B_σV, D_σV (assuming D cooperative), C_NR. `verdict_pool[V] = 3 ≥ qV`.
- Phase-2a end: σ-eligibility-quorum on V. A, B, D have V locally → σ-emit. C does not have V locally → NR per the convergence rule (σ-eligibility met but no V to sign).
- Phase-2b actual: σ-pool = 3 (A, B, D); NR-pool = 1 (C). σ-quorum reached at L_0. Slot succeeds.

##### h_V = 2 (2 of 4 operators received V; 2 did not)

Suppose A, B have V; C, D do not.

- Phase-2a: A_σV, B_σV; C_NR, D_NR (or D arbitrary). `verdict_pool[V] = 2 < qV = 3`. `nr_pool = 2 < qEnc = 3` (if D NR; if D σV claim then 1).
- Phase-2a end: neither quorum eligible. Convergence rule: A and B (had V, σ-eligibility short) → both NR per rule. C, D → NR per default.
- Phase-2b actual: σ-pool = 0; NR-pool = 4 (or 3 if D σ-emitted-but-defected — but D would need to σ-claim and then EKM would block their NR; if D σ-claims at Phase-2a end, they σ-emit, σ-pool = 1, NR-pool = 3 = qEnc; either way NR-quorum reaches).
- NR-quorum at L_0 → fall-through to L_1. If L_1 leader honest and healthy, L_1 σ-quorum reaches in the same Phase 3 reconstruction walk (no extra RTT).
- **Cost vs bare OBFT**: bare OBFT with `h_V = 2` has σ-pool = 2 (honest σ) + 1 (leader Phase-1 σ_V) = 3 = qV → succeeds at L_0. Variant C falls through to L_1. One extra reconstruction-walk iteration in Phase 3 (no RTT).

This is the price of Variant C: the marginal `h_V = 2` case at f=1 n=4 falls through one layer instead of succeeding at L_0. Acceptable trade for the equivocation/h_V=1/validity-divergence recoveries below.

##### h_V = 1

Suppose only A has V; B, C don't; D byzantine (silent or arbitrary).

- Phase-2a: A_σV; B_NR, C_NR; D arbitrary. `verdict_pool[V] = 1 + maybe-byz ≤ 2 < qV`. `nr_pool = 2 + maybe-byz`.
  - If D verdicts NR: `nr_pool = 3 = qEnc` → NR-eligibility quorum reached. All operators (incl. A) commit NR. NR-pool actual = 3 or 4 → fall-through.
  - If D verdicts σV: `nr_pool = 2 < qEnc`, `verdict_pool[V] = 2 < qV` → neither eligibility met. A (had V, σ-eligibility short) → NR per rule. B, C, D → NR or whatever D wants. NR-pool actual = at least 3 (A, B, C) → fall-through.
  - If D silent (no verdict): same as D verdicts NR above with D missing — `nr_pool = 2 < qEnc`, fall back to per-operator default. A → NR (rule), B → NR, C → NR. NR-pool = 3 = qEnc. Fall-through.
- **All sub-cases recover via NR-quorum fall-through.** OBFT base would slot-miss at L_0 here ([docs/OBFT.md / Failure modes](OBFT.md#failure-modes)); Variant C structurally fixes it.

#### Equivocation σ-locked split

Byzantine D = leader equivocates at L_0. Patterns from OBFT's analysis ([docs/OBFT.md / Equivocation handling](OBFT.md#equivocation-handling)). **At 2-1 cases, Variant C regresses vs bare OBFT**; documented after the recovered cases.

##### 1-1-1 split

D delivers V_a to A, V_b to B, V_c to C (each a distinct V) near end of Phase-1, leaving inadequate re-flood time.

- Phase-1 retention: A retains V_a; B retains V_b; C retains V_c.
- Phase-2a: A_σV(V_a), B_σV(V_b), C_σV(V_c); D verdicts arbitrary.
- During Phase-2a window: gossipsub re-flood. Bundles for V_a, V_b, V_c propagate among honest. By Phase-2a end:
  - **If re-flood completes within Phase-2a (`Δ_2a ≥ D + δ` from byz's late Phase-1 delivery)**: A retains V_a + V_b + V_c → equivocation observed → A's verdict was already broadcast as σV(V_a), but A's commit at Phase-2a end is `NR-due-to-equivocation` (the convergence rule's equivocation-observed branch overrides earlier verdict). **However A already broadcast σV(V_a) verdict** — so verdict-vs-action mismatch occurs. Honest A's verdict-vs-action mismatch is permitted under the convergence rule (the rule explicitly allows commit ≠ verdict when equivocation is observed); it is not slashable for honest. (Slashable detection should distinguish honest verdict-vs-action revision from byzantine verdict-vs-action equivocation. See "Edge cases / Honest verdict-vs-action revision".)
  - At Phase-2a end with all honest in `NR-due-to-equivocation`: NR-pool actual = 3 (A, B, C) ≥ qEnc → fall-through to L_1.
- **If re-flood does NOT complete within Phase-2a** (byz times deliveries to push re-flood past T_accept_max for *each* honest): A only retains V_a; B only V_b; C only V_c.
  - Phase-2a: A_σV(V_a), B_σV(V_b), C_σV(V_c). `verdict_pool[V_a] = 1; verdict_pool[V_b] = 1; verdict_pool[V_c] = 1` (plus byz's verdict, ≤ 1 distinct).
  - At Phase-2a end: no V has σ-eligibility quorum (max 2 with byz vote). NR-pool = 0 (none verdict-claimed NR). Per the convergence rule, A had V_a + verdict σV but no σ-quorum-eligibility → A goes NR. Same for B, C.
  - NR-pool actual = 3 (A, B, C). Fall-through. ✓

**Either sub-case recovers.** OBFT base 1-1-1 split slot-misses ([docs/OBFT.md / Failure modes](OBFT.md#failure-modes)); Variant C structurally fixes it via the σ-eligibility-quorum-short rule.

##### 1-1-NR-C / 1-NR-NR

These are sub-cases of byzantine selective-delivery patterns (an honest in `NR` here is either silent-leader-NR or equivocation-NR per bare OBFT's commit rules). The convergence rule resolves them the same way: any honest split that doesn't reach `σ_eligibility_quorum = qV` results in all honest going NR → NR-quorum reaches → fall-through.

##### 2-1 split — REGRESSION vs bare OBFT

D delivers V to {A, B}, V' to {C}. (One of OBFT's "naturally recovered" cases — see [docs/OBFT.md / Equivocation handling](OBFT.md#equivocation-handling).)

- Bare OBFT: D's Phase-1 σ_V(V) is on the wire. σ-pool on V = A + B + D = 3 = qV. Slot succeeds at L_0 *regardless of D's Phase-2 cooperation* — the Phase-1 σ partial is cryptographically locked-in.
- Variant C: D has no Phase-1 σ_V. D issues a Phase-2a verdict (σV(V) | σV(V') | NR | silent) and a Phase-2b action. Outcomes by D's behavior:
  - **D cooperates (verdict σV(V) + Phase-2b σ on V)**: `verdict_pool[V] = 3 ≥ qV`. A, B, D σ-emit. C does not have V_local → C NR per rule. σ-pool = 3 = qV. Slot succeeds at L_0. ✓ Same as OBFT.
  - **D silent (no verdict, no Phase-2b emission)**: `verdict_pool[V] = 2 < qV`. Per rule, A and B (had V, σ-eligibility short) → NR. C → NR (had V' only, σ-eligibility on V' short). NR-pool = 3 = qEnc. Fall-through to L_1. **One layer of latency added vs OBFT.**
  - **D defects (verdict σV(V) + Phase-2b NR-emission)**: `verdict_pool[V] = 3 ≥ qV` (counted from D's verdict). A, B σ-emit; D defects to NR. σ-pool actual = 2 (A + B); NR-pool = 1 (C) + 1 (D) = 2 < qEnc. **Slot misses at L_0.** Cluster does not fall through (NR-pool short). D's verdict-vs-action mismatch is slashable evidence (Rule 6) but does not save the slot. **Strictly worse than OBFT base, which would have succeeded.**

The 2-1-byz-defect regression is real. OBFT's cryptographic lock on Phase-1 σ_V is what makes 2-1 patterns naturally recover; removing σ_V trades that recovery for the structural fixes elsewhere (1-1-1 split, h_V=1, validity-divergence, late-deepest-layer). Across many slots, Rule-6 deterrent absorbs the byzantine-defect-grief cost; per-slot, this case slot-misses cleanly.

A symmetric pattern — D delivers V to {A}, V' to {B, C} — has the same shape with V and V' swapped. Same outcomes by D's behavior.

##### Recovery summary

| Equivocation pattern | Bare OBFT | Variant C |
|---|---|---|
| 1-1-1 split (no re-flood) | Slot misses | Recovered (NR-quorum fall-through) |
| 1-1-1 split (re-flood completes in Phase-2a) | Slot misses | Recovered (equivocation observed → all NR) |
| 2-1, byz cooperates | Succeeds at L_0 | Succeeds at L_0 (tie) |
| 2-1, byz silent | Succeeds at L_0 (Phase-1 σ_V lock) | Falls through to L_1 (one extra layer's latency, still succeeds) |
| 2-1, byz defects (verdict σV + action NR) | Succeeds at L_0 (Phase-1 σ_V lock) | **Slot misses** (NR-pool short of qEnc) |
| All-equivocation-NR (early byz delivery) | Recovered via L_1 fall-through | Recovered via NR-quorum fall-through |

**Net liveness change vs bare OBFT**: gains 1-1-1 recovery (worst-case in OBFT); loses 2-1 byz-defect (new regression). Across realistic byzantine behavior (per the rational-byzantine-deterrent assumption — a rational byz faces the same fee outcome as going offline and gains nothing by defecting with on-wire evidence), Variant C is net positive in expectation. For deployments with short-horizon byzantines that don't value cluster fee accrual, the regression is a real cost.

#### h_V = 1 selective-delivery (Class B in OBFT)

Already covered above under "Marginal-receive cases / h_V = 1". Recovers.

#### Validity-divergence (Class A in OBFT)

A re-org during Phase-1 acceptance window splits honest verdicts: some operators say V valid (parent_root matches their pre-reorg head), some say invalid (parent_root mismatches their post-reorg head).

##### All-honest 2-σ vs 2-NV at f=1 n=4

- 2 operators (incl. leader) verdict σV; 2 operators verdict NV.
- `verdict_pool[V] = 2 < qV = 3`. `nr_pool = 2 < qEnc = 3`. Neither eligibility met.
- Per convergence rule: σV-side honest (had V, σ-eligibility short) → NR. NV-side honest → NR (their verdict). 
- NR-pool actual = 4 (all 4 honest NR) ≥ qEnc → fall-through to L_1. ✓
- Bare OBFT here: slot misses (leader's Phase-1 σ_V locks them to σ; non-leader σ-eligible can't switch; NR-pool capped at 2 < qEnc). Variant C recovers.

##### Higher n/f (e.g., 4-3 split at f=2 n=7)

- 4 σV verdicts, 3 NV verdicts. `verdict_pool[V] = 4 < qV = 5`. `nr_pool = 3 < qEnc = 5`.
- All σV-side honest → NR per rule. NV-side honest → NR.
- NR-pool actual = 7 ≥ qEnc → fall-through. ✓
- Hybrid (Variant B) here would have leader Phase-1 σ_V locked: 1 leader σ-locked + 3 σV-side rule-flipped to NR + 3 NV-side NR = 6 NR partials. NR-pool = 6 ≥ qEnc = 5 → fall-through? Wait, let me recount: the leader is σ-locked in Variant B, so they emit σ partial (1 σ) and not NR; 6 non-leader honest can NR. NR-pool = 6 ≥ qEnc = 5. Variant B *also* fall-throughs at f=2 n=7 cleanly. Hmm.
  - Actually I was wrong earlier. At f=2 n=7 4-3 split with Variant B: σ-pool = 1 leader + 3 σV-side honest = 4 partials. NR-pool from non-leader honest = 6 ≥ qEnc = 5. Fall-through happens.
  - The point where Variant C helps over Variant B is when the leader IS the validity-flipper. If leader's verdict flips to NV (they joined the NV side), Variant C lets the leader NR; Variant B keeps leader σ-locked. At higher splits this matters more, but at the recommended SSV configurations (n ≤ 13) the difference is small and may be invisible at f=1 n=4.
  - **Conclusion**: Variant C is uniformly safer for validity-divergence; Variant B is fine at f=1 n=4 and only differs at higher n/f or when the leader is on the divergent side.

#### Late-deepest-layer leader broadcast (Class A in OBFT)

Bare OBFT's failure mode ([docs/OBFT.md / Failure modes](OBFT.md#failure-modes)): deepest-layer leader broadcasts past T_accept_max → all honest treat as silent → NR-quorum at L_{K-1} → walk advances past L_{K-1}, no L_K → slot misses.

In Variant C, late-arriving Phase-1 bundles are auth-only-retained until Phase-2a ends. If the bundle re-floods to all honest before `T_commit + Δ_2a − (D + δ)`, honest can verdict-claim σV on V; verdict-pool reaches qV; Phase-2b σ-emit on the late V. **Slot succeeds where bare OBFT fails.**

Conditions for recovery:
- Bundle propagates to all honest before `T_commit + Δ_2a − (D + δ)`. At Config A recommended Δ_2a = 300ms, this is 150ms past T_commit.
- Operationally: the leader's late broadcast is observed by at least one honest peer who re-floods immediately. The re-flood completes within `D + δ` of the late observation.

#### Mesh-flakiness coordinated with byz σ-refusal (Class B in OBFT)

Bare OBFT's failure mode ([docs/OBFT.md / Failure modes](OBFT.md#failure-modes)): mesh-flaky honest A NR-emits early → A is NR-locked → byz refuses σ → σ-pool short → deadlock.

Variant C: A's verdict at Phase-2a is based on whether A has V locally. If A's mesh recovers during Phase-2a (delivering V via re-flood), A can verdict σV. Or, if A's mesh stays poor and A verdict-claims NR, but other honest converge on σ (verdict_pool[V] ≥ qV), A still defaults to NR per the rule (V_local missing). σ-pool from healthy honest may still reach qV without A. **At f=1 n=4 with leader honest + 2 healthy honest**: σ-pool = 3 = qV. Slot succeeds without A.

This is wider mesh-flakiness mitigation than bare OBFT. The Phase-2a observation window is a structural buffer for transient flakiness.

#### Sustained partition (Class A — unchanged)

Real propagation > absorption window: bundles don't reach honest in time → all honest NR-pool short → slot misses cleanly. Same as OBFT.

Variant C's absorption window is `Δ_2a + (D + δ)` (the Phase-2a-end horizon) — same shape as OBFT's `Δ_2 + (D + δ)`. At recommended sizing both are ~450ms at Config A.

#### > f operators offline/byzantine (Class A — unchanged)

Standard 3f+1 violation → slot misses. Same as OBFT.
### Edge cases — where things can go wrong

#### Verdict broadcast timing

**E1: Operator broadcasts verdict too early in Phase-2a.** They commit before observing late-arriving bundles. If a late bundle would have changed their verdict, they're locked on the early verdict (verdict envelope is op-identity-signed; broadcasting a second different verdict is verdict-equivocation, slashable).

- **Mitigation**: operators broadcast verdict as late as possible within Phase-2a, no earlier than `T_commit + Δ_2a − (D + δ)`. This gives maximum time for bundle re-flood while still allowing the verdict to propagate before Phase-2a end.
- **Failure mode**: an honest operator with a buggy timer broadcasts verdict at `T_commit + 50ms` (way too early). They may verdict NR before a late bundle arrives. Their NR verdict counts in `nr_pool`. If the cluster reaches NR-quorum, fall-through happens (still recovers). If not, the operator may have to emit NR at Phase-2b (since their verdict was NR) even though V arrived later — but then they'd have V_local but NR-verdict; the convergence rule says: if `nr_eligibility_quorum` is met, NR (regardless of V_local); if not, follow own verdict. Per rule, they NR-emit. Slot may still recover via other operators' σ-emits if `verdict_pool[V] ≥ qV` from those who waited.
- **Recommendation**: implementation should default to "verdict at `T_commit + Δ_2a − (D + δ)` minus a small operator-side processing buffer", not earlier.

#### Verdict equivocation by operator

**E2: Byzantine `i` broadcasts σV(V) to peers A and σV(V') to peers B and NR to peers C.** Each peer first-observes a different verdict.

- **Honest detection**: any peer who observes ≥ 2 distinct verdicts from `i` at the same (slot, layer) treats `i` as a verdict-equivocator. Both envelopes are slashable evidence.
- **Convergence input**: each peer counts only `i`'s **first-observed** verdict; subsequent verdicts are dropped from convergence pools.
- **Cluster-wide effect**: at f=1 with byz `i`, byz contributes ≤ 1 verdict per pool per peer. Different peers may count `i` toward different pools (A counts `i` as σV(V); B counts `i` as σV(V'); C counts `i` as NR). Per-peer convergence may differ. **This is a real surface area**: peers may converge differently because their first-observed verdicts differ.

  Concrete: at f=1 n=4 with leader honest + 3 honest σV(V) verdicts, byz issues 4 different verdicts to 4 different peers:
    - A's first-observed from byz = σV(V'). A counts `verdict_pool[V] = 3 (honest) + 0 (byz, on V'); verdict_pool[V'] = 1 (byz)`. A converges on σ on V (3 ≥ qV).
    - B's first-observed = NR. `verdict_pool[V] = 3; nr_pool = 1`. B converges on σ on V.
    - Etc.
  - Across all peers, σ-eligibility on V is consistently observed → all 4 honest σ-emit on V → σ-pool = 4 ≥ qV. ✓
- **Edge of convergence divergence**: at higher f or pathological verdict equivocation patterns, peers may converge differently. For SSV proposer at f=1 n=4, the verdict-equivocation-induced divergence is bounded; need careful analysis at higher f.

#### Honest verdict-vs-action revision

**E3: Honest A verdicts σV(V) at Phase-2a; mid-Phase-2a A retains a second V' (equivocation observed via late re-flood); A's Phase-2b commit is NR-due-to-equivocation per the convergence rule.**

- A's Phase-2a verdict envelope says σV(V); A's Phase-2b NR partial on `nr_tag_k`. Rule 6 (verdict-vs-action equivocation) would flag this, but A is honest.
- **Fix**: Rule 6 is conditional on the cluster's verdict pool. Honest revision when equivocation is observed is permitted. Receivers who observe A's mismatch should check whether A had grounds to revise:
  - Did the cluster also observe equivocation at this layer? (E.g., is there a `verdict_pool[V']` with at least one entry?) If so, A's revision is plausibly honest.
  - Was σ-eligibility-quorum reached cluster-wide? If yes and A still NR'd, that's a stronger byzantine signal.
- **Implementation**: receivers who detect a Rule-6 mismatch should retain the evidence (verdict + action) but not propagate as slashable until they have enough cluster-wide context. This is a fundamental limitation of Rule 6 (gossipsub-pattern-quality) — it's a deterrent, not a clean self-contained rule.

#### Byzantine verdict-vs-action equivocation

**E4: Byzantine D verdicts NR in Phase-2a, then Phase-2b σ-emits on V (or vice versa).**

- The cluster may have converged on σ-eligibility-quorum on V (verdict_pool[V] = qV from honest). D's Phase-2b σ on V doesn't violate that — D's σ contributes to σ-pool, slot succeeds.
- The slashable mismatch is between D's Phase-2a verdict (NR) and Phase-2b action (σ). Rule 6 evidence is the verdict envelope + the σ partial (or its auth envelope).
- **Effect on slot**: minor — D's σ partial may put σ-pool above qV. Slot likely succeeds. Slashable.

#### Late-arriving bundle vs op-identity verdict equivocation

**E5: Honest A first-observes V_a at T_commit + 100ms, broadcasts verdict σV(V_a) at T_commit + 150ms. Then at T_commit + 250ms, a re-flooded V_b (byzantine equivocation) arrives at A. A now retains V_a + V_b.**

- A's Phase-2a verdict was σV(V_a); A's commit at Phase-2a end is `NR-due-to-equivocation` (equivocation observed override).
- A cannot broadcast a second verdict to revise — that would be A's verdict equivocation.
- A emits NR at Phase-2b. Rule 6 evidence (verdict σV(V_a) + action NR) — receivers must check cluster context; equivocation observed cluster-wide is the honest revision condition.
- **Implementation note**: this case requires receivers to retain the operator's first verdict + full Phase-1 retention state. To enable receivers to verify a Rule 6 mismatch is honest, the gossiped equivocation evidence (the V_a, V_b bundle pair) must be globally observable. Receivers without the equivocation evidence may falsely flag A as Rule-6 byzantine.
- **Mitigation**: Rule 6 attribution is best-effort and should not trigger automated slashing or automated blacklist without cluster-wide consensus on the evidence. This is consistent with OBFT's slashing model (manual blacklist by surviving operators; planned protocol extension).

#### Encrypted Phase-2b σ at deeper layers

**E6: At layer k > 0, Phase-2b σ partials are chained-IBE-encrypted under nr_tag_0..nr_tag_{k-1}. They cannot be verified by receivers until prior NR-quorums unlock decryption.**

- Same as OBFT's Phase-2 onion at deeper layers. No new attack surface, but the implementation must wrap Phase-2b σ partials in chained IBE the same way OBFT's Phase-2 onion does.
- The Phase-2b σ-emission code path constructs the chained-IBE onion the same way as the existing OBFT-family onion-build helper.

#### Witness-threshold equivalent for Phase-2b σ emission

**E7: At Phase-2b time, byz emits a σ partial on V at L_0 without ever broadcasting Phase-1 V to the cluster. Honest receivers see byz's σ partial on the wire.**

- In bare OBFT, this is the "fake plaintext σ at L_0" attack (Rule 5).
- In Variant C, the σ-eligibility quorum check at Phase-2a end is the witness mechanism. Byz cannot inflate `verdict_pool[V]` alone (byz contributes ≤ f verdicts; qV − f = f+1 honest must agree to reach qV). Byz cannot get honest to σ on V without honest having V locally and the cluster reaching σ-eligibility quorum.
- Byz's Phase-2b σ on V is wasted (counts in σ-pool but cluster σ-quorum requires qV ≥ 2f+1; byz alone is < qV). At most byz's σ inflates the σ-pool by 1.
- **Result**: byz's lone σ-emission has no liveness effect at f=1 n=4 (cluster either reaches qV with honest cooperation or doesn't reach at all). Same outcome as OBFT. No new grief surface.

#### Phase-2a verdict envelope rate-limit

**E8: Byzantine spams 100 distinct verdicts per (slot, layer) — different `value_root` each time.**

- Each verdict envelope is op-identity-signed. Honest receivers count *first-observed* per `(slot, layer, operator_id)`; subsequent verdicts are dropped from convergence input but recorded for slashing.
- Per-receiver memory: bounded by gossipsub message-id deduplication and the per-(slot, layer) retention cap.
- **Rate-limit (anti-amplification rule)**: same as OBFT's Rule 5 rate-limit ([docs/OBFT.md / Slashing evidence](OBFT.md#slashing-evidence)). Each honest receiver gossips slashable verdict-equivocation evidence at most once per `(slot, layer, operator_id)` tuple. Caps amplification.

#### Phase-2a verdict propagation at high latency

**E9: Verdict from operator i broadcast at Phase-2a start propagates slowly to operator j due to gossipsub mesh anomaly. At Phase-2a end, j has not received i's verdict.**

- j's local convergence input is missing i's verdict. j may compute σ-eligibility-quorum incorrectly (e.g., j sees `verdict_pool[V] = qV − 1` and goes NR, while a cluster-wide-aware operator would see `qV` and go σ).
- **Effect at f=1 n=4**: rare; gossipsub propagates verdicts in `D + δ` ≤ Δ_2a − (D + δ). When mesh is anomalously bad, j's NR-emission joins NR-pool. If the rest of the cluster σ-quorums on V (qV partials reach), slot succeeds at L_0 without j. If not, fall-through to L_1.
- **Mitigation**: same as OBFT's mesh-flakiness mitigation — `Δ_2a ≥ 2(D + δ)` recommended; mesh-diversity at deployment level.

#### EKM atomicity at Phase-2b

**E10: Operator i decides at Phase-2a end to σ on V at layer k. The Phase-2b sign request goes to EKM. Concurrent Phase-2b sign request for layer k' (different layer) from same operator. EKM must serialize or transactionally process both.**

- Cross-keypair atomicity: the Phase-2b onion contains per-layer commits, each requiring a separate EKM sign-and-log per (slot, layer). The implementation's commit batch should serialize EKM ops or use a single transaction spanning all layers.
- **Failure mode**: if EKM logs (slot, layer_0, "σ", V) but crashes before logging (slot, layer_1, "NR", null), restart could result in EKM allowing layer_1 σ on a different V (since no log row exists for layer_1). Not a safety violation per-layer (Pigeonhole 1 still holds at layer_0), but operator's local state is inconsistent.
- **Implementation**: EKM operations within a single Phase-2b emission must be atomic — either all per-layer log rows are written or none. Standard transactional database semantics apply. OBFT's EKM coordination model ([docs/OBFT.md / EKM coordination model](OBFT.md#ekm-coordination-model)) calls this out; it's unchanged here.

#### Phase-2b σ at deeper layer arrives before its decryption is unlockable

**E11: Honest operator emits chained-IBE Phase-2b σ at layer k > 0. Receivers observe the encrypted partial but cannot decrypt until prior NR-quorums aggregate.**

- Receivers must retain the encrypted partial until decryption is possible. Retention bounds: `O(K · n)` partials per slot — bounded.
- If NR-quorums at prior layers never aggregate (e.g., σ-quorum reached at L_0 short-circuiting the walk), the encrypted partial stays sealed. Receivers eventually drop it when the slot's retention window ends.
- Same as OBFT's Phase-2 onion behavior at deeper layers. No new edge case.

#### Byzantine leader emits no Phase-1 bundle but emits Phase-2a verdict σV

**E12: Byzantine leader (= L_0) is silent in Phase-1 but in Phase-2a verdict-claims σV(V) for some V they signed via auth envelope but never broadcast.**

- No honest operator has retained V (byz never broadcast Phase-1 bundle). `verdict_pool[V] = 1 (byz)` from byz's verdict alone.
- Honest operators either have V_local (no — byz didn't broadcast) or don't (yes). Honest verdict NR.
- `verdict_pool[V] = 1 < qV`; `nr_pool = 3 (honest) + 0 (byz) = 3 ≥ qEnc`. NR-quorum reached at L_0 → fall-through. ✓
- Byz's lone σV verdict is wasted; cluster proceeds to L_1.
- **No grief surface**.

#### Multi-leader equivocation across layers

**E13: Byzantine controls multiple layer-leaders (e.g., byz holds L_2 and L_3 at K=4). Byz equivocates at both L_2 and L_3.**

- Each layer's equivocation is independent. The cluster falls through L_0, L_1 normally. At L_2, byz equivocates → all honest go NR (per σ-eligibility-quorum-short rule on the split V's) → fall-through to L_3. At L_3, byz equivocates again → all honest go NR → no L_4 → slot misses. Class A.
- **Rare**: byz controls f operators; pigeonhole at K = n means byz holds f / n layers (e.g., 1/4 at f=1 n=4). At K=4 f=1, byz holds exactly 1 layer; at K=4 f=2 (n=7), byz could hold 2 layers but K=4 < n=7 means not all members are leaders.
- **Mitigation**: K ≥ n − f (i.e., enough layers that byz can't hold all of them at the deepest end). At K=4 f=1 n=4, K = n − f + 1 (one more than minimum), pigeonhole guarantees at least one honest leader. ✓

#### Operator restart mid-slot

**E14: Operator crashes after Phase-2a verdict broadcast but before Phase-2b. After restart, operator's EKM log shows no σ/NR row at (slot, layer). Operator must decide Phase-2b commit fresh.**

- The operator's Phase-2a verdict envelope is already on the wire — counted in cluster's verdict pool.
- On restart, operator re-evaluates convergence rule based on currently observed Phase-2a verdicts (including their own first-observed verdict, if they retain it locally). If their verdict was σV(V) and cluster reached σ-eligibility-quorum, they σ-emit at Phase-2b.
- If operator does NOT retain their pre-crash verdict locally and re-evaluates from scratch (no V_local because retention was lost), they may default to NR. This is verdict-vs-action mismatch (their on-wire σV verdict ≠ NR action). Honest exception applies (operator state recovery is permissible; the σ-pool may still reach qV from other operators).
- **Implementation guidance**: persist Phase-2a verdict and retained V state across restarts. EKM log alone is insufficient (it doesn't include verdicts). A separate per-slot state log (verdicts, retentions) should survive restarts.
- **Failure mode**: if operator state is fully ephemeral (in-memory only) and the operator restarts mid-slot, they may verdict-vs-action mismatch and trigger Rule-6 false-positives. Mitigation: persist Phase-2a state.

#### Adversarial verdict timing — split convergence

**E15: Byzantine times their verdict broadcast such that some honest first-observe byz_σV(V) and some honest first-observe byz_σV(V'). Honest converge differently.**

- Per-peer convergence diverges. Some honest σ-emit on V; some on V'. Pigeonhole 2 ensures only one V can reach qV cluster-wide: the V with more honest converging. Slot succeeds on whichever V has σ-pool ≥ qV.
- Worst case: 50/50 split. At f=1 n=4 with 1 byz + 3 honest, byz issues 2 distinct σV verdicts (one to A, one to B; C either). At first-observed counting: A counts byz on V; B counts byz on V'; C counts byz on whichever first arrives. The 3 honest verdict σV on the V their host validates (presumably the V they retained from Phase-1 — bundle propagation should be consistent across honest by Phase-2a end if `Δ_2a ≥ D + δ`).
  - If all 3 honest have the *same* V_local (say V): all 3 verdict σV(V). `verdict_pool[V] = 3 + maybe-byz = 3 or 4 ≥ qV`. All 3 honest σ-emit on V. σ-pool = 3 ≥ qV. ✓ Slot succeeds.
  - If 2 honest have V and 1 has V' (e.g., partial equivocation propagation), divergence. Not an "adversarial verdict timing" issue per se but bundle propagation issue.
- The verdict-equivocation-by-byz-alone is bounded by f: byz contributes ≤ 1 distinct verdict per pool per peer (first-observed), so byz's adversarial contribution to per-peer convergence is at most 1 σ-pool entry inflation.

#### Receiver-side validity stabilization

**E16: SSV proposer parent_root validity changes between Phase-1 acceptance and Phase-2a verdict time (re-org during the receiver acceptance window).**

- OBFT's host workflow ([docs/OBFT.md / Head-change handling](OBFT.md#head-change-handling)): validate against stable head snapshot at Phase-1 acceptance, lock the verdict.
- In Variant C, the verdict is broadcast at Phase-2a (not at Phase-1). The host must decide the verdict at Phase-2a verdict-broadcast time.
- **Two options**:
  1. **Host locks verdict at Phase-1 acceptance** (OBFT style). Phase-2a verdict echoes the locked verdict. Validity-divergence behavior same as OBFT: re-org during Phase-1 acceptance window splits operators; `verdict_pool[V]` short of qV → NR fall-through. ✓
  2. **Host re-evaluates at Phase-2a verdict-broadcast time** (Variant C "stabilization"). Each operator validates against their current head at verdict time. If the re-org has propagated to all honest by Phase-2a verdict time, all operators evaluate against the same head → unanimous verdict. ✓
- **Recommended**: option 2 — re-evaluate at Phase-2a verdict time. Phase-2a's window IS the stabilization window. This is what Variant C optimizes for.
- **Implementation**: the host's `validate(V_{L_k})` callback is called at Phase-2a verdict-broadcast time, with the operator's current head. If host wants to be conservative, they may keep a hybrid (lock at Phase-1 acceptance but re-evaluate at Phase-2a if head moved significantly).

#### Phase-2b late σ when bundle re-floods very close to Phase-2a end

**E17: Bundle re-floods to operator A at T_commit + Δ_2a − ε for tiny ε. A barely has time to verdict σV(V) and broadcast.**

- A's verdict broadcast time: `T_commit + Δ_2a − ε`. Propagation to peers: peer first-observes by `T_commit + Δ_2a − ε + (D + δ)`.
- Peer's Phase-2a end: `T_commit + Δ_2a`. Peer first-observes A's verdict at `T_commit + Δ_2a + (D + δ) − ε` — past Phase-2a end.
- A's verdict missed Phase-2a's effective deadline; peer doesn't include A in `verdict_pool[V]` for convergence.
- A's vote is wasted; cluster computes convergence without A. If `verdict_pool[V]` still reaches qV without A, slot succeeds. If not (A was the marginal vote), σ-eligibility short → all NR → fall-through.
- **Mitigation**: don't broadcast verdict past `T_commit + Δ_2a − (D + δ)`. This is the effective Phase-2a verdict-broadcast cutoff.

#### Asymmetric verdict observation at Phase-2a end

**E18: Operator i observes `verdict_pool[V] = qV − 1` (one short). Operator j observes `verdict_pool[V] = qV` (one more).**

- i computes σ-eligibility short → NR.
- j computes σ-eligibility met → σ on V (if j has V_local).
- Cluster split at convergence: i NR-emits, j σ-emits. σ-pool actual depends on how many honest converge each way.
- This is a realization of E9 (high-latency verdict propagation). At realistic Δ_2a sizing, this should be rare. If it happens at f=1 n=4 with leader as the marginal voter (j), σ-pool = 1 (j) + maybe-byz, NR-pool = 2 (i + others) + maybe-byz. Outcomes:
  - σ-pool < qV; NR-pool reaches qEnc → fall-through. ✓
- The asymmetric-observation case generally reduces to NR-quorum fall-through. Slot recovers via L_1.

### Open questions / decisions to make at implementation time

1. **Δ_2a vs Δ_2b sizing**: minimum vs recommended? At Config A minimum (Δ_2a = Δ_2b = D+δ = 150ms), submission headroom = 1.95s; at recommended (2(D+δ) = 300ms), headroom = 1.65s. Recommend recommended for mesh-flakiness mitigation; revisit if production telemetry shows submission tail > 1.5s P99.

2. **Verdict equivocation rate-limit**: should honest receivers gossip slashable verdict-equivocation evidence on first detection or wait for cluster confirmation? OBFT Rule 5 uses first-observed gossip with a per-(slot, layer, operator_id) cap; same rule fits here.

3. **Verdict envelope size**: 32-byte `value_root` plus envelope overhead ≈ 100-200 bytes per verdict per layer. Per slot: K verdicts × n operators × 200 bytes ≈ 3.2 KB at K=4 n=4. Within budget.

4. **Persistent Phase-2a state across restarts**: to avoid Rule-6 false-positives on operator restart, persist verdict + retained V at the EKM-log level. Implementation choice: extend the EKM log schema to include verdict envelopes alongside σ/NR rows? Or use a separate per-slot state file (recommended — keeps EKM minimal).

5. **Convergence-rule tie-break at n > 3f+1**: when multiple V's could reach `qV` (only possible at non-tight BFT-bound clusters like n=5 f=1), use lexicographic `value_root` tie-break. At n=3f+1 exactly (the SSV cluster sizes), tie-break is moot. Document for completeness.

6. **Late-bundle Phase-2a verdict path**: should an operator who first-observes V via re-flood at, say, T_commit + Δ_2a/2 still broadcast σV verdict? Yes, if propagation slack permits (broadcast ≤ T_commit + Δ_2a − (D + δ)). Implementation: per-operator timer that fires at the latest-safe verdict-broadcast time.

7. **Rule 6 evidence handling**: how do receivers determine whether a verdict-vs-action mismatch is honest revision (allowed) vs byzantine equivocation (slashable)? Implementation rule: receiver collects mismatch evidence; honest receivers cross-reference with their cluster verdict view; weakly slashable ("behavioral pattern" quality, like OBFT's selective-delivery). Surfacing this evidence requires the manual-blacklist coordination from OBFT's rational-byzantine-deterrent model (planned protocol extension) — not automated.

8. **Should leader broadcast a verdict for their own V?** Yes — leader is an operator like any other in Phase-2a. Their verdict on their own V is σV (typically; could flip to NV on host re-evaluation if state shifted, e.g., re-org). Leader's verdict counts in `verdict_pool[V]`.

9. **What if leader's verdict on their own V is NV?** This is the validity-divergence-with-leader-on-NV-side case. Handled by the convergence rule: leader's verdict NR/NV puts them in `nr_pool`. NR-pool may reach qEnc → fall-through.

10. **Phase-2b emission timing — start-of-window or based on convergence completion?** Operators compute convergence at Phase-2a end (T_commit + Δ_2a) and emit Phase-2b immediately. No need to delay further within Phase-2b; the window is for propagation.

11. **K = 4 vs K = 3 trade-off**: same as OBFT base. K=4 (= n) has maximum fall-through depth at +3 KB onion bandwidth; K=3 (= f+2) saves bandwidth but is less robust to multi-layer adversarial scenarios. Recommend K = n = 4 for SSV proposer.

12. **Hash variant?** Variant C does not need V-plaintext in the Phase-2 onion since Phase-2a's bundle re-flood is the V-recovery mechanism. There is no late-σ-emit-on-V-recovered-from-peer-onion in Variant C. So the hash-vs-full-V distinction does not apply; Phase-2b onions carry σ partials only (encrypted at deeper layers), not V plaintext.

13. **Migration / co-existence with QBFT**: rollout via per-cluster opt-in (DKG event) or feature flag. Wire-protocol versioning via `protocol_tag` (`OBFT-2ab-v1`) prevents cross-protocol message mixing. Operationally: ship behind feature flag, enable per cluster after DKG.

14. **DKG cost**: same as OBFT — one V-keypair DKG (already in SSV) + one IBE-keypair DKG (new, run once at cluster init). Per-cluster setup, not per-slot.

15. **Package layout**: 2abOBFT can be implemented as its own `protocol/v2/obft/` package (or extension of an existing OBFT-family package once one lands). Either approach works; preserving test infrastructure and IBE plumbing reuse argues for extension if a parallel OBFT package already exists.

16. **Verdict EKM-binding (open trade-off)**: should Phase-2a verdicts be logged in the EKM at issue time, with Phase-2b sign requests required to match? Closes the 2-1-byz-defect regression but adds complexity (verdicts become EKM-tracked events; honest revision upon equivocation needs a "verdict-void" EKM operation gated on equivocation evidence). Default recommendation: accept the regression in v1; revisit if production telemetry shows defection-grief at meaningful rates. If adopted, the EKM coordinator gains: `(slot, layer, verdict_side, value_root)` log row at Phase-2a issue + `LogPhase2bSign` checks against the verdict row + `VoidVerdict(equivocation_evidence)` for honest revision.

17. **Verdict-issue timing minimum**: should there be a *minimum* verdict-broadcast time (e.g., `T_commit + Δ_2a/2`) to prevent premature commits? At the boundary case where an honest operator broadcasts verdict immediately on Phase-1-acceptance success and a byzantine equivocation arrives mid-Phase-2a, the honest operator's verdict is on the wire as σV but their commit revises to NR (honest exception under Rule 6). A minimum-broadcast-time would force operators to wait long enough to observe most re-flooded equivocation evidence first. Trade-off: forces all operators to broadcast verdicts in a narrow window near Phase-2a end, potentially adding propagation pressure. Default recommendation: no minimum (broadcast at latest-safe time, which is the natural choice anyway).

### Implementation plan — high-level breakdown

The implementation is broken into phases that can be staged across PRs:

#### Phase 1 — Wire format and EKM schema

- Add `KindVerdict` envelope to [protocol/v2/tbft/wire/](../protocol/v2/tbft/wire/).
- Update `Phase1Bundle` schema to remove `σ_V` partial; auth envelope retains `protocol_tag = "OBFT-2ab-v1"`.
- Extend EKM schema in [ssvsigner/ekm/](../ssvsigner/ekm/) to support `(slot, layer, side, value_root)` log rows for the V-share + IBE-share coordinator. Add per-Phase-2b sign-request handlers.
- Add domain-separation tests confirming `2abOBFT-v1` envelopes don't validate under bare OBFT or OBFTR envelope handlers.

#### Phase 2 — Instance state machine

- New `Phase2abInstance` in [protocol/v2/tbft/](../protocol/v2/tbft/) (extending or alongside existing `Instance`) with state machine: Phase-1-receive → Phase-2a-verdict-emit + receive → Phase-2b-commit → Phase-3-reconstruct.
- `ObserveCandidate(layer, V)` — same as existing.
- `ObserveVerdict(verdict)` — new; record per-(slot, layer, operator) first-observed verdict.
- `ObserveOnion(onion)` — restructured for Phase-2b commit shape.
- `ObserveNonReceipt(nr)` — same as existing, but emitted at Phase-2b end.
- `Resolve()` — same K-layer reconstruction walk.

#### Phase 3 — Convergence rule and Phase-2b emission

- Implement convergence rule per the table in "Convergence rule" section above.
- `BuildPhase2bOnion(operatorID)` — at Phase-2a end, compute commits per layer, sign σ partials (chained IBE at k>0) or NR partials, wrap in auth envelope.
- EKM integration: per-(slot, layer) sign-request before each per-layer partial.

#### Phase 4 — Adapter integration

- Wire the proposer-duty runner to drive the Phase 2a/2b state machine.
- Add Phase-2a verdict broadcast at `T_commit + Δ_2a − (D + δ)`.
- Add Phase-2b emission at `T_commit + Δ_2a`.

#### Phase 5 — Slashing-evidence rule 6

- Detect verdict-vs-action mismatches in Phase-3 reconstruction.
- Honest-revision exception: cross-reference cluster verdict view; slashable iff cluster's verdict pool would have honestly converged on the verdict side.
- Surface evidence via existing slashing-evidence gossip mechanism.

#### Phase 6 — Testing and rollout

- Adversarial-byzantine tests covering the Class B recovery cases (equivocation σ-locked split, h_V=1 selective-delivery, validity-divergence at n=4 f=1).
- Mesh-flakiness simulation tests.
- Integration tests with simulated Δ_2a propagation latency.
- Feature flag rollout: disabled by default, opt-in per cluster.

### Comparison summary

| Aspect | Bare OBFT | OBFT + Phase 2a/2b (this) |
|---|---|---|
| Healthy-path latency (all 4 ops cooperate, all receive V) | ~600ms | ~700ms (+100ms for Phase-2a/2b split) |
| Marginal h_V=2 + byz σ-cooperates | Succeeds at L_0 (σ-pool = 3) | Succeeds at L_0 (verdict-quorum reached, σ-pool = 3) |
| Marginal h_V=2 + byz silent / NR | Slot misses (σ-pool = 2 < qV; NR-pool = 1 or 2 < qEnc; no fall-through) | Falls through to L_1 (verdict-quorum short → all honest NR → NR-pool = 3-4 ≥ qEnc) ✓ |
| Validity-divergence (e.g., 2-σ vs 2-NV at f=1 n=4) | Class A (slot-miss; leader σ_V locked on stale V) | Recovered (NR-quorum fall-through; leader's verdict re-evaluated at Phase-2a) |
| Equivocation 1-1-1 split | Class B (slashable, slot-miss; honest σ-locked on different V's) | Recovered (verdict-quorum-eligibility-short → fall-through) |
| Equivocation 2-1 split, byz σ-cooperates | Succeeds at L_0 (Phase-1 σ_V locked + Phase-2 σ) | Succeeds at L_0 (tie) |
| Equivocation 2-1 split, byz silent | Succeeds at L_0 (Phase-1 σ_V on the wire alone is enough) | Falls through to L_1 (one extra layer's latency, still succeeds) |
| Equivocation 2-1 split, byz defects (verdict σV + action NR) | Succeeds at L_0 (Phase-1 σ_V cryptographically locked; can't defect) | **Slot misses (regression)** — Rule-6 evidence on the wire |
| h_V=1 selective-delivery deadlock | Class B (slashable, slot-miss) | Recovered (verdict-quorum-short → fall-through) |
| Late deepest-layer leader broadcast | Class A | Recovered (Phase-2a re-flood absorbs) |
| Mesh-flakiness coordinated with byz σ-refusal | Class B (slashable, slot-miss) | Mitigated (Phase-2a window absorbs jitter) |
| EKM complexity | Per-(slot, layer, side) coordinator with cross-keypair atomicity | Same shape; one fewer concern (no Phase-1 σ_V to coordinate with Phase-2 σ) |
| Wire format | Phase1Bundle, KindCommit, KindCertificate | + KindVerdict, KindOnion2b, KindNR2b (Phase-2b commit splits back into σ-side / NR-side because Phase-2a observation must complete before σ commitment) |
| Slashing-evidence rules | 5 | 6 (Rule 6: verdict-vs-action equivocation, weakly slashable) |
| Submission headroom (Config A) | 1.95s | 1.65s |
| Bandwidth (healthy, n=4, K=4) | ~27 KB | ~30 KB (+3 KB for verdicts) |

### What 2abOBFT does NOT close

- **Sustained partition** beyond `Δ_2a + (D + δ)` absorption window — still Class A. Multi-round (R ≥ 2) extension of Phase 2a/2b is a future direction.
- **More than f operators offline/byzantine** — Class A by trust-bound assumption.
- **Backup-leader cascade failure** at K < n − f — Class A. K = n recommended.
- **Honest software bugs producing byzantine-equivalent behavior** — same trust posture as OBFT / QBFT (honest-majority cryptographic, not 100% cryptographic).
- **2-1 equivocation byz-defect grief** — strictly worse than bare OBFT (regression). Byz that controls 1 vote out of 4, equivocates V/V', delivers V to 2 honest, V' to 1 honest, verdict-claims σV(V), then defects to NR at Phase-2b. Slot misses cleanly with Rule-6 evidence on the wire. Bare OBFT would have succeeded via Phase-1 σ_V lock. The rational-byzantine deterrent absorbs this across many slots — but per-slot, an adversarial byzantine ignoring the deterrent can grief more reliably than under bare OBFT.

  **Mitigation options at implementation time** (not in current spec; see "Open questions" #16):
  - Make verdict envelope EKM-binding: log verdict at Phase-2a issue time; reject Phase-2b sign request that doesn't match. Adds EKM complexity (verdicts as logged events) AND breaks honest revision when equivocation is observed mid-Phase-2a (operator cannot switch from σV-verdict to NR-action). To restore honest revision, EKM needs a "verdict-void" operation gated on auth-valid equivocation evidence.
  - Accept the regression: rely on rational-byzantine deterrent across slots. Recommended unless production telemetry shows byzantine 2-1 defection at meaningful rates.

### Where this came from

Variant C is the structural extrapolation of OBFT's "Phase 2a/2b" prose ([docs/OBFT.md / Where this came from](OBFT.md#where-this-came-from)) — taking seriously the "without a Phase-1 σ_L^V" hint there. The choice to drop Phase-1 σ_V is what lets 2abOBFT recover validity-divergence at all `n`/`f`.

The verdict-broadcast mechanism is the load-bearing addition: it makes cluster-wide convergence on σ-eligibility observable before any operator commits a partial, which is the structural fix for OBFT's Class A validity-divergence and Class B byzantine-grief patterns. Without verdict broadcasts, a Phase-2a window only gives more time for Phase-1 bundle propagation — equivalent to a wider `Δ_2` in OBFT base.

The Phase 2 split costs +1 RTT of slot budget. At Config A this is +100-300ms depending on sizing; at the recommended `Δ_2a = Δ_2b = 2(D + δ)`, it is +300ms. Submission headroom drops from 1.95s to 1.65s — comfortable margin.

The trade-off vs bare OBFT: a healthy-h_V=2 case falls through to L_1 (rather than succeeding at L_0 via the Phase-1 σ_V head-start). At K = n = 4, fall-through is one local-decryption iteration in Phase 3 — no extra RTT, slot still succeeds.

This is the rationale 2abOBFT was designed to address for SSV proposer duty under realistic adversarial conditions, per OBFT.md's own assessment ([docs/OBFT.md / Where this came from](OBFT.md#where-this-came-from)): "OBFT + Phase 2a/2b should be considered near-term, not future".
