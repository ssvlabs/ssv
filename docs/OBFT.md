# OBFT — Single-Round Onion BFT

A single-round agreement protocol for SSV clusters that produces one collective threshold-signed value per "slot" against a hard deadline. OBFT is the simpler-spec cousin of [OBFTR](OBFTR.md) — it preserves OBFTR's cryptographic safety (cluster-wide unique output via threshold cryptography over EKM-enforced per-operator commitments) and OBFTR's K-layer onion structure for parallel leader fall-through, but drops the R-round retry machinery entirely. Single-round only: agreement runs once per slot against a single hard deadline; there is no round-change, no cross-round re-flood, no L_C cluster-consensus signaling.

OBFT runs K layers (configurable, `max(2, f+1) ≤ K ≤ n`) in a single agreement round. The K-layer reconstruction walk in Phase 3 is the load-bearing fall-through mechanism. Each operator commits exactly once per (slot, layer) at `T_commit` — σ, NR, or NV — and emits a single combined `KindCommit` message carrying that decision. Relative to OBFTR, the R-round retry, cross-round re-flood, cross-round σ-or-NR exclusivity, L_C cluster-consensus signaling (`KindLCClaim`), per-round acceptance widening, auth-only-retention state, and cross-round σ-partial dedup are all removed.

OBFT's recovery scope is intentionally bounded. The protocol's absorption is **per-layer staggered**: the primary L_0 absorbs propagation up to `B_0 = 0.5 BTT = 100ms` at Config A (= optimistic; relative to `Ls_arrival`); deeper backups absorb progressively wider tails, with the deepest layer at K=4 absorbing up to `B_{K-1} = 5 BTT = 1000ms` (= last-resort) — see [§Setting](#setting). Within these bounds, OBFT recovers all in-envelope cases via K-layer fall-through: healthy path, silent leader, multi-leader fall-through within Phase 3's reconstruction walk (no per-layer RTT). The protocol does NOT recover from **view-divergence** patterns: equivocation (byzantine leader sending different V's to different honest) and validity-divergence (e.g., re-org causing some honest to validate V and others to NV). View-divergence cases either succeed naturally (when the split happens to leave 2-of-3 honest σ-committed on the same V at f=1 n=4, allowing leader's σ_L^V to push the pool to qV) or slot-miss. Equivocation in non-naturally-recovered patterns is **slashable** (the byzantine leader pays the stake-based penalty); validity-divergence is not attributable to anyone and just slot-misses. See [Assumptions and implications](#assumptions-and-implications) for the explicit list. The **Phase 2a/2b split** (see [2abOBFT](2abOBFT.md)) closes the equivocation gap at +1 RTT cost; documented as a future improvement, not in current OBFT.

OBFT explicitly does not provide within-slot partition recovery for asymmetric propagation — bundles arriving past `T_commit` at any honest receiver simply don't count, and the cluster falls through to a deeper layer whose bundle did propagate. This trades partition tolerance for spec simplicity. At small clusters (n=4, the SSV proposer-duty default) gossipsub mesh is effectively full and asymmetric propagation is rare; at larger clusters, [OBFTR](OBFTR.md) is the appropriate choice (cross-round retention absorbs propagation tails across rounds).

The protocol description below targets `n = 4` (`f = 1`) as the running example, with `K = 4` (i.e., K = n) as the recommended default for SSV proposer duty (every cluster member is a leader at exactly one layer; pigeonhole guarantees ≥3 honest leaders at f=1). K is tunable per duty within `max(2, f+1) ≤ K ≤ n`. SSV's Ethereum proposer duty is used as the running application example. Generalizations to `n ≥ 7` are noted inline where the algebra changes.

## When to use it

**Suited for:** SSV proposer duty under healthy-network partial synchrony (`P99` ≈ 100ms cluster gossipsub P99/P999), where OBFT's 2-RTT healthy-path latency plus K-layer parallel leader fall-through is sufficient and round-change machinery is not desired. Also well-suited for high-P99 networks (`P99` ≈ 300–500ms) where [OBFTR(R=2)](OBFTR.md) does not fit the 4s relay cutoff but a single round still does. Generally: deployments that prioritize spec/EKM simplicity, more submission headroom, and high-P99 fit over the larger partial-synchrony envelope of multi-round OBFTR.

**Adversarial-byz operating conditions warrant Phase 2a/2b alongside OBFT — with one regression to weigh.** Bare OBFT does not defend against an adversarial byzantine that deliberately engineers σ-locked split equivocation patterns (1-1-1, all-honest-NR, etc.) — these reliably slot-miss when byz is L_0. The rational-byzantine deterrent (assumption 4) handles them across many slots, but per-slot they cause clean slot-miss with weakly-slashable behavioral evidence. The **Phase 2a/2b split** (see [2abOBFT](2abOBFT.md)) ([§Where this came from](#where-this-came-from)) closes *most* of these patterns in-protocol at +1 RTT cost — specifically: 1-1-1 σ-locked splits, h_V=1 selective-delivery, and validity-divergence majorities. **The exception is 2-1-byz-defect** (byz leader equivocates V/V', verdict-claims σV(V) at Phase-2a, defects to NR at Phase-2b), where 2abOBFT *regresses* vs bare OBFT: bare OBFT closes this case via the Phase-1 σ_L^V cryptographic lock; 2abOBFT removed that lock to gain the other recoveries and consequently slot-misses here (Rule-6b evidence on the wire). See [docs/2abOBFT.md / 2-1 split](2abOBFT.md#2-1-split). Net for deployments operating under realistic adversarial conditions (small clusters, transient operators, weak governance, high-stake-to-grief-value ratios): **Phase 2a/2b should be considered near-term, not future** — the patterns it closes are typically more frequent and damaging than the 2-1-byz-defect it opens. OBFT standalone is best understood as the spec-simplest point in the family; OBFT + Phase 2a/2b is *robust against a different (and broader) set of adversarial patterns* — not strictly more robust.

**Not suited for:** deployments where the gossipsub propagation tail commonly delays L_0's bundle past `B_0` (L_0's per-layer budget — `0.5 BTT` relative to `Ls_arrival` at K=4 Config A) AND the cluster needs to preserve V_0's MEV freshness (rather than fall through to a deeper backup's vanilla payload) — use [OBFTR(R=2)](OBFTR.md) instead, which re-floods retained R1 bundles in R2 to deliver V_0 within `R · BTT`. OBFT's K-layer fall-through still absorbs propagation tails up to `B_{K-1} = 5 BTT` (deepest layer; loses MEV freshness on each fall-through), so OBFT alone is fine when the tail rarely exceeds `B_{K-1}` or when MEV-preservation isn't the priority. This typically becomes load-bearing at n ≥ 10 where mesh sparsity makes asymmetric propagation common. Also not suited for: scenarios requiring host-validity-divergence recovery within a slot (OBFT assumes host validity is unanimous at decision time, see [Assumptions](#assumptions-and-implications); QBFT is the appropriate choice when validity is unstable across the consensus window). Not suited for adversarial-byz operating conditions without Phase 2a/2b adoption — see above.

**Also not suited for:** general-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. OBFT (like OBFTR) gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n = 3f+1` (the BFT-tight setting; matches SSV's deployment configurations `n ∈ {4, 7, 10, 13}`). The running example is `n = 4, f = 1`. The threshold formula `qV = qEnc = 2f+1` below equals `n − f` exactly at this setting; this equality is what makes the Pigeonhole arguments in [§Safety](#safety-cryptographic-honest-majority) hold.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) as the threshold-signing scheme for no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety". At `n = 4`, `qEnc = 3`.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts and protocol-level claims, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- **K layers** (`max(2, f+1) ≤ K ≤ n`, configurable; **K ≥ f+2 strongly recommended** — see below) with deterministically-derived leaders: layer 0 with **primary leader** `L_0`, layer 1 with **backup leader** `L_1`, ..., layer `K-1` with `L_{K-1}`. Leaders must be distinct (`L_i ≠ L_j` for `i ≠ j`). For `K = n`, every cluster member is a leader at exactly one layer per slot.

  **Two distinct K bounds:**
  - **`K ≥ f+1` is the BFT-liveness minimum** — ensures at least one layer has an honest leader (by pigeonhole over the f-byz bound). At K < f+1, all leaders could be byzantine and no σ-quorum reaches at any layer.
  - **`K ≥ f+2` is the late-leader-resilience minimum** — ensures at least *two* honest leaders exist, so a single late-broadcasting honest leader doesn't foreclose the slot via the deepest-layer NR-lock pathology (see §Failure modes / Late deepest-layer leader broadcast). At K = f+1 with the single honest leader running late, the slot misses; at K ≥ f+2 a second honest leader provides fall-through redundancy.

  Concrete minimums by f: at `f = 1`, BFT-min `K = 2` but **late-leader-resilient `K = 3` recommended**, with **`K = n = 4` as the OBFT default** for SSV proposer duty (every cluster member leads exactly one layer; maximum honest-leader probability via pigeonhole); at `f = 2, n = 7`, BFT-min `K = 3` but resilient `K = 4` recommended; at `f = 3, n = 10`, resilient `K = 5`.
- **Single agreement round.** OBFT fixes `R = 1`: one Phase 1 → Phase 2 → Phase 3 sequence per slot, no retry, no re-flood across rounds, no L_C cluster-consensus signaling. The slot's reconstruction deadline is the only deadline. Each operator commits exactly once per (slot, layer) at `T_commit` based on what they observed by then; bundles arriving past `T_commit` do not contribute to that layer's σ-pool. For deployments needing within-slot partition recovery via multi-round retry, see [OBFTR(R≥2)](OBFTR.md).
- Per-layer leader-fetch deadlines `T_{K-1} < T_{K-2} < ... < T_1 < T_0`, plus a single cluster deadline `T_commit`. (`T_commit` is the *view-fix point*: each operator commits its stance based on what it observed by `T_commit`. Reconstruction and submission happen after.) The asymmetric fetch times let primary leaders fetch high-MEV values late (`T_0` close to `T_commit`) while deeper layers' leaders fetch safe early values from deeper-confirmed parents (`T_{K-1}` well before `T_0`).
- **Time unit `BTT` (broadcast trip time)** — `P99` is the propagation budget at the deployment's chosen tail percentile (the variable name `P99` is shorthand for the high-percentile propagation latency; deployments may use P99, P999, P9999 etc. as the actual percentile depending on tail tolerance). `δ` is the cluster's clock-skew bound. We define `1 BTT = P99 + δ` — the time needed for one one-way message to propagate from a sender to all honest receivers under partial-synchrony assumptions. This unit is used throughout for time-budget formulas; the underlying `P99` and `δ` are kept distinct only in §Trust model (where partial synchrony is defined) and in safety arguments (Pigeonhole proofs). Concrete sizing at Config A: `P99 = 150ms, δ = 50ms, 1 BTT = 200ms`.

- **Per-layer leader broadcast deadlines `T_broadcast_max_k`** — OBFT uses **asymmetric per-layer broadcast budgets**: the primary `L_0` broadcasts latest with the smallest propagation budget (= freshest MEV); each backup broadcasts progressively earlier with a progressively wider propagation budget. The cluster falls through to whichever layer's bundle actually arrived by `T_commit`.

  General form: `T_broadcast_max_k = Ls_arrival − B_k`, where `Ls_arrival = T_commit − slack` is the latest stable σ-aggregation anchor (slack = operator-local first-observation jitter; typically ≈ 0.5 BTT) and `B_k` is layer `k`'s propagation budget in BTT units measured from `Ls_arrival`. Recommended: `B_0 ≥ 0.5 BTT` (covers V_0 propagation to all-honest first-observation by `Ls_arrival` under typical mesh; the additional `slack` between `Ls_arrival` and `T_commit` absorbs first-observation jitter), `B_k ≥ B_{k-1}` for `k > 0` (deeper layers have ≥ predecessor's budget), and each leader `L_k` broadcasts by `T_broadcast_max_k`. Bundles whose first-observation time is past `T_commit` at any honest receiver are simply not counted by that receiver toward σ-quorum at this layer; the cluster relies on K-layer fall-through to a backup layer whose bundle did propagate in time. (Note: an equivalent formulation anchors `B_k` to `T_commit` directly — `T_broadcast_max_k = T_commit − B_k_T_commit` with `B_k_T_commit = B_k + slack`. Both are used in the literature; this spec uses the `Ls_arrival`-anchored form for cleaner per-layer multipliers.)

  **Sizing intuition.** `L_0`'s budget covers the optimistic case (nominal propagation, maximum MEV fetch); deeper backups absorb progressively wider propagation tails. Concrete K=4 sizing for SSV proposer duty (in §Application): `B_0 = 0.5 BTT`, `B_1 = 1 BTT`, `B_2 = 2 BTT`, `B_3 = 5 BTT` (relative to `Ls_arrival`; equivalently `1, 1.5, 2.5, 5.5 BTT` relative to `T_commit`). The trade-off: `B_0`'s tighter budget gives the primary the longest fetch window (best MEV) but the least propagation safety margin; if real propagation exceeds `B_0` for L_0 specifically, the cluster falls through to L_1 (which has 2× the budget), and so on.
- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

## Assumptions and implications

OBFT's claims hold conditional on a small set of explicit assumptions about the deployment environment. They are consolidated here; the rest of the spec assumes them and refers back to this section rather than re-deriving the trade-offs each time.

### Assumed

1. **Standard BFT trust bound at the tight setting.** `n = 3f + 1`, up to `f` operators may be byzantine. At `n = 4`, `f = 1`. SSV deploys at the BFT-tight bound (`n ∈ {4, 7, 10, 13}` for `f ∈ {1, 2, 3, 4}`); the threshold formula `qV = qEnc = 2f+1` below requires this tightness to give `2f+1 = n − f`, which is what the Pigeonhole arguments depend on. Honest operators run protocol-conformant software (correct EKM rule enforcement, correct host application, correct gossipsub behavior). Byzantine operators may deviate arbitrarily within their f-bound.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `P99` (cluster gossipsub P99/P999) and clock skew `δ`. Safety is unconditional on timing; only liveness depends on this. **OBFT's per-layer absorption is staggered: `B_k = Ls_arrival − T_broadcast_max_k`** — `B_0 = 0.5 BTT` for the primary up to `B_{K-1} = 5 BTT` for the deepest backup at Config A K=4 (see [§Setting](#setting)). Bundles arriving past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum. Real propagation from leader L_k's broadcast to any honest first-observation that exceeds `B_k` causes that layer to fail at that receiver; the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time (deeper layers have wider `B_k`). Multi-round OBFTR (R ≥ 2) extends absorption further via cross-round retention with re-flood; OBFT trades that for spec simplicity (see [Where this came from](#where-this-came-from)).

3. **Host validity is unanimous at decision time** (best-effort assumption). OBFT assumes the host application's `valid` / `not-valid` verdict on `V_{L_k}` is the same across all honest operators by the time they emit Phase 2. Operators may transiently diverge (e.g., a head change observed by some but not others). The host's job is to make divergence rare via per-operator stabilization — typically by validating against a stable head snapshot taken at Phase-1 acceptance time, then locking the verdict for the remainder of the slot. The validity-locking window narrows to bundle arrivals before `T_commit` (~`1 BTT` at Config A). When divergence does occur — a re-org during the acceptance window with operators accepting on either side of it — the assumption is violated and the slot may miss; the protocol does not recover. See [Application: SSV Ethereum proposer duty / Head-change handling](#head-change-handling) for the SSV-specific stabilization workflow and the residual divergence window.

   The validity check exists to prevent the cluster from agreeing on a garbage / invalid V — it is not a divergence-recovery mechanism. NV is operationally identical to NR for protocol counting; it does not trigger any in-protocol divergence-handling path.

4. **Persistent operator set with rational-byzantine deterrent.** OBFT operates within a stable SSV cluster running protocol instances over many slots. The deterrent is the same one that already disciplines an offline operator under SSV's network-wide threat model: per-validator operator fees flow continuously to all cluster operators regardless of per-slot contribution (the remaining `n − f` honest carry the work at zero ops cost to the silent/byzantine), and stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters, collapsing the silent/byzantine operator's fee accrual to zero. SSV is already designed for the operator-down case ("the cluster and stakers deal with it"); the rational-byzantine claim is that a byzantine operator gains nothing an offline operator wouldn't already get, and has reputation (persistent across slots) to lose.

   **Asymmetry — Byzantine vs Down — and what restores equivalence.** With QBFT, `Byzantine ≡ Down` automatically: round-change rotates past silent or malformed PROPOSE/PREPARE/COMMIT, so the worst a byzantine can do per-slot is silently going offline. With OBFT-family, byzantine is *significantly worse on latency than Down* — equivocation σ-locked splits, h_V=1 selective-delivery, fake-encrypted-presence, and behavioral σ-refusal can engineer per-slot grief above what equivalent offline behavior would produce. The expected mitigation is **manual blacklisting**: the cluster's surviving `n − f` operators agree out-of-band on the misbehaving operator's identity, push a config-file update to their nodes treating that operator's messages as silent for subsequent slots, and the byzantine's effective contribution becomes identical to offline — restoring the `Byzantine ≡ Down` guarantee. **The blacklist mechanism is a planned OBFT-family protocol extension** — current OBFT/OBFTR/2abOBFT do not specify it; until added, the byzantine's per-slot grief surface above offline behavior is bounded only by stakers eventually migrating validators away from the cluster.

   The on-wire byzantine-fault evidence ([§Slashing evidence](#slashing-evidence)) informs both (a) staker migration decisions and (b), once the extension lands, the cluster operators' blacklist trigger. Filing a stake-slashing transaction via the SSV contract is a complementary punitive action available where the evidence is cryptographically self-contained, but is not the primary deterrent — the primary deterrent is the economic equivalence to going offline plus the eventual `Byzantine ≡ Down` collapse via blacklisting. See [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the evidence-quality discussion and how it interacts with the blacklist's detection latency.

5. **Coordinated EKM across both keypair shares.** The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log keyed on `(slot, layer, side, value_root)`. OBFT simplifies the EKM coordination model relative to OBFTR(R≥2): no cross-round atomicity, no persistent partial-sig cache for cross-round re-emission, no deterministic re-signing fallback — see [EKM coordination model](#ekm-coordination-model).

6. **Independent V-keypair and IBE-keypair DKGs over the same operator set at the same threshold `qV = qEnc = 2f+1`.** Both DKGs are run once at cluster init with all `n` operators participating; the shared operator set and threshold is what makes Pigeonhole 1's algebra work (`h_σ + h_NR ≤ 2f+1` across the cross-keypair signing log).

### Implications of safety being honest-majority cryptographic, not 100% cryptographic

OBFT's safety holds against an offline-aggregating byzantine *given the protocol rules are correctly enforced by honest operators*. Enforcement happens at two independent layers per operator — the **protocol layer** (operator software implementing the OBFT state machine, deciding when to request σ vs NR) and the **EKM** (slashing-protection log that rejects bad signing requests as defense-in-depth). Together they ensure single-σ-V per (slot, layer) per operator and σ-XOR-NR per layer; these rules reduce the safety proof to pigeonhole arguments on the cluster-wide signed-message set.

An operator whose software actively misbehaves (maliciously compromised, or with compounding protocol+EKM bugs that produce byzantine-equivalent signing behavior) consumes f-budget directly. With `f = 1` byz at `n = 4`, one such operator is tolerated; two violate the trust bound and Pigeonhole arguments can fail. Single-layer bugs (protocol-only or EKM-only) do **not** produce byzantine behavior on their own — the other layer catches them. See [EKM coordination model](#ekm-coordination-model) for the full defense-in-depth analysis.

**This is the same trust posture as QBFT.** QBFT's safety also holds under f-byz with honest-majority correct code paths. A bug in `2f+1` honest operators (e.g., the post-consensus signing path signs both candidates from a split decision, or the prepared-certificate verification accepts conflicting commit certificates) would equally violate QBFT's safety guarantees. Neither protocol is "100% cryptographic" against operator-side software bugs; both rely on operator software correctness for honest operators.

Accordingly, "cryptographic safety" in this spec means: against a partially-synchronous byzantine network adversary plus up to `f` byzantine operators, honest operators following the protocol cannot produce two contradictory outputs. It does not mean: safety against arbitrary bugs in honest operator software.

### Implications of validity-divergence not being recovered (assumption 3)

**This is an OBFT-family limitation, not specific to OBFT.** The same algebraic deadlock applies to OBFTR with the same root cause: per-operator independent validity verdict + leader's Phase-1 σ_V locked + cross-phase exclusivity. OBFT inherits all three. [OBFTR(R≥2)](OBFTR.md) doesn't help either (verdicts are locked at acceptance, so re-flood across rounds doesn't reconcile divergence). [2abOBFT](2abOBFT.md) is the OBFT-family member that recovers this case in-protocol via the Phase-2a observation phase.

If assumption 3 is violated mid-slot — honest verdicts genuinely diverge after Phase 2 emit — OBFT cannot recover within the slot. There is no fresh-V refetch mechanism. The byzantine leader's Phase-1 σ_V is locked; honest who NV cannot switch to σ; cluster deadlocks at L_k or falls through to L_{k+1} (where the same divergence pattern may repeat).

For SSV proposer duty, the host's stabilization workflow (validate parent_root once at acceptance, lock the verdict) is the design's path to satisfying assumption 3. If the host cannot guarantee unanimous validity (e.g., re-orgs are common enough that locking-at-acceptance leads to too many submission rejections), the appropriate fixes are at the protocol-family level, not OBFT-specific:

- **Phase 2a/2b** (see [2abOBFT](2abOBFT.md)). Delays σ-commitment until after a Phase-2a observation phase that lets the cluster converge on a stabilized validity verdict before any operator binds. This is the structural fix at the OBFT family level. Costs +1 RTT per slot.
- **Use a deterministic / finalized parent.** Validity criterion that doesn't depend on each operator's chain view at evaluation time — e.g., parent must be a finalized block (2 epochs old, all operators agree). Eliminates divergence by construction but loses late-MEV (you can only build on finalized parents).
- **QBFT.** Round-changes through with a new leader fetching at the moved head — covers validity-divergence as a side-effect of round-change recovery. Comes with QBFT's own ~2s round-change latency.

These three are the structural options. Smaller mitigations (relax cross-phase exclusivity, allow NV → σ flip via aggregator filtering, etc.) all break safety against an offline-aggregating byzantine — see the Phase 2a/2b discussion at [Path forward](#where-this-came-from) for why.

### Implications of equivocation not being recovered

OBFT does not provide an in-protocol equivocation recovery mechanism. Outcomes split into three classes:

- **σ-quorum reaches at L_0 naturally** (slot succeeds): honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool to qV.
- **NR-quorum reaches at L_0 → fall-through to L_1** (slot succeeds at L_1 if L_1 honest): all 3 honest retained ≥ 2 distinct V's by `T_commit` (typical when byz delivers V's early enough for gossipsub re-flood to spread conflicts before T_commit). All 3 honest emit NR per the equivocation-NR rule, producing qEnc-quorum at L_0; decryption unlocks L_1; σ-quorum at L_1 reaches in the same Phase 3 reconstruction walk.
- **σ-locked split patterns** (slot misses): honest σ-states split (1-1-1 split, 1-1-NR, etc. at f=1 n=4 — different honest σ-locked on different V's before observing equivocation). σ-pools split below qV; NR-pool capped by σ-locked operators below qEnc; no fall-through.

A byzantine controlling delivery timing picks the class. Delivering near end-of-Phase-1 (insufficient re-flood time) reliably engineers the σ-locked split slot-miss outcome. Equivocation evidence is slashable in all cases; the rational-byzantine deterrent (assumption 4) is what makes the slot-miss tolerable across many slots.

The rational-byzantine deterrent (assumption 4) is what makes this a tolerable failure mode in expectation: a byzantine that equivocation-griefs in slot N pays for it from slot N+1 onward via the eventual `Byzantine ≡ Down` collapse (manual blacklist by surviving operators; planned protocol extension) plus staker migration collapsing cluster-wide fee accrual; equivocation-evidence bundles additionally enable stake slashing via the SSV contract. Phase 2a/2b (future improvement) closes the recovery gap without relying on the deterrent. The exposure to equivocation σ-locked splits at OBFT is identical to OBFTR(R≥2) — these are R-invariant patterns; the round-retry machinery doesn't help with them at any R, so dropping rounds doesn't make them worse.

### Implications of the rational-byzantine deterrent (assumption 4)

The deterrent affects *liveness only*, not safety. Pigeonholes 1, 2, 3 hold cryptographically against any byzantine within the f-bound regardless of whether the byzantine is rational — a byzantine willing to absorb full reputation cost (e.g., last-slot-before-exit) cannot violate safety, only grief liveness.

Specifically:

- **Safety unaffected:** No matter how aggressively byzantine operators misbehave (1-1-1 equivocation, fake encrypted-presence, cross-signing), at most one V signature reconstructs cluster-wide per slot. This is a property of the cluster-wide signed-message set under EKM enforcement (assumptions 1, 5).
- **Liveness affected:** A short-horizon byzantine ignoring future-slot consequences may grief more slots than a rational byzantine; each affected slot misses cleanly (no safety violation). The deterrent therefore matters for *expected liveness across many slots*, not for per-slot correctness.

**The deterrent mechanism: SSV's existing offline-operator economics.** Per-validator operator fees on SSV are paid continuously to all cluster operators regardless of per-slot contribution — a byzantine that engineers slot-miss earns the same per-slot fee as an operator who is silently online or completely offline. Operations cost is ~zero (the other operators do the work). Stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters; once enough stakers migrate, the cluster's fee inflow drops to zero across all operators including the byzantine. This is the same mechanism that disciplines a permanently-offline operator. The byzantine's gain per slot is bounded above by what an offline operator would get; their loss (reputation, future cluster invitations, validator migration before the offending slot's fees materialize at scale) is real and persistent across slots.

The protocol surfaces every byzantine fault class as on-wire evidence ([§Slashing evidence](#slashing-evidence)) signed by the offender's own keys, verifiable in isolation by any observer. The evidence informs (a) staker migration decisions and (b) the cluster operators' blacklist trigger (next paragraph). Filing a stake-slashing transaction via the SSV contract is a complementary punitive action available where the evidence is cryptographically self-contained, but it is not the primary deterrent — the primary deterrent is the economic equivalence to going offline plus the eventual `Byzantine ≡ Down` collapse via blacklisting.

**Byzantine ≡ Down in QBFT, significantly worse on latency than Down in OBFT-family — manual blacklist is the equalizer.** QBFT's round-change makes any byzantine deviation functionally indistinguishable from operator silence: round 1 times out, round 2 succeeds with a different leader, byzantine pays the round-1-timeout latency and nothing more. OBFT-family has no round-change escape valve; byzantine grief vectors at L_0 (equivocation σ-locked splits, h_V=1 selective-delivery, fake-encrypted-presence, behavioral σ-refusal) can engineer reliable per-slot slot-miss when the byzantine is L_0 — typically ~25% of slots at f=1 n=4 with uniform leader rotation. The grief above offline behavior is the residual the deterrent must absorb.

The expected operational response is a **manual blacklist**: the surviving `n − f` operators, on observing sufficient evidence of byzantine behavior (whether cryptographically self-contained or behavioral-pattern accumulated across slots), push a config-file update treating the byzantine operator's messages as silent for subsequent slots. The protocol must support this — message-level dropping/discarding by operator identity, plus duty-scheduling that excludes the blacklisted operator's leader rotation — as a planned protocol extension. **Current OBFT/OBFTR/2abOBFT do not specify the blacklist mechanism**; once added, the byzantine's residual grief surface above offline behavior is bounded by detection latency + cluster governance reaction time (the same window that disciplines an offline operator who hasn't yet been migrated away from by stakers).

**Sketch of the planned blacklist mechanism.** Each operator attaches a 2-byte (16-bit) **blacklist bitfield** to their first message in each slot — Phase-1 bundle for layer leaders, Phase-2 onion for non-leaders. Each bit indicates "I locally consider this operator blacklisted"; 16 bits accommodates SSV's largest cluster size (`n ≤ 13`). The bitfield is covered by the carrying message's operator-identity-key auth envelope, so the signal is attributable per `(operator, slot)`. Receivers maintain a per-`(slot, target)` ACK count; once an operator observes **`f+1` ACKs** on any target, they treat the target as silent for the slot's duty — leader rotation skips the target's layer (the K-layer fall-through advances to the next layer), the target's σ/NR partials are ignored, and any Phase-1 bundle from the target is dropped.

The `f+1` threshold is the BFT-liveness minimum (any `f+1` distinct ACKs contains at least one honest agreement by pigeonhole at the f-byz bound); 2f+1 would be the full BFT-quorum strength but slower to activate. The threshold is a deployment tuning knob between activation latency and false-positive resistance against byz bit-flipping to falsely flag honest operators. Blacklist state persists in each operator's local store across slots, so a target blacklisted in slot N stays blacklisted in N+1, N+2, ... unless explicitly rehabilitated.

**Within-slot timing.** Because the bitfield piggybacks on first-broadcast, blacklist convergence happens *during* the slot — typically as Phase-1 bundles propagate. The byz can still grief the slot in which they are first being added to others' blacklists (their L_k turn at slot N can still fire if blacklist consensus has not yet reached `f+1` before their fetch deadline). Effective enforcement is from the *following* slot onward, bounding the byz's per-slot latency-grief above offline behavior to the detection-and-convergence window — single slot for cryptographic-evidence faults (every honest operator flips the bit in their next bitfield), longer for behavioral-pattern faults that need multi-slot accumulation before a bit-flip is justified.

**Evidence quality determines blacklist-trigger latency.** Some fault classes leave self-contained cryptographic proofs that any single observation can blacklist confidently:

| Fault class | Evidence type | Time-to-blacklist |
|---|---|---|
| Equivocation, cross-signing, cross-onion partial-sig equivocation, fake plaintext σ at L_0 | **Cryptographic, self-contained** — a single signed message-pair conclusively demonstrates the action | Single observation; bounded by governance reaction time |
| Fake encrypted-presence at k > 0 | **Cryptographic but conditional** — requires NR-quorum to reach at all prior layers for decryption to unlock; can be sealed by adversarial slot-miss | Conditional on slot progression in subsequent slots |
| Selective-delivery / withholding grief, byzantine σ-refusal coordinated with honest transient flakiness | **Behavioral pattern** — no single signed message proves it; requires aggregating observations across operators / slots | Multi-slot accumulation; risk of false-positive against an honest-but-flaky operator |

For high-evidence-quality faults, the surviving operators can blacklist decisively on a single observed fault. For low-evidence-quality faults, they need patience — wait for the pattern to accumulate enough confidence before acting, or risk blacklisting an honest-but-flaky operator. There is no automated trigger that resolves this trade-off; it's an operator-governance judgment per fault class.

**The deterrent works in expectation across many slots, not per-slot.** Per-slot, a byzantine within the f-bound can grief slot success in various ways (Class B failure modes; see [§Failure modes](#failure-modes)). Across many slots, a byzantine that consistently misbehaves accumulates evidence; the surviving operators eventually blacklist them, and stakers migrate validators away. The "permitted byzantine grief" framing in §Failure modes is therefore conditional on (a) the surviving operators actively monitoring and willing to coordinate the blacklist update, (b) byzantines being economically deterred by the eventual fee collapse — i.e., operating cluster fee accrual exceeds the expected per-slot grief value, (c) the staker migration mechanism functioning at meaningful pace. Where those conditions don't hold (passive small clusters, byzantines on their way out, dysfunctional governance, high-MEV slots where per-slot grief value spikes), the deterrent's effective strength is correspondingly weaker.

**The asymmetry that matters most: the deterrent is weakest where per-slot grief is most damaging.** Looking across the slashing-evidence rules and the failure-mode taxonomy:

- **High evidence-quality faults** (Rules 1–3, 5: equivocation, cross-signing, cross-onion partial-sig equivocation, fake plaintext σ at L_0) are *cryptographically self-contained* — single-observation blacklistable. But these are the faults the protocol *also* defends against effectively at the slot level: equivocation triggers natural recovery in many cases (when 2-of-3 honest happen to σ-commit on the same V), fake plaintext σ is detect-and-reject on the wire by retained-V receivers, etc. **The deterrent is strongest where the per-slot grief is least damaging.**
- **Low evidence-quality faults** (h_V=1 selective-delivery, byzantine σ-refusal coordinated with mesh-flakiness, validity-divergence with byz passivity) are *behavioral pattern* — no single signed message proves the fault; the surviving operators must aggregate observations across operators / slots before blacklisting with confidence. But these are the *most damaging* per-slot grief vectors: they reliably miss the slot at L_0 when an adversarial byz exercises them. **The deterrent is weakest where the per-slot grief is most damaging.**

**The bottom line: the attacks that matter most for adversarial-byz liveness are precisely the ones the deterrent can least credibly punish on a per-slot basis.** This is not a bug in the deterrent — it follows from the structure of which fault classes leave on-wire cryptographic evidence vs which leave only behavioral patterns. It IS, however, the load-bearing reason why bare OBFT (and bare OBFTR(R≥2), since these failures are R-invariant) is exposed to adversarial byz beyond what assumption 4's expected-value framing captures cleanly until both the blacklist extension lands AND coordination latency is short enough to materially bound the grief window. **Phase 2a/2b closes most of the high-grief-severity / low-evidence-quality faults** at the protocol level — specifically 1-1-1 σ-locked equivocation, h_V=1 selective-delivery, and validity-divergence majorities — restoring the `Byzantine ≡ Down` equivalence in-protocol for those classes (no blacklist needed). It does **not** close the **2-1-byz-defect** pattern (byz leader equivocates V/V', verdict-claims σV(V), defects to NR at Phase-2b) — that case slot-misses with Rule-6b behavioral-pattern evidence on the wire (see [docs/2abOBFT.md / 2-1 split](2abOBFT.md#2-1-split)). 2-1-byz-defect is the cost of removing Phase-1 σ_L^V to gain the other recoveries — a structural single-round tradeoff, not a spec gap. So Phase 2a/2b leaves the deterrent to handle (a) the high-evidence-quality faults (already-recoverable, fast-to-blacklist) and (b) the residual 2-1-byz-defect pattern (low-evidence-quality, slow-to-blacklist).

## Protocol

OBFT runs **a single agreement round** per slot: Phase 1 → Phase 2 → Phase 3. Phase 1 is a fresh broadcast (no re-flood across rounds, since there is only one round). The slot's hard wall is the relay submission deadline (`T_relay_cutoff − T_submit`); a slot that does not reach σ-quorum at any layer with enough time to submit is missed.

### Phase 1 — Candidate broadcast

Phase 1 has K per-layer windows (driven by the asymmetric fetch times): `[T_{K-1}, T_{K-1} + Δ_1]` for the deepest backup, then progressively `[T_{K-2}, T_{K-2} + Δ_1]`, ..., ending at `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "OBFT-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates OBFT Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, OBFTR, [2abOBFT](2abOBFT.md), other OBFT message kinds, etc.). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, and check the first-observation timestamp — accept bundles whose first-observation time is in `[slot_start, T_commit]`. Bundles first-observed past `T_commit` are not counted toward σ-quorum at this layer (no late acceptance: each operator commits once at `T_commit` based on what they observed by then). Bundles failing cryptographic-auth are silently dropped. (A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.)
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. A `not-valid` verdict turns the operator's commitment into NV (operationally identical to NR — see "Operator commitments — σ, NR, NV" below).

If a leader `L_k` fails to broadcast at all (or broadcasts so late that its bundle arrives past `T_commit` at every honest receiver), that layer is unavailable; the cluster falls through to deeper layers via NR-quorum. If all K leaders fail, the slot is missed.

**Bundle propagation.** Honest receivers re-flood every bundle via standard gossipsub on first observation. OBFT has no protocol-level second re-flood event for whole Phase-1 bundles (no rounds): cluster-wide reception of `(V, σ_L^V, σ_L^op)` relies on gossipsub's organic propagation completing before `T_commit`. The leader's σ_L^V partial alone is re-broadcast in Phase 2 via every operator's `KindCommit` witness section (see §Phase 2 / Wire format), providing redundancy against σ_L^V-drop but not against V-drop. Honest leaders broadcasting by their per-layer deadline `T_broadcast_max_k = Ls_arrival − B_k` reach all honest within partial-synchrony assumptions for that layer's propagation budget `B_k` (see §Setting).

**Retention bounds.** Retention state for accepted Phase-1 bundles is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **two distinct `(V, σ_{L_k}^V, σ_{L_k}^{op})` tuples** — the first auth-valid bundle observed, plus, if a second auth-valid bundle with a *distinct* `value_root` is later observed, that one (sufficient as leader-equivocation evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Bundles whose cryptographic-auth checks fail are dropped without retention. Retention lifetime: until the operator's local end of Phase 3 (i.e., until reconstruction halts or the slot is declared missed); slot state is then cleared. Memory bound: `O(K · n)` bundles per slot in the worst case (every leader equivocates).

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with honest threshold partials produced in Phase 2 by operators who received the leader's bundle, the cluster reaches `qV` real partials on `V_{L_k}` — closing the byzantine-leader selective-delivery grief under partial synchrony (see "Fault tolerance / Liveness").

**Equivocation handling — detect and slash.** If a participant observes two distinct `σ_V` partials from the same leader at the same slot/layer, that's leader equivocation. The pair of signed bundles is self-contained slashable evidence, gossipped for out-of-band slashing.

Local protocol response, by current state at T_commit:

- **Retained 0 V**: the operator has no V to σ on → NR (silent-leader rule).
- **Retained exactly 1 V** (only one bundle reached this operator before T_commit): σ on that V if host validates; otherwise NV.
- **Retained ≥ 2 distinct V** (equivocation observed pre-T_commit): NR. The operator does not attempt to pick a winner; the slot may still succeed if other honest happened to retain only one V and σ on it (Pigeonhole 2 still ensures at most one V reaches qV cluster-wide).

The leader is required to sign `σ_V` exactly once per slot/layer (refreshes in the host's fetch loop are pre-signing only — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance); any second `σ_V` from the same leader is a protocol violation regardless of intent.

**Equivocation is permitted as a slashable byzantine fault.** OBFT does not provide an in-protocol equivocation recovery mechanism. Some equivocation patterns naturally reach σ-quorum on one V (e.g., 2-of-3 honest σ-commit on the same V plus the leader's σ_L^V on that V = 3 = qV at f=1 n=4) — the slot succeeds in those cases as a side-effect, not via a specific protocol mechanism. Other patterns (1-1-1 split where each honest σ-commits on a different V before observing equivocation, or asymmetric-retention patterns where honest see different V-pairs under byz-controlled delivery order) do not reach σ-quorum and the slot misses.

**Practically, an adversarial byzantine controls which pattern occurs.** A byzantine that times equivocation deliveries near the end of Phase 1 (leaving insufficient time for cross-honest gossipsub re-flood to spread the conflict before T_commit) reliably engineers σ-locked split patterns (1-1-1, etc.) that don't reach qV. The natural-recovery cases (2-1 split where 2 honest happen to σ-commit on the same V) only fire when the byzantine fumbles the timing — delivers V's early enough that re-flood converges honest views before σ-emit. **In expectation, byzantine-leader equivocation slot-misses; the rational-byzantine deterrent (assumption 4) is the practical defense, not natural recovery.** A byzantine indifferent to the deterrent (e.g., last-slot-before-exit) can grief reliably; a byzantine that values future participation pays once and stops.

In all cases, the byzantine leader pays the stake-based slashing penalty — equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained on each pair of conflicting bundles.

The **Phase 2a/2b split** (see [2abOBFT](2abOBFT.md)) (broadcast-only Phase-2a, then σ-emit on a deterministically-chosen V in Phase-2b after Phase-2a observation completes) closes most equivocation patterns (1-1-1 σ-locked splits, all-honest-NR cases, h_V=1) and preserves cryptographic safety AND f-tolerant liveness; the 2-1-byz-defect pattern regresses vs bare OBFT (see [docs/2abOBFT.md / 2-1 split](2abOBFT.md#2-1-split)). Documented as a future improvement, not in current OBFT.

**Operator commitments — σ, NR, NV.** Each operator commits exactly once per (slot, layer) at `T_commit`, based on what they observed by then. Three states:

- **σ (sign-on-V)**: the operator received the leader's bundle by `T_commit`, both protocol-level and application-level checks passed, and the operator did not retain ≥ 2 distinct V's at this layer (no equivocation observed). Materializes as a σ partial in the operator's `KindCommit` message at this layer (or as the leader's Phase-1 σ for the layer's own leader). Once committed, the operator is **σ-locked** at this layer for the entire slot.
- **NR (non-receipt)**: by `T_commit`, the operator did not receive an auth-valid Phase-1 bundle for this layer (the leader is treated as silent from this operator's perspective).
- **NV (non-validity)**: host application returned `not valid` for `V_{L_k}`.

NR and NV are operationally interchangeable on the wire: both materialize as a partial `σ_i^{IBE}(nr_tag_k)` on the layer's NR tag, carried in the operator's `KindCommit` message. The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to as "NR-quorum" throughout). The distinction is local-only diagnostic. References to "NR" elsewhere in this document encompass NR-silent + NV unless stated otherwise.

Equivocation observed pre-T_commit (≥ 2 distinct V's retained) collapses to NR per the rule above — there is no Defer state. The operator emits NR; cross-phase exclusivity locks them out of σ on either V at this layer.

### Phase 2 — Onion broadcast `[T_commit, T_commit + Δ_2]`

Each participant `i` constructs a K-layer onion. Layer 0's σ partial is plaintext; deeper layers' σ partials are wrapped in **chained encryption** — a layer-`k` σ partial is encrypted such that decryption requires NR-quorum at every prior layer (`L_0, ..., L_{k-1}`):

```
layer 0:  σ_i^V(V_{L_0})                                                       # plaintext
layer 1:  E_{nr_tag_0}( σ_i^V(V_{L_1}) )
layer 2:  E_{nr_tag_0}( E_{nr_tag_1}( σ_i^V(V_{L_2}) ) )
...
layer k:  E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( σ_i^V(V_{L_k}) ) ... )
```

where:

- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share (threshold `qV`).
- `E_{nr_tag_k}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc`) such that decryption succeeds iff `qEnc` partials on `nr_tag_k` exist.
- The chain depth at layer `k` is `k`, applied in order outermost-first when constructing (innermost-first when decrypting outer→inner). At K=2 the chain has only one tag (`nr_tag_0`), so chained encryption reduces to single-tag.

**Phase 2 timing — hard cluster-wide deadline.** Each operator emits exactly one `KindCommit` message at `T_commit`. The Phase 2 window `[T_commit, T_commit + Δ_2]` is sized for that message to propagate to all honest peers before Phase 3 reconstruction begins. **`T_commit + Δ_2` is a hard cluster-wide deadline**: it is the moment by which every honest operator's σ/NR commitment must be on the wire and observable cluster-wide. Phase 3 cannot begin until then (the σ/NR pools are not yet stable).

**Δ_2 sizing.** `Δ_2 ≥ 1 BTT` minimum (propagation budget for `KindCommit` messages emitted at `T_commit` to reach all honest by start of Phase 3). **Recommended for production: `Δ_2 = 2 BTT`** to absorb mesh-jitter and per-operator processing variance — one full propagation cycle of slack on top of the P99 propagation budget. At Config A (P99=150ms, δ=50ms): minimum `Δ_2 = 200ms`, recommended `Δ_2 = 400ms`. Concrete tables and downstream timing throughout this document use the recommended sizing.

**Δ_3 sizing.** `Δ_3 ≥ ε_3` (BLS aggregation + IBE decryption walk + certificate construction). At Config A: `Δ_3 ≈ 100ms`. Phase 3 begins when all expected `KindCommit` messages have arrived (i.e., at `T_commit + Δ_2`); reconstruction is local-CPU work. The slot's hard wall is the relay submission deadline `T_relay_cutoff − T_submit`, not a fixed Phase-3-end deadline — a slow operator's reconstruction can spill into submission slack, and `KindCertificate` gossip from a faster peer (see §Final-certificate gossip) lets an operator that hasn't completed local reconstruction submit `(V, S)` directly.

**Wire format: a single auth-wrapped message kind.** Each operator emits exactly one `KindCommit` per (slot, operator) at `T_commit`, carrying:

- The K-layer onion of σ partials (plaintext at L_0, chained-encrypted at deeper layers) for layers where the operator is σ-state.
- NR/NV partials `σ_i^{IBE}(nr_tag_k)` for layers where the operator is NR-state.
- **Leader σ_L^V witness section.** For every Phase-1 bundle the operator has retained at this point (per §Phase 1's retention rules — typically one bundle per layer, two per layer in the equivocation-observed case), a plaintext copy `(layer k, value_root, σ_{L_k}^V(V_{L_k}))` extracted from the bundle. These are byte-for-byte copies of the leader's partial as `i` observed it — **not** new signings by `i` (no EKM event, no new signing obligation, no new cryptographic primitive). The section provides redundancy against Phase-1 bundle drop at peer receivers: a peer that didn't receive the leader's bundle directly can harvest σ_L^V from `i`'s witness section into the layer-`k` σ-pool (subject to the layer's chained-decryption gate when `k > 0`). Bandwidth is small (~96 bytes per σ partial × K × n; ≈ 1.5 KB cluster-wide at K=4, n=4). The mechanism does **not** address V-drop — σ_L^V verifies against `value_root(V)`, so a receiver lacking V cannot use a witnessed σ_L^V; receivers without V locally still rely on gossipsub Phase-1 re-flood for V itself. See [Appendix C](#appendix-c--message-re-broadcast-considerations) for full-bundle re-broadcast that would address V-drop additionally at higher bandwidth cost (optional defensive engineering, not core).

Auth-envelope binding: `(protocol_tag = "OBFT-v1", message_kind = "commit", cluster_id, slot, operator_id i, onion_payload, nr_partials, sigma_L_witnesses)` signed by `i`'s operator-identity key. Emitted at most once per operator per slot. Receivers reject any `KindCommit` whose envelope auth fails verification.

The K-layer onion construction (chained encryption depth `k` at layer `k`) is exactly as defined above.

Each operator includes per layer based on the three-state model from Phase 1:

- **σ-state**: include the (encrypted) σ partial in the onion at this layer.
- **NR-state** (silent-leader, equivocation-observed, or NV): include a partial `σ_i^{IBE}(nr_tag_k)` in the NR-partials section. These IBE partials are the witnesses that unlock the next layer's chained encryption.

**Per-operator commitment is exclusive across phases.** OBFT enforces cross-phase exclusivity. The commitment is *one decision per operator per layer, spanning Phase 1 and Phase 2*:

- An operator who emitted `σ_i^V(V_{L_k})` at layer `k` on any value `V` has σ-side committed at this layer; they may **not** subsequently broadcast an NR/NV partial on `nr_tag_k`. They may also **not** σ on a different `V'` at the same `(slot, layer)` — see Pigeonhole 2 in "Fault tolerance / Safety".
- An operator who emitted an NR/NV partial on `nr_tag_k` has NR-side committed at this layer; they may **not** subsequently emit σ on any V at L_k.
- The layer-`k` **leader**'s Phase-1 σ_V counts as their σ-side commitment at layer `k`. They may not subsequently emit NR/NV on `nr_tag_k`.
- Across layers, commitments are **independent**: an operator's σ-or-NR commitment at layer `k` does not constrain their commitment at layer `j ≠ k`. Hedging across layers is preserved (an operator may σ at multiple layers if they validated multiple V's).

EKM enforces these exclusivities cryptographically (slashing-protection log keyed on `(slot, layer, commitment-side)`); see "Preconditions on the host application / Slashing-protection scope". OBFT's EKM does **not** need cross-round atomicity (no rounds), persistent partial-sig caching for cross-round re-emission, or deterministic re-signing fallback — see [EKM coordination model](#ekm-coordination-model) for the simplified rules.

A byzantine operator that publishes both σ and NR on the same `(slot, layer)` is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For each layer's leader specifically, "cross-signing" includes any Phase-1 σ + later NR pair on the same slot.

### Phase 3 — Local decryption and reconstruction (from `T_commit + Δ_2`)

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches on some V (output produced), or NR-quorum reaches (advance to next layer), or neither (slot misses).

**Δ_3 sizing.** Phase 3 is purely local CPU work — BLS aggregation, IBE decryption walk across K layers, certificate construction. So `Δ_3 ≥ ε_3` ≈ 100ms at Config A. (`KindCommit` propagation is already covered by `Δ_2`; by the start of Phase 3, all expected commits have arrived at all honest receivers.)

```
L_C = 0                                        # current frontier; advances per loop iteration
sigs[k] for k in [0, K)                        # σ pools, indexed by layer (initially empty)
nrs[k]  for k in [0, K)                        # NR pools, indexed by nr_tag_k (initially empty)

for k in [0, K):
    # Aggregate σ pool at layer k.
    sigs[k] = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
            ∪ {σ_{L_k}^V(V_{L_k}) from peer KindCommit witness sections at layer k, if valid}
            ∪ {σ_j^V(V) from received layer-k onion contents on any value V}
              (decrypted via accumulated NR-decryption keys at layers 0..k-1, if k > 0)
            # deduplicated per operator: leader's Phase-1 σ, witness-section
            # copies of σ_L^V (which collapse to identical bytes across peer
            # KindCommits), and onion σ from the same operator all collapse
            # to one partial.
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

**Slot's hard wall: relay submission deadline.** Reconstruction runs from `T_commit + Δ_2` until either σ-quorum reaches (output `V`, halt) or the operator's local relay-submission deadline (`T_relay_cutoff − T_submit`) is reached without a result (slot misses for that operator). Expected reconstruction completion is `T_commit + Δ_2 + ε_3` (`≈ 1 BTT + 100ms` after `T_commit` at Config A); a slow operator overrunning this can still complete inside the submission slack `[T_commit + Δ_2 + ε_3, T_relay_cutoff − T_submit]`. A faster peer's `KindCertificate` broadcast (see §Final-certificate gossip) lets an operator that hasn't completed local reconstruction submit `(V, S)` directly.

**Re-running on late `KindCommit` arrivals.** Under nominal partial synchrony, all `KindCommit` messages arrive within `Δ_2` and the reconstruction walk above runs once on the stable snapshot at `T_commit + Δ_2`. If `KindCommit` messages arrive late (out-of-envelope, after `T_commit + Δ_2`), the operator may re-run the reconstruction walk to incorporate the new partials. This can salvage slots where:

- A late σ partial pushes σ-pool past `qV` at some layer that didn't reach on the initial walk → output `V` at that layer.
- A late NR partial pushes NR-pool past `qEnc` at a layer that previously had NR-pool short of `qEnc` → derive the layer-`k` decryption key, unlock chained decryption for layer `k+1`'s σ partials, advance the walk past `k`.

Pigeonhole semantics still hold (at most one `V` reconstructs cluster-wide regardless of timing), so re-running is safe. Note this only applies to late `KindCommit` messages; Phase-1 *bundles* arriving after `T_commit` are explicitly rejected per the Phase-1 acceptance rule and cannot be incorporated.

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s `KindCommit` at decryption time treats `j` as not having contributed at any layer: no σ partial, no NR partial. Standard threshold cryptography — only signed messages count. Within Phase 2's hard cluster-wide deadline (`Δ_2 ≥ 1 BTT`), gossipsub propagation is expected to deliver all honest `KindCommit` messages to all honest receivers before Phase 3 starts.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within the slot's relay-submission deadline (see "Fault tolerance / Liveness"). If more than `f` operators are offline or byzantine combined, neither σ nor NR quorums reach their thresholds and the slot is missed. Within the f-bound, OBFT covers in-envelope cases via K-layer fall-through: healthy path, silent-leader fall-through, multi-leader fall-through (sequential within Phase 3 reconstruction). View-divergence cases — equivocation σ-locked splits and host-validity divergence — are explicitly out of recovery scope (some splits succeed naturally as a side-effect of leader-σ-V counting; rest slot-miss). Asymmetric propagation past `T_commit` is also out of recovery scope at OBFT (a deeper backup whose bundle did propagate in time is the only recovery path). See [Assumptions and implications](#assumptions-and-implications).

### Slot structure

OBFT runs a single agreement round per slot. The slot proceeds as follows:

1. **Phase 1**: each leader `L_k` broadcasts its Phase-1 bundle by its per-layer deadline `T_broadcast_max_k = Ls_arrival − B_k` (with `B_k` ordered `B_0 < B_1 < ... < B_{K-1}` so deeper layers have wider propagation budgets — see §Setting). Receivers accept bundles first-observed in `[slot_start, T_commit]`.
2. **Phase 2** `[T_commit, T_commit + Δ_2]`: each operator emits a single `KindCommit` message at `T_commit` carrying their per-layer σ partials (for σ-state layers) and NR partials (for NR-state layers). The window is sized for `KindCommit` propagation to all honest peers.
3. **Phase 3** (from `T_commit + Δ_2`): each operator runs the K-layer reconstruction walk. If σ-quorum reaches on some V at any layer, output the V; halt. If NR-quorum reaches up to some layer `L_C < K`, advance L_C and continue the walk. If neither σ-quorum nor NR-quorum advance unlock at any layer, the slot misses (re-running may incorporate late `KindCommit` arrivals — see §Phase 3).

**Slot timing**: Phase 1 fetch occupies `[slot_start, T_commit]`. The slot's consensus budget (Phase 2 + Phase 3) is `Δ_2 + Δ_3 ≈ 1 BTT + ε_3` ≈ 250ms at Config A; consensus is expected to complete at `T_commit + Δ_2 + Δ_3`, leaving the rest of the slot as submission slack to `T_relay_cutoff`.

## Preconditions on the host application

OBFT is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at the moment the operator considers committing σ to it. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be signed (the σ_V locks it). See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` before committing σ. A `not-valid` verdict turns the operator's commitment into NV (see "Phase 1 / Operator commitments — σ, NR, NV").

The protocol-level checks (cryptographic auth, envelope re-derivation, timing cutoff at `T_commit`) are protocol-internal and do not depend on the application.

For SSV's Ethereum proposer duty, application-level checks include slot match, proposer index match, fork/domain match, parent root match against the operator's view of the head, relay metadata validity, doppelganger / slashing-protection checks, and block encoding well-formedness. See the Application section for details. The protocol body does not enumerate or interpret these — they're host concerns.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot (potentially up to K, one per layer they validated). Constraints per (slot, layer):

- **Each layer's leader signs σ_V exactly once per (slot, layer)** — on the final V they commit to, after any pre-signing refreshes during the host's fetch loop. Refreshes update V via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this single-σ-V-per-(slot, layer) constraint cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational workflow.
- **Each operator commits σ-or-NR exclusively per (slot, layer), across phases.** Honest who include σ on any V at layer `k` may not subsequently broadcast NR/NV on `nr_tag_k` AND may not σ on a different V' at the same layer (single-σ-V per operator per layer); honest who broadcast NR/NV may not subsequently include σ at L_k. Each layer's leader's Phase-1 σ counts as their σ-side commitment — they cannot emit NR/NV for that layer. EKM enforces this cross-phase + single-V exclusivity by coordinating across the operator's V-signing and IBE-signing shares (distinct keys, but slashing-protection log keys on (slot, layer)): an NR/NV partial on `nr_tag_k` is rejected if the same operator has previously signed σ at L_k, and vice versa; a σ partial on V' at L_k is rejected if the same operator has previously signed σ on V ≠ V' at L_k. Pigeonhole 1 and 2 below rely on these rules.
- Every operator signs each layer's V_{L_k} they consider valid (host returns `valid`) in their Phase-2 onion, provided they retained exactly one V at that layer (no equivocation observed). Operators with no V retained at T_commit emit NR; operators with ≥ 2 V's retained also emit NR. See "Phase 1 / Equivocation handling — detect and slash".

EKM/slashing-protection must permit the operator's per-layer Phase-2 σ signings (one σ per layer per slot) plus the leader's Phase-1 σ_V (exactly one per slot/layer the operator is leading), without flagging duplicates — the cluster's safety property collapses these to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating points are **candidate signing** (Phase-1 leader and Phase-2 onion alike) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`).

### EKM coordination model

The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log. Production EKMs (Web3Signer, Dirk, etc.) typically expose per-key signing checks; OBFT requires a coordination layer above them.

**Log schema.** One row per signing event: `(slot, layer, side, value_root)` where `side ∈ {"σ", "NR"}`; `value_root` is set on σ-side entries, null on NR-side. No round dimension (single-round protocol).

**Per-request checks.**

- **Sign σ on V at (slot, layer)** (V-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "σ", value_root(V))`. The single-σ-V rule means a second signing attempt with `V' ≠ V` at the same `(slot, layer)` is rejected even though the side matches — the existing row already contains `value_root(V)`.
- **Sign NR on `nr_tag_k` at (slot, layer)** (IBE-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "NR", null)`.

**Simplifications relative to OBFTR(R≥2).** OBFT drops three EKM concerns that OBFTR requires for cross-round operation:

- **No cross-round atomicity.** OBFTR(R≥2) requires sign-and-log to be atomic across both shares (V-share + IBE-share) so that a partial signed in round 1 is correctly recognized in round 2. OBFT has no rounds, so atomicity collapses to standard per-request transactional behavior.
- **No persistent partial-sig cache.** OBFTR(R≥2) caches σ partials so they can be re-emitted in rounds 2..R without re-signing; the cache must survive operator restarts. OBFT has no re-emission, no cache requirement.
- **No deterministic re-signing fallback.** OBFTR(R≥2) needs a fallback if the cached partial is lost mid-slot (allow re-signing if the log row matches the same `(slot, layer, side, value_root)`). OBFT has no re-signing path; the slot's σ-partial is signed exactly once per (slot, layer) per operator and never reused across the protocol.

**Implementation paths.** (a) A custom EKM that maintains the unified log natively across both keypair shares. (b) A coordinator component in the SSV operator process that wraps two standard EKMs (one per keypair share) and enforces the cross-keypair rules in the operator's slashing-protection database — a single transactional log, two keypair-specific signing-request handlers reading and writing it. The coordinator is **simpler** than the OBFTR-equivalent: it requires only the unified log to be transactionally consistent across both shares for the **single signing event per (slot, layer)**; no atomicity-spanning-rounds, no persistence-of-cached-partials, no deterministic-re-signing-fallback. Path (b) is the path SSV will most likely take.

**EKM correctness is defense-in-depth for safety, not the sole enforcement.** OBFT's safety (Pigeonholes 1 and 2) holds when honest operators correctly implement the protocol rules — single-σ-V per (slot, layer) per operator, σ-XOR-NR per layer. The **protocol layer** (operator software implementing the OBFT state machine) is the primary enforcement point: it determines when σ vs NR is requested from the EKM in the first place. The **EKM** is a catch-net: it rejects signing requests that violate the slashing-protection invariants, providing defense-in-depth even if the protocol layer is buggy.

For a single honest operator to produce safety-violating behavior (e.g., σ on V and σ on V' at the same `(slot, layer)`), **both layers must be buggy in compounding ways**: the protocol layer must request the second σ (violation of σ-eligibility logic) AND the EKM must fail to reject it (violation of slashing-protection lookup or atomicity). A single-layer bug typically does not break safety:

- Protocol-layer bug only: the EKM rejects the bad request; no double-sign emitted on the wire.
- EKM-layer bug only: the protocol layer doesn't ask for double-signing, so the EKM bug is never exercised.

Cluster-wide safety violation (Pigeonhole 2 producing two qV-quorums on different V's) requires aggregating these single-operator violations to reach `2 · qV = 4f+2` partials across two V's. At `f = 1, n = 4`, one byzantine operator contributes ≤ 1 partial per V (≤ 2 total); three correct honest contribute exactly 3 partials total (single-σ-V each); sum 5 < 6 = 2 · qV. The minimum safety-violating configuration is therefore **one byzantine operator plus one honest operator with compounding protocol+EKM bugs** — together producing the missing partial. This is two misbehaving operators total, exceeding the `f = 1` trust budget. Single-layer bugs alone are tolerated; safety requires both layers to be correct on at least `n − f = 3` operators.

**Trust posture is the same as QBFT.** Both protocols rely on honest-majority correct implementation of the protocol logic *plus* correct slashing-protection — neither is "100% cryptographic" against operator-side software bugs (see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic)). The difference is in the slashing-protection layer's maturity: QBFT's per-key slashing-protection (Web3Signer, EIP-3076 interchange format) has decade-of-production hardening; the OBFT coordinator is novel, so reaching comparable defense-in-depth robustness requires deliberate engineering investment in (a) test coverage on cross-keypair atomicity for the single signing event per (slot, layer), (b) fault-injection testing of the operator-restart scenario, (c) optionally operational margin via larger `n` (e.g., `n ≥ 5` keeps `f = 1` while expanding the bug-budget headroom). OBFT's smaller surface (no cross-round atomicity, no cache persistence, no re-signing fallback) means the coordinator is closer to an EIP-3076-style per-key extension than OBFTR's bigger novel coordinator.

**Summary of EKM failure modes.** A **maliciously compromised** EKM (signs requests outside protocol rules, or generates signatures the protocol layer didn't request) is byzantine-equivalent and directly consumes f-budget. A **passively buggy** EKM (fails to reject bad requests but doesn't generate signatures on its own) requires the protocol layer to also have a compounding bug for safety-violating behavior to actually occur — see the defense-in-depth analysis above. In both cases, the cluster's overall trust posture follows the standard "honest-majority cryptographic" framing — see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic).

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model. Operational rules that handle specific faults (equivocation detect-and-slash, cross-signing detection, head-change refresh) are described inline in the relevant Phase sections; the analyses of *what those rules buy* and *under what conditions* live here.

### Trust model

- **Byzantine bound `f`** with cluster size `n = 3f+1` (the BFT-tight setting; see [§Assumed / Standard BFT trust bound at the tight setting](#assumed)): up to `f` operators may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, etc.). Exactly `2f+1` honest. Running example: `n = 4, f = 1` (3 honest guaranteed).
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `P99` (propagation P99/P999) and clock skew `δ`. Per-layer leader broadcast deadlines `T_broadcast_max_k = Ls_arrival − B_k` (with `B_k` increasing monotonically from primary to deepest backup — see §Setting); bundles first-observed past `T_commit` are not counted. Phase 2's `T_commit + Δ_2` is a **hard cluster-wide deadline**; Phase 3 has no fixed end — reconstruction runs until σ-quorum reaches or the relay-submission deadline forces termination. Late `KindCommit` arrivals can be incorporated by re-running the reconstruction walk (see §Phase 3). Safety holds against arbitrary network adversaries; only liveness depends on synchrony.

  **OBFT's per-layer absorption is staggered**: `B_k` for layer `L_k`, with `B_0 = 0.5 BTT = 100ms` for the primary up to `B_{K-1} = 5 BTT = 1000ms` for the deepest backup at Config A K=4 (relative to `Ls_arrival`; see [§Setting](#setting)). Bundles arriving past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum at this layer; the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time (deeper layers' wider `B_k` means slower propagation is still absorbed there). Multi-round OBFTR (R ≥ 2) extends absorption further via cross-round retention with re-flood; OBFT trades that for spec simplicity at small clusters where asymmetric propagation past the deepest layer's budget is rare.

### Safety (cryptographic, honest-majority)

**Claim:** at most one full `V` signature is ever produced per OBFT instance per slot — across any layer, on any value, across any combination of σ sources — cluster-wide, against an offline-aggregating byzantine within the f-bound, regardless of which honest aggregation rules are followed. (See [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic) for what this scope does and doesn't cover.)

The proof rests on three pigeonhole arguments — Pigeonholes 1 and 2 (single-layer) plus Pigeonhole 3 (chained encryption at `K > 2`). All three are properties of cluster-wide signed messages, enforced cryptographically by [EKM coordination model](#ekm-coordination-model) rules at the operator level — no honest-aggregation rule required.

The pool definitions used in the arguments below:

- **σ-pool on V at L_k**: `{σ_i^V(V) partials at L_k}`, deduplicated per operator. Includes the leader's Phase-1 σ_V when valid. Once a partial is emitted, it stays on the wire — no "revocation" semantics.
- **NR-pool at L_k**: `{σ_i^{IBE}(nr_tag_k) partials}`, deduplicated per operator.

**Pigeonhole 1 — σ-vs-NR at the same layer.** σ-quorum on `V` (any V) at `L_k` and NR-quorum on `nr_tag_k` cannot both reach.

- σ-quorum on V: `h_σ + byz_σ ≥ qV = 2f+1` (where h_σ counts honest with σ partials on V at L_k from any phase, deduplicated per operator).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Cross-phase exclusivity (per "Slashing-protection scope"): `h_σ + h_NR ≤ n − f = 2f+1` (equality at `n = 3f+1`). Each honest commits σ-or-NR per layer at most once, EKM-enforced.
- **Leader-counting.** If the layer's leader is honest, their Phase-1 σ_V partial counts toward `h_σ` for the V they signed; cross-phase exclusivity then forbids them from emitting NR/NV on `nr_tag_k`. If the leader is byzantine and equivocates, each per-V partial they publish counts toward `byz_σ_V` for that V (capped at 1 per byz per V by deduplication).
- Byzantine cross-signing: `byz_σ + byz_NR ≤ 2f` (each byz contributes at most 1 to each pool).
- If both quorums reached: `h_σ + h_NR ≥ 4f+2 − 2f = 2f+2`. But `h_σ + h_NR ≤ 2f+1`. Contradiction. ∎

**Pigeonhole 2 — two σ-quorums on different values at the same layer.** Two distinct `V`'s cannot both reach σ-quorum at the same layer (e.g., via leader equivocation that some honest σ-commit on early before observing evidence):

- `(h_σ_V + h_σ_V') + (byz_σ_V + byz_σ_V') ≥ 2 · qV = 2(2f+1)`.
- Honest sign at most one V per layer (single-σ-V exclusivity, EKM-enforced — see "Slashing-protection scope"): `h_σ_V + h_σ_V' ≤ 2f+1`. The layer's leader counts here: they sign σ_V exactly once per (slot, layer), contributing to one V's pool.
- Byzantine can sign multiple V's: `byz_σ_V + byz_σ_V' ≤ 2f`.
- Bound: `(2f+1) + 2f = 4f+1 < 4f+2`. Contradiction. ∎

This is the key safety constraint underlying OBFTR's "permit equivocation, slot-miss on view-divergence" framing: regardless of which V's honest σ-commit on under equivocation, at most one V can reach qV cluster-wide. There is no two-output safety failure even when honest operators split across V's; the cluster either reaches qV on a single V (some patterns recover naturally) or no V reaches qV (slot misses).

**Pigeonhole 3 — cross-layer safety under chained encryption.** Two distinct V signatures (V_k and V_{k+m} for any `m ≥ 1`) cannot both be reconstructed cluster-wide. Proof by induction on `m`, applying Pigeonhole 1 at every L_j with `j ∈ [k, k+m−1]`.

- *Decryption requirement.* V_{k+m} σ partials at L_{k+m} are encrypted under `nr_tag_k ∧ nr_tag_{k+1} ∧ … ∧ nr_tag_{k+m−1}`. Decryption requires NR-quorum on every `nr_tag_j` for `j ∈ [k, k+m−1]` (chained-IBE oracle).
- *Inductive step.* For each such `j`, Pigeonhole 1 applied at L_j gives: σ-quorum at L_j ⇒ NR-quorum at L_j does not reach. Therefore if V_k σ-quorum reaches at L_k, NR-quorum at L_k fails, the chain at L_k stays sealed, and V_{k+m}'s σ partials are inaccessible. The induction proceeds at every `j` from `k` to `k+m−1`.
- *Symmetric direction.* If V_{k+m} reconstructs, NR-quorum at L_k must have reached (chained-decryption requirement), so by Pigeonhole 1 σ-quorum at L_k did not reach, so V_k does not reconstruct. ∎

Applied to every pair of layers, at most one V signature reconstructs cluster-wide across all K layers.

**Cryptographic primitive — chained IBE.** Layer-`k` σ partials are encrypted under `nr_tag_0 ∧ nr_tag_1 ∧ ... ∧ nr_tag_{k-1}`. Decryption requires NR-quorum at each prior layer. Implementation: each level of the chain is a standard threshold-IBE ciphertext (drand/tlock) using `nr_tag_j` as the tag. At K=2 the chain has only one level (single tag `nr_tag_0`); at K=3 there are two levels nested; etc.

The arguments above apply symmetrically to all K layers. **None of the proofs depends on honest operators excluding cross-signers from their aggregation** — they're properties of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule. Cross-phase exclusivity (σ XOR NR per layer) and single-σ-V (one V per operator per layer) are enforced cryptographically by EKM at signing time, not by aggregator-side filtering.

### Liveness (synchrony-conditional)

OBFT's liveness is **partial-synchrony-conditional within the slot's relay-submission deadline** — the protocol's slot budget. Bundles arriving past `T_commit` at any honest receiver are not counted toward σ-quorum at that layer; the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time. View-divergence (equivocation, validity-divergence) is NOT recovered by the protocol; some splits succeed naturally as a side-effect of leader-σ-V counting toward whichever V reaches qV first, others slot-miss. Equivocation cases are slashable (leader pays the stake-based penalty); validity-divergence is not attributable.

If propagation between leader L_k's broadcast and any honest receiver's first-observation stays bounded by that layer's per-layer budget `B_k` (`B_0 = 0.5 BTT` for the primary up to `B_{K-1} = 5 BTT` for the deepest backup at K=4 Config A; see §Setting), the protocol terminates with a V signature on some layer (the first layer where σ-quorum reaches under cluster-wide V receipt by `Ls_arrival`, or the deepest layer reachable via NR fall-through with a valid backup leader). If propagation exceeds `B_k` for layer L_k specifically, the cluster falls through to a deeper backup whose own `B_{k+1}` is wider. If all K layers fail to propagate in time (real propagation > `B_{K-1}` at the deepest layer), the slot misses. **Safety holds in either case.**

**Best case (healthy at L_0)**: all honest receive V_{L_0} within `1 BTT`; all 3 honest σ-emit + leader's Phase-1 σ = 4 ≥ qV. Slot succeeds in 2 RTTs (Phase 1 + Phase 2).

**Asymmetric propagation past `T_commit`**: not recovered within-slot at the layer it affects. Honest who got V before T_commit σ-emit; honest who got V after T_commit treat the leader as silent and NR. If the σ-pool reaches qV (e.g., 2-of-3 honest + leader's σ_L^V), the slot still succeeds at this layer. If not, the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time at all honest. This is a deliberate trade-off: OBFT optimizes for spec simplicity at small clusters where asymmetric propagation is rare; deployments needing within-slot partition recovery should use [OBFTR(R≥2)](OBFTR.md).

**Equivocation handling (no protocol-level recovery; outcome depends on byzantine timing)**: byzantine `L_0` equivocates, broadcasting distinct V's selectively. Case analysis at `f = 1, n = 4` (3 honest A, B, C; byz D = leader):

Recovered via σ-quorum at L_0 (when honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool):

- **D delivers V to {A, B}, V' to {C}.** σ-pool on V = A + B + D's σ_L^V(V) = 3 = qV. σ-quorum reaches. Slot outputs V.
- **D delivers V to {A}, V' to {B, C}.** σ-pool on V' = B + C + D's σ_L^V(V') = 3 = qV. Reaches. Slot outputs V'.

Recovered via NR-quorum at L_0 → fall-through to L_1 (when all honest retain ≥ 2 distinct V's by `T_commit` and emit NR per the equivocation-NR rule):

- **All-honest-NR outcome (byz delivers V's early enough that re-flood spreads conflicts before T_commit).** Each honest retains ≥ 2 distinct V's by `T_commit` → all 3 emit NR. σ-pools at L_0 ≤ byz partials per V < qV. NR-pool: 3 honest + 0 byz (σ-locked from Phase 1) = 3 = qEnc → NR-quorum reaches at L_0 → in the same Phase 3 reconstruction walk, advance to L_1; if L_1 honest, σ-quorum at L_1 reaches and slot succeeds at L_1.

Not recovered — slot misses at L_0 with no fall-through (σ-locked split patterns; slashable):

- **D delivers V to {A}, V' to {B}, both to C (or neither).** A σ-locks on V; B σ-locks on V'; C either retains both (NR per equivocation rule) or has nothing (NR per silent-leader rule). σ-pool on V = A + leader = 2 < qV. σ-pool on V' = B + leader = 2 < qV. NR-pool = 1 (C) < qEnc → no fall-through. Slot misses.
- **D delivers distinct V_1 to A, V_2 to B, V_3 to C (1-1-1 split).** Each honest σ-commits on their V_i on receipt before observing equivocation. All 3 honest σ-locked on different V's. σ-pool on each V_i = 1 honest + leader's σ_L^V = 2 < qV. NR-pool = 0 (all σ-locked, cross-phase exclusivity). Slot misses.

**Byzantine timing controls which class fires — and an *adversarial* byzantine reliably picks the slot-miss class.** Delivering V's early in Phase 1 leaves time for gossipsub re-flood to spread conflicts → all-honest-NR outcome → slot succeeds at L_1. Delivering near end-of-Phase-1 leaves insufficient re-flood time → σ-locked split outcome → slot misses at L_0. **In expectation against an adversarial byz primary, these patterns slot-miss reliably.** The rational-byzantine deterrent (assumption 4) is what makes this tolerable across many slots — but the *evidence quality* for these patterns is the *behavioral* class (not the cryptographically-self-contained class), so single-observation slashing is not credible (see [§Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4)). Practical effect: byz can grief many slots before the pattern accumulates enough confidence for honest operators to act. (This exposure is identical to OBFTR(R≥2) — these are R-invariant patterns; round-machinery does not help.)

In all cases, Pigeonhole 2 ensures at most one V can reach qV cluster-wide regardless of how honest σ-emissions split — there is no two-output safety violation. The **Phase 2a/2b split** (see [2abOBFT](2abOBFT.md)) recovers most equivocation patterns at +1 RTT cost (1-1-1 splits, all-honest-NR, h_V=1) but regresses on 2-1-byz-defect (see [docs/2abOBFT.md / 2-1 split](2abOBFT.md#2-1-split)); documented as future improvement, not in current OBFT.

**Multi-failure fall-through (K ≥ 3)**: with `K > 2` layers, the cluster can fall through past multiple silent or non-validating layers **within Phase 3's single reconstruction walk** — the walk processes layers sequentially using local decryption (no per-layer RTT). E.g., at K=4 with L_0, L_1, L_2 all silent: NR-quorum reaches at L_0 in Phase 2 (silent-leader rule fires for all 3 honest cluster-wide); same at L_1 and L_2; σ-quorum at L_3 if L_3 leader honest. All happens in Phase 2 + Phase 3 windows. Recovery scope at K layers is achieved without round-change overhead — a structural difference from QBFT (which round-changes once per failed leader). At K = n = 4, every cluster member is a leader exactly once; pigeonhole guarantees ≥3 honest leaders at f=1, providing maximum K-fall-through depth within a single round.

**Adversarial scheduling within partial synchrony**: the network adversary can delay messages by up to `1 BTT`.

- *Safety unaffected.* Pigeonholes 1, 2, 3 are properties of the cluster-wide signed-message set, not of arrival times.
- *Liveness — adversary delays V to ≤ 1 honest past `T_commit`.* The other 2 honest σ-emit on time; σ-pool = 2 + leader = 3 = qV. **Quorum reaches without the delayed operator.**
- *Liveness — adversary delays V to ≥ 2 honest past `T_commit`.* σ-pool < qV at this layer; cluster falls through to a deeper backup whose bundle did propagate in time.

For wider adversarial-scheduling tolerance (delays beyond `B_{K-1}` at the deepest layer — i.e., 5 BTT at K=4 Config A — or recovery at L_0 specifically preserving MEV), use [OBFTR(R=2)](OBFTR.md).

### Liveness comparison: OBFT vs OBFTR(R=2) vs QBFT

The table below puts OBFT, OBFTR(R=2), and QBFT side-by-side per scenario at the standard SSV proposer-duty configuration (n=4, f=1, K=4, ~4s relay cutoff). Timing assumes the SSV proposer-duty operating point (`BTT = 200ms` = P99=150ms + δ=50ms; staggered K=4 with `B_3 = 5 BTT = 1000ms` deepest-layer absorption — see [Timing budget](#timing-budget)). For QBFT, `RT = 2s` per round-change is SSV's current production tuning at minimum sizing (4 BTT per round).

| Scenario | OBFT outcome | OBFTR(R=2) outcome | QBFT outcome |
|---|---|---|---|
| Healthy (all honest receive V_{L_0}) | σ-quorum reaches in 2 RTTs. ✓ at L_0 in ~600ms (3 BTT: broadcast slack + Phase 2 + Phase 3 local CPU). | Same. ✓ at L_0 in ~1000ms (5 BTT). | PROPOSE→PREPARE→COMMIT (3 RTTs) + post-consensus (1 RTT). ~800ms (4 BTT minimum sizing). ✓ |
| Byzantine leader silent | 0 σ-emits → all honest NR (silent-leader rule) → NR-quorum at L_0 reaches in Phase 2 → automatic fall-through to L_1 in Phase 3 walk; σ-quorum at L_1 if leader honest. ✓ in ~600ms. | Same. ✓ in ~1000ms. | Round 1: no PROPOSE arrives; round timeout (~2s). Round 2: new leader proposes; succeeds in ~800ms. ✓ in ~2.8s. |
| Asymmetric propagation (≤1 of 3 honest miss V at T_commit) | Other 2 honest σ-emit on time; σ-pool = 2 + leader = qV. ✓ at L_0 in ~600ms. The miss-honest's NR partial is unused. | Same (within OBFT's absorption). ✓ in ~1000ms. | Round 1: PREPARE-pool short; timeout (~2s). Round 2: re-fetch + propose; succeeds in ~800ms. ✓ in ~2.8s. |
| Asymmetric propagation (≥2 of 3 honest miss V at T_commit) | σ-pool < qV at L_0; cluster falls through to L_1 (whose bundle did propagate in time). ✓ at L_1 in ~600ms (assuming L_1 honest and propagation OK). | Within OBFTR(R=2)'s wider absorption: round 2 re-flood may deliver V to the miss-honest; σ-quorum at L_0 reaches in round 2. ✓ in ~1.8s. | Round 1: timeout. Round 2: new leader; succeeds in ~800ms. ✓ in ~2.8s. |
| Byzantine leader equivocates, 2-1 split where one V already has 2 honest + leader-σ_L^V | σ-pool on the 2-honest-V = 2 + leader = 3 = qV; reaches naturally. ✓ in ~600ms. | Same. ✓ in ~1000ms. | Round 1: PREPARE-pool split; timeout. Round 2: new leader proposes; succeeds. ✓ in ~2.8s. |
| Byzantine leader equivocates, σ-locked split pattern (1-1-1, etc.) | σ-pools split below qV; NR-pool below qEnc (σ-locked operators can't NR). **✗ slot misses at L_0;** no fall-through. Equivocation slashable. | Same exposure (R-invariant). ✗ slot misses. Equivocation slashable. | Round 1: PREPARE split; timeout. Round 2: new leader proposes a fresh V; honest converge; succeeds. ✓ in ~2.8s. **QBFT recovers what OBFT/OBFTR don't** via "new leader proposes fresh V" mode. |
| Byzantine leader equivocates, all-honest-NR outcome (byz delivers V's early; re-flood spreads conflicts before T_commit) | All 3 honest retained ≥ 2 V's by T_commit → all NR per equivocation rule → NR-quorum at L_0 → fall-through to L_1 in Phase 3 walk; if L_1 honest, ✓ at L_1 in ~600ms. Equivocation slashable. | Same recovery via Round-R force-NR. ✓ in ~1000ms (round 1) or ~1.8s (round 2). | Same recovery as σ-locked split row: round-2 new leader proposes fresh V. ✓ in ~2.8s. |
| Multi-failure fall-through (multiple silent leaders) | At K=4 with L_0, L_1, L_2 silent: NR-quorum reaches at each in Phase 2; Phase 3's walk decrypts down to L_3; σ-quorum at L_3 if honest. **All in single Phase 2 + Phase 3 windows** (Phase 3 ε_3 grows with K-layer decryption walks). ✓ in ~1000ms (3 BTT consensus + ~400ms ε_3 × K). | Same. ✓ in ~1000ms. | Round 1: leader 1 silent → timeout (~2s). Round 2: leader 2 silent → timeout (~2s). Round 3: leader 3 silent → timeout (~2s). Round 4: succeeds. ✓ in ~9s — past 4s cutoff. ✗ for proposer duty. **OBFT's K-layer parallel fall-through beats QBFT's serial round-change**. |
| Host-validity divergence (head-change mid-slot, strict host) | Out of scope (assumption 3 — host stabilizes verdict at Phase-1 acceptance). Same as OBFTR(R=2). | Same. | Round 1: validators with stale head don't PREPARE; timeout. Round 2: new leader fetches at moved head; if quorum agrees, succeeds. ✓ in ~2.8s. **QBFT recovers what OBFT-family doesn't** via fresh-V refetch on round-change. |
| Adversarial scheduling — adversary delays V at all K layers beyond their respective per-layer budgets | **Out of envelope** (Class A). ✗ Slot misses. The deepest layer's `B_{K-1} = 5 BTT = 1000ms` at K=4 SSV operating point is the cluster-wide tolerance ceiling. | If delay ≤ OBFTR(R=2)'s cross-round retention (~1500ms at this operating point): in envelope at R=2; round 2 re-flood may resolve at L_0. ✓ in ~1.8s. Else: out of envelope. ✗ | Round 1: PREPARE delayed → timeout (~2s). Round 2: new leader; if adversary delays again → round 2 timeout. ✗ Within 4s, QBFT tolerates ≤ 1 round of adversarial delay. |
| Sustained partition (real propagation > all layers' budgets) | OBFT deepest-layer budget `B_{K-1} ≈ 1000ms` at K=4 SSV operating point; exceeded → ✗ slot misses. Safety holds. | OBFTR(R=2) cross-round retention ~1500ms at this operating point (recovers at L_0 via re-flood, preserves MEV); exceeded → ✗ slot misses. Safety holds. | QBFT round-budget × RT exceeded; slot misses. ✗ Safety holds. |
| > f operators offline/byzantine | Standard 3f+1 violation; slot misses. ✗ Safety holds. | Same. ✗ | Same. ✗ |

**Summary of recovery-scope differences:**

- **OBFT and OBFTR(R=2) differ in *where* they recover, not just how much they tolerate.** OBFT's per-layer staggered budgets (`B_0 = 0.5 BTT` up to `B_{K-1} = 5 BTT = 1000ms` at K=4 SSV operating point) recover via K-layer fall-through — propagation up to 1000ms is absorbed, but the slot succeeds at a deeper layer (different leader's V, less MEV freshness). OBFTR(R=2)'s ~1500ms cross-round retention recovers at L_0 specifically via round-2 re-flood (preserves MEV freshness) at the cost of an extra round of consensus.
- **OBFTR(R=2) > OBFT for slots where MEV preservation matters during partition tail**: round-2 re-flood at L_0 gets the freshest MEV V even when L_0's broadcast didn't propagate in round 1. OBFT loses one layer's worth of MEV per fall-through.
- **OBFT-family > QBFT in latency and multi-leader-failure**: OBFT's healthy path is ~600ms (vs ~800ms QBFT); K-layer parallel fall-through is in-round (vs QBFT's serial round-change at ~2.8s per round-change cycle, exceeding the 4s budget at K-1=3 silent leaders).
- **QBFT > OBFT-family in 1-1-1 equivocation and host-validity divergence**: QBFT's "round-change with fresh-V" handles these structurally; OBFT-family relies on assumption 3 and assumption 4.
- **All three fail equivalently** on sustained partition beyond their respective envelopes and on > f byzantine.

The choice between OBFT, OBFTR(R=2), and QBFT for SSV proposer duty depends on (a) MEV freshness sensitivity — OBFT's deeper-layer fall-through preserves liveness but uses backup-leader V's (less fresh MEV), OBFTR(R=2)'s round-2 re-flood preserves L_0's MEV at +1200ms latency cost (R1+R2 - R1 = 1.8s - 0.6s); (b) observed re-org rate; (c) the cluster's tolerance for 1-1-1 equivocation (handled by rational-byzantine deterrent in OBFT-family, recovered in QBFT). Detailed cost-side trade-offs (latency, bandwidth, cryptographic primitive maturity) are in [Appendix A.3](#a3--comparison-with-qbft).

**Note on apples-to-apples framing.** The above table compares OBFT and OBFTR(R=2) at their natural consensus budgets (~600ms and ~1000ms) vs QBFT at production sizing (RT=2s per round; ~2.8s for 2-round recovery at minimum sizing). This reads as "QBFT recovers more failure modes than OBFT-family" but conflates two different effects:

- **QBFT's larger consensus-budget allocation** — round-2 access scales recovery with T. Several QBFT-wins in this table (σ-locked equivocation 1-1-1, host-validity-divergence-majority, byz-leader-grief patterns) are time-conditional: at small T (e.g., compressed to 800ms = 1 round only), QBFT loses these too. At larger T, they're recoverable structurally by [2abOBFT](2abOBFT.md) within a single round (via the Phase 2a/2b convergence rule), so the "needs more T" caveat is QBFT-specific.
- **QBFT's structural advantages** that hold regardless of T:
  - **2-2 validity-divergence recovery** via refetch at moved head — a genuine T-independent advantage of QBFT over both bare OBFT and OBFTR(R=2). The OBFT family has no refetch step within a round.
  - **2-1-byz-defect equivocation recovery** via σ-lock abandonment across rounds — only relevant when comparing against [2abOBFT](2abOBFT.md); bare OBFT also recovers this via Phase-1 σ_V cryptographic lock.
  - **Absence of verdict-equivocation surface** — only relevant when comparing against [2abOBFT](2abOBFT.md); bare OBFT has no separable verdicts either.

Pure all-honest network failures (jitter, mesh-flakiness with all-honest operators, transient outages, partition recovery within absorption) recover identically across all three protocols at apples-to-apples T. The "QBFT recovers what OBFT-family doesn't" framing applies specifically to adversarial-byz patterns and validity-divergence, not to network failures with honest operators trying their best. See [docs/2abOBFT.md / A.3](2abOBFT.md#a3--comparison-with-bare-obft-and-qbft) for the cleaner 4-bucket taxonomy and a 3-way comparison including 2abOBFT.

### Equivocation handling

See "Phase 1 / Equivocation handling — detect and slash" for the operational rule. Summary: at `T_commit`, each operator decides per layer based on retained V's:

1. 0 V retained → NR (silent-leader rule).
2. Exactly 1 V retained, host validates → σ on that V.
3. Exactly 1 V retained, host returns not-valid → NV.
4. ≥ 2 distinct V's retained (equivocation observed pre-T_commit) → NR (the operator does not pick a winner; cross-phase exclusivity locks them out of σ on either V).
5. Gossip the equivocation evidence (the pair of equivocating Phase-1 bundles) for out-of-band slashing.

The leader is required to sign `σ_V` *exactly once per (slot, layer)*; refreshes in the host's fetch loop are pre-signing only (see "Application: SSV Ethereum proposer duty / Head-change handling") and don't surface multiple `σ_V` partials on the wire. Any second `σ_V` from the same leader is a protocol violation.

OBFT does not provide in-protocol equivocation recovery. Some equivocation patterns naturally reach qV on a single V (when honest happen to split such that 2-of-3 σ-emit on the same V; leader's σ_L^V on that V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. See "Liveness / Equivocation handling" for the full case analysis. Equivocation is treated as a slashable byzantine fault (Phase-1 bundles signed by leader's key are self-contained slashing evidence — see "Slashing evidence"); the rational-byzantine deterrent (assumption 4) is what makes the slot-miss tolerable across many slots. The **Phase 2a/2b split** (see [2abOBFT](2abOBFT.md)) recovers most equivocation patterns at +1 RTT cost (1-1-1 splits, all-honest-NR, h_V=1) but regresses on 2-1-byz-defect (where bare OBFT succeeds via Phase-1 σ_L^V cryptographic lock — see [docs/2abOBFT.md / 2-1 split](2abOBFT.md#2-1-split)); documented as future improvement.

**Cross-onion (operator-side) equivocation — counting and suppression.** If an operator `i` is observed with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different V, the dual partials are slashable evidence on the same logic as leader equivocation. For aggregation: by single-σ-V exclusivity (EKM-enforced — see "Slashing-protection scope"), an honest operator only ever emits σ on one V per layer, so any dual-V σ partials from the same operator are byzantine. The byzantine f-bound on per-V σ-pool contributions therefore holds without explicit suppression: byz contributes ≤ 1 partial per V regardless. Honest receivers MAY additionally elect to fully suppress `i`'s partials upon observing the equivocation evidence — this is not load-bearing for safety.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_k` is a slashable cross-signer. The pair is detected uniformly across phases:

- **σ from Phase 1 + NR/NV from Phase 2** — applies to a layer's leader specifically: a `σ_{L_k}^V(V_{L_k})` from the Phase-1 bundle paired with an `σ_{L_k}^{IBE}(nr_tag_k)` from the same operator at the same slot.
- **σ from Phase 2 onion + NR/NV from Phase 2** — any operator who included σ in their onion *and* broadcast a no-σ attestation.

Detection is straightforward — the dual partials are public. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it cryptographically — the EKM-enforced cross-phase exclusivity is what gives Pigeonhole 1 its load-bearing constraint, and a byzantine cross-signer simply consumes their f-bound contribution to one of σ-pool or NR-pool, not both effectively). The detection is purely for **attribution** and out-of-band punishment.

### Slashing evidence

Five rules surface byzantine fault evidence for *attribution and punishment*. Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to inform the surviving operators (and stakers) that a byzantine fault has occurred so the cluster can act on it — see [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the offline-equivalence model. The protocol surfaces the evidence; the surviving operators verify it and decide whether to (a) blacklist the byzantine via cluster-wide config update (restoring `Byzantine ≡ Down`; planned protocol extension), (b) file a stake-slashing transaction via the SSV contract where the evidence is cryptographically self-contained, and/or (c) propagate the signal to stakers and to future cluster formation.

- **Self-contradiction (σ + NR/NV at the same layer).** If operator `i` emitted `σ_i^V(V)` at some layer AND `σ_i^{IBE}(nr_tag_k)` at the same layer, the dual partials are slashable evidence (cross-signing).
- **Leader equivocation.** Two distinct `σ_V` partials from the same leader at the same (slot, layer) are a self-contained slashable fault proof. Any observable double-signing is protocol-violating regardless of the leader's stated intent.
- **Cross-onion partial-sig equivocation.** Operator `i` emitting `σ_i^V(V)` and `σ_i^V(V')` for different `V` at the same layer is detectable from the partial sigs alone — single-σ-V exclusivity is EKM-enforced, so any dual-V observation is a slashable byzantine fault.
- **Fake encrypted-presence (post-decryption garbage at k > 0).** Operator `i` broadcasting an auth-signed `KindCommit` with an encrypted partial at layer `k > 0` that, after NR-quorum unlocks decryption, decrypts to garbage (not a valid σ partial on any leader-known V at L_k, or fails to decrypt entirely) is a slashable byzantine fault. The auth envelope binds `i` to the encrypted payload at signing time; post-decryption verification surfaces the garbage. Detection is **delayed and conditional on NR-quorum reaching at all prior layers** (so the chained encryption can be unlocked); when the slot misses cleanly without any NR-quorum reaching (e.g., σ-locked split at L_0, or NR-pool short of qEnc cluster-wide), the chained encryption stays sealed and the evidence is not surface-able through this rule. **Honest detection of fake encrypted-presence is therefore conditional on the slot progressing far enough for the relevant layer's encryption to unlock.** This is a real deterrent-strength reduction for adversarial byzantine that engineers slot-miss precisely to seal evidence; mitigated only by Rule 5 (when applicable at L_0) or by post-hoc decryption coordination outside the protocol's current scope.
- **Fake plaintext σ at the cluster's plaintext layer.** Operator `i` broadcasting an auth-signed `KindCommit` with a plaintext σ partial at the cluster's plaintext layer (L_0 in bare OBFT; L_Bid in the L_Bid extension, on the verdict-bound `V_X`) that does not verify against any retained candidate V at that layer (where the receiver has retained at least one such V — a Phase-1 bundle V_{L_0} for L_0; a `bid_set` member V_X for L_Bid) is a slashable byzantine fault. The auth envelope binds `i` to the partial; partial-vs-V verification is a deterministic local check by any receiver with retained V. Detection is immediate (no decryption-unlocking dependency, unlike Rule 4) — the receiver can attribute the fault as soon as it observes both `i`'s auth-signed `KindCommit` and any retained candidate V at the plaintext layer. The fake partial does not contribute to σ-quorum (it doesn't verify against any retained V); it's purely a slashable accountability artifact.

Each piece of evidence is verifiable in isolation (signed by the offending operator's own keys) — any observer with the published partials and the (eventually) decrypted onion contents can independently confirm the byzantine action. **Acting on the evidence (slashing transaction, cluster removal) is a human-coordinated process**, not an automated protocol step; honest operators judge whether the evidence is compelling and decide whether to act.

**Evidence quality and surface-ability vary by rule:**

| Rule | Detection timing | Surface-ability | False-positive risk |
|---|---|---|---|
| 1. Self-contradiction (σ + NR/NV) | Immediate (dual partials on the wire) | Always — public partials | Very low |
| 2. Leader equivocation | Immediate (two σ_V from same leader) | Always — public bundles | Very low |
| 3. Cross-onion partial-sig equivocation | Immediate (two σ partials on different V) | Always — public partials | Very low |
| 4. Fake encrypted-presence (k > 0) | Delayed (post-decryption) | **Best-effort, conditional on slot progressing past prior layers' NR-quorum** — sealed if slot misses early | Very low when surfaced |
| 5. Fake plaintext σ at the plaintext layer (L_0; L_Bid in the L_Bid extension) | Immediate (partial vs retained V check) | Always when retained-V receivers gossip evidence (MUST-gossip rule, rate-limited per `(slot, layer, operator_id)`) | Very low |

**Rule 4 has a structural detection limit.** The chained-encryption construction makes Rule 4 evidence cryptographically inaccessible when prior layers' NR-quorum doesn't reach: decryption requires `qEnc` partials on `nr_tag_{0..k-1}`, those partials don't exist if honest σ'd at those layers, and honest cannot preemptively sign IBE partials on layers they intend to σ-side-commit to (that violates cross-phase exclusivity = cross-signing). This is a **fundamental limit of the IBE primitive as used here**, not a fixable spec gap.

**The seal applies in BOTH slot-success and slot-miss outcomes**, not just slot-misses:

- **Slot succeeds at L_0** (σ-quorum reaches at L_0): per Pigeonhole 1, NR-quorum at L_0 does NOT reach (σ and NR mutually exclusive at the same layer). Hence the chained encryption at L_1, L_2, ... stays sealed, and any fake encrypted-presence at deeper layers in this slot is invisible. **This is the common case for healthy slots** — a byzantine that fakes encrypted-presence at L_2 in every slot pays no per-slot cost on healthy slots where the cluster succeeds at L_0. The fake-presence is essentially "rehearsing the attack with no consequences" until a slot-miss-at-L_0 path happens to unlock the relevant encryption.
- **Slot misses at L_0** (no quorum reaches): σ-locked split equivocation, h_V=1 selective-delivery, validity-divergence deadlock — each leaves the chained encryption sealed. Compounded with byz fake-presence at deeper layers, byz gets two grief actions per detection.

Phase 2a/2b mitigates both cases by widening the set of paths that reach a quorum somewhere (so more layers' encryption unlocks more often), but does not fully close it — slots that miss for non-byz reasons (host-validity divergence, sustained partition) still leave Rule 4 evidence sealed.

**Practical implication for deployments.** Rule 4 functions as a *probabilistic* deterrent rather than an unconditional one: a byzantine that fakes encrypted-presence at L_k>0 expects detection only with the probability that NR-quorum reaches at all prior layers in subsequent slots where the deterrent's coordination process can still act. Deployments relying on assumption 4 (rational-byzantine deterrent) for L_k>0 fake-presence should weight the deterrent's effective strength accordingly — Rule 4 is *best-effort, not guaranteed surface-able*. Rule 5 (Fake plaintext σ at the cluster's plaintext layer — L_0 in bare OBFT, L_Bid in the L_Bid extension) does not have this limitation since the plaintext layer's σ is unencrypted.

The five classes are all *cryptographically self-contained* (high-confidence, low false-positive risk against honest operators) once surfaced. The asymmetry above — Rule 4's slot-progress-conditional surface-ability — is a real limitation that adversarial byzantine can exploit by engineering slot-miss precisely to seal Rule-4 evidence. Behavioral-pattern grief (selective-delivery, σ-refusal coordinated with honest flakiness) leaves no on-wire cryptographic evidence at all and is correspondingly harder for humans to act on with confidence — see [Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4).

### Failure modes

The slot misses (no V signature is produced) under any of the following. The cases split into two classes by relationship to OBFT's operating assumptions:

- **Class A — assumption violations** (the listed condition violates one of OBFT's assumptions; the protocol does not promise liveness when an assumption is violated). These are out-of-scope for OBFT's recovery guarantees by construction.
- **Class B — permitted byzantine grief within the f-bound** (occurs *under* valid assumptions; one byzantine operator within the f-byzantine bound deliberately misbehaves to cause slot-miss). These are *permitted because they are eventually bounded* — every Class B grief leaves evidence on the wire (cryptographically self-contained for some classes, behavioral-pattern for others), and the rational-byzantine deterrent (assumption 4) bounds the byzantine's grief across slots via the eventual `Byzantine ≡ Down` collapse (manual blacklist by the surviving `n − f` operators; planned protocol extension) plus staker migration that collapses cluster-wide fee accrual. The boundedness is what makes Class B "permitted" rather than "fatal" — an attacker that griefs reliably ends up in the same fee position as if they had gone permanently offline (and worse, for cryptographically-self-contained faults: stake-slashable via the SSV contract).

**OBFT's failure-mode set is identical to OBFTR(R≥2)'s, with the recovery shape differing**: OBFT recovers partition tails via K-layer fall-through (succeeds at a deeper backup with less MEV freshness) up to `B_{K-1} = 1000ms` at K=4 Config A; OBFTR(R≥2) recovers them via round-2 re-flood (succeeds at L_0 with MEV preserved) at +1200ms latency. All other failure modes are R-invariant and the exposure is the same.

The slot misses under any of:

- **[Class A]** **Asymmetric propagation past `T_commit` (real propagation > `B_k` at layer L_k)** — violates assumption 2 (partial synchrony) for that layer. Honest who first-observe V past `T_commit` treat the leader as silent and NR. If the resulting σ-pool falls below qV at this layer, the cluster falls through to a deeper backup whose own `B_{k+1}` is wider (per-layer budgets are staggered: `B_0 = 0.5 BTT` for the primary up to `B_{K-1} = 5 BTT` for the deepest at K=4 Config A; see §Setting). If propagation also exceeds the deepest layer's `B_{K-1}` budget, slot misses cleanly. **No safety violation.** Deployments that need within-slot absorption of asymmetric propagation tails wider than `B_{K-1}` should use [OBFTR(R≥2)](OBFTR.md), which retains bundles across rounds.
- **[Class A]** **More than `f` faults** — violates assumption 1 (BFT trust bound). More than `f` operators offline/byzantine combined. Standard `3f+1` trust bound. Slot misses regardless of protocol structure.
- **[Class A]** **Assumption 3 violated (host validity diverges mid-slot)** — violates assumption 3 (host validity unanimous at decision time). See [Implications of validity-divergence not being recovered](#implications-of-validity-divergence-not-being-recovered-assumption-3). The host's stabilization workflow (validate-once-and-lock at Phase-1 acceptance) is the design's path to keeping this from occurring; if it does occur, the slot misses cleanly. Not slashable (re-orgs are real-world events, not protocol violations); rational-byzantine deterrent does not apply.
- **[Class B — cryptographically slashable]** **Equivocation σ-locked split patterns** (1-1-1 split, 1-1-NR at f=1 n=4 — patterns where some honest σ-commit on different V's before observing equivocation, leaving neither σ-quorum nor NR-quorum reachable at L_0 and no fall-through) — occurs under valid assumptions; one byzantine within f-bound equivocates to engineer the split. See [Implications of equivocation not being recovered](#implications-of-equivocation-not-being-recovered). **Equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained cryptographically slashable**; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots. The all-honest-NR case (every honest retains ≥ 2 V's by T_commit and emits NR per the equivocation-NR rule) actually recovers via L_1 fall-through and is not a failure mode at f=1 n=4 with L_1 honest. **R-invariant** — same exposure in OBFT and OBFTR(R≥2).
- **[Mostly Class A]** **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates in non-recoverable patterns at every layer). At K ≥ f+1, byzantines alone (within f-bound) cannot cause this — pigeonhole guarantees ≥ 1 honest leader. So this typically requires either >f faults (Class A: violates assumption 1) or coincident non-byzantine independent failures (relay timeouts, host issues, etc. — also Class A in the sense that the cluster's operational assumptions about host availability are violated). Rare in practice. The K parameter controls the fall-through depth; **at K = n = 4 (recommended OBFT default for proposer duty), pigeonhole guarantees ≥ 3 honest leaders, providing maximum fall-through redundancy**.
- **[Class A]** **Late deepest-layer leader broadcast.** A deepest-layer leader L_{K-1} whose Phase-1 bundle's first cluster-observation arrives after `T_commit` — e.g., the leader's fetch loop overruns substantially due to slow beacon node, MEV-relay timeout, or head-change refresh — is not counted by any honest receiver. All 3 honest at L_{K-1} treat as silent-leader-NR, NR-quorum at L_{K-1} reaches → walk advances past L_{K-1}, but no L_K layer exists. **Slot misses.**

  **Why Class A?** None of the six explicit assumptions is violated: BFT trust bound holds, partial synchrony bounds *gossipsub* propagation but not the leader's *application-internal fetch latency*, validity is orthogonal, EKM and DKGs are orthogonal. The failure relies on an *implicit* operational assumption — that honest leaders broadcast by their per-layer deadline `T_broadcast_max_k = Ls_arrival − B_k` (with `B_k` sized so propagation completes before `Ls_arrival`). When this implicit assumption fails (legitimate operational delay overruns even the deepest layer's wide budget `B_{K-1}`, no byzantine action), the protocol cannot fall through past the deepest-layer. Note that in the staggered model the deepest layer has the *largest* propagation budget (e.g., `B_3 = 5 BTT = 1000ms` at K=4 Config A), so this failure requires either an extreme operational delay or a leader that hasn't pre-fetched a bundle in time for the very early `T_broadcast_max_{K-1}` deadline.

  **Mitigation paths (in order of recommendation):**
  - **Use K ≥ f+2** (the recommended config; see §Setting). Adds at least 2 honest leaders so a single late-broadcasting honest leader doesn't foreclose the slot — fall-through to a deeper honest leader recovers. At f=1 n=4 this means K ≥ 3 (with K = n = 4 as the OBFT default for proposer duty, providing maximum fall-through depth at minimal extra bandwidth ~3KB per onion). At f=2 n=7, K ≥ 4. **No protocol change**, just deployment configuration; this is the cleanest fix that actually recovers the slot. **The OBFT default `K = n = 4` already satisfies this.**
  - **Host-side hard deadline** (defense-in-depth on top of K ≥ f+2; minor host-side discipline, no protocol change). Leader `L_k`'s fetch loop should abort and broadcast no bundle if it hasn't terminated by `T_broadcast_max_k`. Converts "late broadcast missed cutoff" into "silent leader" (handled cleanly via NR-quorum → fall-through). At K = f+1 (BFT-min) this *cleans up the spec-tension* but doesn't recover the slot (silent + L_0-fail still falls through to non-existent layer). At K ≥ f+2 it complements the K choice by ensuring late broadcasts always fall into the silent-leader path.
  - **Phase 2a/2b** ([Path forward](#where-this-came-from)). Structural fix that handles K = f+1 too. No Phase-1 σ_V from the leader → no early commitment by anyone → late bundle observed in Phase-2a is σ-emittable in Phase-2b. Costs +1 RTT per slot. Worth adopting if the deployment also wants the validity-divergence and Class B byzantine grief recovery that Phase 2a/2b brings.
- **[Class A]** **Validity-divergence deadlock (network-induced; no byzantine action required in the cleanest case).** **This is an OBFT-family issue inherited by OBFT.** Bare OBFT and OBFTR share the three structural causes: per-operator independent validity verdict, leader's σ_V locked in Phase 1, and cross-phase exclusivity per operator. [2abOBFT](2abOBFT.md) recovers this case in-protocol via the Phase-2a observation phase.

  A beacon-chain re-org landing inside the bundle-acceptance window can split honest operators between σ-eligibility (parent_root matched their pre-reorg head snapshot) and NV (parent_root didn't match their post-reorg head). The host's per-operator validate-once-and-lock workflow (§Head-change handling) preserves each operator's verdict but cannot reconcile cluster-wide divergence. At the boundary split (e.g., 2-σ vs 2-NV at f=1 n=4 with all 4 honest, where leader's Phase-1 σ_V counts as σ-side via cross-phase exclusivity): σ-pool = 2 < qV = 3; NR-pool = 2 < qEnc = 3. **No safety violation** — just no quorum on either side; slot misses cleanly. The host's stabilization workflow narrows the divergence window to ≈ `1 BTT` (the time between earliest possible bundle first-observation and `T_commit`), but doesn't eliminate it.

  **The deadlock zone widens when a byzantine within f-bound exercises f-budget passively** (silent or σ-on-V — neither is independently slashable; passive silence is the natural f-budget consumption pattern for an absent operator). At f=1 n=4 with leader honest, additional deadlock configurations include:

  - "1 non-leader σ + 1 non-leader NV + byz silent" (σ-pool = leader + 1 = 2 < qV; NR-pool = 1 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz silent" (σ-pool = leader = 1 < qV; NR-pool = 2 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz σ-on-V" (σ-pool = leader + byz = 2 < qV; NR-pool = 2 < qEnc).
  - Symmetric configs when leader's host-verdict is itself NV (Phase-1 σ_V locks leader σ-side regardless of leader's host's later opinion).

  The all-honest example captures the cleanest case; in real deployments with byz exercising f-budget, the practical slot-miss rate from validity-divergence is meaningfully higher than the all-honest example alone suggests. **The expected rate scales with `re-org rate × byz-passivity-rate`**, not re-org rate alone — i.e., a deployment with re-orgs in 1% of slots and a byzantine present and adopting passive grief in some fraction of slots compounds these probabilities into validity-divergence slot-misses. The host's stabilization workflow narrows the divergence window but does not eliminate it; byzantine passive f-budget consumption (silence or σ-on-V — neither cryptographically slashable individually) is essentially "free" within the f-bound, so byz can reliably contribute the passivity factor whenever exercising the deterrent's weak-attribution corner is favorable. See [Implications of validity-divergence not being recovered (assumption 3)](#implications-of-validity-divergence-not-being-recovered-assumption-3) for the full discussion. Phase 2a/2b (see [Path forward — Phase 2a/2b](#where-this-came-from)) eliminates this deadlock structurally via late σ-emit on cluster-stabilized verdict.
- **[Class B — partially closed by design, partially R-invariant]** **Byzantine selective-delivery grief (h_V variants).** The "h_V=1" framing covers two attack patterns; current OBFT closes one and leaves the other.

  **Closed as a side-effect of Defer removal — withhold-then-fake-σ.** Earlier OBFT-family designs included a Defer state with a "no-V fallback" rule (receivers without V deferred their NR-decision based on observed peer σ-claims). That rule was the structural enabler of an attack where a byzantine L_0 withheld Phase-1 from *all* honest, emitted an auth-signed fake σ-claim, then selectively delivered Phase-1 late to one honest — engineering a deadlock via Defer→force-NR pacing. The current spec removes Defer **primarily for spec/wire/EKM simplification** (3-state vs 4-state commitment lattice, single `KindCommit` emission vs multi-emission, no auth-only-retention pre-state, no transitional EKM events — see [§Where this came from](#where-this-came-from) and [Appendix E](#appendix-e--defer-state-within-slot-partition-recovery)); closure of this attack is a structural side-effect of that removal, not its motivating reason. With Defer gone, receivers without V at `T_commit` immediately NR per the silent-leader rule, so a byzantine that withholds Phase-1 produces NR-quorum at L_0 and clean fall-through to L_1.

  **Still open — selective Phase-1 delivery.** A byzantine that broadcasts Phase-1 to exactly one honest (rather than withholding) σ-locks via their own Phase-1 σ_V, and the receiving honest σ-locks too. Cluster pools then sit at σ-pool = 2 (recipient + byz σ_V) and NR-pool = 2 (the two no-V honest; byz and recipient σ-locked, can't NR). Neither reaches qV/qEnc=3; the slot misses. This is an algebraic limit at f=1, n=4 and is **R-invariant** — Defer removal doesn't help, OBFTR(R≥2) doesn't help either. Equivocation evidence isn't generated (only one V was broadcast); attribution relies on behavioral patterns (selective-delivery detection across slots). The Phase 2a/2b split closes this case structurally.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

OBFT uses **`K-1` IBE tags per slot** (the K-1 NR tags; the deepest layer has no NR tag). Each tag is a standard threshold-IBE tag using the cluster's IBE keypair. The chained encryption at each layer-transition is implemented as a single IBE ciphertext under `nr_tag_k`, nested across layers. At K=2 the chain has 1 level; at K=3, 2 levels; at K=4, 3 levels.

The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init. Long-lived, no per-slot rotation. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

**Chained encryption cost.** At layer K-1 (deepest), each σ partial is wrapped in `K-1` levels of IBE encryption. Per-onion size grows as `O(K)` ciphertext bytes (`K-1` levels × ~32-64 bytes ciphertext per level). At K=2 the chain has 1 level; at K=4 it has 3 levels. Concrete sizes: ~1 KB per onion at K=2, ~3 KB at K=4. Within practical SSV bandwidth budgets.

## Properties summary

| Property | OBFT |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1` + EKM-enforced per-operator commitments (single-σ-V per (slot, layer), σ-XOR-NR per layer, cross-phase exclusivity), holds against offline-aggregating byzantine within the f-bound. Honest-majority cryptographic, not 100% cryptographic — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). Same trust posture as QBFT. |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition (assumption 3) |
| Termination (output guaranteed) | Conditional. **One-liner: consensus expected to complete by `slot_start + 3.90s` at SSV proposer-duty operating point (BTT = 200ms, K = 4, T_commit = 3.40s, recommended Δ_2 = 2 BTT = 400ms, Δ_3 = ε_3 = 100ms), with submission slack to `slot_start + 4.00s` for relay submit (`header_submit_headroom = 100ms`); under conditions: (a) ≤ f operators byzantine/offline, (b) real propagation from leader L_k broadcast to any honest first-observation ≤ that layer's per-layer budget `B_k` (staggered: B_0 = 0.5 BTT, ..., B_3 = 5 BTT at K=4 relative to Ls_arrival — see §Setting), (c) host validity unanimous at decision time (assumption 3), (d) `K ≥ 3` (late-leader resilience).** Single-round protocol; per-layer staggered budgets give wider absorption at deeper layers (~1000ms at L_3) than OBFTR(R=2)'s uniform cross-round-retention window. |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates form self-contained slashable evidence |
| Byzantine-leader-grief resistance | **Partial under non-adversarial byzantine; substantially weaker against adversarial byz that deliberately engineers grief patterns.** Recovery via "natural" σ-quorum patterns (leader's σ_L^V completes a 2-of-3-honest pool) only fires when byz isn't actively timing deliveries. **Adversarial byzantine reliably engineers slot-miss when L_0** via σ-locked split equivocation (1-1-1, 1-1-NR, etc.). At f=1 n=4 with uniform leader rotation, an adversarial byz primary can deterministically grief ~25% of slots (whenever they're L_0). **Same exposure as OBFTR(R≥2)** — these patterns are R-invariant; round-machinery does not help. The rational-byzantine deterrent (assumption 4) is the only protocol-level defense, and it works *across slots in expectation*, not per-slot. Effective deterrent strength is deployment-specific (stake-to-grief-value ratio, governance responsiveness, slashability evidence quality — see [§Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4)). For deployments under realistic adversarial conditions, **Phase 2a/2b is the structural fix and should be considered near-term, not future** — it converts all R-invariant byz grief patterns into clean fall-through at +1 RTT cost. |
| Mesh-flakiness tolerance (honest operator with poor gossipsub mesh visibility) | **Limited.** A mesh-flaky honest operator who fails to observe peer σ-emits within the NR-decision window can NR-emit incorrectly, becoming a byzantine-equivalent f-budget consumer for that slot. Combined with byz σ-refusal, this creates a deadlock that the protocol cannot recover from within the slot. The recommended `Δ_2 ≥ 2 BTT` absorbs typical mesh-jitter (up to one full `1 BTT` of additional slack on top of P99 propagation) but doesn't cover wider mesh outliers. **Same exposure as OBFTR(R≥2)** — cross-round NR-lock blocks recovery there too. QBFT's round-reset semantics handle this case better (a flaky operator's bad PREPARE doesn't lock them across rounds); OBFT-family enforces cross-phase exclusivity per slot. Phase 2a/2b mitigates by deferring commitment until mesh visibility has had a full additional propagation cycle to stabilize. |
| Validity-divergence under strict host | **Out of scope** — see [Assumptions](#implications-of-validity-divergence-not-being-recovered-assumption-3); host stabilizes the verdict at Phase-1 acceptance |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (K-layer fall-through within Phase 3's reconstruction walk; K configurable, K = n recommended for proposer duty) |
| Round-change recovery | **No** — single-round design. Late re-flood within Phase 2's receiver acceptance window is the only within-slot partition-recovery mechanism. For wider partition absorption, use [OBFTR(R≥2)](OBFTR.md) at the cost of additional slot budget and EKM cross-round atomicity. |
| Partial-synchrony absorption | Per-layer staggered: `B_k` for layer L_k. At K=4 Config A: `B_0 = 0.5 BTT = 100ms` for primary L_0 (tightest, freshest MEV) up to `B_{K-1} = 5 BTT = 1000ms` for deepest L_3 (last-resort tail; relative to `Ls_arrival`). Cluster falls through layers based on which one's bundle actually arrived by `T_commit`. For recovery at L_0 specifically (preserving MEV) under wider partition tails, use [OBFTR(R≥2)](OBFTR.md) at the cost of an extra round. |
| Recovery scope vs OBFTR(R=2) | **OBFT recovers via K-layer fall-through to deeper backup leaders (loses MEV freshness per fall-through); OBFTR(R=2) recovers via round-2 re-flood at L_0 (preserves MEV) at +1200ms latency.** Per-layer staggered budgets give OBFT wider absorption at the deepest layer (1000ms at L_3) than OBFTR(R=2)'s ~1500ms uniform cross-round retention. All other failure modes (byzantine grief, view-divergence) are R-invariant — same exposure. |
| Recovery scope vs QBFT | Multi-leader fall-through is in-round (vs QBFT's serial round-change), so OBFT wins on K-leader-failure cases and healthy-path latency. View-divergence (validity-divergence and equivocation patterns that don't naturally reach qV) is out of scope — handled by assumptions 3 and 4. Phase 2a/2b (future improvement) would close these gaps at +1 RTT. |

## Application: SSV Ethereum proposer duty

For SSV's proposer duty, the recommended OBFT configuration is **`K = 4 = n`** — every cluster member leads exactly one layer, providing maximum K-layer fall-through depth (`f+1 = 3` honest leaders guaranteed by pigeonhole at f=1). Concretely, **`V_0`** is the slot's designated MEV proposer (fetches the freshest relay-bundle); **`V_1, V_2, V_3`** are backup leaders fetching progressively safer / earlier vanilla beacon-node payloads (deeper-confirmed parents → lower re-org exposure; see §Head-change handling).

Cryptographic safety (`qEnc = qV` + chained encryption + EKM-enforced cross-phase / single-σ-V exclusivity) ensures only one block can ever get a valid validator signature, regardless of K. The single-round design simplifies the EKM coordinator (no cross-round atomicity).

### Proposer-duty terminology

| Term | Meaning |
|---|---|
| Slot start | t = 0 (anchored to consensus-layer slot start) |
| `RANDAO` | RANDAO-reveal completion; cluster-wide ≈ slot_start + 150ms — earliest possible Phase-1 fetch start |
| `Relay_cutoff` | slot_start + **4000ms** — slot's hard relay-submission deadline |
| `BTT` | broadcast trip time = P99 + δ; one one-way gossipsub propagation cycle. **`BTT = 200ms`** (P99 ≈ 150ms + δ ≈ 50ms) at the operating point below |
| `slack` | operator-local first-observation jitter: spread between earliest and latest receiver timestamps for the same Phase-1 bundle. **`slack = 0.5 BTT = 100ms`** at the operating point below |
| `Ls_arrival` | latest stable σ-aggregation anchor: `T_commit − slack`. The cluster-wide latest moment by which a leader's bundle must have arrived at all honest receivers for σ-quorum to form within the layer's window |
| `T_commit` | view-fix deadline: `Relay_cutoff − 3 BTT`. Receivers stop counting Phase-1 bundles past this point |
| `header_submit_headroom` | budget for cert broadcast + relay submit after Phase 3 completes; **100ms** |
| `V_0` | MEV-optimized block fetched late from the relay |
| `V_1, V_2, V_3` | safe earlier-fetched blocks from vanilla beacon-node payloads, refreshed on head changes within each leader's pre-signing fetch loop |
| `T_broadcast_max_k` | per-layer leader broadcast deadline: `Ls_arrival − B_k`. Deeper layers broadcast earlier (wider `B_k`) to absorb wider propagation tails |

### Timing budget

**Operating point.** `BTT = 200ms`, `header_submit_headroom = 100ms`, `slack = 0.5 BTT = 100ms`, `Δ_2 = 2 BTT = 400ms` recommended (KindCommit propagation + jitter), `Δ_3 = ε_3 ≈ 100ms` local CPU.

**Derived anchors.** `T_commit = Relay_cutoff − 3 BTT = 3400ms`; `Ls_arrival = T_commit − slack = 3300ms`.

The 3 BTT after `T_commit` decomposes as: `Δ_2` (2 BTT = 400ms) + `Δ_3` (0.5 BTT = 100ms = ε_3) + `header_submit_headroom` (0.5 BTT = 100ms). Both `Δ_3` and `header_submit_headroom` are exactly 100ms at this BTT — they're written as 0.5 BTT for unit consistency.

| t (ms) | Event | Targets / notes |
|---|---|---|
| 0 | Slot start | |
| 150 | `RANDAO` done | Earliest Phase-1 fetch start |
| 2300 | `V_3` broadcast (`Ls_arrival − 5 BTT`) | Targets propagation tails up to **1000ms**; MEV-fetch budget **2150ms** (last-resort tail; pre-fetched at deepest-confirmed parent) |
| 2900 | `V_2` broadcast (`Ls_arrival − 2 BTT`) | Targets up to **400ms**; MEV-fetch budget **2750ms** |
| 3100 | `V_1` broadcast (`Ls_arrival − 1 BTT`) | Targets up to **200ms**; MEV-fetch budget **2950ms** |
| 3200 | `V_0` broadcast (`Ls_arrival − 0.5 BTT`) | Targets up to **100ms**; MEV-fetch budget **3050ms** (freshest relay-bundle) |
| 3300 | `Ls_arrival` (= `T_commit − slack`) | Latest stable arrival anchor; all leaders' bundles must reach honest receivers by here for σ-aggregation within their layer's window |
| 3400 | `T_commit` (= `Relay_cutoff − 3 BTT`) | View-fix deadline; receivers stop counting Phase-1 bundles. `KindCommit` broadcast |
| 3800 | `T_commit + Δ_2` | Hard cluster-wide deadline; σ/NR pools stable; Phase 3 begins |
| 3900 | Phase 3 complete | Local IBE-walk + BLS aggregation + certificate; `Δ_3 ≈ 100ms` |
| 4000 | `Relay_cutoff` | Cert broadcast + relay submit fit in `header_submit_headroom = 100ms` |

**Recovery scope.** Within Phase 3's single reconstruction walk: silent V_0 → fall through to V_1; silent V_0 + V_1 → V_2; silent V_0 + V_1 + V_2 → V_3 (always honest at f=1 by pigeonhole). All in one round (sequential local decryption, no per-layer RTT). Per-layer absorption: V_0 covers up to 100ms propagation, V_1 up to 200ms, V_2 up to 400ms, V_3 up to 1000ms. Beyond V_3's budget is out-of-envelope and slot-misses cleanly.

**MEV-fetch-budget asymmetry.** V_0's 3050ms is the freshest budget; deeper layers trade fetch time for propagation slack. Per-leader budgets at this operating point: `[V_3: 2150ms, V_2: 2750ms, V_1: 2950ms, V_0: 3050ms]`. The staggered design lets V_0 capture maximum MEV under healthy propagation while deeper backups absorb tails when V_0's bundle doesn't reach in time.

For wider partition absorption (cross-round retention with explicit re-flood), see [OBFTR(R≥2)](OBFTR.md#application-ssv-ethereum-proposer-duty) at the cost of an extra round of consensus and tighter submission headroom.

### Comparison vs QBFT (RT = 2000ms, 2-round target)

QBFT under SSV's production round-timeout (`RT = 2s = 10 BTT`) at the same operating point (`BTT = 200ms`, `Relay_cutoff = 4000ms`, `header_submit_headroom = 100ms`):

| t (ms) | Event | Notes |
|---|---|---|
| 0 | Slot start | |
| 150 | `RANDAO` done | |
| 900 | `PROPOSE_1` | Round-1 leader's MEV-fetch budget = **750ms** (RANDAO + fetch must fit before PROPOSE_1) |
| 1500 | Round-1 success target | `BFT_start_1 + 3 BTT` — PROPOSE → PREPARE → COMMIT |
| 2900 | `RT_1` fires | Round 1 timed out; round-change |
| 3100 | `PROPOSE_2` | Round-2 leader's MEV-fetch budget = **2950ms** (re-fetch resumes at RANDAO; broadcast at 3100) |
| 3700 | Round-2 consensus done | `BFT_start_2 + 3 BTT` |
| 3900 | Post-consensus done | `+1 BTT` for σ aggregation |
| 4000 | `Relay_cutoff` | Cert + submit fit in 100ms |

**MEV-freshness ranking** at this operating point (incl. partial-sigs-on-pre-agreed-V baseline):

| Rank | Leader | MEV-fetch budget | Notes |
|---|---|---|---|
| 1 † | Partial-sigs on pre-agreed V | **3550ms** | Floor; only available if V is pre-agreed (no MEV / no V-disagreement) — not directly applicable to SSV proposer duty |
| 2 | OBFT V_0 | **3050ms** | Best BFT-consensus leader for proposer duty |
| 3 (tie) | OBFT V_1 | 2950ms | |
| 3 (tie) | QBFT R2 leader | 2950ms | |
| 5 | OBFT V_2 | 2750ms | |
| 6 | OBFT V_3 | 2150ms | |
| 7 | QBFT R1 leader | 750ms | |

† **Partial-sigs assumes V is pre-agreed across operators** — this works for non-MEV duties (attestations, sync committee) where V is determined by beacon-spec computation, but not for proposer duty where V varies per operator. Listed as the no-consensus floor: BFT consensus protocols pay 500-2800ms over this baseline to resolve V-disagreement.

**Comparison vs partial-sigs floor**: OBFT V_0 pays a **500ms BFT-consensus tax** over the partial-sigs floor (3550 − 3050ms) — this is the structural cost of resolving V-disagreement in a single round at this operating point. The 500ms = 2.5 BTT decomposes as: **1 BTT V_0 leader-broadcast propagation** (OBFT has a single leader source V; partial-sigs assumes V is independently agreed at each operator and skips this step), **1 BTT Δ_2 widening** (OBFT's recommended Δ_2 = 2 BTT for jitter absorption vs partial-sigs' 1 BTT propagation cycle), and **0.5 BTT Phase 3 ε_3** (IBE decryption walk + certificate construction beyond simple BLS aggregation). QBFT R1 pays a **2800ms tax** (5.6× larger), structurally constrained by needing PROPOSE_1 to fire early enough that consensus + post-consensus + R2 retry can fit the slot.

**Comparison OBFT vs QBFT**: OBFT's V_0 captures **100ms more** MEV-fresh fetch time than QBFT's R2 leader (3050 vs 2950ms), and **2300ms more** than QBFT's R1 leader (3050 vs 750ms). All four OBFT leaders beat QBFT R1 by ≥1.4s; only QBFT R2 reaches V_1 parity, and only after paying the round-1 timeout gap (`RT_1 − BFT_start_1 = 2000ms` of wall-clock that QBFT must spend committed to round 1's PROPOSE before round 2 can fire its fresh-V fetch). OBFT's K-layer fall-through is in-round (sequential local IBE decryption, no per-layer RTT), so it lands the same `Relay_cutoff` budget without the round-change penalty.

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_k` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_k`. The protocol consumes the verdict; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_k)` *exactly once per slot/layer*, on the final `V_k` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_k, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_k` is locked on the originally-signed value. The layer's leader cannot subsequently emit NR/NV on `nr_tag_k` — the Phase-1 σ is their σ-side commitment per cross-phase exclusivity.

**Receiver-side validity stabilization — host best-effort toward assumption 3.** OBFT requires host validity to be unanimous at decision time (see [Assumptions](#assumed)). For SSV's proposer duty, the host narrows the divergence window via per-operator validity stabilization: each operator runs the validity check (including `parent_root`-vs-head) **once at Phase-1 acceptance** against a stable head snapshot, then locks the verdict for the remainder of the slot. Subsequent head movements within that operator do not flip the verdict.

**The validity-locking window is per-V, bounded by `B_k`.** Operators accept Phase-1 bundles in `[slot_start, T_commit]`. Each operator locks their verdict per V at first-observation of that V. For V_k, the cluster-wide spread of lock-times is at most `B_k` (= L_k's propagation budget) — the time from earliest possible first-observation (right after `T_broadcast_max_k`) to latest (just before `T_commit`). At the operating point above: V_0's window ≈ 100ms (= 0.5 BTT), V_1's ≈ 200ms (= 1 BTT), V_2's ≈ 400ms, V_3's ≈ 1000ms. **For the practical case where the cluster reconstructs V_0 (healthy path), the relevant window is V_0's 100ms.** Validity-divergence at deeper layers (V_k for k > 0) has a proportionally wider window, but those layers are only reached on fall-through (silent/late primary leader), so the divergence-rate × fall-through-rate product is the practical metric. A re-org landing inside V_0's window can split honest verdicts; the all-honest rate of validity-divergence slot-misses scales with re-org distribution within V_0's `B_0`. **In adversarial-byz deployments the operational rate is multiplicatively higher** — a byzantine within the f-bound exercising passive f-budget (silence or σ-on-V; neither cryptographically slashable individually) widens the deadlock zone beyond the all-honest case. See [§Failure modes / Validity-divergence deadlock](#failure-modes) for the `re-org rate × byz-passivity-rate` scaling.

**This per-operator locking does NOT guarantee cluster-wide convergence.** Operators accept Phase-1 bundles at different `t` within the gossipsub propagation window (operator-local first-observation timestamps, plus clock skew δ). If a re-org happens during this acceptance window, operator A may snapshot pre-reorg (verdict = valid) while operator B snapshots post-reorg (verdict = invalid). Both locks hold; cluster views diverge. The locking ensures *per-operator* stability across the slot, narrowing the divergence window to "re-org during gossipsub-acceptance window" — typical P99 ≈ 100–500ms — but does not eliminate it.

When the residual window fires (re-org lands inside the acceptance window AND operators split across the boundary), assumption 3 is violated and the slot may miss in-protocol — neither σ-quorum (capped at honest-σ-validators + leader's σ_L^V) nor NR-quorum (capped at honest-NV-count) reaches threshold under adversarial byz that withholds NR. This deadlock is the cost of strict re-validation. The looser alternative (validate once at acceptance, never re-check) avoids the in-protocol deadlock but commits on a V whose parent may become orphaned (relay/beacon submission rejection at submit time, also a slot miss). Hosts pick between the two failure modes based on observed re-org rates.

The "permit and slot-miss" framing parallels OBFT's equivocation handling: validity-divergence is a view-divergence pattern that the protocol does not recover from. Unlike equivocation, it's not attributable to anyone (re-orgs are real-world events, not protocol violations), so no slashing applies; it relies on assumption 3 being approximately true. **Same exposure as OBFTR(R≥2)** — R-machinery doesn't help with validity-divergence at any R.

**Backup-leader re-org resistance.** Fetching `V_k` for k ≥ 1 from a deeper-confirmed parent (the asymmetric `T_broadcast_max_3 < ... < T_broadcast_max_0` schedule already accommodates this) reduces the likelihood that the backup's parent becomes orphaned. Backups are structurally re-org-resistant by construction.

Implementation notes:

- Each operator validates `parent_root` against a stable head snapshot taken at Phase-1 acceptance time; the verdict is locked into the operator's local state for the remainder of the slot.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" and "single V per operator per layer" — a second signing attempt with a different V at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing AND operator-side double-signing detectable cluster-wide.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same. **Same DKG cost as OBFTR and [2abOBFT](2abOBFT.md).**

2. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. The deadlines:

   - **`T_broadcast_max_k = Ls_arrival − B_k`** per-layer: leader L_k must finish broadcasting by this deadline so its bundle propagates to all honest before `Ls_arrival` (= `T_commit − slack`) under that layer's per-layer propagation budget `B_k`. See §Setting for the staggered-budget design (`B_0 < B_1 < ... < B_{K-1}`).
   - **`T_commit`**: receiver acceptance cutoff. Bundles first-observed past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum at this layer.

   Phase-window minimums:

   - **`Δ_2 ≥ 1 BTT`**: `KindCommit` propagation budget — operators emit `KindCommit` at `T_commit`, peers must receive it before Phase 3.
   - **`Δ_3 ≥ ε_3`**: Phase 3 is purely local reconstruction processing (BLS aggregation, IBE decryption walk, certificate construction); ≈ 100ms at Config A.

   All are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

3. **Choosing K (layer count).** K is per-duty:

   - `K = 2`: BFT-min at f=1; **not recommended for OBFT** — exposes the late-deepest-layer-leader-broadcast Class A failure mode (see §Failure modes).
   - `K = 3..n`: provides multiple fall-through layers within Phase 3's single reconstruction walk. At `n = 4`, max K is 4 (one layer per operator). Each extra layer adds a level of chained encryption (~1 KB per onion at K=3, ~3 KB at K=4 — within practical bandwidth).

   **Two K bounds (see §Setting for the full discussion):**
   - **`K ≥ f+1`** — BFT-liveness minimum (≥ 1 honest leader exists by pigeonhole over the f-byz bound).
   - **`K ≥ f+2`** — late-leader-resilience recommendation (≥ 2 honest leaders, so a single late-broadcasting honest leader doesn't foreclose the slot via the deepest-layer NR-lock pathology — see §Failure modes / Late deepest-layer leader broadcast).

   Recommended for OBFT proposer duty: **`K = n = 4`** (maximum fall-through depth at f=1, every cluster member leads exactly one layer). `K = f+2 = 3` is also viable at slightly lower bandwidth.

4. **R is fixed at 1.** OBFT is single-round by design; for deployments needing the larger `R · P99` partial-synchrony envelope, use [OBFTR(R≥2)](OBFTR.md). The trade-off: OBFT saves ~1200ms of slot budget vs OBFTR(R=2), drops cross-round EKM atomicity / cached-partial-persistence / re-signing-fallback requirements, and fits at higher P99 where OBFTR(R=2) doesn't (e.g., P99 = 500ms) — at the cost of narrower partition envelope.

5. **Tag construction and replay.** Each NR tag must uniquely bind `(slot, cluster, layer)` to prevent replay across slots/layers/clusters. Structure: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Same construction as OBFTR.

6. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one OBFT instance and assumes:
   - Single OBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between OBFT (`protocol_tag = "OBFT-v1"`) and any other path that signs against the V-signing share (OBFTR, [2abOBFT](2abOBFT.md), QBFT, etc.).
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`), not just submission.

7. **Equivocation is permitted, not recovered.** OBFT does not provide an in-protocol equivocation recovery mechanism. Some patterns reach qV naturally (when 2-of-3 honest happen to σ-commit on the same V at f=1 n=4; leader's σ_L^V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. Equivocation is treated as a slashable byzantine fault; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots. **Same exposure as OBFTR(R≥2)**. The **Phase 2a/2b split** (see [2abOBFT](2abOBFT.md)) recovers most equivocation patterns at +1 RTT per slot (1-1-1 splits, all-honest-NR, h_V=1) and preserves cryptographic safety AND f-tolerant liveness; the 2-1-byz-defect pattern regresses vs bare OBFT (see [docs/2abOBFT.md / 2-1 split](2abOBFT.md#2-1-split)). Documented as future improvement.

## Where this came from

OBFT is the simpler-spec point in the OBFT family, derived from [OBFTR](OBFTR.md) by fixing R = 1 and stripping the round-retry machinery. The motivation: under analysis, OBFTR's R-round structure was found to add real partial-synchrony envelope (`R · P99` vs `P99`) but introduce significant spec/EKM complexity (cross-round σ-or-NR exclusivity, cross-round atomicity in EKM, persistent partial-sig caching, deterministic re-signing fallback, L_C cluster-consensus signaling, per-round acceptance widening, auth-only-retention state, cross-round dedup logic) — most of which adds nothing for the failure modes that R-machinery was originally introduced to address (byzantine grief patterns are R-invariant; only network-partition tails in `(P99, R · P99]` benefit from R ≥ 2).

OBFT is the design point that asks: **what's the minimum machinery needed to get K-layer parallel fall-through, without rounds and without within-slot partition recovery?** The answer:

1. **Drop L_C cluster-consensus.** Its only purpose in OBFTR is round-transition coordination; with R = 1, no transitions to coordinate.
2. **Drop cross-round σ-or-NR exclusivity.** With R = 1, no cross-round to enforce; cross-phase exclusivity (within Phase 1 + Phase 2) is sufficient for Pigeonhole 1.
3. **Drop cross-round σ-partial dedup, retention widening, auth-only-retention.** Bundles first-observed past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum at this layer.
4. **Drop EKM cross-round atomicity, cached-partial persistence, deterministic re-signing fallback.** Single signing event per (slot, layer) per operator; standard transactional sign-and-log.
5. **Drop the Defer state and Phase-2 sub-phasing — primarily for spec/wire/EKM simplification.** The Defer state requires a 4-state commitment lattice (σ, NR, NV, Defer), multi-emission wire format (separate σ-side and NR-side messages with different timings), auth-only-retention pre-state for tracking peer σ-claims in the pre-`T_commit` window, transitional EKM signing-event boundaries (Defer→σ vs initial σ-commit), and a wider Phase-2 window — see [Appendix E](#appendix-e--defer-state-within-slot-partition-recovery). Removing Defer collapses the protocol to a 3-state lattice (σ, NR, NV) with a single combined `KindCommit` message per operator; each operator commits exactly once at `T_commit` based on what they observed by then. The cost is no within-slot partition recovery — the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time. Suited for small clusters where gossipsub mesh is effectively full and asymmetric propagation is rare; for larger clusters or wider partition tails, use [OBFTR](OBFTR.md).
6. **Keep K-layer fall-through, chained encryption, equivocation detect-and-slash, slashing-evidence rules.** These are R-orthogonal — same value at R = 1 as R ≥ 2.

The result is a protocol that gives K-layer parallel fall-through without partition-recovery machinery, paying in narrower envelope for spec/EKM simplicity, more submission headroom, and high-P99 fit. **A structural side-effect of Defer removal — not the motivating reason for the removal — is closure of the h_V=1 withhold-then-fake-σ adversarial-byz attack** that earlier OBFT-family designs were exposed to (see §Failure modes for the attack mechanics).

**Path forward — Phase 2a/2b is the OBFT-family-level structural fix (R-orthogonal).** Phase 2a/2b is specified as [2abOBFT](2abOBFT.md); it addresses an OBFT-family limitation (validity-divergence + adversarial byzantine deadlock) that bare OBFT and OBFTR both inherit. The Phase-2 split (broadcast-only Phase-2a where operators re-flood retained Phase-1 bundles without σ-emitting, then Phase-2b where σ-emits happen on a cluster-converged V after Phase-2a observation completes) is the structural fix for the limitations that bare OBFT and OBFTR document in their respective failure-mode sections. **Smaller variants (relax cross-phase exclusivity, allow NV → σ flip via aggregator filtering, separate cryptographic tags for tentative-vs-final commitment, etc.) all either break safety against an offline-aggregating byzantine or are isomorphic to Phase 2a/2b under a different name.** Phase 2a/2b's structural shape is essentially forced: synchronous consensus on validity (or σ-commitment more generally) requires explicit cluster-wide coordination, which means an observation phase before commitment.

The mechanism — deferring σ-commitment until after cluster-wide Phase-2a observation — has three effects on the failure-mode taxonomy that apply equally to OBFT and OBFTR(R≥2):

1. **Recovers the Class A validity-divergence deadlock** (assumption 3 violated by re-org during acceptance window). Phase-2a's observation step lets operators see cluster-wide σ-eligibility state and converge on a stabilized validity verdict; Phase-2b σ-emit happens on that verdict rather than each operator's local at-acceptance snapshot. This brings validity-divergence into recovery scope rather than leaving it as an out-of-scope assumption violation. (The "leader σ_L^V locking on stale V" concern collapses into this same fix — without a Phase-1 σ_L^V, the leader doesn't pre-lock; Phase-2b's σ-emit is on the post-observation stabilized V.)

   **Same fix also resolves the Class A late-deepest-layer-leader-broadcast pathology** at K = f+1 (see §Failure modes). Without Phase-1 σ_V from the leader, no early operator-side σ/NR commitment; a late-arriving Phase-1 bundle observed in Phase-2a is σ-emittable in Phase-2b without any pre-locked NR commitments to override.

2. **Removes the Class B byzantine grief surface** for byzantine actions that exploit early σ-commitment:
   - *Equivocation σ-locked split patterns* (1-1-1, etc.). By deferring σ-emit, no honest carries an "initial" σ partial on a non-winner V; single-σ-V exclusivity stays intact; Pigeonhole 2 holds; at most one V reaches qV cluster-wide *regardless of equivocation pattern*.
   - *Byzantine refusal coordinated with honest transient flakiness*. Honest operator who would NR due to transient flakiness can defer their commitment to Phase-2b; if the condition resolves before Phase-2b, they σ instead of NR-locking themselves out for the slot.

   These Class B grief patterns are currently "permitted" because they're eventually accountable via rational-byzantine deterrent, but with weak (gossipsub-pattern / operator-unreliability) slashability. Phase 2a/2b removes the protocol-level grief surface so the deterrent doesn't need to do this work.

3. **Bonus: improves per-operator participation efficiency** under transient errors — operators with brief flakiness during Phase 2 don't lose their slot contribution.

Costs **+1 RTT per slot** at OBFT (Phase 2 grows by ~1 BTT for the Phase-2a observation window). Preserves cryptographic safety AND f-tolerant liveness — strictly better than the qV-bump alternative which trades f-tolerance for safety.

**Status: near-term for any deployment under realistic adversarial conditions.** Current OBFT (without Phase 2a/2b) trades the recovery properties above for spec simplicity and the +1 RTT savings. The trade-off is acceptable when **all three** hold:

1. The deployment's re-org rate is low enough that assumption 3 (host validity unanimous at decision time) holds in practice (re-orgs during gossipsub-acceptance window are sufficiently rare).
2. Byzantine operators value future participation enough that assumption 4 (rational-byzantine deterrent) is *quantitatively* effective, not just *qualitatively* present — stake-to-grief-value ratio is high, governance is responsive, slashability evidence quality is strong (the cryptographic-self-contained slashing rules cover most byzantine fault classes the deployment is exposed to).
3. The cluster's coordination SLA is short enough that across-slot accountability bounds grief faster than byz can re-enter.

For deployments where any of these is weak — small clusters, transient operators, weak governance, high-stake-to-grief-value MEV proposer slots, low-evidence-quality fault classes (selective-delivery, mesh-flakiness-correlated NR-refusal) — **Phase 2a/2b should be considered near-term, not future**. The +1 RTT cost (~200ms at BTT=200ms) is small relative to OBFT's submission headroom and substantially smaller than the cost of running adversarial-byz exposure under the bare protocol.

**This is a more aggressive framing than "future improvement."** OBFT standalone is the spec-simplest *single-round point*; [2abOBFT](2abOBFT.md) is the more robust *production* point for adversarial deployments and arguably should be the recommended configuration for SSV proposer duty unless deployment conditions explicitly support assumption 4 strength.

The relationship across the OBFT family:

| Protocol | R | K | Phase-2 split | Role |
|---|---|---|---|---|
| OBFT | 1 | configurable | no | this protocol — minimum machinery for K-layer fall-through |
| [OBFTR](OBFTR.md) | configurable (typically 2) | configurable | no | OBFT + R-round retry for `(P99, R · P99]` partition coverage |
| [2abOBFT](2abOBFT.md) | 1 | configurable | yes (Phase 2a/2b) | OBFT + Phase 2a/2b for view-divergence and adversarial-byz recovery |

## Appendix A — Protocol comparisons

This appendix gives high-level structural comparisons against the protocols OBFT relates to: [OBFTR](OBFTR.md) (the multi-round generalization), [2abOBFT](2abOBFT.md) (the Phase-2-split successor that recovers equivocation and validity-divergence in-protocol), and QBFT (SSV's existing consensus protocol).

### A.1 — Comparison with [OBFTR](OBFTR.md) (R ≥ 2)

OBFT is OBFTR with R fixed at 1 and the round-retry machinery stripped. They share Phase 1 / Phase 2 / Phase 3 structure, K-layer fall-through, chained encryption, the four commitment states, and the four slashing-evidence rules. OBFT differs from OBFTR by what's *removed* from the spec rather than by adding anything.

| Aspect | [OBFTR](OBFTR.md) (R = 2) | OBFT |
|---|---|---|
| Round structure | Up to R rounds with re-flood retry; round transitions on timer or L_C-quorum promotion | Single round, no retry, no transitions |
| L_C cluster-consensus signaling | `KindLCClaim` message kind in Phase 2.5 for round-transition coordination | **Removed** (no rounds to transition between) |
| Phase 2.5 | Present (carries `KindLCClaim`) | **Removed** entirely |
| Per-round acceptance widening | `T_candidate_accept_r` widens across rounds; late bundles auth-only-retained for next round | **Removed** — bundles first-observed past `T_commit` not counted; cluster relies on K-layer fall-through |
| Cross-round σ-or-NR exclusivity | EKM enforces across rounds + cross-phase | **Cross-phase only** — no rounds to span |
| Cross-round σ-partial dedup | Phase 3 deduplicates per-operator across rounds | **Removed** — single round, no cross-round duplicates possible |
| EKM cross-share atomicity | Required across rounds; sign-and-log atomic across V-share + IBE-share for re-emission semantics | Required per single signing event only; standard transactional behavior |
| EKM persistent partial-sig cache | Required (cached σ partial must survive operator restart for cross-round re-emission) | **Not required** — single signing event per (slot, layer) |
| EKM deterministic re-signing fallback | Required (allow re-sign if log row matches same `(slot, layer, side, value_root)`) | **Not required** |
| Within-slot partition recovery | Defer + late-σ-emit; round-2 retry | **None** — bundles past `T_commit` are simply not counted; cluster falls through to a deeper backup |
| Commitment states per layer | σ / NR / NV / Defer (Defer enables late-σ-emit recovery within Phase 2) | σ / NR / NV (3-state; no Defer) |
| Wire format per operator | Separate `KindOnion` (σ-side, may emit multiple times) + `KindNR` (NR-side, end-of-window) | Single `KindCommit` at `T_commit` carrying both σ and NR partials |
| Partial-synchrony envelope | `R · P99` (e.g., `2·P99` at R=2) | `P99` (single round) |
| MEV-fetch budget for primary leader (K=4, BTT=200ms, `header_submit_headroom = 100ms`) | ~1.45s (T_commit_1 = 2.00s constrained by R1+R2 fit within 4s slot) | **~3.05s** (T_commit = 3.40s; single-round, no retry budget needed) — **+1.6s more MEV-fresh fetch** |
| Submission headroom (`header_submit_headroom`) | 100ms | 100ms |
| Consensus complete | slot_start + 3.90s | slot_start + 3.90s (same anchor; OBFT redirects the saved BTT-budget into the MEV-fetch window) |
| Bandwidth (healthy, n=4, K=4) | ~28 KB | ~28 KB (same; both include the σ_L^V witness section ≈ +1.5 KB) |
| Bandwidth (worst case at R=2 with round-1 failure) | ~52 KB | n/a (no round 2) |
| High-D fit (P99 = 500ms) | Does not fit 4s relay cutoff | Fits with ~1.3s submission headroom |

**Recovery scope:**

- **Within OBFT's `1 BTT` envelope at any single layer**: same K-layer fall-through, same equivocation natural-recovery, same byz-grief exposure (R-invariant patterns). OBFTR(R=2) additionally recovers asymmetric propagation past `T_commit` at L_0 via Defer-state late-σ-emit and round-2 retry; OBFT does not (cluster falls through to L_1 instead).
- **Outside OBFT's envelope but inside OBFTR(R=2)'s `2·P99` envelope**: aggressive-marginal partition tails in `(P99, 2·P99]` are recovered at OBFTR(R=2) (Class B partition recovery via round-2 re-flood), out-of-envelope at OBFT (cluster falls through to a deeper backup whose bundle did propagate, or slot-misses if no layer's bundle propagated in time).
- **Outside OBFTR(R=2)'s envelope**: both fail equivalently (slot misses, safety holds).

**When to choose which:**

- Use **OBFT** when the cluster's gossipsub propagation is reliably within `1 BTT` (typically n=4 or n=7 where the mesh is dense enough that asymmetric propagation past `T_commit` is rare); or when the deployment runs at higher P99 (≈ 300–500ms) where OBFTR(R=2) doesn't fit; or when spec/EKM simplicity and submission headroom outweigh OBFTR(R=2)'s wider absorption.
- Use **[OBFTR(R=2)](OBFTR.md)** when the propagation tail is meaningfully wider than P99/P999 budget and within-envelope coverage of `(P99, 2·P99]` partitions is needed; the extra ~1200ms of slot budget and EKM cross-round atomicity are acceptable costs.

A cluster could mix-and-match per duty: OBFT for proposer (where headroom matters and slot is short), OBFTR(R≥3) for attestation (where slot budget is generous and envelope width is the priority). The wire formats differ — OBFT's `KindCommit` collapses what OBFTR carries as separate `KindOnion` + `KindNR` — so per-duty migration requires per-duty wire-format selection.

### A.2 — Comparison with [2abOBFT](2abOBFT.md)

[2abOBFT](2abOBFT.md) is the Phase-2a/2b successor to OBFT — same K-layer onion structure and chained encryption, but with σ-commitment deferred to a Phase-2b after a Phase-2a observation window. The Phase-2 split adds +1 RTT per slot but recovers in-protocol the equivocation and validity-divergence patterns OBFT cannot.

| Aspect | OBFT | 2abOBFT |
|---|---|---|
| Phase 1 σ_V | Yes — leader signs σ_V at Phase 1 (cryptographic head-start) | **No** — leader broadcasts only `(V, σ^op)` at Phase 1; σ-commitment happens at Phase 2b alongside non-leaders |
| Phase 2 split | Single Phase 2 (operators emit one `KindCommit` at `T_commit`) | 2a (verdict broadcast / σ-eligibility observation) + 2b (σ-emit on cluster-converged V) |
| Equivocation σ-locked split (1-1-1, etc.) | **Slot misses at L_0** — slashable but no in-slot recovery | **Recovered** via convergence rule (verdict-quorum-short → NR fall-through to L_1) |
| h_V=1 selective-delivery deadlock | **Partially closed**: withhold-then-fake-σ pattern closed as a side-effect of Defer removal (which was motivated by spec/wire/EKM simplification — see [§Where this came from](#where-this-came-from)); selective Phase-1 delivery to one honest still slot-misses (algebraic limit at f=1, n=4) | **Closed** via Phase-2a verdict pool |
| Validity-divergence (re-org during acceptance) | **Out of scope** (assumption 3) — slot misses cleanly | **Recovered within f-bound** (3-of-4 majorities at f=1 n=4); 2-2 splits still slot-miss |
| Mesh-flakiness deadlock | Slot misses (cross-phase exclusivity locks flaky NR) | **Recovered** (Phase-2a defers commitment past mesh outliers) |
| New regression vs OBFT | n/a | **2-1-byz-defect** — byz leader equivocates V/V', verdict-claims σV(V), defects to NR at Phase-2b. Slot misses (slashable Rule 6b). Bare OBFT succeeded here via Phase-1 σ_V cryptographic lock. |
| Healthy-path latency | 2 RTTs | 3 RTTs (Phase 1 + Phase 2a + Phase 2b) |

**2abOBFT is OBFT's natural recovery-scope extension** at +1 RTT cost. For deployments where the Class B grief patterns matter (high-MEV proposer slots, small adversarial-byz clusters, mesh-flakiness conditions), 2abOBFT closes them in-protocol rather than relying on assumption 4 alone. See [Where this came from](#where-this-came-from) for the design rationale.

### A.3 — Comparison with QBFT

QBFT is SSV's existing consensus protocol. The key structural difference: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus partial-sig collection)**; OBFT (and the rest of the OBFT family) fuses the two by embedding partial signatures inside Phase-2 onions. Most observable trade-offs trace back to this structural difference.

For per-scenario liveness behavior (recovery scope, mechanism, outcome) see [Liveness comparison: OBFT vs OBFTR(R=2) vs QBFT](#liveness-comparison-obft-1-vs-obftr2-vs-qbft). This appendix covers the structural / cost dimensions: protocol shape, latency, bandwidth, safety posture, primitive complexity, and deployment maturity.

| Aspect | QBFT | OBFT (K=4 for proposer) |
|---|---|---|
| Protocol shape | Multi-round (PROPOSE → PREPARE → COMMIT) with per-round leader rotation; round-change on timeout | Single round, K-layer onion fall-through |
| Consensus and signing | Decoupled: consensus reaches a decided value, then a separate post-consensus phase collects 2f+1 partial sigs | Fused: Phase-2 onions carry σ partials directly, Phase 3 reconstructs |
| RTTs to signed output (min, healthy) | 3 (consensus) + 1 (post-consensus) ≈ 4 | 2 (Phase 1 + Phase 2) |
| Termination guarantee | Eventually-terminating across rounds (under partial synchrony) | Conditional on `D ≥ real_propagation`; for larger envelope, use [OBFTR(R≥2)](OBFTR.md) |
| Safety posture | Honest-majority (`2f+1` honest) + correct quorum-certificate verification | Honest-majority cryptographic via `qEnc = qV = 2f+1` + chained IBE + EKM-enforced per-operator commitments. Same trust posture as QBFT — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). |
| Bandwidth (healthy n=4) | ~14 KB | ~28 KB at K=4 (includes σ_L^V witness section ≈ +1.5 KB) |
| Latency (healthy, n=4, BTT=200ms) | ~800 ms | ~600 ms (Phase 2 + Phase 3 with Δ_2 = 2 BTT) |
| Latency (1 round failure, n=4) | ~3.0 s (round-1 timeout 2s + round-2 success ~750 ms) | n/a (single round; failure → slot miss) |
| Slashing-protection scope | Single block sig per slot, gated at submission time | Multiple per-share sigs per slot at candidate-signing (Phase-1 leader, Phase-2 onion) gated by EKM cross-keypair coordination — see [EKM coordination model](#ekm-coordination-model) |
| Cryptographic primitive | BLS threshold signatures only | BLS threshold signatures + threshold IBE / SWE (drand/tlock-style) |
| Application contract | Decides on any value the application proposes | K-layer with primary + backup-leader fetches |
| Production maturity | What SSV runs today; corner cases hit and fixed | New codebase; deployment confidence to be derived |

**Cost-side trade-off summary:**

- **Latency.** OBFT wins on healthy-path (~600ms vs ~800ms). On round-1 failure, QBFT can still recover via round-change (at ~2.8s total), while OBFT single-round failures are slot-misses; OBFTR(R=2) covers round-1-failure cases at ~1.8s within the same envelope. OBFT's recovery scope is narrower than QBFT's but available much faster within scope.
- **Bandwidth.** QBFT lower healthy-path; OBFT higher due to onion encryption. On round failure, QBFT's round-change has its own bandwidth cost (~12KB extra round + a full additional consensus round); OBFT doesn't recover, so no failure-case bandwidth.
- **Cryptography.** QBFT only needs BLS threshold signatures. OBFT additionally needs threshold IBE / SWE (drand/tlock-style; audited, deployed since 2023). The IBE primitive is more novel; for risk-averse deployments, this is a real consideration.
- **Spec surface.** OBFT is meaningfully smaller spec than [OBFTR(R≥2)](OBFTR.md) (no rounds, no L_C consensus, no Phase 2.5, simpler EKM). Comparable in size to QBFT once you account for QBFT's view-change protocol and prepared-certificate verification.
- **Maturity.** QBFT is production. OBFT is a new codebase — deployment confidence has to be derived.

**Where QBFT genuinely wins for proposer duty:**

- **Validity-divergence recovery.** Head re-org mid-slot invalidates parent_root for L_0 candidate; honest verdicts genuinely diverge. QBFT round-changes through with new leader fetching at new head. OBFT requires the host to stabilize the verdict at Phase-1 acceptance (assumption 3). (Same gap as OBFTR(R≥2).)
- **1-1-1 equivocation recovery.** QBFT's round-change with new leader proposing a fresh V breaks the deadlock. OBFT relies on the rational-byzantine deterrent (assumption 4). (Same gap as OBFTR(R≥2).)
- **Cryptographic primitive simplicity.** BLS-only, no IBE.
- **Production maturity.** QBFT is what SSV runs today.

**Where OBFT wins:**

- **Healthy-path latency.** ~600ms vs ~800ms.
- **Multi-leader-failure recovery.** OBFT's K-layer parallel fall-through resolves K-1 silent layers within Phase 3's reconstruction walk (sequential local decryption, no per-layer RTT). For K=4 with 3 silent leaders, OBFT recovers in ~600ms; QBFT round-changes 3 times serially, exceeding the 4s budget.
- **All-honest-NR equivocation recovery.** When byz delivers V's early enough for re-flood to spread conflicts before T_commit, all 3 honest retain ≥ 2 V's and emit NR per the equivocation rule; NR-quorum at L_0 → fall-through to L_1. Same recovery as QBFT but in single round (~600ms vs ~2.8s).
- **Spec/EKM simplicity vs OBFTR(R≥2).** No cross-round atomicity, no L_C consensus, no per-round widening — see [§A.1](#a1--comparison-with-obftr-r--2).

**The operational bottom line:** QBFT covers more failure modes (its round-change-with-fresh-V handles validity-divergence and 1-1-1 equivocation that OBFT-family doesn't). OBFT wins on common-case latency and multi-leader-failure recovery. For SSV proposer duty under a 4s relay cutoff, the choice depends on observed re-org rate (favors QBFT), cluster's tolerance for the 1-1-1 equivocation case via the rational-byzantine deterrent (favors OBFT-family), and deployment complexity tolerance (favors OBFT over OBFTR(R≥2) in the family).

## Appendix B — L_Bid mini-consensus extension

This appendix specifies an opportunistic bid-routing extension to OBFT. **L_Bid** is a bid-determined top layer prepended to OBFT's K rotation-determined layers (yielding `K' = K + 1`). The extension adds a **mini-consensus phase** between Phase 1 and Phase 2 that resolves L_Bid's identity cluster-wide before σ-commitment. The mini-consensus is a single round of all-to-all verdict broadcast with quorum-based binding — verdicts are op-identity-signed claims, not threshold partials, so it adds no new cryptographic primitives and does not change OBFT's safety analysis.

The extension closes three deadlock surfaces that any naive bid-routing extension would expose ([§Background — bid-layer deadlocks](#background--bid-layer-deadlocks)) and adds two adversarial-byz residual surfaces at L_Bid (2-1-byz-defect, verdict-equivocation) plus the standard 2-2 validity-divergence hard algebraic limit. Healthy-path latency rises by `Δ_minicon` (~200-400ms at Config A); rotation-layer recovery scope is unchanged; safety is identical to bare OBFT. L_Bid relies on one additional assumption beyond bare OBFT's threat model — **bid-value honesty** (see [§Additional assumption — bid-value honesty](#additional-assumption--bid-value-honesty)).

### Background — bid-layer deadlocks

Any bid-routing extension that gates σ-eligibility on per-operator local computation over a bid set ("did I see enough bids? what's the highest?") has three deadlock surfaces under f-byz adversarial behavior. The mini-consensus exists to close them.

- **C1 — Selective bid-withholding.** A byzantine bidder withholds their `KindBid` from a subset of honest peers. Honest with incomplete bid sets cannot compute σ-eligibility correctly (their argmax may differ from cluster truth); honest with complete sets can. Honest σ-emit decisions fragment; the σ-pool fragments below `qV`; no V reaches quorum.

- **C2 — Bidder equivocation.** A byzantine bidder sends different `KindBid` envelopes (V vs V', possibly with different `bid_value`) to different peers. Each honest peer's argmax may yield a different winner; honest σ-commit on different V's; σ-pool fragments per V below `qV`.

- **C3 — Validity-divergence on the bid winner.** Some honest find the highest-bid V valid (parent-root match, fork/domain, etc.); others do not. Honest with valid view σ-commit; honest with invalid view NR. σ-pool below `qV` from one side; NR-pool below `qEnc` capped by σ-locked operators; deadlock with no fall-through.

Without a cluster-wide convergence step, all three lead to slot-miss. The mini-consensus replaces "each operator computes σ-eligibility from their local bid view" with "each operator broadcasts a verdict; the cluster binds when verdict-quorum reaches" — verdicts are observable, so honest peers converge on whether quorum reached even when underlying bid sets diverge.

### Additional assumption — bid-value honesty

L_Bid introduces one assumption beyond bare OBFT's: **operators report their bid values truthfully**. Without this, a byzantine operator can claim arbitrary `bid_value` for their preferred `V_i` and reliably win L_Bid argmax, routing the cluster to sign a low-MEV (potentially self-dealt) block. The grief leaves no on-wire evidence and produces no slot-miss, so none of [assumption 4's](#assumed) deterrent mechanisms (slashable evidence, slot-miss visibility to stakers, behavioral patterns visible to honest peers) discipline it per-slot. Safety and liveness are unaffected (Pigeonholes hold regardless); only L_Bid's MEV-value-capture motivation breaks.

The protocol does not enforce bid-value honesty cryptographically. Deployments satisfy it via one of:

- **Relay/builder attestation verification** (recommended for SSV proposer duty) — `KindBid` carries a `relay_attestation` field cryptographically binding `(V, bid_value)` to a cluster-recognized relay or builder; receivers verify before admitting the bid into `bid_set_i`. Spec'd as an optional protocol extension — see [§Optional extension — relay/builder attestation verification](#optional-extension--relaybuilder-attestation-verification).
- **Institutional / permissioned operator set** — operators are vetted such that bid-value lying carries external (legal, reputational, business) consequences sufficient to discipline it. The cluster relies on those external mechanisms; in-protocol attestation is unnecessary.
- **Post-hoc payment reconciliation** — stakers monitor cluster MEV revenue against expected relay payouts and migrate validators from clusters where claimed bid-values systematically fail to materialize. Same evidence-quality character as OBFT's behavioral-pattern fault detection (slow, false-positive-prone) but viable when in-protocol attestation isn't available.

This assumption is L_Bid-specific — bare OBFT does not depend on it. Deployments choosing OBFT + L_Bid implicitly load this onto their threat model, satisfied by one of the paths above.

### When to use it

**Suited for**: deployments where MEV bid-routing upside is significant relative to (a) the +1 RTT slot-budget cost of the mini-consensus phase, and (b) the new adversarial-byz residual surfaces at L_Bid. For SSV proposer duty under Config A: high-MEV slots where bid-routed block value capture exceeds the slot-loss-rate cost from the new L_Bid failure modes.

**BTT regime guidance** (see [§Deployment envelope by BTT](#deployment-envelope-by-btt) for full table):
- **BTT ≤ 400ms** (production-typical mesh): L_Bid recommended sizing fits with comfortable submission slack (700ms+).
- **BTT 500-1000ms** (degraded mesh): switch to minimum sizing (`Δ_minicon = Δ_2 = 1 BTT`); recommended sizing becomes tight or fails. Trade-off: less mesh-jitter absorption.
- **BTT > 1100ms** (severely degraded): L_Bid does not fit at the standard `T_commit` anchor; bare OBFT (3-BTT cycle) remains the available alternative.

**Not suited for**: deployments prioritizing minimum slot latency (the +1 RTT is non-trivial), or where adversarial-byz at the bid layer is a hard constraint (the new residuals are slashable but slot-miss without fall-through; see [§Liveness](#liveness)).

### Setting

Adds to OBFT's setting:

- **K' = K + 1 layers**: L_Bid (top, bid-determined) + OBFT's rotation-determined L_0, L_1, ..., L_{K-1}.
- **Bid envelopes**: every operator (not just rotation leaders) broadcasts a `KindBid` envelope at Phase 1.
- **Mini-consensus window** `Δ_minicon`: between Phase 1 and Phase 2, for verdict broadcast and convergence on L_Bid identity.
- **L_Bid σ-eligibility**: determined cluster-wide by mini-consensus, not by per-operator local computation.

`qV = qEnc = 2f+1` and the BLS+IBE keypair structure are unchanged from bare OBFT. The mini-consensus adds no new threshold cryptography.

### Wire kinds

In addition to OBFT's `Phase1Bundle`, `KindCommit`, `KindCertificate`:

- **`KindBid`** — operator `i`'s bid envelope. Payload `(protocol_tag = "OBFT-LBid-v1", message_kind = "bid-envelope", cluster_id, slot, operator_id i, bid_value, V_i, relay_attestation)`, signed by `i`'s operator-identity key. Carries `i`'s candidate `V_i` (the block they would commit to if their bid wins L_Bid) and the bid value. The `relay_attestation` field is host-defined; verification is governed by an optional protocol extension (see [§Optional extension — relay/builder attestation verification](#optional-extension--relaybuilder-attestation-verification)). When the extension is disabled, the field MAY be empty or host-supplied.
- **`KindBidVerdict`** — operator `i`'s mini-consensus verdict. Payload `(protocol_tag = "OBFT-LBid-v1", message_kind = "minicon-verdict", cluster_id, slot, operator_id i, predicted_LBid_value_root_or_null)`, signed by `i`'s operator-identity key. `predicted_LBid_value_root` is set when `i` claims a specific V is the cluster's L_Bid winner; null when `i` claims no L_Bid (insufficient bid-set visibility, parent-root filter failure, or no consensus reachable).

### Per-layer windows and deadlines

Phase 1 inherits bare OBFT's per-layer staggered broadcast deadlines (each leader `L_k` broadcasts by `T_broadcast_max_k = Ls_arrival − B_k`; see §Setting). Bid envelopes are broadcast in parallel within Phase 1 under L_0's deadline budget (treated as a parallel L_0-tier broadcast). The mini-consensus phase is inserted between Phase 1 and Phase 2:

| Phase | Window | Activity |
|---|---|---|
| Phase 1 | `[slot_start, T_commit]` | Rotation leaders broadcast Phase-1 bundles per per-layer windows; all operators broadcast `KindBid` in parallel. Receivers accept bundles first-observed in `[slot_start, T_commit]` per bare OBFT. |
| Mini-consensus | `[T_commit, T_commit + Δ_minicon]` | Bid-envelope re-flood; operators compute predicted L_Bid (argmax over received bid set) and broadcast `KindBidVerdict`. |
| Phase 2 | `[T_commit + Δ_minicon, T_commit + Δ_minicon + Δ_2]` | σ-or-NR commit at all K' layers (L_Bid + L_0..L_{K-1}). |
| Phase 3 | (from `T_commit + Δ_minicon + Δ_2`) | K'-layer reconstruction walk. |

Sizing:
- `Δ_minicon ≥ 1 BTT` (verdicts must propagate within the window).
- **Recommended** `Δ_minicon = 2 BTT` for jitter absorption, mirroring `Δ_2`'s widening.
- `Δ_2`, `Δ_3`: same as bare OBFT.

At Config A (BTT=200ms, recommended sizing: Δ_2 = 2 BTT, Δ_3 = ε_3): `Δ_minicon = 400ms`; total slot budget post-`T_commit` ≈ 900ms (vs bare OBFT's ~500ms). At §Application's max-MEV anchor (T_commit = 3.40s, `header_submit_headroom = 100ms`), adding `Δ_minicon` consumes 400ms from the MEV-fetch budget — V_X broadcast deadline shifts ~400ms earlier vs bare OBFT's V_0; primary leader's MEV-fetch budget reduces from ~3050ms (bare OBFT V_0) to ~2650ms (OBFT+L_Bid V_X) at the same submission headroom.

### Protocol

#### Phase 1 — Bid envelopes + rotation-leader broadcast

Each operator `i` (regardless of rotation role):

1. Fetches a candidate `V_i` from the host (e.g., MEV-Boost relay or vanilla beacon-node-built block) with `bid_value` (relay-attested or 0).
2. Constructs `KindBid` binding `(operator_id, bid_value, V_i, relay_attestation)`.
3. Signs with operator-identity key; gossips via gossipsub.

In parallel, each rotation leader L_k for `k ∈ {0, ..., K-1}` broadcasts their Phase-1 bundle as in bare OBFT.

Bid retention is keyed by `(slot, operator_id)`. A second distinct `KindBid` from the same operator is bid-equivocation, slashable via Rule 7. Honest operators count `i`'s **first-observed** bid for argmax computation; subsequent bids are dropped from convergence input but recorded for slashing.

The coherence rule binding a rotation leader's `KindBid` `V_i` to their Phase-1 bundle V is detailed in [§Coherence and cross-layer independence](#coherence-and-cross-layer-independence).

#### Mini-consensus — verdict broadcast

Each operator `i`, by their verdict-broadcast deadline `T_commit + Δ_minicon − 1 BTT`:

1. Computes `bid_set_i` = set of received-and-validated bid envelopes — operator-identity signature valid AND bid format valid AND host-validity verdict = valid. Host-validity follows the same locking semantics as bare OBFT's Phase-1 bundle validation (see [§Head-change handling](#head-change-handling)): on first-observation of `V_i`, the host validates `V_i` against a stable head snapshot taken at that observation moment and **locks the verdict (valid/not-valid) for the remainder of the slot**. Subsequent host-validity checks on `V_i` (in particular, the L_Bid Phase-2 emit-time check below) are reads of this locked verdict, never re-evaluations against a moved head. Bids whose locked verdict is not-valid are excluded from `bid_set_i`. Optional host-supplied filters MAY further restrict the set: parent-root within cluster-recognized set (see [Application: SSV Ethereum proposer duty](#application-ssv-ethereum-proposer-duty) for the SSV-specific filter); relay/builder attestation verification (see [§Optional extension — relay/builder attestation verification](#optional-extension--relaybuilder-attestation-verification)). Bids that fail any enabled filter are excluded from `bid_set_i`. (Coherence note: when a rotation leader's `KindBid` `V_i` and Phase-1 bundle V are the same value per [§Coherence and cross-layer independence](#coherence-and-cross-layer-independence), a single host-validity check on first-observation of either envelope produces the locked verdict applied to both.)
2. Computes `predicted_LBid_i`:
   - If `|bid_set_i| ≥ n − f` AND optional parent-root filter passes: `predicted_LBid_i = argmax_{V in bid_set_i} bid_value`, with `op_id` tiebreak on equal bids.
   - Else: `predicted_LBid_i = null` (insufficient visibility or no consensus reachable from `i`'s view).
3. Constructs `KindBidVerdict` binding the prediction. Signs with operator-identity key; gossips.

Operators broadcast verdict as late as possible within the mini-consensus window — at `T_commit + Δ_minicon − 1 BTT` — to maximize bid-envelope visibility (late re-flooded bids may change the argmax).

A second distinct `KindBidVerdict` from `i` for the same slot is verdict-equivocation, slashable via Rule 8. Honest receivers count `i`'s first-observed verdict toward convergence; subsequent verdicts are dropped from convergence input but recorded for slashing.

#### Convergence rule for L_Bid

At mini-consensus end (`T_commit + Δ_minicon`), each operator computes:

- `verdict_pool[V] = | { distinct ops j : j broadcast first-observed KindBidVerdict(j, slot, hash(V)) } |`
- `verdict_quorum_V = ∃V : verdict_pool[V] ≥ qV`

If `verdict_quorum_V` is true on some `V_X`, `V_X` is the cluster's L_Bid winner. Otherwise no L_Bid winner; cluster falls through to L_0 via NR-quorum at L_Bid in Phase 2.

**Pigeonhole on verdicts** — at most one V satisfies `verdict_quorum`: each operator contributes ≤ 1 first-observed verdict to one V's pool, so `Σ_V verdict_pool[V] ≤ n = 3f+1 < 2(2f+1) = 2 · qV` for f ≥ 1. Two V's both reaching `qV` would require > n verdicts. (Same shape as Pigeonhole 2 but on verdict envelopes rather than σ partials.)

#### Phase 2 — σ-or-NR commit at K' layers

Each operator constructs a K'-layer onion. L_0's σ partial is no longer plaintext; it is wrapped under `nr_tag_LBid` (outermost), gating decryption on L_Bid NR-quorum (see [§Why L_Bid is the outermost chained-encryption gate](#why-l_bid-is-the-outermost-chained-encryption-gate)):

```
layer L_Bid:        σ_i^V(V_X)                                                      # plaintext
layer L_0:          E_{nr_tag_LBid}( σ_i^V(V_{L_0}) )
layer L_k (k ≥ 1):  E_{nr_tag_LBid}( E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( σ_i^V(V_{L_k}) ) ) )
```

Per-layer commitment:

- **L_Bid**:
  - `verdict_quorum_V` reached on `V_X` AND operator retains `V_X` locally AND operator's locked host-validity verdict for `V_X` (anchored at first-observation per the Mini-consensus rule above) is valid → emit plaintext `σ_i^V(V_X)` at L_Bid.
  - Else → emit NR partial `σ_i^{IBE}(nr_tag_LBid)`.
- **L_0, ..., L_{K-1}**: same σ-or-NR commitment logic as bare OBFT (σ-state, NR-state, NV-state — three-state model from §Phase 1's operator commitments). Each operator emits a single `KindCommit` carrying their per-layer σ partials and NR partials.

Cross-phase exclusivity per `(slot, layer)` and single-σ-V per `(slot, layer)` per operator continue to hold across all K' layers; EKM enforces.

#### Phase 3 — Reconstruction walk

K'-layer walk starting from L_Bid:

1. **L_Bid**: σ-pool plaintext on `V_X`. If `|σ-pool[V_X]| ≥ qV`: reconstruct `(V_X, S)`; halt and broadcast `KindCertificate`.
2. Else: check NR-pool on `nr_tag_LBid`. If `≥ qEnc`: aggregate decryption key; unlock L_0's σ-pool; continue.
3. **L_0, ..., L_{K-1}**: same walk as bare OBFT's K-layer reconstruction.

If L_Bid reaches neither σ-quorum nor NR-quorum, the chained encryption to L_0 stays sealed, no fall-through, slot misses. See [§Why L_Bid is the outermost chained-encryption gate](#why-l_bid-is-the-outermost-chained-encryption-gate) for the design trade-off.

### Coherence and cross-layer independence

L_Bid extends OBFT's per-layer commitment model with one cross-layer constraint and preserves cross-layer independence elsewhere.

**Coherence — rotation-leader bid/bundle binding.** A rotation leader L_k commits to a single V per slot:

- Their Phase-1 bundle V (which they σ-sign at L_k as `σ_{L_k}^V`) and their bid envelope `V_i` are the same value. EKM enforces single-σ-V per `(slot, layer)`, so signing two distinct V's at the same `(slot, L_k)` is rejected; the coherence rule operates one level higher: the V the leader commits to in their bid is the V they bring to their rotation layer.
- A `KindBid` `V_i ≠ Phase-1 bundle V` at the same `(slot, leader)` is operator equivocation, slashable via Rule 7.

**Independence — non-rotation operators and cross-layer commitments:**

- Non-rotation operators (those not L_k for any k) have only a bid envelope `V_i`; no rotation-layer Phase-1 bundle. The coherence constraint does not apply.
- An operator's σ-or-NR commitment at one layer does not constrain another layer. A rotation leader L_k whose bid loses L_Bid still σ-emits at L_k on their (matching) bundle V if rotation-layer σ-eligibility holds; the L_Bid outcome and the L_k σ-decision are independent at the operator level.
- L_Bid σ-quorum on `V_X` reconstructs `(V_X, S)`; rotation-layer signatures are not used in that case. Conversely, L_Bid NR-quorum unlocks rotation-layer decryption; whichever rotation layer reaches σ-quorum supplies the output.

### Why L_Bid is the outermost chained-encryption gate

Chained encryption wraps L_0..L_{K-1} σ partials under `nr_tag_LBid` (outermost) plus the existing `nr_tag_0..nr_tag_{k-1}` chain. Decrypting any rotation-layer σ partial requires L_Bid NR-quorum first. This is a deliberate design choice with a concrete trade-off:

- **Benefit — cryptographic enforcement of L_Bid priority.** If the cluster reaches L_Bid σ-quorum on `V_X`, no rotation layer can produce an output (their σ partials remain encrypted). Pigeonhole 3's induction extends to K' layers: at most one V signature reconstructs cluster-wide, even when honest operators σ-commit at multiple layers. The bid-routed value is preferred when consensus reaches at L_Bid; rotation values are only accessible after L_Bid NR-quorum signals "no bid winner".
- **Cost — no fall-through if L_Bid deadlocks.** Adversarial-byz patterns at L_Bid (2-1-byz-defect, verdict-equivocation; see [§New residual failure modes](#new-residual-failure-modes-at-l_bid)) can produce σ-pool < `qV` AND NR-pool < `qEnc` simultaneously. Without NR-quorum, the rotation-layer chained encryption stays sealed, and rotation-layer σ-pools — even if they would otherwise reach `qV` — cannot reconstruct. The slot misses.

The alternative (L_Bid as a non-gating layer with rotation layers reachable independently) would close the no-fall-through gap but would also let rotation outputs fire whenever an honest operator σ-emits at their rotation layer regardless of L_Bid state, defeating bid-routing priority and creating split-output races. The chosen design preserves bid-routing semantics at the cost of L_Bid adversarial-byz exposure.

### Safety

Identical to bare OBFT.

The mini-consensus does not bind threshold partials. `KindBid` and `KindBidVerdict` envelopes contribute zero to either σ-pool or NR-pool — they only influence which Phase-2 emission an operator chooses. EKM enforces single-σ-V per `(slot, layer)` per operator at Phase-2 sign time exactly as in bare OBFT.

Pigeonholes 1, 2, 3 hold unchanged at K' = K + 1 layers. The additional `nr_tag_LBid` gate is structurally a deeper chained `nr_tag` and falls under Pigeonhole 3's inductive argument. Byzantine verdict misbehavior is slashable (Rule 8) but cannot violate cryptographic safety.

### Slashing-evidence rules

Inherits OBFT's 5 rules unchanged. Rule 5's "plaintext layer" binds to L_Bid in this extension (since L_0 is no longer the plaintext layer here — its σ is encrypted under `nr_tag_LBid`); a fake plaintext σ at L_Bid that doesn't verify against any retained `bid_set` member is slashable on the same construction as L_0 in bare OBFT. Two new rules cover L_Bid-specific surfaces:

- **Rule 7 — Bid equivocation / bid-bundle incoherence.** Two distinct `KindBid` envelopes from the same operator at the same slot, OR a `KindBid` `V_i` mismatching the same operator's Phase-1 bundle V at the same `(slot, rotation_layer)`. Self-contained slashable evidence — both envelopes signed by `i`'s operator-identity key.
- **Rule 8 — Verdict equivocation / verdict-vs-action equivocation.** Operator `i` either broadcasts two distinct `KindBidVerdict` envelopes for the same slot, OR broadcasts `KindBidVerdict(σV(V_X))` and emits Phase-2 NR partial on `nr_tag_LBid` (or claims null verdict and emits Phase-2 σ on `V_X`). Self-contained slashable evidence — both signed messages exist on the wire.

**Evidence quality** (paralleling [§Implications of the rational-byzantine deterrent (assumption 4)](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the main spec's rules):

| Fault class | Evidence type | False-positive risk |
|---|---|---|
| Bid equivocation, verdict equivocation, clear verdict-vs-action equivocation (verdict claims σ on V, Phase-2 emits NR — or vice versa) | Cryptographic, self-contained — single signed message-pair conclusively demonstrates the action | Very low |
| Verdict-vs-action at boundary-of-convergence cases (verdict claims null because `\|bid_set_i\| < n − f` at broadcast time, then late re-flood completes the bid set and operator σ-commits to the cluster's `V_X`) | Behavioral pattern — verdict timing vs late bid arrivals must be reconstructed from gossipsub history; the wire signature pair alone doesn't distinguish honest-late from byzantine-defect | Higher — hard to distinguish byzantine intent from legitimate late-bid observation |
| Bid silence (C1 — withholding own `KindBid` without any alternate emission) | Behavioral pattern — no signed message proves the operator failed to broadcast; observable only via aggregate honest reception failure across slots | Higher — same character as silence-grief in bare OBFT |

The same asymmetry as in bare OBFT applies: high-evidence-quality faults (clear equivocation) are also the ones the protocol handles cleanly within the slot (verdict-quorum reaches or doesn't, cluster falls through); low-evidence-quality faults (silence, boundary verdict-vs-action) are the load-bearing adversarial-byz attacks that engineer slot-miss-without-fall-through. The rational-byzantine deterrent's strength is correspondingly weakest where adversarial grief is most damaging — same structural property as bare OBFT, surfaced at L_Bid as well.

### Liveness

#### Recovery scope at L_Bid

The mini-consensus closes the three deadlock classes from [§Background — bid-layer deadlocks](#background--bid-layer-deadlocks):

- **C1 — Selective bid-withholding.** Byz withholds own `KindBid` from a subset of honest peers. Honest with `|bid_set_i| < n − f` verdict null. Verdict pool fragments below `qV` on every V → `verdict_quorum` false → all NR_LBid → fall-through to L_0 via NR-quorum. **Closed** (clean fall-through, no deadlock).
- **C2 — Bidder equivocation.** Byz sends conflicting `KindBid` envelopes to different peers. Honest argmax computations diverge → verdicts split across V's → no V reaches `qV` → all NR_LBid → fall-through. **Closed.**
- **C3 — Validity-divergence majority on V_LBid (3-of-4 at f=1, n=4).** 3 honest verdict `σV(V_X)`, 1 honest verdict NV/null. `verdict_pool[V_X] = 3 = qV` → cluster σ-binds on `V_X`. The dissenting honest NRs at L_Bid; the σ-pool reaches `qV` from the V_X-side honest. **Closed for 3-of-4 majority.**

#### Recovery scope at rotation layers L_0, ..., L_{K-1}

Identical to bare OBFT. Mini-consensus failure cleanly falls through to L_0 via NR-quorum at L_Bid. Bare OBFT's K-layer fall-through and three-state commitment model apply unchanged at rotation layers.

#### New residual failure modes at L_Bid

Three failure modes at the bid layer, all resulting in slot-miss without fall-through (chained encryption to L_0 stays sealed when L_Bid has neither σ-quorum nor NR-quorum):

- **2-1-byz-defect.** Byz bid-equivocates (V to majority-honest, V' to minority-honest), verdict-claims `σV(V)` to push `verdict_pool[V]` to `qV` via byz's own verdict, then defects to NR partial at Phase 2. At f=1, n=4: σ-pool[V] = 2 (majority-honest who have V and σ-emit on it); NR-pool = 1 (minority honest) + 1 (byz NR) = 2 < `qEnc`. Deadlock. Slashable via Rule 8 (verdict-vs-action equivocation).
- **Verdict equivocation at L_Bid.** Byz issues different verdicts to different peers, fragmenting per-peer verdict-pool views. Some honest see `qV`-quorum on `V_X` (and σ-bind); others don't (and NR). σ-pool < `qV`; NR-pool < `qEnc`. Same algebraic deadlock, same slot-miss-without-fall-through. Slashable via Rule 8.
- **2-2 validity-divergence at L_Bid.** Hard algebraic limit: when honest split 2-2 on `V_X` validity (and byz aligns to extend the split), no `verdict_pool` reaches `qV` and the NR-pool may also fall short under adversarial byz alignment. The same hard limit applies symmetrically in bare OBFT at L_0 — no protocol decides 2-2 validity divergence at f=1, n=4 without breaking BFT bound symmetry. (See [§Implications of validity-divergence not being recovered (assumption 3)](#implications-of-validity-divergence-not-being-recovered-assumption-3).)

#### Trigger frequency

Byz is **always** a bidder, so they can attempt bid-equivocation or verdict-equivocation in any slot. Bare OBFT's adversarial-byz L_0 surfaces (σ-locked-split equivocation, h_V=1 selective-delivery) trigger only when byz is rotation L_0 — `1/n` slots under uniform rotation. The L_Bid surfaces are correspondingly higher-frequency. Per-slot effect is the same order (slot-miss, slashable post-hoc via the cluster's coordination process); per-protocol-instance frequency is `n×` higher.

### Slot timing

Measured from `T_commit` (mini-consensus start). At Config A (P99=150ms, δ=50ms, `Δ_minicon = Δ_2 = 400ms` recommended):

| Scenario | Time | Mechanism |
|---|---|---|
| L_Bid σ-quorum reaches early in Phase 2 (early-reconstruct path) | ~`Δ_minicon + 1 BTT ≈ 600ms` | Verdict-quorum determines `V_X`; σ-emit propagation completes 1 RTT into Phase 2; operator reconstructs at L_Bid plaintext |
| L_Bid σ-quorum reaches at end of Phase 2 (canonical) | ~`Δ_minicon + Δ_2 + Δ_3 ≈ 900ms` | Full Phase 2 + Phase 3 walk |
| Mini-consensus fails (C1/C2 patterns) → fall-through to L_0 | ~`Δ_minicon + Δ_2 + Δ_3 ≈ 900ms` | NR-quorum at L_Bid + Phase-3 walk decrypts L_0; L_0 σ-quorum |
| Multi-layer fall-through after L_Bid | ~`Δ_minicon + Δ_2 + Δ_3 ≈ 900ms` | K'-layer walk in Phase 3 (sequential local decryption, no extra RTT per layer) |
| L_Bid 2-1-byz-defect or verdict-equivocation | slot misses | Deadlock at L_Bid blocks fall-through |

Best (success) ≈ 600ms; worst (success) ≈ 900ms; ~1.5× spread (smaller than bare OBFT's wider spread because the mini-consensus phase is mandatory). Healthy-path is +100-400ms vs bare OBFT (~500ms canonical post-`T_commit` at recommended Δ_2 = 2 BTT, Δ_3 = ε_3).

### Deployment envelope by BTT

The +1 RTT cost of `Δ_minicon` narrows L_Bid's deployment envelope vs bare OBFT meaningfully at higher BTT. The table below shows L_Bid's post-`T_commit` consumption and submission slack across BTT regimes (T_commit anchored at slot_start + 1.5s; T_relay_cutoff = 4.0s; `header_submit_headroom = 100ms`):

| BTT | Recommended sizing (`Δ_minicon = Δ_2 = 2 BTT`) | Minimum sizing (`Δ_minicon = Δ_2 = 1 BTT`) |
|---|---|---|
| 200ms | post-`T_commit` 900ms; consensus end 2.40s; slack 1.50s ✓ | 500ms; 2.00s; 1.90s ✓ |
| 300ms | 1300ms; 2.80s; 1.10s ✓ | 700ms; 2.20s; 1.70s ✓ |
| 400ms | 1700ms; 3.20s; 0.70s ✓ | 900ms; 2.40s; 1.50s ✓ |
| 500ms | 2100ms; 3.60s; 0.30s ✓ tight | 1100ms; 2.60s; 1.30s ✓ |
| 600ms | 2500ms; 4.00s; **−0.10s ✗** | 1300ms; 2.80s; 1.10s ✓ |
| 700ms | 2900ms; 4.40s; **−0.50s ✗** | 1500ms; 3.00s; 0.90s ✓ |
| 800ms | 3300ms; 4.80s; **−0.90s ✗** | 1700ms; 3.20s; 0.70s ✓ |
| 1000ms | 4100ms; 5.60s; **−1.70s ✗** | 2100ms; 3.60s; 0.30s ✓ tight |
| 1200ms | 4900ms; 6.40s; **−2.50s ✗** | 2500ms; 4.00s; **−0.10s ✗** |

(Slack = consensus deadline − consensus end, where the consensus deadline is `T_relay_cutoff − header_submit_headroom = 3.90s`. Negative slack means consensus end exceeds the deadline; the slot misses.)

**Sizing fallback at higher BTT.** Recommended sizing fits comfortably up to BTT ≈ 400ms, becomes tight at BTT = 500ms (300ms slack), and fails at BTT ≥ 600ms. **Switching to minimum sizing recovers the envelope** through BTT ≈ 1000ms — at the cost of absorbing only `1 BTT` of jitter beyond P99 propagation (vs `2 BTT` at recommended). The minimum-sizing fallback is appropriate when production telemetry shows P99/P999 propagation is tight against `1 BTT` and mesh-jitter is well-characterized.

**When L_Bid stops fitting at all.** At BTT ≥ 1200ms even minimum sizing fails — total post-`T_commit` exceeds the available slot budget at the same `T_commit` anchor (BTT=1100ms minimum sizing still fits with 100ms slack; BTT=1200ms minimum sizing misses by 100ms). Earlier `T_commit` (sacrificing primary leader's MEV-fetch budget) buys some additional headroom, but the slot envelope is fundamentally narrower than bare OBFT's.

**Net for deployment selection.** At production-typical BTT (200-400ms), L_Bid recommended sizing fits with comfortable submission slack (700-1500ms). At degraded mesh (BTT 600-1000ms), L_Bid requires switching to minimum sizing or accepting tighter mesh-jitter tolerance. At severely degraded mesh (BTT ≥ 1200ms), L_Bid does not fit; bare OBFT (3-BTT cycle) remains the available alternative.

### Optional extension — relay/builder attestation verification

The protocol-level mitigation for [§Additional assumption — bid-value honesty](#additional-assumption--bid-value-honesty). When enabled, the cluster cryptographically rejects bids with unattested `bid_value` claims.

**Wire format.** `KindBid`'s `relay_attestation` payload field carries the relay/builder's signature over `(V, bid_value)` plus the relay/builder identity (or a key reference into a cluster-recognized identity set). When the extension is disabled, the field MAY be empty or host-supplied; the wire format is unchanged across enable/disable, so clusters can toggle the check without coordinating on schema.

**Validation rule.** During mini-consensus bid-set computation, each receiver additionally checks: `relay_attestation` verifies against a cluster-recognized relay/builder identity AND binds the same `(V, bid_value)` pair carried in the `KindBid` payload. Bids that fail are excluded from `bid_set_i` and contribute nothing to argmax.

**What it closes:**

- **Bid-value inflation** — byzantine cannot claim higher `bid_value` than a recognized relay actually committed to.
- **Permanent L_Bid hijack via fake-bid spam** — byzantine's bids are capped by what cluster-recognized relays actually offer them.

**What it does not close:**

- **Selective bid-withholding** (C1) — handled at the protocol level via verdict-quorum-short → fall-through.
- **Bid equivocation** — covered by Rule 7.
- **Operator selecting a low-MEV bid from a recognized relay** — within the operator's discretion; not a protocol concern.

**Cost.** One signature verification per `KindBid` per receiver (small relative to overall cryptographic work) plus the bandwidth of carrying the attestation. Verification must complete within the mini-consensus window; sign-check throughput should be confirmed for the deployment's relay-set size.

**When to enable.** SSV proposer duty under realistic adversarial conditions (operators may run builders or collude with builders for self-dealing). Default ON for SSV's deployment.

**When safe to disable.** Deployments where bid-value honesty holds via the [institutional / permissioned operator set or post-hoc payment reconciliation paths](#additional-assumption--bid-value-honesty). Bandwidth and CPU savings are modest; the trade-off is the explicit assumption-shift documented in the additional-assumption section.

### Comparison with bare OBFT

| Aspect | Bare OBFT | OBFT + L_Bid mini-consensus |
|---|---|---|
| Slot structure | Phase 1 → Phase 2 → Phase 3 | Phase 1 → Mini-consensus → Phase 2 → Phase 3 |
| Layers | K (rotation-determined) | K' = K + 1 (L_Bid + K rotation-determined) |
| Wire kinds | `Phase1Bundle`, `KindCommit`, `KindCertificate` | + `KindBid`, `KindBidVerdict` |
| Slashing-evidence rules | 5 | 7 (+ Rule 7 bid equivocation/incoherence, + Rule 8 verdict equivocation/verdict-vs-action) |
| Healthy-path latency (post-`T_commit`) | ~500ms | ~600-900ms (+100-400ms) |
| Best-case latency | ~200ms | ~600ms |
| Worst-case latency (within success envelope) | ~400ms | ~700ms |
| Time-to-completion spread | ~2.7× best/worst | ~1.5× best/worst |
| Bandwidth (n=4, K=4 healthy) | ~28 KB | ~33 KB (+n bid envelopes, +n verdicts, +1 chained encryption layer) |
| MEV-fetch budget (4s cutoff, `header_submit_headroom = 100ms`, §Application's max-MEV anchor) | ~3050ms (V_0; T_commit = 3.40s) | ~2650ms (V_X; T_commit = 3.00s — Δ_minicon shifts T_commit ~400ms earlier) |
| Cryptographic primitives | BLS threshold + threshold IBE/SWE | Same (no new primitives) |
| **Safety** | Cryptographic via Pigeonholes 1, 2, 3 | **Same** |
| Rotation-layer (L_0/.../L_{K-1}) liveness | OBFT base recovery scope | **Same** (mini-consensus failure falls through cleanly; rotation layers unchanged) |
| L_Bid liveness — C1 selective bid-withholding | n/a (no bid layer) | **Closed** (verdict-quorum-short → fall-through) |
| L_Bid liveness — C2 bidder equivocation | n/a | **Closed** (verdict-quorum-short → fall-through) |
| L_Bid liveness — C3 validity-divergence majority (3-of-4) | n/a | **Closed** (verdict-quorum reaches on majority; minority NR) |
| L_Bid liveness — 2-1-byz-defect | n/a | **Open**: slot-miss-without-fall-through; slashable Rule 8 |
| L_Bid liveness — verdict-equivocation | n/a | **Open**: slot-miss-without-fall-through; slashable Rule 8 |
| L_Bid liveness — 2-2 validity split | shared hard algebraic limit at L_0 (BFT-theoretical, not protocol-specific) | **Open**: same hard limit at L_Bid |
| Bid-routing value capture | n/a | Highest-bid block on healthy path |
| Adversarial-byz trigger frequency at the bid layer | n/a | `n×` higher than bare OBFT's L_0 surfaces — byz is always a bidder, vs L_0-leader-only |

**Net trade vs bare OBFT**: pays +1 RTT and additional adversarial-byz residual surface at L_Bid (slashable but slot-miss-without-fall-through) in exchange for bid-routing value capture on the healthy path. The L_0/.../L_{K-1} layers' recovery scope is unchanged. Whether favorable depends on byzantine-frequency assumption and MEV-value-capture upside vs slot-loss cost from the new failure modes.

## Appendix C — Message re-broadcast considerations

This appendix examines whether non-leader operators should explicitly re-broadcast leader Phase-1 bundles at the application layer, beyond what libp2p's gossipsub already does at the network layer. The conclusions inform implementation choices and clarify which OBFT failure modes can vs cannot be addressed by faster propagation.

### Background — gossipsub auto-forwarding

OBFT relies on libp2p's gossipsub for cluster-wide message propagation. Gossipsub's standard behavior:

- When a node first receives a message it hasn't seen (deduplicated by message ID, typically a content hash), it **automatically forwards the message to its mesh peers** without any application-layer code.
- The mesh is a per-topic set of peers (typical 6-12 at production sizing). At small cluster sizes (n=4), the mesh is effectively full — every peer is in every other peer's mesh, so auto-forward reaches all peers in one hop.
- At larger clusters (n=10, n=13), mesh sampling means the first auto-forward reaches a subset of peers; second-hop forwards spread further. Total propagation P99 = `1 BTT` under partial synchrony.

So when the OBFT spec talks about "gossipsub re-flood", it refers to this network-layer mechanism — not application-layer re-broadcast. The OBFT protocol itself only requires the leader to publish their Phase-1 bundle once; the cluster-wide propagation happens via gossipsub.

### What explicit application-layer re-broadcast would add

Adding a "re-broadcast on first observation" step at the application layer (every non-leader operator publishes the leader's bundle to gossipsub on receipt) provides:

1. **Defense against gossipsub mesh sparsity / score-based pruning**. Gossipsub can prune mesh peers under score-based heuristics (slow-message detection, peer-score limits, mesh churn under load). Pruned peers might miss messages forwarded only by the source. With n−1 explicit re-broadcasts of the same bundle, drops are recovered by other operators' publishes.
2. **Faster cluster-wide convergence at larger n**. At n=4, gossipsub auto-forward is single-hop; explicit re-broadcast is redundant. At n ≥ 7 with sparse mesh, every operator becoming a publisher can compress propagation from 2-3 hops × heartbeat to 1 hop × heartbeat. This tightens the receiver-acceptance window's effective margin.
3. **Redundancy against partial mesh failures**. If a small subset of mesh links is degraded, multiple sources of the same bundle help it find a path. Gossipsub's gossip-fanout already provides this; explicit re-broadcast strengthens it.

### What explicit re-broadcast does NOT add

- **No additional defense against h_V=1 byz selective-delivery deadlock**. The withhold-then-fake-σ variant is closed as a side-effect of Defer removal (motivated by spec/wire/EKM simplification — see [§Where this came from](#where-this-came-from)); the selective-Phase-1-delivery variant remains an algebraic limit at f=1, n=4. Re-broadcast can't help with the latter — byz only delivered Phase-1 to one honest, and that's already what gossipsub auto-forward delivers from. The structural fix is Phase 2a/2b.
- **No additional defense against σ-locked equivocation 1-1-1 splits**. These failures are about cross-phase exclusivity locking honest into different σ commits, not about V propagation speed. Re-broadcast doesn't change the algebra.
- **No defense against silent leader**. If the leader doesn't broadcast at all, no non-leader has the bundle to re-broadcast. Recovery is via NR-quorum fall-through to L_1 (in-protocol).
- **No defense against sustained partition** (real propagation > absorption window). Re-broadcast can't deliver what no honest peer has received.

### Costs

- **Bandwidth**: O(n) multiplier per bundle. At n=4 with K=4, leader-only broadcast is 4 publishes per slot (one per layer's leader); non-leader re-broadcast adds 12 more publishes (each non-leader publishes each leader's bundle). At Config A's ~30 KB onion bandwidth, this adds a few KB. Modest at small n; meaningful at n=13 where it becomes ~10× the bundle bandwidth.
- **Gossipsub deduplication overhead**: each peer receives the same auth-signed bundle from multiple sources. Gossipsub deduplicates by message ID (content hash for self-validating messages), so this is mostly CPU/memory cost on each peer. Minor.

### σ_L^V re-inclusion is part of the core protocol

The σ_L^V witness re-inclusion mechanism — each operator including byte-for-byte copies of received Phase-1 σ_L^V partials in their own `KindCommit` — is part of the core protocol; see the Wire format paragraph in §Phase 2. Bandwidth is modest (~96 bytes per σ partial × K × n; ≈ 1.5 KB cluster-wide at K=4, n=4); it adds no new EKM event, no new signing obligation, and no new cryptographic primitive (operators forward bytes they already received); Pigeonholes 1, 2, 3 are unchanged. The mechanism protects σ_L^V against Phase-1 bundle drop at peer receivers but does **not** address V-drop — that requires the full-bundle re-broadcast described elsewhere in this appendix.

### Recommendations by cluster size

- **n = 4**: skip explicit re-broadcast. Gossipsub auto-forward is single-hop; mesh is effectively full. The bandwidth cost has no offsetting benefit.
- **n = 7**: marginal. Mesh sampling adds 1-2 hops; explicit re-broadcast may compress P99 propagation by 50-100ms. Worth measuring; not a clear win.
- **n ≥ 10-13**: explicit re-broadcast becomes more useful as mesh sampling adds more hops. Score-based pruning and peer-score variance also become more significant. Consider explicit re-broadcast as defensive engineering, especially in deployments with adversarial peer behavior or aggressive scoring config.

### Conclusions

1. **OBFT's spec relies on gossipsub auto-forwarding**, not on explicit application-layer re-broadcast by non-leaders. The cluster-wide propagation guarantee comes from gossipsub's standard mesh-forwarding behavior under partial synchrony.
2. **Explicit non-leader re-broadcast is a latency/bandwidth trade-off, not a safety/liveness change**. The OBFT protocol's stated guarantees don't depend on whether non-leaders explicitly re-broadcast — they're property of gossipsub's propagation under partial synchrony.
3. **Explicit re-broadcast addresses gossipsub-layer issues** (mesh sparsity, score-pruning, multi-hop latency) but does not address OBFT's adversarial-byz failure modes (h_V=1, σ-locked equivocation, validity-divergence). Those are structural; the fix is at the protocol level (Phase 2a/2b in [2abOBFT](2abOBFT.md)), not at the propagation level.
4. **Implementation choice should be driven by production telemetry**: if observed gossipsub propagation P99 ≈ `1 BTT`, gossipsub is already optimal and explicit re-broadcast is redundant. If P99 > 1.5 × `1 BTT` (suggesting multi-hop or pruning issues), explicit re-broadcast can compress the tail. Defensive deployments at larger n may opt in regardless.
5. **The σ_L^V re-inclusion mechanism is part of the core protocol** — every `KindCommit` carries a witness section with copies of received Phase-1 σ_L^V partials (see §Phase 2 / Wire format). Full Phase-1 bundle re-broadcast (V + σ_L^V + σ_L^op) remains optional defensive engineering on top of σ_L^V witness re-inclusion; the trade-off is the bandwidth O(n) multiplier per bundle vs the protection against V-drop in addition to σ_L^V-drop.


## Appendix D — OBFT-replenish (layer-staged extension)

This appendix sketches a candidate enhancement to OBFT — **OBFT-replenish** — that stages layer broadcasts across rounds instead of broadcasting all K layers up-front. The design preserves OBFT's chained-encryption and σ-or-NR-commit machinery but introduces new leaders per round, growing K dynamically as needed.

OBFT-replenish is positioned as an **enhancement of OBFT** (not OBFTR): it keeps OBFT's per-layer mechanics and adds a multi-round structure that introduces fresh leaders per round, where OBFTR's multi-round structure re-floods the same K leaders' bundles.

### Design idea

Instead of OBFT's "broadcast all K leaders' bundles in Phase 1 up-front; if no consensus, slot misses", OBFT-replenish runs:

- **Round 1**: 2 leaders (L_0, L_1) broadcast their Phase-1 bundles. Operators emit a 2-layer onion at Phase 2 (σ at validated layers, NR at non-validated). Phase 3 walks layers 0–1.
- **Round 2** (if round 1 inconclusive): 2 new leaders (L_2, L_3) broadcast. Decided layers from round 1 (σ-quorum or NR-quorum reached) are pruned from active state; undecided layers (where neither quorum reached, typically because asymmetric propagation prevented σ-quorum) are retained. Round-2 onion contains new layers L_2, L_3 plus retained layers' re-emissions if needed. Phase 3 walks all undecided + new layers.
- **Round N** (continuing): each round adds 2 new leaders, prunes decided layers, retries undecided layers. Round count is bounded by remaining slot budget.

K grows from 2 (round 1) to 2N (round N). The protocol terminates when σ-quorum reaches at any layer (success) or slot deadline expires (miss).

The "2 layers per round" choice is a parameter; could be 1 (slowest growth, most bandwidth-frugal) or 3+ (faster but bigger Phase-1 broadcast per round). 2 is a reasonable midpoint.

### Mechanics preserved from OBFT

- **σ-or-NR per (operator, layer)**: independent across layers (cross-layer hedging preserved).
- **Chained encryption**: L_k's σ partials encrypt under `nr_tag_0 ∧ ... ∧ nr_tag_{k-1}`. Chain extends naturally as new layers are added.
- **Reconstruction walk**: at each round's Phase 3, walk layers 0 → 1 → ... → K_current, where K_current is the highest layer index introduced so far.
- **EKM scope**: per-(slot, layer, side) slashing-protection rows; same schema as OBFT.
- **Slashing-evidence rules**: unchanged from OBFT base.

### Mechanics added (vs bare OBFT)

- **Cross-round σ-or-NR exclusivity**: an operator that σ-locked at L_0 in round 1 stays σ-locked at L_0 in round 2 (cannot retract). Cross-round atomicity in EKM (similar to OBFTR's requirement).
- **Round-transition signaling**: cluster needs to determine when round r is inconclusive and round r+1 should start. Could be timer-based (a per-round duration) or quorum-driven (similar to OBFTR's `KindLCClaim`).
- **Per-round Phase 1**: round-r leaders broadcast bundles in round-r's Phase 1, vs OBFT's single Phase 1 + OBFTR's round-1-only Phase 1.
- **Variable-K Phase 3 walk**: handle dynamic layer count instead of K fixed.
- **Layer-numbering extension**: deterministic rotation extends to unbounded layer indices (e.g., `L_{2N} = operator (slot + 2N) mod n`).

### Comparison with bare OBFT

#### Healthy-path latency

- OBFT: ~3D (Phase 1 + Phase 2 + Phase 3), ~600ms at P99=200ms.
- OBFT-replenish (round-1 success at L_0 or L_1): ~3D for round 1, **identical to OBFT**.

The reduced layer count in round 1 doesn't speed up consensus — the bottleneck is the Phase 1/2/3 cycle, not the number of layers. Healthy-case latency is a wash.

#### Bandwidth

| Case | OBFT | OBFT-replenish |
|---|---|---|
| Round-1 success (L_0 or L_1 σ-quorum) | K=4 bundles + n × 4-layer onion + n × σ_L^V witness ≈ 28 KB | 2 bundles + n × 2-layer onion + n × σ_L^V witness ≈ 15 KB |
| Round-2 success | n/a (single-round, slot misses if R1 fails on adversarial pattern) | ~26 KB (4 bundles + 2 × n × ~3-layer onion + 2 × n × witness) |
| Round-3 success | n/a | ~37 KB (exceeds OBFT) |
| Multi-round failure | flat 28 KB | grows linearly with R |

OBFT-replenish wins bandwidth in **round-1-success scenarios** (most production slots in healthy conditions) and breaks even at round 2. Past round 2, bandwidth exceeds OBFT.

#### V freshness (MEV value capture)

- OBFT: all K leaders fetch within the same slot prefix at asymmetric `T_{K-1} < ... < T_1 < T_0` deadlines. L_0 has the freshest V; backups (L_1..L_{K-1}) fetch earlier.
- OBFT-replenish: round-r leaders fetch *between rounds (r-1) and r* — strictly later than any round-1 leader's fetch.

**Real upside specific to OBFT-replenish** that bare OBFT can't match at any value of K. In OBFT, all K leaders' fetches share the round-1 Phase-1 window; in replenish, later layers' fetches are deferred until round transitions, capturing late-MEV moves. For high-MEV slots, replenish's late-layer freshness can recover MEV that OBFT misses.

#### Recovery scope (fall-through depth)

- OBFT: K=4 layers, 3 fall-throughs available (silent L_0 → L_1 → L_2 → L_3). Fixed.
- OBFT-replenish: K grows with rounds, fall-through depth = 2R. At R=3 rounds (typical fit in 4s slot budget), 6 layers ≈ 5 fall-throughs.

OBFT-replenish has **deeper fall-through** at the cost of round-by-round retry. Useful if multi-leader-silent patterns are observed.

#### Adversarial-byz failure modes

- OBFT: σ-locked equivocation 1-1-1, h_V=1 selective-delivery, validity-divergence — slot-miss patterns at any layer.
- OBFT-replenish: **identical exposure**. New layers introduced in later rounds are subject to the same byzantine patterns. If byz exercises σ-locked split at L_0 in round 1, the L_0 σ-locks block fall-through; later layers' chained encryption stays sealed under L_0/L_1's nr_tags.

R-invariant (same as OBFTR). The structural fix for these patterns is Phase 2a/2b ([2abOBFT](2abOBFT.md)), independent of layer count or replenishment.

#### Slot timing complexity

- OBFT: single set of phase deadlines (`T_commit`, relay-submission deadline); single Phase 1/2/3 cycle.
- OBFT-replenish: per-round deadlines (`T_commit_r` plus a round-duration); round-transition signaling needed.

OBFT-replenish's slot scheduling is closer to OBFTR's complexity than to bare OBFT's.

### When OBFT-replenish is worth the complexity over bare OBFT

- **Healthy-dominated production environments** (most slots succeed at L_0 or L_1): bandwidth saving is real; freshness advantage materializes on late-MEV slots; multi-round complexity is the cost.
- **High-MEV proposer duty with late-moving MEV**: round-r leaders' freshness advantage (vs OBFT's all-in-round-1 fetch) captures MEV that bare OBFT misses regardless of K.
- **Clusters with intermittent operator unreliability**: deeper fall-through depth (2R vs OBFT's K=4) provides more recovery against silent-rotation operators.

### When bare OBFT is preferable

- **Implementation simplicity is valuable**: OBFT has one round, one set of phase deadlines, fixed K, no cross-round atomicity. Replenish needs variable K, cross-round σ-locks, round-transition coordination — closer to OBFTR territory.
- **Failure rate is uncertain or stress-test environments**: OBFT's flat K=4 bandwidth is predictable; replenish's bandwidth grows with rounds and can exceed OBFT past round 2.
- **Adversarial-byz-heavy deployments**: replenish doesn't add adversarial-byz coverage; extra rounds are wasted under byz patterns.
- **Late BFT-start (e.g., BFT_start ≥ 2s, slot budget ≤ 1.25s)**: tight budget doesn't admit multi-round retries. Replenish's recovery depth is gated by round count, which is gated by budget. At late BFT-start, replenish reduces to roughly OBFT-with-K=2 with no recovery advantage.

### Conclusions

1. **OBFT-replenish is positioned as a multi-round enhancement of OBFT**, but its operational complexity profile (cross-round σ-locks, round-transition signaling, per-round Phase 1) is closer to OBFTR than to bare OBFT. The naming "enhancement of OBFT" is structurally fair — it keeps OBFT's chained-encryption + per-layer commitments machinery — but the protocol implementation effort is comparable to OBFTR.
2. **The genuine OBFT-replenish-specific advantage is V freshness at later rounds**, which neither OBFT (any K) nor OBFTR (re-flood, no fresh fetches) provides. For late-MEV proposer slots, this is structurally meaningful.
3. **Healthy-case bandwidth saving is real but conditional** — OBFT-replenish wins in round-1-success scenarios (K=2 instead of K=4 of bandwidth) and breaks even at round 2; past round 2, bandwidth exceeds OBFT. Whether favorable depends on observed round-1-success rate.
4. **Adversarial-byz exposure is unchanged from OBFT/OBFTR**. Replenish does not provide additional protection against σ-locked equivocation, h_V=1 selective-delivery, or validity-divergence patterns. The structural fix for those is Phase 2a/2b ([2abOBFT](2abOBFT.md)), orthogonal to replenishment.
5. **Healthy-path latency is identical to OBFT**. Reduced layer count in round 1 doesn't compress consensus time — the Phase 1/2/3 cycle is the bottleneck.
6. **OBFT-replenish vs OBFT trade summary**:
   - **+** Bandwidth saving on healthy slots (real if round-1-success dominates).
   - **+** V freshness at later rounds (genuine MEV upside for late-resolving slots).
   - **+** Deeper fall-through at the cost of multi-round retry.
   - **−** Multi-round implementation complexity comparable to OBFTR.
   - **−** Bandwidth grows past OBFT in multi-round failure cases.
   - **=** Same adversarial-byz exposure as OBFT (R-invariant patterns unfixed).
   - **=** Same healthy-path latency.

Treat OBFT-replenish as a research direction worth specifying further if the late-MEV freshness motivation is significant for the target deployment. If the priority is simplicity and predictable behavior, bare OBFT (K=4 fixed) remains the cleaner choice. If the priority is adversarial-byz coverage, [2abOBFT](2abOBFT.md) is the relevant lever — independent of whether replenish is adopted.

## Appendix E — Defer state (within-slot partition recovery)

This appendix describes a candidate enhancement to OBFT — **OBFT+Defer** — that adds a 4th per-(operator, layer) commitment state ("Defer") for receivers still waiting on V at `T_commit`. Defer enables late σ-emission within a `[T_commit, T_accept_max]` absorption window, recovering aggressive-marginal partition cases where a re-flooded bundle reaches some honest after `T_commit` but before `T_accept_max`.

OBFT+Defer is positioned as an **enhancement of OBFT** for partition tolerance — keeping OBFT's single-round, K-layer fall-through structure but trading spec/wire simplicity for a recovery mode that bare OBFT lacks. Defer was part of earlier OBFT-family designs and was removed from the current spec **primarily for spec/wire/EKM simplification** (see [§Where this came from](#where-this-came-from)); closure of the withhold-then-fake-σ adversarial-byz attack documented later in this appendix is a structural side-effect of that removal, not its motivating reason. This appendix documents the trade-offs so future spec revisions can re-evaluate whether the recovery scope is worth the costs in a given deployment.

### Design idea

Add a 4th commitment state, **Defer**, to the per-(operator, layer) commitment lattice. Each operator's per-layer state at `T_commit` is one of:

- **σ** (sign-on-V) — observed and validated V; emit σ partial.
- **NR** (no-receipt / silent-leader) — no V observed by deadline AND no peer σ-claims observed in the auth-only-retention window; emit NR partial.
- **NV** (non-validity) — host returned `not valid`; emit NR partial (operationally identical to NR).
- **Defer** *(new)* — no V observed by `T_commit` BUT peer σ-claims observed (cluster σ-side appears active); uncommitted, emit nothing yet.

Phase 2 splits across two emission points:

- **`T_commit`** (early signal): receivers with V emit σ; receivers without V either NR-immediately (silent-leader rule) if no peer σ-claims observed, or enter Defer if peer σ-claims observed.
- **`T_accept_max = T_commit + W`** (late horizon): Defer-state operators transition Defer→σ if V arrived during the window, or force-NR if not.

Phase 3 reconstruction starts at `T_accept_max + 1 BTT` (after late emissions propagate). The cluster relies on K-layer fall-through if neither σ-quorum nor NR-quorum reaches at any layer.

### Mechanics preserved from OBFT

- **K-layer chained encryption**: identical. Defer affects per-operator commitment timing, not the cryptographic structure.
- **Pigeonholes 1, 2, 3**: hold under Defer because Defer-state operators emit nothing (no σ, no NR), so they don't contribute to either pool until they transition.
- **Slashing-evidence rules 1, 2, 3, 5, 6**: unchanged. Rule 4 (fake encrypted-presence) timing unchanged.
- **Reconstruction walk**: same per-layer σ-or-NR resolution; just runs after a wider Phase-2 window.

### Mechanics added (vs bare OBFT)

- **4th commitment state** with its own EKM signing-event boundary. Defer→σ transition logs a separate EKM event from initial σ-commit (same `value_root`, but different `(slot, layer, side, transition)` key). Defer→force-NR is the standard NR signing event.
- **Multi-emission wire format**: separate `KindOnion` (σ-side, possibly emitted at `T_commit` OR late within `[T_commit, T_accept_max]`) and `KindNR` (NR-side, emitted at `T_accept_max` for force-NR cases). Cannot be combined into a single `KindCommit` — receivers need an early-NR signal at `T_commit` to know whether to defer, and late σ-emits arrive after that decision is recorded.
- **Auth-only-retention pre-state**: receivers track peer σ-claims observed in `[slot_start, T_commit]` separately from their own commitment state. Used to decide whether to NR-immediately (no peer σ observed → silent-leader rule) or Defer (peer σ observed → wait) at `T_commit`.
- **Cross-phase exclusivity across Defer→σ transition**: an operator who Defer'd at `T_commit` and then σ-emitted at `T_commit + ε` must not have NR-emitted in between. EKM enforces — Defer is a distinct EKM state from "uncommitted/silent."
- **Wider Phase-2 window**: `Δ_2 = W + 1 BTT` instead of bare OBFT's `Δ_2 = 1 BTT`. Phase 3 starts at `T_accept_max + 1 BTT` instead of `T_commit + 1 BTT`.

### Comparison with bare OBFT

#### Recovery scope

Aggressive-marginal partition: 1-of-3 honest receives V by `T_commit`; 2 others receive V late but within `[T_commit, T_accept_max]`.

- **Bare OBFT**: 1 honest σ-emits at `T_commit`; 2 honest NR-immediately (silent-leader rule). σ-pool = 1 + leader = 2 < qV; NR-pool = 2 < qEnc. **Slot misses at L_0**, fall-through to L_1 if L_1 honest.
- **OBFT+Defer**: 1 honest σ-emits at `T_commit`; 2 honest enter Defer (observed peer σ-claim from operator 1). V arrives within W; both Defer→σ-transition. σ-pool = 3 + leader = 4 ≥ qV. **Slot succeeds at L_0**.

Defer recovers cases where the propagation tail falls within `[T_commit, T_accept_max]` — strictly more than bare OBFT for that pattern at L_0. (Bare OBFT recovers via K-layer fall-through to L_1 instead, which works if L_1 honest and reachable, at the cost of one layer's worth of MEV opportunity.)

#### Healthy-path latency

Under fixed `T_relay_cutoff` (relay submission deadline; reconstruction must complete by `T_relay_cutoff − T_submit`):

- **Bare OBFT**: reconstruction completes at `T_commit + Δ_2 + Δ_3`. Phase 3 starts at `T_commit + Δ_2` (after `KindCommit` propagates).
- **OBFT+Defer**: reconstruction completes at `T_accept_max + Δ_2 + Δ_3 = T_commit + W + Δ_2 + Δ_3`. Phase 3 starts at `T_accept_max + Δ_2` (after late emissions propagate).

Both reach the same wall-clock completion time when `T_relay_cutoff` is fixed. The Defer model shifts `T_commit` earlier by W (to make room for the absorption window before Phase 3 starts) but completion time vs the relay deadline is identical.

**Idle-wait detail**: under healthy conditions where bundle reaches all honest by `T_commit + ε`, σ-quorum is determined early but Phase 3 still has to wait for `T_accept_max` in case Defer→NR transitions matter for fall-through. Optimistic early-Phase-3-start is theoretically possible (σ-pool only grows; once σ-quorum reached, it's reached) but adds spec complexity (re-running Phase 3 if Defer→NR transitions land late) and isn't part of the standard Defer formulation.

#### MEV impact (`T_broadcast_max_0` — primary leader fetch deadline)

**Defer does not compress MEV.** Under fixed `T_relay_cutoff` and the staggered per-layer broadcast model (see §Setting), L_0's deadline is what matters for MEV:

- **Bare OBFT**: `T_broadcast_max_0 = T_commit − B_0 = T_commit − 1 BTT`; reconstruction must complete by `T_relay_cutoff − T_submit ≥ T_commit + Δ_2 + Δ_3`, so `T_broadcast_max_0 ≤ T_relay_cutoff − T_submit − Δ_2 − Δ_3 − 1 BTT = T_relay_cutoff − T_submit − 2 BTT − Δ_3` at recommended Δ_2 = 1 BTT.
- **OBFT+Defer**: `T_broadcast_max_0 = T_accept_max − B_0 = T_accept_max − 1 BTT`; reconstruction must complete by `T_relay_cutoff − T_submit ≥ T_accept_max + Δ_2 + Δ_3`, so `T_broadcast_max_0 ≤ T_relay_cutoff − T_submit − 2 BTT − Δ_3`.

Same `T_broadcast_max_0`. The `B_0 = 1 BTT` leader-broadcast budget for L_0 applies relative to `T_accept_max` instead of `T_commit` (in Defer), but `T_broadcast_max_0` relative to `T_relay_cutoff` is unchanged.

The intuition that "Defer adds W to `T_broadcast_max_0 ↔ T_commit`" treats `T_commit` as the broadcast deadline. With Defer, `T_commit` is the early-signal point; `T_accept_max` is the broadcast deadline. With Defer, `T_commit` shifts earlier by W to fit the absorption window, but `T_broadcast_max_0` is anchored to `T_accept_max` (the late horizon), which is anchored to `T_relay_cutoff − T_submit − 1 BTT − Δ_3` either way.

#### Wire bandwidth

OBFT+Defer requires multi-emission per operator:

- KindOnion at `T_commit` (early σ-emits) + possible second KindOnion in `[T_commit, T_accept_max]` (late Defer→σ).
- KindNR at `T_accept_max` (force-NR for operators still in Defer at end-of-window).

vs bare OBFT's single KindCommit at `T_commit`. Per-operator wire footprint roughly **2× higher** in worst case (early σ-emit + late KindNR for force-NR; or auth-only-retention pre-emission + late σ).

#### Adversarial-byz failure modes

OBFT+Defer **opens the withhold-then-fake-σ h_V=1 attack** (Variant A — see [§Failure modes / Class B](#failure-modes)). The attack chain:

1. Byzantine L_0 withholds Phase-1 from all honest peers in `[slot_start, T_accept_max − ε]`.
2. Byzantine emits an auth-signed "I claim σ-side" envelope at `T_commit` (no V on the wire — a deliberate protocol-violation deviation from honest σ-emit, which is always a follow-on to a Phase-1 broadcast).
3. Honest receivers without V observe the byz σ-claim → enter Defer (per the no-V fallback rule that triggers on observed peer σ-claims).
4. At `T_accept_max − ε`, byzantine selectively unicasts Phase-1 to exactly one honest. That honest Defer→σ-transitions; the other two force-NR.
5. Final pools: σ-pool = 1 honest + byz σ_V = 2 < qV; NR-pool = 2 < qEnc. **Deadlock at h_V=1**, slot misses at L_0 with no fall-through (NR-pool short of qEnc).

All three byzantine actions are deliberate protocol-deviations — gossipsub broadcasts by default (withholding requires actively suppressing it), honest Phase-2 σ is a follow-on to Phase-1 broadcast (faking σ-claim with no V is a direct violation), gossipsub propagates broadcast-style (selective unicast requires bypassing it). **Under honest protocol operations the attack does not fire.**

Bare OBFT closes Variant A by removing Defer: receivers without V at `T_commit` immediately NR (silent-leader rule, no peer-σ-claim fallback) → NR-quorum reaches → fall-through to L_1. Cost of closing Variant A: lose aggressive-marginal recovery within `[T_commit, T_accept_max]`.

Variant B (selective Phase-1 delivery — byz broadcasts Phase-1 to exactly one honest, not withholding) is **R-invariant**; Defer doesn't help, removing Defer doesn't help. Phase 2a/2b ([2abOBFT](2abOBFT.md)) is the structural fix for Variant B.

#### Spec / EKM complexity

OBFT+Defer requires:

- 4-state commitment lattice (σ, NR, NV, Defer) with explicit transitions (Defer→σ, Defer→force-NR).
- EKM signing-event boundaries for Defer→σ vs initial σ-commit, distinct schema rows.
- Auth-only-retention pre-state for tracking peer σ-claims in `[slot_start, T_commit]` (used to gate the Defer-vs-NR-immediate decision).
- Two separate Phase-2 emission timings (`T_commit`, `T_accept_max`) with cross-phase exclusivity enforcement spanning the window.
- Larger Phase-2 window: `Δ_2 = W + 1 BTT` vs bare OBFT's `Δ_2 = 1 BTT`.

Bare OBFT requires:

- 3-state commitment lattice (σ, NR, NV).
- Single signing event per (slot, layer) per operator.
- Single emission point at `T_commit`.
- `Δ_2 = 1 BTT`.

Defer adds materially to the spec/EKM surface and to the slashing-protection schema.

### When OBFT+Defer is worth the complexity

- **Sustained partial-synchrony deployments** where re-flood completion past `T_commit` is common (e.g., wide-area clusters with high `P99` variance, larger n where mesh is sparser). Defer's aggressive-marginal recovery scope materializes regularly.
- **Higher-`f` deployments**: at f=1 n=4, K-layer fall-through to L_1 already covers most "no honest received V at L_0" cases. At higher f, fall-through depth is more constrained relative to byz patterns; Defer's within-layer recovery becomes more valuable.
- **Trust assumption 4 (rational-byz deterrent) is strong**: deployments where Variant A's three coordinated deliberate deviations (withhold + fake σ-claim + selective unicast) are detectable as a behavioral signature across slots and the surviving operators can blacklist accordingly.
- **MEV is not a primary driver**: since `T_broadcast_max_0` is invariant to Defer, MEV isn't the trade-off lever; the lever is recovery scope vs spec/wire complexity vs Variant A exposure.

### When bare OBFT is preferable

- **n=4 healthy-mesh SSV clusters**: aggressive-marginal partition is rare under fully-connected gossipsub; K-layer fall-through to L_1 covers the failure cases bare OBFT misses with adequate frequency.
- **Adversarial-byz protection is the priority**: removing Defer closes Variant A, which is otherwise a reliable grief vector when byz is L_0 (~25% of slots at f=1 n=4 with uniform leader rotation). Variant A leaves *behavioral-pattern* evidence rather than cryptographically self-contained evidence, so the rational-byz deterrent's punishment quality is weak — making the attack particularly attractive to adversarial byz.
- **Spec / wire / EKM simplicity is valuable**: bare OBFT's 3-state, single-emission, single-EKM-event-per-(slot, layer) shape is materially simpler to implement and audit. The single `KindCommit` envelope is a structural simplification that depends on Defer's absence.
- **Production deployment under existing assumptions**: SSV's current proposer-duty deployment runs at n=4 with healthy gossipsub mesh and treats adversarial-byz as the dominant concern; the simpler 3-state model is the better fit.

### Conclusions

1. **Defer was removed from the current OBFT spec** in favor of a 3-state (σ, NR, NV) commitment lattice with a single `KindCommit` emission per operator per slot. **The primary motivation was spec/wire/EKM simplification** (3-state vs 4-state lattice, single emission vs multi-emission, no auth-only-retention pre-state, no transitional EKM events); closure of the withhold-then-fake-σ adversarial-byz attack is a structural side-effect of the same removal, not its motivating reason. The trade-off cost is loss of aggressive-marginal partition recovery (one specific pattern at the boundary of the `[T_commit, T_accept_max]` window).

2. **The MEV cost intuition is a misread.** Defer doesn't compress `T_broadcast_max_0` (the primary leader's fetch deadline) — under the binding `T_broadcast_max_0 = T_relay_cutoff − T_submit − 2 BTT − Δ_3` equation (at Δ_2 = 1 BTT, B_0 = 1 BTT), the leader fetch deadline is invariant to Defer. The `B_0` safety margin is anchored to `T_accept_max` (with Defer) or `T_commit` (without), but `T_relay_cutoff` minus that anchor is the same in both cases. What does shift is `T_commit` (W earlier with Defer, to make room for the absorption window before Phase 3 starts).

3. **The withhold-then-fake-σ attack requires deliberate byz** — three coordinated deliberate deviations (withhold, fake σ-claim, selective unicast). None happens under honest protocol operations. The attack does NOT fire incidentally. So Defer's adversarial-byz cost is conditional on facing an actively-malicious byz within the f-bound, with weaker rational-byz-deterrent punishment quality than Variant B (behavioral-pattern evidence, not cryptographically self-contained).

4. **The trade-off framing for keeping Defer**: gain aggressive-marginal partition recovery within `[T_commit, T_accept_max]`, expose deliberate-byz Variant A attack with weak punishment evidence quality, accept multi-emission wire format + 4-state commitment lattice + auth-only-retention pre-state + transitional EKM events. For SSV n=4 with healthy mesh, the latter ledger dominates.

5. **OBFT+Defer vs bare OBFT trade summary**:
   - **+** Aggressive-marginal partition recovery within `[T_commit, T_accept_max]` window.
   - **+** Slightly wider partial-synchrony tolerance at L_0 (vs falling through to L_1 for the same pattern).
   - **−** Reopens withhold-then-fake-σ adversarial-byz attack vector (Variant A).
   - **−** Multi-emission wire format (no single-`KindCommit` simplification possible).
   - **−** 4-state commitment lattice + auth-only-retention pre-state + transitional EKM events.
   - **=** Same `T_broadcast_max_0` (primary leader MEV fetch deadline) under fixed `T_relay_cutoff`.
   - **=** Same healthy-path completion time (= `T_relay_cutoff − T_submit`).
   - **=** Same K-layer fall-through structure / chained encryption / Pigeonholes.
   - **=** Same slashing-evidence rules (1, 2, 3, 5, 6 unchanged; Rule 4 timing unchanged).

Treat OBFT+Defer as a candidate enhancement worth re-evaluating if deployment conditions change — wider partial-synchrony, higher f, stronger rational-byz deterrent infrastructure, or production telemetry showing aggressive-marginal partition slot-misses at non-trivial rates. Under SSV's current n=4 healthy-mesh proposer-duty profile, bare OBFT (3-state, single-emission) is the cleaner trade. For deployments wanting adversarial-byz coverage as well as partition recovery, [2abOBFT](2abOBFT.md) is the orthogonal lever — closes both Variant A and Variant B structurally at +1 RTT cost.
