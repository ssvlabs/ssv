# 2abOBFT — Two-Phase Witness BFT for Distributed Validators

A single-round agreement protocol for SSV clusters that produces one collective threshold-signed value per slot against a hard deadline. 2abOBFT separates the cluster's σ-eligibility *observation* from its cryptographic σ-*commitment* via a two-window Phase-2 split: Phase 2a is verdict broadcast (op-identity-signed claims about local σ-eligibility, no threshold partials), and Phase 2b is the binding σ-or-NR commit driven by Phase-2a's converged cluster-view.

The "2ab" in the name reflects this split — the protocol's defining feature relative to OBFT-family ancestors. Cryptographic safety is identical to single-Phase-2 protocols (chained IBE + EKM-enforced per-operator commitments + qV = qEnc = 2f+1); the split buys *liveness* — equivocation σ-locked split recovery, h_V=1 selective-delivery deadlock recovery, validity-divergence recovery within the f-bound, mesh-flakiness mitigation — at +1 RTT cost vs single-Phase-2 designs.

2abOBFT operates with K configurable layers (`max(2, f+1) ≤ K ≤ n`), each layer with its own deterministically-derived leader, falling through within a single Phase-3 reconstruction walk (sequential local decryption, no per-layer RTT). The running example throughout is `n = 4, f = 1, K = 4` for SSV Ethereum proposer duty; algebra generalizes to higher cluster sizes.

## When to use it

**Suited for:**
- SSV proposer duty under healthy-network partial synchrony (`P99` ≈ 150ms cluster gossipsub P99/P999) where the +1 RTT vs single-Phase-2 designs fits the slot budget.
- Deployments operating under realistic adversarial conditions: small clusters, transient operators, weak governance, high-stake-to-grief-value ratios. The witness phase closes the σ-locked split equivocation, h_V=1 selective-delivery, and within-window validity-divergence patterns that single-Phase-2 designs leave as Class B / Class A failures.
- High-P99 networks (`P99` ≈ 300–500ms) where multi-round protocols don't fit a 4s relay cutoff but a single round with the Phase-2 split still does.

**Not suited for:**
- Deployments where every millisecond of submission headroom is critical and the Class B grief patterns 2abOBFT closes are not relevant (e.g., low-stake testnet clusters with cooperative byzantines). [OBFT](OBFT.md) saves 200-600ms but exposes the closed-here failure modes.
- Deployments where the **2-1-byz-defect grief pattern** dominates the adversarial-byz threat profile. 2abOBFT regresses on this case vs bare OBFT — see [§2-1 split](#2-1-split) and [§What 2abOBFT does NOT close](#what-2abobft-does-not-close). Bare OBFT would have closed it via the Phase-1 σ_L^V cryptographic lock; 2abOBFT removed that lock to gain validity-divergence and 1-1-1 equivocation recovery. The two are complementary recovery profiles, not strictly comparable: 2abOBFT closes more cases overall but specifically opens this one.
- General-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. 2abOBFT (like the rest of the OBFT family) gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.
- Scenarios requiring host-validity-divergence recovery in 2-2 splits at f=1 n=4 (still Class A; the witness phase narrows the divergence window but cannot eliminate it). [QBFT](#a3--comparison-with-bare-obft-and-qbft) is the appropriate choice when validity is meaningfully unstable across the consensus window.
- Sustained partition tails beyond the absorption window (`Δ_2a + 1 BTT` ≈ 600ms at Config A recommended). Multi-round extensions are a future direction.

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

- **Time unit `BTT` (broadcast trip time)** — `P99` is the propagation budget at the deployment's chosen tail percentile (the variable name `P99` is shorthand for the high-percentile propagation latency; deployments may use P99, P999, P9999 etc. as the actual percentile depending on tail tolerance). `δ` is the cluster's clock-skew bound. We define `1 BTT = P99 + δ` — the time needed for one one-way message to propagate from a sender to all honest receivers under partial-synchrony assumptions. This unit is used throughout for time-budget formulas; the underlying `P99` and `δ` are kept distinct only in §Trust model (where partial synchrony is defined) and in safety arguments (Pigeonhole proofs). Concrete sizing at Config A: `P99 = 150ms, δ = 50ms, 1 BTT = 200ms`.

- **Three distinct deadlines** (do not conflate):

  - **Leader broadcast deadline** `T_broadcast_max = T_commit − 2 BTT`. Each layer's leader must finish broadcasting by this time so that under worst-case propagation, all honest first-observe by `T_commit − 1 BTT`. Per-layer fetch windows fit: `T_k + Δ_1 ≤ T_broadcast_max` for each leader `L_k`.
  - **Receiver acceptance horizon** `T_accept_max = T_commit + Δ_2a − 1 BTT`. Receivers accept Phase-1 bundles whose first-observation time is in `[slot_start, T_accept_max]`. A bundle first-observed past `T_accept_max` is auth-only-retained (usable for verifying re-flooded V's during Phase-2a, but cannot drive a Phase-2a verdict from the receiving operator; see [§Phase 1 / Late-bundle behavior](#phase-1--candidate-broadcast)).
  - **Verdict broadcast horizon** `T_verdict_max = T_commit + Δ_2a − 1 BTT`. Operators must emit their Phase-2a verdict envelope by this time so it propagates to all honest peers before Phase-2a end (`T_commit + Δ_2a`). Coincides with `T_accept_max` by construction.

- **Phase-window minimums:**

  - **`Δ_2a ≥ 1 BTT`** so verdict envelopes and re-flooded Phase-1 bundles propagate before Phase-2a end. **Recommended: `Δ_2a ≥ 2 BTT`** to absorb mesh-jitter and accommodate late-arriving bundles within Phase-2a's window.
  - **`Δ_2b ≥ 1 BTT`** so Phase-2b σ partials propagate before Phase 3.
  - **`Δ_3 ≥ 1 BTT + ε_3`** where `ε_3` ≈ 100ms is local processing time. Phase 3 must absorb (a) end-of-Phase-2b NR-partial propagation and (b) reconstruction processing.

- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

## Assumptions and implications

2abOBFT's claims hold conditional on six explicit assumptions. They are the same six as [OBFT](OBFT.md) except that assumption 5 simplifies (no Phase-1 σ_V to coordinate with later signings). The rest of the spec assumes them and refers back to this section rather than re-deriving the trade-offs each time.

### Assumed

1. **Standard BFT trust bound at the tight setting.** `n = 3f + 1`, up to `f` operators may be byzantine. At `n = 4`, `f = 1`. SSV deploys at the BFT-tight bound (`n ∈ {4, 7, 10, 13}` for `f ∈ {1, 2, 3, 4}`); the threshold formula `qV = qEnc = 2f+1` below requires this tightness to give `2f+1 = n − f`, which is what the bare Pigeonhole arguments depend on. Honest operators run protocol-conformant software (correct convergence-rule enforcement, correct EKM rule enforcement, correct host application, correct gossipsub behavior). Byzantine operators may deviate arbitrarily within their f-bound.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `P99` (cluster gossipsub P99/P999) and clock skew `δ`. Safety is unconditional on timing; only liveness depends on this. **2abOBFT's effective absorption window is `Δ_2a + 1 BTT`** — the Phase-1-broadcast-to-receiver-first-observation tolerance for late bundles to still drive a Phase-2a verdict. Real propagation that exceeds this window is Class A "sustained partition" — out of scope by definition.

3. **Host validity is best-effort unanimous at decision time.** 2abOBFT consumes the host application's `valid` / `not-valid` verdict on `V_{L_k}` at Phase-2a verdict-broadcast time and at Phase-2b sign time. Operators may transiently diverge (e.g., a head change observed by some but not others). The host's job is to make divergence rare via per-operator stabilization. **Phase-2a's window narrows the divergence window structurally** — operators re-evaluate at Phase-2a verdict-broadcast time (not at Phase-1 acceptance time as in OBFT-family) and the convergence rule routes verdict-divergent operators through NR fall-through to a deeper layer's leader. Validity-divergence is *recovered within the f-bound* in 2abOBFT (e.g., 3-of-4 verdict σV vs 1-of-4 NV at f=1 n=4 succeeds at L_0 or L_1) but still cannot cross 2-2 splits at f=1 n=4 (insufficient cluster majority on either side; slot-misses cleanly).

4. **Persistent operator set with rational-byzantine deterrent.** Same shape as the OBFT deterrent — see [docs/OBFT.md / Implications of the rational-byzantine deterrent](OBFT.md#implications-of-the-rational-byzantine-deterrent-assumption-4) for the full mechanism (continuous per-validator fees regardless of per-slot contribution, staker migration on per-cluster slot-miss rates, eventual `Byzantine ≡ Down` collapse via the planned manual-blacklist extension). 2abOBFT's structural delta: most byzantine grief surfaces (equivocation σ-locked splits, h_V=1 selective-delivery, validity-divergence) are recovered in-protocol via the convergence rule, leaving **2-1-byz-defect** and **non-leader verdict-equivocation** as the residual surfaces the deterrent must absorb.

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

The exception: **2-1 equivocation with byz-defect**. When byzantine leader delivers V to majority + V' to minority, verdict-claims σV(V) at Phase-2a, then withholds σ at Phase-2b (NR-emit → Rule 6b cryptographic; silent → behavioral only), the slot misses at L_0 with no fall-through. Bare OBFT succeeds via Phase-1 σ_V lock — 2abOBFT's regression is the cost of removing Phase-1 σ_V to gain validity-divergence and 1-1-1 equivocation recovery. Across many slots, the rational-byzantine deterrent (assumption 4) absorbs it. See [§Liveness / Equivocation patterns](#equivocation-σ-locked-split) for the full case analysis.

### Implications of the rational-byzantine deterrent (assumption 4)

The deterrent's mechanism, evidence-quality framing, and the planned manual-blacklist extension (bitfield on first-broadcast, `f+1` ACK threshold, persistence across slots, within-slot vs following-slot timing) are identical to OBFT's — see [docs/OBFT.md / Implications of the rational-byzantine deterrent](OBFT.md#implications-of-the-rational-byzantine-deterrent-assumption-4) for the full discussion. Two 2abOBFT-specific deltas:

- **Narrower residual scope.** 2abOBFT structurally closes most byzantine grief surfaces in-protocol (equivocation σ-locked splits, validity-divergence within the f-bound, h_V=1 selective-delivery — all recovered via the convergence rule). The deterrent only has to absorb **2-1-byz-defect** and **non-leader verdict-equivocation**; the protocol itself is the primary defense.
- **Seven evidence rules instead of five.** Rule 6a (verdict-vs-verdict equivocation) is cryptographically self-contained and decisively blacklistable on a single observed envelope-pair. Rule 6b (verdict-vs-action equivocation) is behavioral-pattern quality — receivers must cross-reference the cluster verdict view to distinguish honest revision from byzantine equivocation, so false-positive risk is correspondingly higher.

## Protocol

2abOBFT runs **a single agreement round** per slot: Phase 1 → Phase 2a → Phase 2b → Phase 3. Phase 1 is a fresh broadcast (no re-flood across rounds, since there is only one round). The slot's hard wall is the relay submission deadline (`T_relay_cutoff − T_submit`); a slot that does not reach σ-quorum at any layer with enough time to submit is missed.

### Phase 1 — Candidate broadcast

Phase 1 has K per-layer windows (driven by the asymmetric fetch times): `[T_{K-1}, T_{K-1} + Δ_1]` for the deepest backup, then progressively `[T_{K-2}, T_{K-2} + Δ_1]`, ..., ending at `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see [§Preconditions on the host application](#preconditions-on-the-host-application)).
2. Signs `V_{L_k}` with the **operator-identity key** — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a structured envelope binding `(protocol_tag = "2abOBFT-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates 2abOBFT Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, other 2abOBFT message kinds, other consensus protocols sharing the same identity key). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. **No threshold partial signature is produced at Phase 1** — the leader's σ-side commitment happens at Phase 2b, uniformly with all other operators.
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

Standard gossipsub re-flood of any retained Phase-1 bundles. Honest receivers forward bundles to peers on first observation. By Phase-2a end (under partial synchrony with `Δ_2a ≥ 2 BTT`), bundles broadcast at the leader's `T_broadcast_max` deadline have propagated to all honest receivers within Phase-2a's effective acceptance window. Late-arriving bundles past `T_accept_max` enter auth-only retention.

This is the primary bundle-distribution path. The Phase-2a window's purpose for bundle re-flood is identical to OBFT's Phase-2 widening — late re-flood absorption — except that 2abOBFT's split structure means late receivers can issue Phase-2a verdicts on the recovered V (at the cost of the verdict propagating before `T_verdict_max = T_commit + Δ_2a − 1 BTT`).

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

**Verdict propagation budget.** Verdicts broadcast at `T_verdict_max − ε_proc` propagate to all honest peers within `1 BTT` under partial synchrony, reaching them by `T_verdict_max + 1 BTT − ε_proc = T_commit + Δ_2a − ε_proc`. With recommended `Δ_2a = 2 BTT`, this leaves `1 BTT − ε_proc` of slack between verdict arrival and Phase-2a end (`T_commit + Δ_2a`) — enough margin for one full propagation cycle's variance. At minimum sizing `Δ_2a = 1 BTT`, slack collapses to `−ε_proc` (the verdict barely makes it; processing-delay variance can push it past Phase-2a end for some peers). **Recommendation: never use minimum Δ_2a sizing in production**; the recommended `2 BTT` is what makes the verdict propagation budget viable.

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

### Phase 3 — Local decryption and reconstruction (from `T_commit + Δ_2a + Δ_2b`)

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

**Reconstruction completion target** is `T_commit + Δ_2a + Δ_2b + Δ_3` where `Δ_3 ≥ 1 BTT + ε_3`. Phase 3 has no fixed end — reconstruction runs until σ-quorum reaches or the slot's relay-submission deadline (`T_relay_cutoff − T_submit`) forces termination. Late `KindOnion2b` arrivals can be incorporated by re-running the reconstruction walk (Pigeonhole semantics still hold; safe).

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

### Treatment of missing onions

A participant that hasn't received `j`'s Phase-2b onion at decryption time treats `j` as not having contributed at that layer: no σ partial, no NR partial. Standard threshold cryptography — only signed messages count. Within 2abOBFT's absorption window (`Δ_2a + 1 BTT`), gossipsub propagation is expected to deliver all honest broadcasts to all honest receivers before reconstruction starts.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within the slot's relay-submission deadline. If more than `f` operators are offline or byzantine combined, neither σ nor NR quorums reach their thresholds and the slot is missed.

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

1. **Phase 1** `[slot_start, T_commit]`: K leaders broadcast their Phase-1 bundles per their per-layer fetch windows (`[T_{K-1}, T_{K-1} + Δ_1]`, ..., `[T_0, T_0 + Δ_1]`), with `T_0 + Δ_1 ≤ T_broadcast_max = T_commit − 2 BTT`. **No σ_V partial in Phase-1 bundles** (Variant C). Receivers accept bundles first-observed in `[slot_start, T_accept_max]` where `T_accept_max = T_commit + Δ_2a − 1 BTT`.
2. **Phase 2a** `[T_commit, T_commit + Δ_2a]`: each operator broadcasts a per-layer verdict envelope (`KindVerdict`) reflecting their σ-eligibility per layer based on observed Phase-1 bundles and host validity verdicts. Bundle re-flood absorbs late-arriving Phase-1 bundles within the window. Operators emit verdicts at the latest-safe time (around `T_commit + Δ_2a − 1 BTT`) to maximize observed peer state.
3. **Phase 2b** `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]`: each operator computes per-layer convergence decisions from the observed Phase-2a verdict pool (per the convergence rule) and emits σ-or-NR partials per layer. EKM enforces single-σ-V per (slot, layer) per operator at sign time.
4. **Phase 3** (from `T_commit + Δ_2a + Δ_2b`): each operator runs the K-layer reconstruction walk. If σ-quorum reaches on some V at any layer, output the V; halt. If NR-quorum reaches up to some layer `L_C < K`, advance L_C and continue the walk. If neither σ-quorum nor NR-quorum advance unlock at any layer, the slot misses (re-running may incorporate late `KindOnion2b` arrivals).

**Slot timing**: Phase 1 fetch occupies `[slot_start, T_commit]`. Total consensus budget (Phase 2a + Phase 2b + Phase 3) is `Δ_2a + Δ_2b + Δ_3 = 2 BTT + 2 BTT + 300ms ≈ 1100ms` at recommended Config A sizing (≈ 700ms at minimum sizing); consensus is expected to complete at `T_commit + Δ_2a + Δ_2b + Δ_3`, leaving the rest of the slot as submission slack to `T_relay_cutoff`.

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
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `P99` (propagation P99/P999) and clock skew `δ`. Three distinct cutoffs operationalize this bound: `T_broadcast_max = T_commit − 2 BTT` (leader broadcast deadline), `T_accept_max = T_commit + Δ_2a − 1 BTT` (receiver acceptance horizon), `T_verdict_max = T_commit + Δ_2a − 1 BTT` (verdict broadcast horizon, coincident with `T_accept_max`). Reconstruction is expected to complete at `T_commit + Δ_2a + Δ_2b + Δ_3`; the slot's hard wall is the relay-submission deadline `T_relay_cutoff − T_submit`.

  **2abOBFT's effective absorption window** = `T_accept_max − T_broadcast_max = Δ_2a + 1 BTT`:
  - At `Δ_2a = 1 BTT` (BFT-minimum): `2 BTT` = 400ms at Config A.
  - At `Δ_2a = 2 BTT` (recommended): `3 BTT` ≈ 600ms at Config A.

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

2abOBFT's liveness is **partial-synchrony-conditional within the slot's relay-submission deadline**. The protocol absorbs network-induced failures via the late-bundle re-flood window in Phase 2a. Equivocation σ-locked split, h_V=1 selective-delivery, and validity-divergence within the f-bound are recovered structurally via the convergence rule's σ-eligibility-quorum-short → NR-pool fall-through path.

Running example: `f = 1, n = 4, K = 4`. Honest A, B, C; byzantine D.

#### Healthy path

All 4 operators receive `V_{L_0}` via gossipsub within `1 BTT`.

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

- **If re-flood completes within Phase-2a** (`Δ_2a ≥ 1 BTT` from byz's late delivery): A retains V_a + V_b + V_c → equivocation observed → A's verdict NR. Same for B, C. NR-pool actual = 3 ≥ qEnc. **Fall through to L_1.**
- **If re-flood does not complete** (byz times deliveries past T_accept_max for each honest): A retains V_a only; B retains V_b only; C retains V_c only. Each honest issues σV verdict on their respective V. `verdict_pool[V_a] = 1`, `verdict_pool[V_b] = 1`, `verdict_pool[V_c] = 1` (all < qV). All honest go NR per rule. NR-pool = 3 ≥ qEnc. **Fall through.**

In both sub-cases, the slot recovers via L_1 fall-through. OBFT base 1-1-1 slot-misses at L_0 with no fall-through; 2abOBFT structurally fixes this.

##### 2-1 split

D delivers V to {A, B}, V' to {C}.

- **D cooperates (verdict σV(V) + Phase-2b σ on V)**: verdict_pool[V] = 3 ≥ qV. A, B, D σ-emit. C does not have V_local = V → NR. σ-pool = 3 = qV. **Slot succeeds at L_0.**
- **D silent (no verdict, no Phase-2b emit)**: verdict_pool[V] = 2 < qV. A, B → NR (verdict-quorum short); C → NR. NR-pool actual = 3 ≥ qEnc. **Fall through to L_1.** (Bare OBFT would have succeeded at L_0 here via Phase-1 σ_V lock; 2abOBFT pays one extra layer of latency.)
- **D defects (verdict σV(V) + Phase-2b NR-emit or silent)**: verdict_pool[V] = 3 ≥ qV. A, B σ-emit on V; D withholds σ. σ-pool = 2 < qV; NR-pool = 1 (C) + (1 if NR-emit, 0 if silent) ≤ 2 < qEnc. **Slot misses at L_0 with no fall-through.** Evidence: Rule-6b cryptographic under NR-emit, behavioral only under silent. **Strictly worse than OBFT base** (Phase-1 σ_V lock would have succeeded).

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

The recommended `Δ_2a ≥ 2 BTT` absorbs typical mesh-jitter (one full `1 BTT` of additional slack on top of P99 propagation). For wider mesh outliers, deployment-level mesh-diversity remains relevant.

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

- **[Class A]** **Sustained partition (real propagation > absorption window)** — violates assumption 2 (partial synchrony) under 2abOBFT's framing (absorption = `Δ_2a + 1 BTT`, ≈ 600ms at Config A recommended). Slot misses cleanly. No safety violation.
- **[Class A]** **More than `f` faults** — violates assumption 1 (BFT trust bound). Slot misses regardless of protocol structure.
- **[Class A]** **Validity-divergence at the 2-2 boundary at f=1 n=4** — re-org lands inside Phase-1-to-Phase-2a window and produces a 2-σ vs 2-NV honest split. Both σ-eligibility and NR-eligibility quorums short of threshold; cluster falls through to L_1 (per rule). If L_1 also exhibits the same divergence (same re-org affects both layers' parent_roots), slot misses cleanly. **In practice, backup leaders fetch from deeper-confirmed parents and rarely share L_0's re-org exposure.**
- **[Class A]** **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates in non-recoverable patterns at every layer). At K ≥ f+1, byzantines alone (within f-bound) cannot cause this — pigeonhole guarantees ≥ 1 honest leader. So this typically requires either >f faults (Class A) or coincident non-byzantine independent failures.
- **[Class B]** **Equivocation 2-1 split with byz-defect** — byz delivers V to majority, V' to minority (byz must be leader); verdict-claims σV(V) at Phase-2a; withholds σ at Phase-2b (NR-emit or silent). σ-pool short by 1; NR-pool short by 1 either way (< qEnc at f=1 n=4). Slot misses at L_0. **Strictly worse than OBFT base** (Phase-1 σ_V lock would have succeeded). Triggerable only on byz-leader slots (1/K). Evidence: Rule 6b cryptographic under NR-emit, behavioral only under silent — same split as L_Bid 2-1-byz-defect.

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
| Termination (output guaranteed) | Conditional: consensus expected to complete by reconstruction target if real propagation between leader broadcast and any honest first-observation ≤ absorption window `Δ_2a + 1 BTT` (≈ 600ms at Config A recommended) and ≤ f operators byzantine/offline. |
| Equivocation detection | Yes — leaders sign Phase-1 envelopes; conflicting signed candidates form self-contained slashable evidence (Rule 2) |
| Equivocation recovery | **Structural for 1-1-1, all-equivocation-NR, h_V=1 patterns** via convergence-rule fall-through. **2-1-byz-defect regresses** vs single-Phase-2 protocols (slot misses at L_0; Rule-6 evidence). |
| Validity-divergence recovery | **Majority recovers** (e.g., 3-of-4 σV vs 1 NV at f=1 n=4 reaches σ-quorum at L_0). 2-2 split at f=1 n=4 still slot-misses cleanly (no majority). |
| Byzantine-leader-grief resistance | Substantial. h_V=1 (both withhold-then-fake-σ and selective Phase-1 delivery variants), 1-1-1 equivocation split, mesh-flakiness, late-deepest-layer-broadcast — all closed structurally via Phase-2a observation. 2-1-byz-defect remains a Class B residual. |
| Mesh-flakiness tolerance | Good — Phase-2a window absorbs typical mesh-jitter (recommended `Δ_2a ≥ 2 BTT` accommodates one full propagation cycle of variance). Wider outliers fall back through NR-quorum to L_1. |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide. Same as OBFT-family. |
| Built-in leader fallback | Yes (K-layer fall-through within Phase 3's reconstruction walk; K configurable, K = n recommended for proposer duty) |
| Round-change recovery | No — single-round design. Late re-flood within Phase-2a's absorption window is the only within-slot partition-recovery mechanism. |
| Partial-synchrony absorption window | `Δ_2a + 1 BTT` (single round) — ≈ 600ms at Config A recommended. |
| Healthy-path latency (post-`T_commit`) | ~1100ms at Config A recommended (Δ_2a=Δ_2b=2 BTT=400ms each + Δ_3=300ms); ~700ms at minimum sizing (Δ_2a=Δ_2b=1 BTT=200ms each + Δ_3=300ms) |
| Slot budget cost vs single-Phase-2 ([OBFT](OBFT.md)) | +600ms at recommended sizing (extra Phase 2a window of 400ms + Δ_3 +200ms vs single Phase 2); +400ms at minimum sizing (both have Phase 2 window summing to 1 BTT-equivalent, plus Δ_3 difference) |
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
| `T_broadcast_max` | leader broadcast deadline — `T_commit − 2 BTT`; per-layer fetch windows fit within `[0, T_broadcast_max]` |
| `T_accept_max` | receiver acceptance horizon — `T_commit + Δ_2a − 1 BTT`; bundles first-observed past this are auth-only-retained |
| `T_verdict_max` | verdict broadcast horizon — coincident with `T_accept_max` |
| `T_relay_cutoff` | slot's hard relay-submission deadline (`slot_start + 4.0s` for SSV proposer); reconstruction must complete with `T_submit ≈ 100ms` of slack to land (matches OBFT.md's `header_submit_headroom`) |

### Timing budget — concrete configurations

The slot's hard relay-submission deadline is `slot_start + 4.0s`; a minimum `T_submit ≈ 100ms` is reserved for relay submission. The slot's reconstruction must complete by `slot_start + 4.0s − T_submit ≤ slot_start + 3.90s`.

Common parameters: **P99 = 150ms (cluster gossipsub P99/P999), δ = 50ms, n = 4, f = 1**.

#### 2abOBFT(n=4, K=4) recommended sizing

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch (effective) | 1200ms | slot_start + 1.20s | `T_broadcast_max = T_commit − 2 BTT = 1.20s` |
| Phase-1 propagation slack | 400ms | slot_start + 1.60s = T_commit | Bundles broadcast at deadline propagate to all honest within `1 BTT` |
| Phase 2a | 400ms | slot_start + 2.00s | `Δ_2a = 2 BTT`; verdict broadcast horizon = `T_commit + Δ_2a − 1 BTT = 1.80s`; absorbs late bundles arriving up to 1.80s |
| Phase 2b | 400ms | slot_start + 2.40s | `Δ_2b = 2 BTT`; σ/NR partials propagate to peers before Phase 3 |
| Phase 3 | 300ms | slot_start + 2.70s | `Δ_3 = 1 BTT + ε_3 = 300ms`; absorbs end-of-Phase-2b NR-partial propagation + reconstruction |
| Submission | 1300ms | slot_start + 4.00s | 13× the 100ms minimum — comfortable headroom |

#### 2abOBFT(n=4, K=4) minimum sizing

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch (effective) | 1200ms | slot_start + 1.20s | Same |
| Phase-1 propagation slack | 400ms | slot_start + 1.60s | Same |
| Phase 2a | 200ms | slot_start + 1.80s | `Δ_2a = 1 BTT` (BFT-minimum); narrower late-bundle absorption |
| Phase 2b | 200ms | slot_start + 2.00s | `Δ_2b = 1 BTT` |
| Phase 3 | 300ms | slot_start + 2.30s | Same |
| Submission | 1700ms | slot_start + 4.00s | 17× the 100ms minimum |

**Recommended sizing trades 400ms of submission headroom for mesh-jitter absorption**. Production telemetry should drive the choice.

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

   - **`T_broadcast_max = T_commit − 2 BTT`**: leader broadcast deadline.
   - **`T_accept_max = T_commit + Δ_2a − 1 BTT`**: receiver acceptance horizon.
   - **`T_verdict_max = T_commit + Δ_2a − 1 BTT`**: verdict broadcast horizon (coincident with T_accept_max).

   Phase-window minimums: `Δ_2a ≥ 1 BTT`, `Δ_2b ≥ 1 BTT`, `Δ_3 ≥ 1 BTT + ε_3`. Recommended: `Δ_2a = Δ_2b = 2 BTT` for jitter absorption.

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

Trade-off: 2-1 equivocation patterns where bare OBFT's Phase-1 σ_V crypto-locks byz's σ — succeeding at L_0 even if byz then goes silent — regress in 2abOBFT (byz can defect from σV verdict to NR or silent at Phase-2b; Rule 6b cryptographic only under NR-emit, behavioral under silent; slot misses at L_0 either way). A second regression on the same theme: non-leader verdict-equivocation under marginal h_V — byz verdict-equivocation injects per-peer convergence divergence, leading to under-quorum splits at L_0. Across many slots the rational-byzantine deterrent absorbs both costs. See [§Liveness / Equivocation 2-1 split](#2-1-split) and [§Failure modes / Non-leader verdict-equivocation under marginal h_V](#failure-modes).

The relationship across the OBFT family:

| Protocol | R | K | Phase-2 split | Phase-1 σ_V | Role |
|---|---|---|---|---|---|
| [OBFT](OBFT.md) | 1 | configurable | no | yes | minimum machinery for K-layer fall-through |
| [OBFTR](OBFTR.md) | configurable (typically 2) | configurable | no | yes | OBFT + R-round retry for `(P99, R · P99]` partition coverage |
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
| Healthy-path latency (post-`T_commit`, recommended sizing) | ~500ms | ~1100ms (+600ms for Phase-2a window + Δ_3) |
| Marginal h_V_honest=2 + byz silent | Slot misses (σ-pool short, NR-pool short) | Falls through to L_1 ✓ |
| Equivocation 1-1-1 split | Slot misses | Falls through to L_1 ✓ |
| Equivocation 2-1, byz cooperates | Succeeds at L_0 | Succeeds at L_0 (tie) |
| Equivocation 2-1, byz silent | Succeeds at L_0 (Phase-1 σ_V locked) | Falls through to L_1 (one extra layer) |
| Equivocation 2-1, byz defects | Succeeds at L_0 (Phase-1 σ_V locked) | **Slot misses (regression)** — Rule 6b evidence under NR-emit; behavioral under silent abstention |
| Non-leader verdict-equivocation at marginal h_V | n/a (no verdicts in OBFT) | **Slot misses (regression)** — Rule 6a evidence |
| h_V=1 selective-delivery deadlock | Partially closed (withhold-then-fake-σ variant closed by the absence of the Defer state in OBFT); selective Phase-1 delivery still slot-misses (algebraic limit at f=1, n=4) | Falls through to L_1 ✓ |
| Validity-divergence at majority | Slot misses (Class A) | Recovered ✓ |
| Validity-divergence at 2-2 boundary | Slot misses (Class A) | Slot misses (Class A — same algebraic limit) |
| Late deepest-layer leader broadcast | Class A | Recovered (Phase-2a re-flood absorbs) ✓ |
| Mesh-flakiness | Class B | Mitigated ✓ |
| Submission headroom (Config A recommended) | ~2.0s | ~1.3s |
| Bandwidth (healthy, n=4, K=4) | ~28 KB (includes σ_L^V witness section ≈ +1.5 KB) | ~30 KB (no σ_L^V witness — 2abOBFT has no Phase-1 σ_L^V; +3 KB for verdicts vs OBFT baseline before witness) |
| EKM complexity | Phase-1 σ_V + Phase-2 σ + NR coordination | Phase-2b σ XOR NR only — simplest in the family |
| Slashing-evidence rules | 5 | 7 (Rules 1-5 inherited + Rule 6a verdict-vs-verdict cryptographic + Rule 6b verdict-vs-action gossipsub-pattern-quality) |

**Migration**: cluster running OBFT can adopt 2abOBFT by (1) extending the wire format with `KindVerdict`, (2) replacing the single-Phase-2 commit with the Phase-2a/Phase-2b split, (3) modifying the Phase-1 bundle schema to drop σ_V, (4) updating the protocol-tag to `2abOBFT-v1` for envelope domain separation. EKM coordination simplifies (one fewer signing event per (slot, layer)).

### A.2 — Comparison with [OBFTR(R≥2)](OBFTR.md)

OBFTR(R≥2) is the multi-round extension of OBFT with cross-round acceptance widening for wider partition absorption. Comparing 2abOBFT to OBFTR(R≥2):

- **2abOBFT covers more failure modes within R=1** than OBFTR(R≥2) does — Phase-2 split closes equivocation 1-1-1, h_V=1, validity-divergence-majority, mesh-flakiness, late-deepest-layer-broadcast that OBFTR(R≥2) leaves uncovered.
- **OBFTR(R≥2) covers wider partition tails** within `(P99, R · P99]` than 2abOBFT does — multi-round retry extends the absorption envelope, while 2abOBFT is bounded by `Δ_2a + 1 BTT` (single round).

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

- **2-1-byz-defect equivocation**: byz leader equivocates V/V', verdict-claims σV(V), withholds σ at Phase-2b (NR-emit or silent). Bare OBFT ✓ (Phase-1 σ_V lock); QBFT ✓ (round-2 fresh V). 2abOBFT ✗ — Variant C's σ_V removal exposes the defection surface; rational-byzantine deterrent is the only protocol-level defense (Rule 6b cryptographic under NR-emit, behavioral under silent).
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

At SSV proposer-duty default budget (~4s relay cutoff with P99 = 150ms, δ = 50ms), each protocol allocates the budget differently:

All counts at recommended sizing (2 BTT per emission cycle — see [docs/BFT-comparison.md / Sizing convention](BFT-comparison.md#sizing-convention)).

| Aspect | bare OBFT | 2abOBFT | QBFT-SSV (RT=2s, current SSV) |
|---|---|---|---|
| Consensus budget | ~600ms | ~1200ms | ~3.6s (2 rounds: RT + R2 = 2s + 1.6s) |
| Submission headroom | ~2.0s | ~1.3s | ~0.4s |
| Healthy-path latency | ~600ms | ~1200ms | ~1600ms |
| Bandwidth (n=4, K=4 healthy) | ~28 KB | ~30 KB | ~14 KB |
| Cryptographic primitives | BLS + threshold IBE/SWE | BLS + threshold IBE/SWE | BLS threshold |
| Production maturity | spec only | spec only | SSV runs this today |

#### Apples-to-apples vs production-T framing

The production-T allocation reflects current deployment choices, which are **not** strictly apples-to-apples for failure-mode coverage — QBFT uses ~3.6s for 2-round consensus + ~0.4s for submission, while 2abOBFT uses ~1200ms for consensus + ~2.7s submission headroom. Comparing failure-mode coverage at production-T can read as "QBFT recovers more failure modes than 2abOBFT" but conflates two different effects:

- **Time-conditional recoveries**: at small T, QBFT fits only 1 round and loses most of its multi-round-conditional recoveries (mesh-flakiness with byz leader, σ-locked equivocation, h_V=1, validity-majority — Bucket-3-equivalents). 2abOBFT recovers all of these at small T via single-round convergence rule. **At T = 1200ms apples-to-apples (2abOBFT's healthy budget), 2abOBFT has a strictly larger deadlock-free set than QBFT.** At larger T, QBFT's round-2 access scales recovery and matches 2abOBFT on most Bucket-1 + Bucket-3 patterns — but only when the slot budget admits R2 (e.g., (0s, BTT=200ms) cell at recommended sizing for QBFT-SSV).
- **Structural recoveries**: Bucket 2 (OBFT-family-only — multi-leader silent, crypto safety primitive) and Bucket 4 (2abOBFT regressions — 2-1-byz-defect, verdict-equivocation, validity-2-2-refetch) are independent of T. They reflect protocol structure, not budget.

If you give 2abOBFT a 3s consensus budget (extending Δ_2a / Δ_2b), the recovery scope does not grow — single-round protocols don't add recoveries with more time, only wider absorption windows. If you compress QBFT to a 1200ms consensus budget (1 round only at recommended sizing — barely enough for R1's 8 BTT = 1.6s, requires further sizing compromise), QBFT's recovery scope shrinks to single-round failures only, losing all time-conditional Bucket-3-equivalent recoveries.

The bucket structure makes the protocol-vs-protocol comparison stable across T choices. The production-T table reflects deployment-cost trade-offs (latency, submission headroom, primitive maturity) on top of the structural recovery scope.

#### Composability note

Composing Phase 2a/2b with R-round retry would close Bucket-4 partially: round-change with σ-lock abandonment recovers 2-1-byz-defect and validity-2-2-refetch; verdict-equivocation surface remains. The combined "2abOBFT + R" design point would have a deadlock-free set roughly comparable to QBFT's in adversarial-byz scenarios, with Bucket-2 OBFT-family advantages (multi-leader silent in-round) preserved. Not specified here.

## Design notes archive

The design-plan discussion accumulated during development of 2abOBFT — variants considered, detailed scenario walkthroughs, edge cases, open implementation questions, and the build-phase plan — has been moved to [2abOBFT-design-notes.md](2abOBFT-design-notes.md). That document is non-load-bearing reference material; this spec is authoritative.
