# OBFT — Single-Round Onion BFT

A single-round agreement protocol for SSV clusters that produces one collective threshold-signed value per "slot" against a hard deadline. OBFT achieves agreement *cryptographically* — a unique cluster-wide output via threshold cryptography over EKM-enforced per-operator commitments — over a configurable K-layer onion structure with parallel leader fall-through. Single-round only: agreement runs once per slot against a single hard deadline; there is no round-change and no cross-round re-flood.

OBFT runs K layers (configurable, `f+1 ≤ K ≤ n`) in a single agreement round. The K-layer reconstruction walk in Phase 3 is the load-bearing fall-through mechanism. Each operator commits exactly once per (slot, layer) by `T_commit` — σ, NR, or NV — and emits a single combined `KindCommit` message carrying that decision. The commit is emitted as soon as the operator has retained and host-validated `L_0`'s Phase-1 bundle (typically before `T_commit` in healthy mesh), with `T_commit` as the fallback deadline if `L_0` never arrives — see §Phase 2 emission-timing.

OBFT's recovery scope is intentionally bounded. The protocol's absorption is **primary-vs-backup**: the primary L_0 absorbs real propagation up to `B_0 = 2·BTT + RefloodDelay = 1100ms` at Config A with default RefloodDelay=700ms (matches SSV's gossipsub HeartbeatInterval); all backups L_1..L_{K-1} broadcast at BFT_start with deepest-confirmed-parent fetch and absorb up to `B_k = T_commit = 3600ms` ("earliest possible" — backup leaders broadcast at BFT_start, absorbing real propagation up to the entire slot's commit budget) — see [§Setting](#setting) for the per-layer budget `B_k`. Within these bounds, OBFT recovers all in-envelope cases via K-layer fall-through: healthy path, silent leader, multi-leader fall-through within Phase 3's reconstruction walk (no per-layer RTT). The protocol does NOT recover from **view-divergence** patterns: equivocation (byzantine leader sending different V's to different honest) and validity-divergence (e.g., re-org causing some honest to validate V and others to NV). View-divergence cases either succeed naturally (when the split happens to leave 2-of-3 honest σ-committed on the same V at f=1 n=4, allowing leader's σ_L^V to push the pool to qV) or slot-miss. Equivocation in non-naturally-recovered patterns is **slashable** (the byzantine leader pays the stake-based penalty); validity-divergence is not attributable to anyone and just slot-misses. See [Assumptions and implications](#assumptions-and-implications) for the explicit list.

OBFT explicitly does not provide within-slot partition recovery for asymmetric propagation — bundles arriving past `T_commit` at any honest receiver simply don't count, and the cluster falls through to a deeper layer whose bundle did propagate. This trades partition tolerance for spec simplicity, suited to small clusters (n=4, the SSV proposer-duty default) where the gossipsub mesh is effectively full and asymmetric propagation is rare.

The protocol description below targets `n = 4` (`f = 1`) as the running example, with **`K = f+1 = 2`** as the running-example layer count (the BFT-liveness minimum at f=1; the recommended default for SSV proposer duty — see [§Application](#application-ssv-ethereum-proposer-duty). K=2 has one fall-through layer `L_0 → L_1`; at L_0 the primary fetches the MEV bundle, at L_1 a backup fetches a deepest-confirmed-parent vanilla payload). K is tunable per duty within `f+1 ≤ K ≤ n`; the choice is deployment-dependent (see [§Setting](#setting) for the K-bounds discussion). K=4 (= K=n at n=4) worked examples appear inline as up-tier illustrations of multi-layer fall-through. SSV's Ethereum proposer duty is used as the running application example. Generalizations to `n ≥ 7` are noted inline where the algebra changes.

## When to use it

**Suited for:** SSV proposer duty under healthy-network partial synchrony (`P99` ≈ 150ms cluster gossipsub P99/P999, i.e. `1 BTT ≈ 200ms`; see [§Setting](#setting)), where OBFT's 2-RTT healthy-path latency plus K-layer parallel leader fall-through is sufficient and round-change machinery is not desired. Also well-suited for high-P99 networks (`P99` ≈ 300–500ms) where multi-round protocols do not fit the 4s relay cutoff but a single round still does. Generally: deployments that prioritize spec/EKM simplicity, more submission headroom, and high-P99 fit.

**Not suited for:** deployments where the gossipsub propagation tail commonly delays L_0's bundle past `T_commit` (L_0's per-layer budget is `B_0 = 2·BTT + RefloodDelay = 1100ms` at Config A with default RefloodDelay) AND the cluster needs to preserve V_0's MEV freshness (rather than fall through to a deeper backup's vanilla payload). OBFT's K-layer fall-through absorbs propagation tails up to the entire `T_commit` budget at the deepest layer (`B_{K-1} = T_commit`; loses MEV freshness on each fall-through), so OBFT alone is fine when the tail rarely exceeds the deepest-layer absorption or when MEV-preservation isn't the priority. This typically becomes load-bearing at n ≥ 10 where mesh sparsity makes asymmetric propagation common. Also not suited for: scenarios requiring host-validity-divergence recovery within a slot (OBFT assumes host validity is unanimous at decision time, see [Assumptions](#assumptions-and-implications); QBFT is the appropriate choice when validity is unstable across the consensus window).

**Also not suited for:** general-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. OBFT gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n = 3f+1` (the BFT-tight setting; matches SSV's deployment configurations `n ∈ {4, 7, 10, 13}`). The running example is `n = 4, f = 1`. The threshold formula `qV = qEnc = 2f+1` below equals `n − f` exactly at this setting; this equality is what makes the Pigeonhole arguments in [§Safety](#safety-cryptographic-honest-majority) hold.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) as the threshold-signing scheme for no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety". At `n = 4`, `qEnc = 3`.

> **Implementation note (Option A reuse).** Implementations may reuse the V-keypair shares as IBE-keypair shares with cryptographic separation achieved via distinct domain-separation tags (DSTs) in the BLS primitive — saving a second DKG. The Pigeonhole 1 algebraic argument depends only on the SHARED threshold (`qEnc = qV = 2f+1`), which is preserved under DST reuse; the "two distinct keypairs" framing above is normative for the safety argument but a single keypair with DST separation is operationally equivalent. SSV's reference implementation defaults to this Option A; see `docs/IBE-INTEGRATION.md`. Implementations that prefer cryptographic key separation (Option B) run the second DKG and key the IBE primitive separately.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts and protocol-level claims, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- **K layers** (`f+1 ≤ K ≤ n`, configurable) with deterministically-derived leaders: layer 0 with **primary leader** `L_0`, layer 1 with **backup leader** `L_1`, ..., layer `K-1` with `L_{K-1}`. Leaders must be distinct (`L_i ≠ L_j` for `i ≠ j`). For `K = n`, every cluster member is a leader at exactly one layer per slot.

  **Spec-level K bounds:**
  - **`K ≥ f+1` is the BFT-liveness minimum** — pigeonhole over the f-byz bound guarantees at least one honest leader. At `K < f+1`, all leaders could be byzantine and no σ-quorum reaches at any layer. This is the only K-floor the protocol mandates.
  - At `K = f+1`, the cluster has exactly one honest leader; a single late-broadcasting honest leader can foreclose the slot via the deepest-layer NR-lock pathology (see §Failure modes / Late deepest-layer leader broadcast). `K ≥ f+2` guarantees ≥ 2 honest leaders, providing late-leader-resilience at the cost of one additional layer's leader-broadcast budget.

  Choice of K is **deployment-dependent** and left to the operator. **Recommended default: `K = f+1` (BFT-liveness minimum)** — at n=4 (f=1) this is K=2, motivated by production testing showing K > f+1 doesn't materially improve outcomes once peer-reflood-V closes the healthy-mesh `h_V=1` case at L_0 (see [§Phase 2 / Peer-reflood V via early commit](#phase-2--onion-broadcast-t_emit-t_emit--%CE%94_2-with-t_emit--t_commit)). Under sustained degraded-mesh operation the residual `h_V=1` deadlock returns; clusters with strong mesh-flakiness exposure, operating closer to the partial-synchrony tail, treating adversarial-byz tolerance as a hard requirement, or preferring deeper fall-through may prefer `K ≥ f+2` or `K = n` (more layers, deeper chained-encryption, longer Phase-3 IBE walks). The running protocol example below uses `n = 4, f = 1, K = 2` for clarity but the spec applies uniformly across `f+1 ≤ K ≤ n`; K=4 worked examples appear inline as up-tier illustrations of multi-layer fall-through.
- **Single agreement round.** OBFT fixes `R = 1`: one Phase 1 → Phase 2 → Phase 3 sequence per slot, no retry, no re-flood across rounds. The slot's reconstruction deadline is the only deadline. Each operator commits exactly once per (slot, layer) by `T_commit` based on what they observed at emit time (see §Phase 2 emission-timing); bundles arriving past `T_commit` do not contribute to that layer's σ-pool.
- Per-layer leader-fetch deadlines `T_{K-1} < T_{K-2} < ... < T_1 < T_0`, plus a single cluster deadline `T_commit`. (`T_commit` is the *view-fix point*: each operator commits its stance based on what it observed by `T_commit`. Reconstruction and submission happen after.) The asymmetric fetch times let primary leaders fetch high-MEV values late (`T_0` close to `T_commit`) while deeper layers' leaders fetch safe early values from deeper-confirmed parents (`T_{K-1}` well before `T_0`).
- **Time unit `BTT` (broadcast trip time)** — `P99` is the propagation budget at the deployment's chosen tail percentile (the variable name `P99` is shorthand for the high-percentile propagation latency; deployments may use P99, P999, P9999 etc. as the actual percentile depending on tail tolerance). `δ` is the cluster's clock-skew bound. We define `1 BTT = P99 + δ` — the time needed for one one-way message to propagate from a sender to all honest receivers under partial-synchrony assumptions. This unit is used throughout for time-budget formulas; the underlying `P99` and `δ` are kept distinct only in §Trust model (where partial synchrony is defined) and in safety arguments (Pigeonhole proofs). Concrete sizing at Config A: `P99 = 150ms, δ = 50ms, 1 BTT = 200ms`.

- **`BFT_start` — slot-relative offset at which Phase 1 begins.** Pre-fetch and pre-consensus (block builder calls, partial-sig RANDAO aggregation, etc.) sit in `[slot_start, BFT_start]`; the BFT consensus phase runs in `[BFT_start, T_commit + Δ_2 + ε_3]`. In the spec's pure-timing model `BFT_start = 0` (Phase 1 fires at slot start with no pre-fetch overhead). SSV's proposer-duty application sets `BFT_start > 0` — see §Application / [Timing budget](#timing-budget) for the SSV-specific sizing (the running example uses `BFT_start = 0` at Config A for clarity — backups broadcast at slot start, no pre-fetch row; production SSV-adapter operating points use up to `BFT_start = 1200ms`). Leader broadcasts cannot land before `BFT_start`; the per-layer broadcast target clamps to `BFT_start` when `B_k ≥ T_commit` (see [Per-layer leader broadcast deadlines](#per-layer-leader-broadcast-deadlines) below).

- **Per-layer leader broadcast deadlines `T_broadcast_max_k`** — OBFT uses **primary-vs-backup broadcast budgets**: the primary `L_0` broadcasts latest with a small propagation budget (= freshest MEV); all backups `L_1..L_{K-1}` broadcast at `BFT_start` with the maximally-wide propagation budget (the entire commit window). The cluster falls through to whichever layer's bundle actually arrived by `T_commit`.

  General form: `T_broadcast_max_k = max(BFT_start, T_commit − B_k)`, where `B_k` is layer `k`'s propagation budget. Per the post-tighten sizing:

  ```
  B_0           = 2·BTT + RefloodDelay  (primary, MEV-fresh)
  B_1..B_{K-1}  = T_commit              (backups broadcast at BFT_start)
  ```

  `RefloodDelay` is the worst-case gossipsub-lazy-push latency before a retransmission cycle completes — bounded by the cluster's HeartbeatInterval (SSV's default = 700ms at [network/topics/params/gossipsub.go](../network/topics/params/gossipsub.go)). The primary's `2·BTT` base decomposes as 1·BTT P99 initial propagation + 1·BTT IWANT round-trip = minimum reflood-cycle coverage at L_0; the additive RefloodDelay accommodates one full IHAVE/IWANT cycle when initial eager-push fails to reach mesh-flaky receivers. Deployments on fully-meshed clusters (typically n=4) where eager-push reliably reaches all peers may opt out by setting `RefloodDelay = 0` (or near-zero); the primary budget collapses to 2·BTT and recovers MEV-fetch headroom.

  > **Cross-protocol note**: 2abOBFT has a separate protocol-level configurable named `SafetyBuffer` that plays the analogous mesh-tolerance role, but lives in a different part of the timing budget. OBFT's `RefloodDelay` is baked into `B_0 = 2·BTT + RefloodDelay` (the leader's pre-Phase-2 broadcast budget), absorbing one IHAVE/IWANT cycle BEFORE commit fires — appropriate because OBFT's critical path post-bundle-arrival is one hop (early-commit fires immediately on L0Ready close). 2abOBFT's `SafetyBuffer` instead widens the σ-ward arm of the post-`TPhase2a` resolve window (`1·BTT + SafetyBuffer`), absorbing σ-pool fill via IHAVE/IWANT recovery when the initial `KindValue` eager-push misses a peer (the NR-ward arm is the genuinely two-hop cascade at `2·BTT`; the window reserves the `max` of the two arms). 2abOBFT's `B_k` stays at the structural minimum `(k+2)·BTT` — no SafetyBuffer term. Where OBFT's `RefloodDelay` is the gossipsub network primitive (bounded by HeartbeatInterval), 2abOBFT's `SafetyBuffer` is a deployment-tunable protocol parameter decoupled from the network constant — default `SafetyBuffer = RefloodDelay` produces equivalent total post-broadcast structural budgets across the two protocols. See [docs/2abOBFT.md §Timing parameters](2abOBFT.md#timing-parameters) for the cross-protocol placement rationale.

  **Why backups all broadcast at BFT_start.** Backups L_1..L_{K-1} are last-resort safety nets — the primary L_0 carries the slot's MEV in the vast majority of slots. Trading L_1+ MEV-fetch (small expected value under the rational-byzantine deterrent) for L_1+ absorption width = T_commit substantially improves fall-through reliability under Class A partition tails and degraded mesh. Backups all fetch from a deepest-confirmed parent at slot start (re-org resistant by construction) and broadcast immediately, giving the cluster the entire commit budget for that bundle's propagation.

  `Δ_2 = 1·BTT` recommended — one synchronous-fallback `KindCommit` propagation cycle; reflood absorption is structurally provided by `B_0` via RefloodDelay, so `Δ_2` no longer carries a reflood cushion. See §Phase 2.

  Each leader `L_k` broadcasts by `T_broadcast_max_k`. Bundles whose first-observation time is past `T_commit` at any honest receiver are not counted toward σ-quorum at this layer; the cluster relies on K-layer fall-through to a backup layer whose bundle did propagate in time. Phase 2's `KindCommit` emit is bounded by `T_commit` but may fire earlier on `T_L_0_observed` — see §Phase 2 emission-timing.

  **`B_k` is a target absorption budget, not a hard runtime cap on broadcast time.** The only runtime acceptance gate is `T_commit`: receivers admit bundles first-observed in `[slot_start, T_commit]` regardless of which layer they came from. `B_k` parameterizes the design — it tells each leader *when to aim to broadcast by* so that, under partial-synchrony assumptions, the bundle reaches all honest first-observation within the layer's per-layer budget. A leader that cannot meet `T_broadcast_max_k` does best-effort: broadcast as soon as the bundle is ready. The cluster still benefits from any in-envelope propagation that happens to complete before `T_commit`.
  - **Clamp at `BFT_start` when `B_k ≥ T_commit`.** The `max(BFT_start, ·)` form means the layer's broadcast target is `T_broadcast_max_k = BFT_start` — leader broadcasts at the BFT phase start, best effort. This is the *default* configuration for every backup layer (`B_k = T_commit`): the backup leader fetches from a deepest-confirmed parent at `BFT_start` and broadcasts immediately. At extreme degraded operating points (`B_0 > T_commit`, e.g. T_commit ≤ 2·BTT + RefloodDelay), the primary also clamps to BFT_start; the primary then becomes a redundant peer of the backups (cluster still operates). Operators choosing extreme BTT or large RefloodDelay must decide whether the resulting reduced-depth schedule meets their deployment's liveness goals; the protocol does not reject the configuration on its behalf.

  **Sizing intuition.** `L_0`'s budget covers the optimistic case (nominal propagation + one reflood cycle, maximum MEV fetch); backups absorb propagation up to the entire commit budget. Concrete K=2 default sizing for SSV proposer duty (in §Application) at Config A with `RefloodDelay = 700ms`: `B_0 = 2·BTT + 700ms = 1100ms`, `B_1 = T_commit = 3600ms` (backup broadcasts at BFT_start). At `RefloodDelay = 0` (fully-meshed cluster opt-out): `B_0 = 400ms` (backup unchanged). K=4 up-tier extends the backup pattern uniformly: `B_1 = B_2 = B_3 = T_commit = 3600ms`. The trade-off: `B_0`'s tighter budget gives the primary the longest fetch window (best MEV) but the least propagation safety margin; if real propagation from L_0's broadcast exceeds `B_0` (= bundle arrives past `T_commit`), the cluster falls through to the backups, which had the entire slot to propagate.
- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

- **Implementation note: BFT-liveness floors are deployment recommendations, not enforced constraints.** The thresholds called out above — `Δ_2 ≥ 1 BTT` (Phase-2 propagation), `T_commit ≥ B_0 = 2·BTT + RefloodDelay` (so the primary's broadcast budget fits before the receiver acceptance horizon — reduces to `2·BTT` only at the `RefloodDelay = 0` fully-meshed opt-out; at the SSV default `RefloodDelay = 700ms` the floor is `T_commit ≥ 1100ms`), and `B_0 ≥ 2·BTT` (primary-layer liveness guarantee — the underlying propagation cycle that `B_0` extends with the reflood cushion) — describe the minimum sizing under which the cluster has a primary-layer liveness guarantee. The reference implementation's `Config.Validate` enforces only basic feasibility: every duration field is positive, `B_k > 0` on every layer, the `B_k` slice is non-decreasing, the `T_k` (fetch) slice is non-increasing. Below the BFT-liveness floors the cluster systematically misses (broadcasts don't fit before their phase boundary; messages don't propagate before commit); the protocol and simulator still execute, producing informative 0%-success-rate data rather than blanket rejection. Operators studying or knowingly running degraded operating points get those points' data without intervention.

## Assumptions and implications

OBFT's claims hold conditional on a small set of explicit assumptions about the deployment environment. They are consolidated here; the rest of the spec assumes them and refers back to this section rather than re-deriving the trade-offs each time.

### Assumed

1. **Standard BFT trust bound at the tight setting.** `n = 3f + 1`, up to `f` operators may be byzantine. At `n = 4`, `f = 1`. SSV deploys at the BFT-tight bound (`n ∈ {4, 7, 10, 13}` for `f ∈ {1, 2, 3, 4}`); the threshold formula `qV = qEnc = 2f+1` below requires this tightness to give `2f+1 = n − f`, which is what the Pigeonhole arguments depend on. Honest operators run protocol-conformant software (correct EKM rule enforcement, correct host application, correct gossipsub behavior). Byzantine operators may deviate arbitrarily within their f-bound.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `P99` (cluster gossipsub P99/P999) and clock skew `δ`. Safety is unconditional on timing; only liveness depends on this. **OBFT's absorption is primary-vs-backup: `B_k = T_commit − T_broadcast_max_k`** — `B_0 = 2·BTT + RefloodDelay` for the MEV-fresh primary; `B_1..B_{K-1} = T_commit` for all backups (which broadcast at BFT_start with deepest-confirmed-parent fetch) at Config A K=2 default (see [§Setting](#setting) for the design; K=4 up-tier extends backup pattern to L_2, L_3). Bundles arriving past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum. Real propagation from leader L_k's broadcast to any honest first-observation that exceeds `B_k` causes that layer to fail at that receiver; the cluster relies on K-layer fall-through to a backup whose bundle did propagate in time (backups have the maximally-wide `B_k = T_commit`).

3. **Host validity is unanimous at decision time** (best-effort assumption). OBFT assumes the host application's `valid` / `not-valid` verdict on `V_{L_k}` is the same across all honest operators by the time they emit Phase 2. Operators may transiently diverge (e.g., a head change observed by some but not others). The host's job is to make divergence rare via per-operator stabilization — typically by validating against a stable head snapshot taken at Phase-1 acceptance time, then locking the verdict for the remainder of the slot. The validity-locking window narrows to bundle arrivals before `T_commit` (~`1 BTT` at Config A). When divergence does occur — a re-org during the acceptance window with operators accepting on either side of it — the assumption is violated and the slot may miss; the protocol does not recover. See [Application: SSV Ethereum proposer duty / Head-change handling](#head-change-handling) for the SSV-specific stabilization workflow and the residual divergence window.

   The validity check exists to prevent the cluster from agreeing on a garbage / invalid V — it is not a divergence-recovery mechanism. NV is operationally identical to NR for protocol counting; it does not trigger any in-protocol divergence-handling path.

4. **Persistent operator set with rational-byzantine deterrent.** OBFT operates within a stable SSV cluster running protocol instances over many slots. The deterrent is the same one that already disciplines an offline operator under SSV's network-wide threat model: per-validator operator fees flow continuously to all cluster operators regardless of per-slot contribution (the remaining `n − f` honest carry the work at zero ops cost to the silent/byzantine), and stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters, collapsing the silent/byzantine operator's fee accrual to zero. SSV is already designed for the operator-down case ("the cluster and stakers deal with it"); the rational-byzantine claim is that a byzantine operator gains nothing an offline operator wouldn't already get, and has reputation (persistent across slots) to lose.

   **Asymmetry — Byzantine vs Down — and what restores equivalence.** With QBFT, `Byzantine ≡ Down` automatically: round-change rotates past silent or malformed PROPOSE/PREPARE/COMMIT, so the worst a byzantine can do per-slot is silently going offline. OBFT has no round-change escape valve, so byzantine is *significantly worse on latency than Down* — equivocation σ-locked splits, `h_V=1` selective Phase-1 delivery, fake-encrypted-presence, and behavioral σ-refusal can engineer per-slot grief above what equivalent offline behavior would produce. The expected mitigation is **manual blacklisting**: the cluster's surviving `n − f` operators agree out-of-band on the misbehaving operator's identity, push a config-file update to their nodes treating that operator's messages as silent for subsequent slots, and the byzantine's effective contribution becomes identical to offline — restoring the `Byzantine ≡ Down` guarantee. **The blacklist mechanism is a planned protocol extension; the current OBFT spec does not specify it.** Until added, the byzantine's per-slot grief surface above offline behavior is bounded only by stakers eventually migrating validators away from the cluster.

   The on-wire byzantine-fault evidence ([§Slashing evidence](#slashing-evidence)) informs both (a) staker migration decisions and (b), once the extension lands, the cluster operators' blacklist trigger. Filing a stake-slashing transaction via the SSV contract is a complementary punitive action available where the evidence is cryptographically self-contained, but is not the primary deterrent — the primary deterrent is the economic equivalence to going offline plus the eventual `Byzantine ≡ Down` collapse via blacklisting. See [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the evidence-quality discussion and how it interacts with the blacklist's detection latency.

5. **Coordinated EKM across both keypair shares.** The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log keyed on `(slot, layer, side, value_root)`. See [EKM coordination model](#ekm-coordination-model).

6. **Independent V-keypair and IBE-keypair DKGs over the same operator set at the same threshold `qV = qEnc = 2f+1`.** Both DKGs are run once at cluster init with all `n` operators participating; the shared operator set and threshold is what makes Pigeonhole 1's algebra work (`h_σ + h_NR ≤ 2f+1` across the cross-keypair signing log).

### Implications of safety being honest-majority cryptographic, not 100% cryptographic

OBFT's safety holds against an offline-aggregating byzantine *given the protocol rules are correctly enforced by honest operators*. Enforcement happens at two independent layers per operator — the **protocol layer** (operator software implementing the OBFT state machine, deciding when to request σ vs NR) and the **EKM** (slashing-protection log that rejects bad signing requests as defense-in-depth). Together they ensure single-σ-V per (slot, layer) per operator and σ-XOR-NR per layer; these rules reduce the safety proof to pigeonhole arguments on the cluster-wide signed-message set.

An operator whose software actively misbehaves (maliciously compromised, or with compounding protocol+EKM bugs that produce byzantine-equivalent signing behavior) consumes f-budget directly. With `f = 1` byz at `n = 4`, one such operator is tolerated; two violate the trust bound and Pigeonhole arguments can fail. Single-layer bugs (protocol-only or EKM-only) do **not** produce byzantine behavior on their own — the other layer catches them. See [EKM coordination model](#ekm-coordination-model) for the full defense-in-depth analysis.

**This is the same trust posture as QBFT.** QBFT's safety also holds under f-byz with honest-majority correct code paths. A bug in `2f+1` honest operators (e.g., the post-consensus signing path signs both candidates from a split decision, or the prepared-certificate verification accepts conflicting commit certificates) would equally violate QBFT's safety guarantees. Neither protocol is "100% cryptographic" against operator-side software bugs; both rely on operator software correctness for honest operators.

Accordingly, "cryptographic safety" in this spec means: against a partially-synchronous byzantine network adversary plus up to `f` byzantine operators, honest operators following the protocol cannot produce two contradictory outputs. It does not mean: safety against arbitrary bugs in honest operator software.

### Implications of validity-divergence not being recovered (assumption 3)

The deadlock has three structural causes: per-operator independent validity verdict, leader's Phase-1 σ_V locked, and cross-phase exclusivity. If assumption 3 is violated mid-slot — honest verdicts genuinely diverge after Phase 2 emit — OBFT cannot recover within the slot. There is no fresh-V refetch mechanism. The byzantine leader's Phase-1 σ_V is locked; honest who NV cannot switch to σ; cluster deadlocks at L_k or falls through to L_{k+1} (where the same divergence pattern may repeat).

For SSV proposer duty, the host's stabilization workflow (validate parent_root once at acceptance, lock the verdict) is the design's path to satisfying assumption 3. If the host cannot guarantee unanimous validity (e.g., re-orgs are common enough that locking-at-acceptance leads to too many submission rejections), the available structural alternatives are:

- **Use a deterministic / finalized parent.** Validity criterion that doesn't depend on each operator's chain view at evaluation time — e.g., parent must be a finalized block (2 epochs old, all operators agree). Eliminates divergence by construction but loses late-MEV (you can only build on finalized parents).
- **QBFT.** Round-changes through with a new leader fetching at the moved head — covers validity-divergence as a side-effect of round-change recovery. Comes with QBFT's own ~2s round-change latency.

Smaller mitigations (relax cross-phase exclusivity, allow NV → σ flip via aggregator filtering, etc.) all break safety against an offline-aggregating byzantine — they let the byzantine concurrently aggregate σ on V and NR/NV at the same layer to reach two contradictory thresholds.

### Implications of equivocation not being recovered

OBFT does not provide an in-protocol equivocation recovery mechanism. Outcomes split into three classes:

- **σ-quorum reaches at L_0 naturally** (slot succeeds): honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool to qV.
- **NR-quorum reaches at L_0 → fall-through to L_1** (slot succeeds at L_1 if L_1 honest): all 3 honest retained ≥ 2 distinct V's by `T_commit` (typical when byz delivers V's early enough for gossipsub re-flood to spread conflicts before T_commit). All 3 honest emit NR per the equivocation-NR rule, producing qEnc-quorum at L_0; decryption unlocks L_1; σ-quorum at L_1 reaches in the same Phase 3 reconstruction walk.
- **σ-locked split patterns** (slot misses): honest σ-states split (1-1-1 split, 1-1-NR, etc. at f=1 n=4 — different honest σ-locked on different V's before observing equivocation). σ-pools split below qV; NR-pool capped by σ-locked operators below qEnc; no fall-through.

A byzantine controlling delivery timing picks the class. Delivering near end-of-Phase-1 (insufficient re-flood time) reliably engineers the σ-locked split slot-miss outcome. Equivocation evidence is slashable in all cases; the rational-byzantine deterrent (assumption 4) is what makes the slot-miss tolerable across many slots: a byzantine that equivocation-griefs in slot N pays for it from slot N+1 onward via the eventual `Byzantine ≡ Down` collapse (manual blacklist by surviving operators; planned protocol extension) plus staker migration collapsing cluster-wide fee accrual; equivocation-evidence bundles additionally enable stake slashing via the SSV contract.

### Implications of the rational-byzantine deterrent (assumption 4)

The deterrent affects *liveness only*, not safety. Pigeonholes 1, 2, 3 hold cryptographically against any byzantine within the f-bound regardless of whether the byzantine is rational — a byzantine willing to absorb full reputation cost (e.g., last-slot-before-exit) cannot violate safety, only grief liveness.

Specifically:

- **Safety unaffected:** No matter how aggressively byzantine operators misbehave (1-1-1 equivocation, fake encrypted-presence, cross-signing), at most one V signature reconstructs cluster-wide per slot. This is a property of the cluster-wide signed-message set under EKM enforcement (assumptions 1, 5).
- **Liveness affected:** A short-horizon byzantine ignoring future-slot consequences may grief more slots than a rational byzantine; each affected slot misses cleanly (no safety violation). The deterrent therefore matters for *expected liveness across many slots*, not for per-slot correctness.

**The deterrent mechanism: SSV's existing offline-operator economics.** Per-validator operator fees on SSV are paid continuously to all cluster operators regardless of per-slot contribution — a byzantine that engineers slot-miss earns the same per-slot fee as an operator who is silently online or completely offline. Operations cost is ~zero (the other operators do the work). Stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters; once enough stakers migrate, the cluster's fee inflow drops to zero across all operators including the byzantine. This is the same mechanism that disciplines a permanently-offline operator. The byzantine's gain per slot is bounded above by what an offline operator would get; their loss (reputation, future cluster invitations, validator migration before the offending slot's fees materialize at scale) is real and persistent across slots.

The protocol surfaces every byzantine fault class as on-wire evidence ([§Slashing evidence](#slashing-evidence)) signed by the offender's own keys, verifiable in isolation by any observer. The evidence informs (a) staker migration decisions and (b) the cluster operators' blacklist trigger (next paragraph). Filing a stake-slashing transaction via the SSV contract is a complementary punitive action available where the evidence is cryptographically self-contained, but it is not the primary deterrent — the primary deterrent is the economic equivalence to going offline plus the eventual `Byzantine ≡ Down` collapse via blacklisting.

**Byzantine ≡ Down in QBFT, significantly worse on latency than Down in OBFT-family — manual blacklist is the equalizer.** QBFT's round-change makes any byzantine deviation functionally indistinguishable from operator silence: round 1 times out, round 2 succeeds with a different leader, byzantine pays the round-1-timeout latency and nothing more. OBFT-family has no round-change escape valve; byzantine grief vectors at L_0 (equivocation σ-locked splits, `h_V=1` selective Phase-1 delivery, fake-encrypted-presence, behavioral σ-refusal) can engineer reliable per-slot slot-miss when the byzantine is L_0 — typically ~25% of slots at f=1 n=4 with uniform leader rotation. The grief above offline behavior is the residual the deterrent must absorb.

The expected operational response is a **manual blacklist**: the surviving `n − f` operators, on observing sufficient evidence of byzantine behavior (whether cryptographically self-contained or behavioral-pattern accumulated across slots), push a config-file update treating the byzantine operator's messages as silent for subsequent slots. The protocol must support this — message-level dropping/discarding by operator identity, plus duty-scheduling that excludes the blacklisted operator's leader rotation — as a planned protocol extension. **The current OBFT spec does not specify the blacklist mechanism**; once added, the byzantine's residual grief surface above offline behavior is bounded by detection latency + cluster governance reaction time (the same window that disciplines an offline operator who hasn't yet been migrated away from by stakers).

**Sketch of the planned blacklist mechanism.** Each operator attaches a 2-byte (16-bit) **blacklist bitfield** to their first message in each slot — Phase-1 bundle for layer leaders, Phase-2 onion for non-leaders. Each bit indicates "I locally consider this operator blacklisted"; 16 bits accommodates SSV's largest cluster size (`n ≤ 13`). The bitfield is covered by the carrying message's operator-identity-key auth envelope, so the signal is attributable per `(operator, slot)`. Receivers maintain a per-`(slot, target)` ACK count; once an operator observes **`f+1` ACKs** on any target, they treat the target as silent for the slot's duty — leader rotation skips the target's layer (the K-layer fall-through advances to the next layer), the target's σ/NR partials are ignored, and any Phase-1 bundle from the target is dropped.

The `f+1` threshold is the BFT-liveness minimum (any `f+1` distinct ACKs contains at least one honest agreement by pigeonhole at the f-byz bound); 2f+1 would be the full BFT-quorum strength but slower to activate. The threshold is a deployment tuning knob between activation latency and false-positive resistance against byz bit-flipping to falsely flag honest operators. Blacklist state persists in each operator's local store across slots, so a target blacklisted in slot N stays blacklisted in N+1, N+2, ... unless explicitly rehabilitated.

**Within-slot timing.** Because the bitfield piggybacks on first-broadcast, blacklist convergence happens *during* the slot — typically as Phase-1 bundles propagate. The byz can still grief the slot in which they are first being added to others' blacklists (their L_k turn at slot N can still fire if blacklist consensus has not yet reached `f+1` before their fetch deadline). Effective enforcement is from the *following* slot onward, bounding the byz's per-slot latency-grief above offline behavior to the detection-and-convergence window — single slot for cryptographic-evidence faults (every honest operator flips the bit in their next bitfield), longer for behavioral-pattern faults that need multi-slot accumulation before a bit-flip is justified.

**Evidence quality determines blacklist-trigger latency.** Some fault classes leave self-contained cryptographic proofs that any single observation can blacklist confidently:

| Fault class | Evidence type | Time-to-blacklist |
|---|---|---|
| Equivocation, cross-signing, cross-onion partial-sig equivocation, fake plaintext σ at L_0 | **Cryptographic, self-contained** — a single signed message-pair conclusively demonstrates the action | Single observation; bounded by governance reaction time |
| Fake encrypted-presence at k > 0 | **Cryptographic but conditional** — requires NR-quorum to reach at all prior layers for decryption to unlock; can be sealed by adversarial slot-miss | Conditional on slot progression in subsequent slots |
| Selective-delivery / withholding grief, byzantine σ-refusal coordinated with honest transient flakiness | **Behavioral pattern** — no single signed message proves it; requires aggregating observations across operators / slots | Multi-slot accumulation; risk of false-positive against an honest-but-flaky operator |

**The deterrent works in expectation across many slots, not per-slot.** A byzantine that consistently misbehaves accumulates evidence; the surviving operators eventually blacklist them, and stakers migrate validators away. The "permitted byzantine grief" framing in §Failure modes is therefore conditional on (a) the surviving operators actively monitoring and willing to coordinate the blacklist update, (b) byzantines being economically deterred by the eventual fee collapse — i.e., operating cluster fee accrual exceeds the expected per-slot grief value, (c) the staker migration mechanism functioning at meaningful pace. Where those conditions don't hold (passive small clusters, byzantines on their way out, dysfunctional governance, high-MEV slots where per-slot grief value spikes), the deterrent's effective strength is correspondingly weaker.

**An asymmetry to keep in mind: the deterrent is weakest where per-slot grief is most damaging.** Faults with cryptographically self-contained evidence (equivocation, cross-signing, cross-onion partial-sig equivocation, fake plaintext σ at L_0) are single-observation blacklistable, but these are also the faults the protocol *already* defends against at the slot level (natural σ-recovery on equivocation 2-1 splits; detect-and-reject for fake plaintext σ). Faults with only behavioral evidence (`h_V=1` selective Phase-1 delivery, byz σ-refusal coordinated with mesh-flakiness, validity-divergence with byz passivity) are the *most damaging* per-slot grief vectors — they reliably miss the slot at L_0 when an adversarial byz exercises them — and they're the slowest to blacklist with confidence.

## Protocol

OBFT runs **a single agreement round** per slot: Phase 1 → Phase 2 → Phase 3. Phase 1 is a fresh broadcast (no re-flood across rounds, since there is only one round). The slot's hard wall is the relay submission deadline (`T_relay_cutoff − T_submit`); a slot that does not reach σ-quorum at any layer with enough time to submit is missed.

### Phase 1 — Candidate broadcast

Phase 1 has K per-layer fetch windows ending at broadcast deadlines `T_broadcast_max_k = max(BFT_start, T_commit − B_k)`. Under the post-tighten schedule, only the primary L_0 has a positive broadcast deadline; backups L_1..L_{K-1} all broadcast at BFT_start (`B_k = T_commit`). Each leader's window starts whenever the host application is ready to begin its fetch loop (typically slot start) and must terminate by `T_broadcast_max_k`. Concretely at K=2 Config A default with default RefloodDelay=700ms: backup `L_1` broadcasts at `BFT_start` (`T_broadcast_max_1 = 0`); primary `L_0` by `T_commit − (2·BTT + RefloodDelay) = 2500ms`. (At RefloodDelay=0 the primary deadline shifts to `3200ms`; backup unchanged.) K=4 up-tier extends the backup pattern to `L_1, L_2, L_3` (all broadcasting at BFT_start). Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "OBFT-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates OBFT Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, other OBFT message kinds, other consensus protocols sharing the same identity key). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, and check the first-observation timestamp — accept bundles whose first-observation time is in `[slot_start, T_commit]`. Bundles first-observed past `T_commit` are not counted toward σ-quorum at this layer (no late acceptance: each operator commits once at `T_commit` based on what they observed by then). Bundles failing cryptographic-auth are silently dropped. (A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.)
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. A `not-valid` verdict turns the operator's commitment into NV (operationally identical to NR — see "Operator commitments — σ, NR, NV" below).

If a leader `L_k` fails to broadcast at all (or broadcasts so late that its bundle arrives past `T_commit` at every honest receiver), that layer is unavailable; the cluster falls through to deeper layers via NR-quorum. If all K leaders fail, the slot is missed.

**Bundle propagation.** Honest receivers re-flood every bundle via standard gossipsub on first observation. OBFT has no protocol-level second re-flood event for the **whole** Phase-1 bundle `(V, σ_L^V, σ_L^op)` (no rounds): cluster-wide reception of V relies on gossipsub's organic propagation completing before `T_commit`. **A protocol-level re-broadcast does exist for the leader's σ_L^V partial alone**: every operator's Phase-2 `KindCommit` carries a witness section with byte-for-byte copies of retained σ_L^V partials paired with `value_root` cross-references (see §Phase 2 / Wire format). This protects σ_L^V against bundle drop at peer receivers who DID receive V, but does not address V-drop. Honest leaders broadcasting by their per-layer deadline `T_broadcast_max_k = max(BFT_start, T_commit − B_k)` reach all honest within partial-synchrony assumptions for that layer's propagation budget `B_k` (see §Setting).

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

**Operator commitments — σ, NR, NV.** Each operator commits exactly once per (slot, layer) by `T_commit`, based on what they observed at emit time (see §Phase 2 emission-timing). Three states:

- **σ (sign-on-V)**: the operator received the leader's bundle by `T_commit`, both protocol-level and application-level checks passed, and the operator did not retain ≥ 2 distinct V's at this layer (no equivocation observed). Materializes as a σ partial in the operator's `KindCommit` message at this layer (or as the leader's Phase-1 σ for the layer's own leader). Once committed, the operator is **σ-locked** at this layer for the entire slot.
- **NR (non-receipt)**: by `T_commit`, the operator did not receive an auth-valid Phase-1 bundle for this layer (the leader is treated as silent from this operator's perspective).
- **NV (non-validity)**: host application returned `not valid` for `V_{L_k}`.

NR and NV are operationally interchangeable on the wire: both materialize as a partial `σ_i^{IBE}(nr_tag_k)` on the layer's NR tag, carried in the operator's `KindCommit` message. The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to as "NR-quorum" throughout). The distinction is local-only diagnostic. References to "NR" elsewhere in this document encompass NR-silent + NV unless stated otherwise.

Equivocation observed pre-emit (≥ 2 distinct V's retained before the operator broadcasts their `KindCommit`) collapses to NR per the rule above — there is no Defer state. The operator emits NR; cross-phase exclusivity locks them out of σ on either V at this layer. Equivocation observed by the operator after their own emit cannot retroactively change their commitment; the leader's equivocation evidence remains slashable regardless.

### Phase 2 — Onion broadcast `[T_emit, T_emit + Δ_2]` (with `T_emit ≤ T_commit`)

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

**Phase 2 timing — soft propagation budget.** Each operator emits exactly one `KindCommit` message at `T_emit = min(T_L_0_observed, T_commit)`, where `T_L_0_observed` is the first moment the operator has retained and host-validated the Phase-1 bundle for layer `L_0`. The Phase 2 window `[T_emit, T_emit + Δ_2]` is sized for that message to propagate to all honest peers under nominal partial synchrony. `T_commit + Δ_2` is the SOFT cluster-wide worst-case target by which every honest operator's σ/NR commitment is expected to be on the wire and observable (worst case = synchronous fallback `T_emit = T_commit`); in the healthy case, all `KindCommit` messages land materially earlier. **Phase 3 MAY attempt reconstruction opportunistically from `T_emit` onward** — Resolve is idempotent and returns "no quorum yet" cleanly on incomplete pools, so an operator that calls Resolve on each `KindCommit` arrival picks up σ-quorum the moment the last needed partial lands, rather than waiting the full `Δ_2` budget. Operators MUST NOT *rely* on reconstruction completing before `T_commit + Δ_2` (the budget exists for the worst-case propagation tail from `T_commit`-fallback emit), but they MAY (and the canonical implementation DOES) attempt Resolve earlier. See §Phase 3 for the observer-on-arrival pattern.

**Emission-timing rule.** `T_emit = min(T_L_0_observed, T_commit)`:

- `T_L_0_observed`: first moment the operator's L_0 commitment is determinable — any of: (a) a uniquely-retained Phase-1 bundle at `L_0` with the host application's `valid`/`not-valid` verdict on `V_{L_0}` returned, (b) ≥ 2 distinct V's observable at `L_0` across (retained bundles ∪ peer σ-onion entries ∪ verified σ_L^V witnesses) — cluster-wide leader equivocation → forced NR per cross-phase exclusivity, (c) for an operator who is the L_0 leader, the moment they've built and self-observed their own Phase-1 bundle (their σ_V from Phase 1 counts as their σ-side commitment at L_0; the host's validity verdict on their own V is implicit in the FetchCandidate result), or (d) a uniquely-observed peer-V at L_0 (via another operator's σ-onion entry) with a verified leader σ_L^V witness and host validity recorded (peer-reflood-V σ path, see "Peer-reflood V via early commit" below). The primary L_0 broadcasts later than the backups (which broadcast at BFT_start), so in healthy mesh L_0 is the last bundle to arrive — `T_L_0_observed` typically coincides with all-K-layers-observed.
- `T_commit` fallback fires if `L_0`'s bundle never arrives at the operator (silent or grossly-delayed primary leader). At `T_commit`, the operator emits with NR on `L_0` and σ on whichever deeper layers have observed-and-valid bundles.
- At `T_emit` the operator includes σ on each layer with observed-and-valid bundle and NR on each layer without — regardless of whether that layer's leader has broadcast yet. Deeper-layer "early NR" is the cost of optimizing for `L_0`'s success; under healthy operation `L_0` σ-quorums and the deeper-layer commitments don't matter.

Safety is unaffected by emission timing: per-operator σ/NR decisions are locally well-defined from the operator's observation set at `T_emit`, and Pigeonhole 2 (at-most-one σ-quorum per `(slot, layer)` cluster-wide) holds structurally regardless of when individual operators commit. The byzantine equivocation-window does widen slightly compared to synchronous emission — an attacker timing equivocation delivery into `(T_emit, T_commit]` at an early-emitting operator engineers a "σ-on-V-despite-observed-equivocation" outcome: the operator's σ on V is on the wire (committed pre-emit) but the operator now retains both V and V'. Their σ partial still counts toward V's σ-pool; the equivocation evidence (Rule 2) is also on the wire and slashable. The regression vs synchronous emit is that an asymmetric byzantine — delivering V' to the early-emitter only after their emit — can split the cluster's σ-pool (some honest σ on V, some NR due to observed equivocation), potentially preventing both σ-quorum and NR-quorum fall-through at L_0. This pattern is byzantine-driven and bounded by the rational-byzantine deterrent (assumption 4); it's already an out-of-envelope view-divergence case (see §Assumptions and implications).

**Peer-reflood V via early commit.** Early-commit semantics double as a V-drop recovery mechanism. A `KindCommit` from an L_0-σ-state operator carries V plaintext in the σ-side onion entry at L_0 (per "Wire format" below) plus a σ_L^V witness for the same V — together a complete redelivery of `(V, σ_L^V)` to any V-drop receiver. A V-drop receiver j who observes such a peer commit before j's own emit can:

1. Harvest V plaintext from the peer's σ-side onion entry at L_0 (`peerOnions[0][peer].Value`).
2. Harvest σ_L^V from the peer's witness section and verify it against V (via the leader's pubshare).
3. Validate V via the host application.
4. σ_j^V on V themselves and include the partial in their own (later) `KindCommit`.

**This closes the h_V=1 selective-Phase-1-delivery deadlock at L_0 under healthy mesh.** Before: byz leader delivers V to only 1 honest → σ-pool = leader σ_L^V + 1 honest = 2 < qV=3, no fall-through (`σ-locked` honest can't NR). After: the 1 honest's early commit lets V-drop receivers σ on peer-V → σ-pool = leader σ_L^V + 3 honest = 4 ≥ qV. Slot succeeds at L_0. The degraded-mesh residual (sender's early commit doesn't reach V-drop receivers before their `T_commit` fallback) is discussed in the Timing window paragraph below.

**Byz-safety gate.** Peer-V is only usable when the leader's σ_L^V on V has been verified (witnessed in `witnessedLeaderSigma`). A byzantine peer fabricating V_fake without a real σ_L^V cannot trick honest receivers — the missing witness blocks the σ_j^V emission path.

**Equivocation handling.** If two distinct V's are observable across (retained bundles ∪ peer onions ∪ verified σ_L^V witnesses), the equivocation predicate fires and the operator commits NR per cross-phase exclusivity. Same recovery shape as bundle-observed equivocation today.

**Timing window.** For peer-reflood-V to fire at receiver j, the sender's early-commit must arrive at j before j's T_commit fallback fires. At Config A under healthy mesh: V-holder early-emits at ~`T_commit − 1·BTT − RefloodDelay`; peer commit reaches V-drop receivers within 1·BTT — comfortably ahead of T_commit. When the sender's emit is closer to T_commit (degraded mesh) or the V-drop receiver is mesh-flaky on inbound, the receiver falls back to T_commit fallback (NR on L_0) — degraded but graceful (no regression vs pre-§1 behavior).

**Impl wire.** The runner exposes a per-slot `WantsHostValidationCh` channel; the Instance enqueues `(layer, V)` requests on first peer-V observation at any layer (practical benefit at L_0; deeper layers have B_k = T_commit and rarely need this path). The runner drains the channel, invokes the host hook, and feeds the verdict back via `ApplyHostValidity` — which closes `L0ReadyCh` on the σ-via-peer-V branch.

**Δ_2 sizing.** **Recommended for production: `Δ_2 = 1 BTT`** — one propagation cycle for `KindCommit` messages emitted by `T_commit` (the synchronous fallback) to reach all honest by start of Phase 3. Reflood absorption is structurally provided by per-layer `B_k` via the reflood-aware schedule (`(k+2)·BTT + RefloodDelay`), so `Δ_2` no longer carries a separate reflood cushion — the only post-`T_commit` work is the single propagation cycle for the synchronous-emit case. At Config A (P99=150ms, δ=50ms): `Δ_2 = 200ms`. Sub-1·BTT sizings are sub-BFT (Phase-2 propagation can't complete within the budget); `Validate` does not enforce the floor (informational only — the cluster systematically misses at runtime).

**ε_3 sizing.** ε_3 covers BLS aggregation + IBE decryption walk + certificate construction. At Config A: `ε_3 ≈ 50ms`. Phase 3 begins when all expected `KindCommit` messages have arrived (i.e., at `T_commit + Δ_2`); reconstruction is local-CPU work. The slot's hard wall is the relay submission deadline `T_relay_cutoff − T_submit`, not a fixed Phase-3-end deadline — a slow operator's reconstruction can spill into submission slack, and `KindCertificate` gossip from a faster peer (see §Final-certificate gossip) lets an operator that hasn't completed local reconstruction submit `(V, S)` directly. (At the §Application max-MEV anchor `T_commit = Relay_cutoff − 2 BTT = 3600ms`, the post-`T_commit` budget of 400ms = 2 BTT decomposes as `Δ_2` (200ms) + `ε_3` (50ms) + `header_submit_headroom` (100ms) + ~50ms residual jitter buffer between Phase-3 completion and cert/submit start. The jitter buffer is the slack between the optimistic Phase-3-complete time and the latest cert-broadcast start that still meets `Relay_cutoff`.)

**Wire format: a single auth-wrapped message kind.** Each operator emits exactly one `KindCommit` per (slot, operator) at `T_emit` (see Emission-timing rule above), carrying:

- The K-layer onion of σ partials (plaintext at L_0, chained-encrypted at deeper layers) for layers where the operator is σ-state.
- NR/NV partials `σ_i^{IBE}(nr_tag_k)` for layers where the operator is NR-state.
- **Leader σ_L^V witness section.** For every Phase-1 bundle the operator has retained at this point (per §Phase 1's retention rules — typically one bundle per layer, two per layer in the equivocation-observed case), a `(layer k, value_root, σ_{L_k}^V(V_{L_k}))` triple extracted from the bundle. The `value_root` is a 32-byte identifier (e.g., `sha256(V)`) sufficient for the σ-pool to cross-reference against any V the receiver has locally observed — either retained from Phase-1 receipt OR present as the plaintext `Value` field of any peer's σ-side onion entry at this layer in any `KindCommit` they've received. The witness itself does NOT carry the full V. These are byte-for-byte copies of the leader's σ partial as `i` observed it — **not** new signings by `i` (no EKM event, no new signing obligation, no new cryptographic primitive). The section provides redundancy against σ_V-drop at peer receivers who DID receive V from Phase 1, AND partial recovery for V-drop receivers when at least one peer's onion entry carries V (see "Broadened V-source" paragraph below): a peer can harvest σ_L^V from `i`'s witness section directly into the layer-`k` σ-pool (the witness bytes are plaintext at every layer, since they're copies of the leader's Phase-1 partial — not subject to chained encryption). What chained encryption gates is the *path to qV at k > 0*: peer onion σ partials at layer `k > 0` are encrypted under `nr_tag_0 ∧ ... ∧ nr_tag_{k-1}` and require NR-quorum at every prior layer to decrypt, so the σ-pool at `k > 0` only grows beyond the witness contributions when the chain unlocks. Bandwidth is small: per witness ≈ 4 + 8 + 32 + 96 + length-prefix overhead ≈ 145 bytes (Layer + Leader + ValueRoot + σ partial); cluster-wide at K=2 n=4 default ≈ 8 witnesses × 145 ≈ 1.2 KB (K=4 up-tier: 16 witnesses × 145 ≈ 2.3 KB).

  **Broadened V-source for witness cross-reference.** A receiver looking up V by `value_root` checks BOTH local Phase-1 retention (the originally-conceived source) AND the plaintext `Value` fields of any peer-onion entries already observed at the same layer in any received `KindCommit`. This widens partial V-drop recovery: a receiver whose Phase-1 bundle was dropped can still harvest σ_L^V from a witness section, provided at least one *other* peer's onion entry at the same layer carries V (typical when at least one honest non-leader received the leader's bundle and σ-emitted on V, embedding V in their KindCommit's plaintext `Value` field). Safety is preserved: σ_L^V verification is cryptographically V-bound (BLS verify against leader's pubshare on V), so a forged V in a peer onion can't be paired with a valid σ_L^V (forgery rejected at verify time). Pigeonhole 2's at-most-one-qV claim depends on what operators *sign*, not on where receivers *find* V — broadening the V-source only changes what σ-pools receivers can assemble locally, not what's signed cluster-wide. Liveness is strictly improved (more V-drop receivers can reach σ-quorum without falling back to `KindCertificate` rescue).

  V-drop receivers when NO peer onion carries V at this layer (all honest peers also missed the bundle, or the bundle never broadcast at all) still rely on `KindCertificate` gossip (see §Final-certificate gossip) for V recovery — a faster peer who reconstructed `(V, S)` gossips the cert, and the V-drop receiver consumes it directly. See [Appendix C](#appendix-c--message-re-broadcast-considerations) for full-bundle re-broadcast that would address V-drop additionally at higher bandwidth cost (optional defensive engineering, not core).

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

EKM enforces these exclusivities cryptographically (slashing-protection log keyed on `(slot, layer, commitment-side)`); see "Preconditions on the host application / Slashing-protection scope" and [EKM coordination model](#ekm-coordination-model).

A byzantine operator that publishes both σ and NR on the same `(slot, layer)` is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For each layer's leader specifically, "cross-signing" includes any Phase-1 σ + later NR pair on the same slot.

### Phase 3 — Local decryption and reconstruction (target completion `T_commit + Δ_2 + ε_3`; opportunistic Resolve from `T_commit` onward)

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches on some V (output produced), or NR-quorum reaches (advance to next layer), or neither (slot misses).

**ε_3 sizing.** Phase 3 is purely local CPU work — BLS aggregation, IBE decryption walk across K layers, certificate construction. So `ε_3` ≈ 50ms at Config A. (`KindCommit` propagation is already covered by `Δ_2`; by the SOFT target `T_commit + Δ_2`, all expected commits are expected to have arrived at all honest receivers.) **`ε_3` scales with the number of layers actually walked**: `ε_3 ≈ 50ms` characterizes single-layer reconstruction (σ-quorum reaches at L_0); under multi-layer fall-through, the IBE-decryption walk runs sequentially through each NR-quorum-unlocked layer, so end-to-end Phase 3 cost grows roughly linearly with the number of fall-throughs (e.g., ~`ε_3 × K` ≈ 100ms at K=2 default with K−1=1 silent layer (K=4 up-tier with 3 silent layers: ≈ 200ms); see [§Liveness comparison / Multi-failure fall-through](#liveness-comparison-obft-vs-obftrr2-vs-qbft)). This isn't a hard sizing constraint because Phase 3 has no fixed end (the slot's wall is the relay submission deadline, not `T_commit + Δ_2 + ε_3`); a slow operator's reconstruction spills into submission slack — see §Phase 3.

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
            # deduplicated per operator: leader's Phase-1 σ_L^V and witness-
            # section copies of σ_L^V from peer KindCommits collapse to one
            # partial (identical bytes across peers); under cross-phase
            # exclusivity an honest leader does NOT also include an onion σ at
            # their own layer, so the only way the same operator's σ appears
            # twice (once as leader's σ_L^V and once in their own onion) is
            # byzantine cross-signing — slashable per Rule 1, deduplicated
            # here for aggregation purposes.
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

**Slot's hard wall: relay submission deadline.** Reconstruction may be attempted opportunistically from `T_commit` onward (see "Observer-on-arrival" below) and continues until either σ-quorum reaches (output `V`, halt) or the operator's local relay-submission deadline (`T_relay_cutoff − T_submit`) is reached without a result (slot misses for that operator). Under nominal partial synchrony, σ-quorum forms by `T_commit + Δ_2 + ε_3` (the SOFT target — ≈ 250ms after `T_commit` at Config A under recommended sizing `Δ_2 = 1 BTT`, ε_3 ≈ 50ms); the average healthy case decides materially earlier because typical mesh-hop propagation is below the worst-case `Δ_2` budget. A slow operator overrunning the soft target can still complete inside the submission slack `[soft target, T_relay_cutoff − T_submit]`. A faster peer's `KindCertificate` broadcast (see §Final-certificate gossip) lets an operator that hasn't completed local reconstruction submit `(V, S)` directly.

**Observer-on-arrival — canonical implementation pattern.** Resolve (the reconstruction walk above) is stateless and idempotent: re-running it on partial state returns "no quorum yet" without mutating any Instance state. The canonical implementation drops the historical "wait `Δ_2`, then poll" pattern in favor of invoking Resolve on every state delta:

```text
on KindCommit observed:       run Resolve; if output, submit + broadcast cert
on KindCertificate observed:  if cert verifies, submit (V, S) directly
otherwise:                    block (no idle wakeups) until next arrival or slot deadline
```

This collapses the per-slot Δ_2 wait (200ms at tightened sizing) into the propagation time the cluster actually needs — measured 1–3 ms median per mesh-hop at SSV's production cluster — so σ-quorum at L_0 typically forms within a few BTTs of `T_commit`, and the slot decides materially before `T_commit + Δ_2`. Late `KindCommit` messages arriving past `T_commit + Δ_2` are still incorporated by the same observer hook; the walk above is re-run, and:

- A late σ partial pushes σ-pool past `qV` at some layer that didn't reach on prior calls → output `V` at that layer.
- A late NR partial pushes NR-pool past `qEnc` at a layer that previously had NR-pool short of `qEnc` → derive the layer-`k` decryption key, unlock chained decryption for layer `k+1`'s σ partials, advance the walk past `k`.

Pigeonhole semantics still hold (at most one `V` reconstructs cluster-wide regardless of timing), so re-running is safe. Note this only applies to late `KindCommit` messages; Phase-1 *bundles* arriving after `T_commit` are explicitly rejected per the Phase-1 acceptance rule and cannot be incorporated.

**Rationale for relaxing the Δ_2 gate.** Empirical measurements on SSV's production mesh (see [`Prod_1_2_3_4_CalibratedLogNormalMixture`](../protocol/v2/consensustest/network.go) in the consensustest framework) put typical mesh-hop propagation at 1–3 ms median, with the long tail of the lognormal mixture extending into hundreds of ms but representing a small fraction of arrivals. The recommended `Δ_2 = 1·BTT = 200 ms` is sized for that worst-case tail under the synchronous-fallback `KindCommit` emit, not the typical case — so gating Resolve on the full `Δ_2` budget forfeits the propagation gap on nearly every slot. Observer-mode reclaims it without changing any wire-format invariant or σ/NR-pool aggregation rule: late arrivals still participate via re-runs, and Pigeonhole 2 still caps cluster-wide reconstruction at one `V`.

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s `KindCommit` at decryption time treats `j` as not having contributed at any layer: no σ partial, no NR partial. Standard threshold cryptography — only signed messages count. Within Phase 2's SOFT propagation target (`Δ_2 ≥ 1 BTT`), gossipsub propagation is expected to deliver all honest `KindCommit` messages to all honest receivers by `T_commit + Δ_2`; opportunistic Resolve picks them up as they arrive (see §Phase 3 / observer-on-arrival), so the receiver's σ-pool builds incrementally rather than being snapshotted at a single moment.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within the slot's relay-submission deadline (see "Fault tolerance / Liveness"). If more than `f` operators are offline or byzantine combined, neither σ nor NR quorums reach their thresholds and the slot is missed. Within the f-bound, OBFT covers in-envelope cases via K-layer fall-through: healthy path, silent-leader fall-through, multi-leader fall-through (sequential within Phase 3 reconstruction). View-divergence cases — equivocation σ-locked splits and host-validity divergence — are explicitly out of recovery scope (some splits succeed naturally as a side-effect of leader-σ-V counting; rest slot-miss). Asymmetric propagation past `T_commit` is also out of recovery scope (a deeper backup whose bundle did propagate in time is the only recovery path). See [Assumptions and implications](#assumptions-and-implications).

### Slot structure

OBFT runs a single agreement round per slot. The slot proceeds as follows:

1. **Phase 1**: each leader `L_k` broadcasts its Phase-1 bundle by its per-layer deadline `T_broadcast_max_k = max(BFT_start, T_commit − B_k)` (primary-vs-backup: `B_0 = 2·BTT + RefloodDelay`; `B_1..B_{K-1} = T_commit` — backups broadcast at BFT_start; see §Setting). Receivers accept bundles first-observed in `[slot_start, T_commit]`.
2. **Phase 2** `[T_emit, T_emit + Δ_2]` (with `T_emit = min(T_L_0_observed, T_commit)`): each operator emits a single `KindCommit` message at `T_emit` carrying their per-layer σ partials (for σ-state layers) and NR partials (for NR-state layers). The window is sized for `KindCommit` propagation to all honest peers (worst case anchored at `T_commit`-fallback emit). See §Phase 2 emission-timing.
3. **Phase 3** (opportunistic from `T_commit` onward; SOFT target `T_commit + Δ_2 + ε_3`): each operator runs the K-layer reconstruction walk on every state delta (`KindCommit` / `KindCertificate` observation), not on a fixed schedule. If σ-quorum reaches on some V at any layer, output the V; halt. If NR-quorum reaches up to some layer `L_C < K`, advance L_C and continue the walk. If neither σ-quorum nor NR-quorum advance unlock at any layer on a given delta, the operator waits for the next delta or the relay-submission deadline (re-running on each subsequent arrival picks up late `KindCommit` arrivals — see §Phase 3).

**Slot timing**: Phase 1 fetch occupies `[slot_start, T_commit]`. The slot's consensus budget (Phase 2 + Phase 3) is `Δ_2 + ε_3 ≈ 1 BTT + ε_3` ≈ 250ms at Config A under recommended sizing (`Δ_2 = 1 BTT`, `ε_3 ≈ 50ms`); this is the SOFT target for the worst-case propagation tail. Under observer-on-arrival (see §Phase 3), the average healthy slot decides materially earlier — typical mesh-hop propagation is 1–3 ms median, so σ-quorum at L_0 generally forms within a few BTTs of `T_commit` and well before the `Δ_2` budget is exhausted. The remainder of the slot is submission slack to `T_relay_cutoff`.

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

The single-round model means no cross-round atomicity, no persistent partial-sig cache, and no deterministic re-signing fallback are required: each (slot, layer, operator) signing event happens at most once and is never re-emitted.

**Implementation paths.** (a) A custom EKM that maintains the unified log natively across both keypair shares. (b) A coordinator component in the SSV operator process that wraps two standard EKMs (one per keypair share) and enforces the cross-keypair rules in the operator's slashing-protection database — a single transactional log, two keypair-specific signing-request handlers reading and writing it. Path (b) is the path SSV will most likely take.

**EKM correctness is defense-in-depth for safety, not the sole enforcement.** OBFT's safety (Pigeonholes 1 and 2) holds when honest operators correctly implement the protocol rules — single-σ-V per (slot, layer) per operator, σ-XOR-NR per layer. The **protocol layer** (operator software implementing the OBFT state machine) is the primary enforcement point: it determines when σ vs NR is requested from the EKM in the first place. The **EKM** is a catch-net: it rejects signing requests that violate the slashing-protection invariants, providing defense-in-depth even if the protocol layer is buggy.

For a single honest operator to produce safety-violating behavior (e.g., σ on V and σ on V' at the same `(slot, layer)`), **both layers must be buggy in compounding ways**: the protocol layer must request the second σ (violation of σ-eligibility logic) AND the EKM must fail to reject it (violation of slashing-protection lookup or atomicity). A single-layer bug typically does not break safety:

- Protocol-layer bug only: the EKM rejects the bad request; no double-sign emitted on the wire.
- EKM-layer bug only: the protocol layer doesn't ask for double-signing, so the EKM bug is never exercised.

Cluster-wide safety violation (Pigeonhole 2 producing two qV-quorums on different V's) requires aggregating these single-operator violations to reach `2 · qV = 4f+2` partials across two V's. At `f = 1, n = 4`, one byzantine operator contributes ≤ 1 partial per V (≤ 2 total); three correct honest contribute exactly 3 partials total (single-σ-V each); sum 5 < 6 = 2 · qV. The minimum safety-violating configuration is therefore **one byzantine operator plus one honest operator with compounding protocol+EKM bugs** — together producing the missing partial. This is two misbehaving operators total, exceeding the `f = 1` trust budget. Single-layer bugs alone are tolerated; safety requires both layers to be correct on at least `n − f = 3` operators.

**Trust posture is the same as QBFT.** Both protocols rely on honest-majority correct implementation of the protocol logic *plus* correct slashing-protection — neither is "100% cryptographic" against operator-side software bugs (see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic)). The difference is in the slashing-protection layer's maturity: QBFT's per-key slashing-protection (Web3Signer, EIP-3076 interchange format) has decade-of-production hardening; the OBFT coordinator is novel, so reaching comparable defense-in-depth robustness requires deliberate engineering investment in (a) test coverage on cross-keypair atomicity for the single signing event per (slot, layer), (b) fault-injection testing of the operator-restart scenario, (c) optionally operational margin via larger `n` (e.g., `n ≥ 5` keeps `f = 1` while expanding the bug-budget headroom).

**Summary of EKM failure modes.** A **maliciously compromised** EKM (signs requests outside protocol rules, or generates signatures the protocol layer didn't request) is byzantine-equivalent and directly consumes f-budget. A **passively buggy** EKM (fails to reject bad requests but doesn't generate signatures on its own) requires the protocol layer to also have a compounding bug for safety-violating behavior to actually occur — see the defense-in-depth analysis above. In both cases, the cluster's overall trust posture follows the standard "honest-majority cryptographic" framing — see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic).

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model. Operational rules that handle specific faults (equivocation detect-and-slash, cross-signing detection, head-change refresh) are described inline in the relevant Phase sections; the analyses of *what those rules buy* and *under what conditions* live here.

### Trust model

- **Byzantine bound `f`** with cluster size `n = 3f+1` (the BFT-tight setting; see [§Assumed / Standard BFT trust bound at the tight setting](#assumed)): up to `f` operators may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, etc.). Exactly `2f+1` honest. Running example: `n = 4, f = 1` (3 honest guaranteed).
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `P99` (propagation P99/P999) and clock skew `δ`. Per-layer leader broadcast deadlines `T_broadcast_max_k = max(BFT_start, T_commit − B_k)` (with `B_k` increasing monotonically from primary to deepest backup — see §Setting); bundles first-observed past `T_commit` are not counted. Phase 2's `T_commit + Δ_2` is the SOFT cluster-wide target by which honest σ/NR commitments are expected to have propagated; Phase 3 has no fixed end — opportunistic reconstruction runs from `T_commit` onward until σ-quorum reaches or the relay-submission deadline forces termination (see §Phase 3 for the observer-on-arrival pattern; operators MUST NOT *rely* on reconstruction completing before `T_commit + Δ_2` but MAY attempt Resolve earlier). Late `KindCommit` arrivals can be incorporated by re-running the reconstruction walk. Safety holds against arbitrary network adversaries; only liveness depends on synchrony.

  **OBFT's absorption is primary-vs-backup**: per-layer budget `B_k` for layer `L_k`, with `B_0 = 2·BTT + RefloodDelay = 1100ms` (at default RefloodDelay=700ms) for the MEV-fresh primary; `B_1..B_{K-1} = T_commit = 3600ms` for all backups at Config A K=2 default (see [§Setting](#setting) for the design; K=4 up-tier extends backup pattern to L_2, L_3). Bundles arriving past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum at this layer; the cluster relies on K-layer fall-through to a backup whose bundle did propagate in time (backups have the maximally-wide absorption window — the entire commit budget).

### Safety (cryptographic, honest-majority)

**Claim:** at most one full `V` signature is ever produced per OBFT instance per slot — across any layer, on any value, across any combination of σ sources — cluster-wide, against an offline-aggregating byzantine within the f-bound, regardless of which honest aggregation rules are followed. (See [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic) for what this scope does and doesn't cover.)

The proof rests on three pigeonhole arguments — Pigeonholes 1 and 2 (single-layer) plus Pigeonhole 3 (chained encryption at `K > 2`). All three are properties of cluster-wide signed messages, enforced cryptographically by [EKM coordination model](#ekm-coordination-model) rules at the operator level — no honest-aggregation rule required.

The pool definitions used in the arguments below:

- **σ-pool on V at L_k**: `{σ_i^V(V) partials at L_k}`, deduplicated per operator. Includes the leader's Phase-1 σ_V when valid. Once a partial is emitted, it stays on the wire — no "revocation" semantics.
- **NR-pool at L_k**: `{σ_i^{IBE}(nr_tag_k) partials}`, deduplicated per operator.

**Pigeonhole 1 — σ-vs-NR at the same layer.** σ-quorum on `V` (any V) at `L_k` AND NR-quorum on `nr_tag_k` cannot both reach.

- σ-quorum on V: `h_σ + byz_σ ≥ qV = 2f+1` (where h_σ counts honest with σ partials on V at L_k from any phase, deduplicated per operator).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Cross-phase exclusivity (per "Slashing-protection scope"): `h_σ + h_NR ≤ n − f = 2f+1` (equality at `n = 3f+1`). Each honest commits σ-or-NR per layer at most once, EKM-enforced.
- **Leader-counting.** If the layer's leader is honest, their Phase-1 σ_V partial counts toward `h_σ` for the V they signed; cross-phase exclusivity then forbids them from emitting NR/NV on `nr_tag_k`. If the leader is byzantine and equivocates, each per-V partial they publish counts toward `byz_σ_V` for that V (capped at 1 per byz per V by deduplication). A byzantine leader's σ_L^V partial reaches the σ-pool via honest peers' witness sections (see §Phase 2 / Wire format) even when the byz suppresses its own gossip; the per-V dedup cap is enforced at aggregation regardless of receipt path.
- Byzantine cross-signing: `byz_σ + byz_NR ≤ 2f` (each byz contributes at most 1 to each pool).
- If both quorums reached: `h_σ + h_NR ≥ 4f+2 − 2f = 2f+2`. But `h_σ + h_NR ≤ 2f+1`. Contradiction. ∎

**Pigeonhole 2 — two σ-quorums on different values at the same layer.** Two distinct `V`'s cannot both reach σ-quorum at the same layer (e.g., via leader equivocation that some honest σ-commit on early before observing evidence):

- `(h_σ_V + h_σ_V') + (byz_σ_V + byz_σ_V') ≥ 2 · qV = 2(2f+1)`.
- Honest sign at most one V per layer (single-σ-V exclusivity, EKM-enforced — see "Slashing-protection scope"): `h_σ_V + h_σ_V' ≤ 2f+1`. The layer's leader counts here: they sign σ_V exactly once per (slot, layer), contributing to one V's pool.
- Byzantine can sign multiple V's: `byz_σ_V + byz_σ_V' ≤ 2f`.
- Bound: `(2f+1) + 2f = 4f+1 < 4f+2`. Contradiction. ∎

This is the key safety constraint underlying OBFT's "permit equivocation, slot-miss on view-divergence" framing: regardless of which V's honest σ-commit on under equivocation, at most one V can reach qV cluster-wide. There is no two-output safety failure even when honest operators split across V's; the cluster either reaches qV on a single V (some patterns recover naturally) or no V reaches qV (slot misses).

**Pigeonhole 3 — cross-layer safety under chained encryption.** Two distinct V signatures (V_k and V_{k+m} for any `m ≥ 1`) cannot both be reconstructed cluster-wide. Pigeonhole 1 applied at L_k alone seals the chain:

- *Decryption requirement.* V_{k+m} σ partials at L_{k+m} are encrypted under `nr_tag_k ∧ nr_tag_{k+1} ∧ … ∧ nr_tag_{k+m−1}`. Decryption requires NR-quorum on every `nr_tag_j` for `j ∈ [k, k+m−1]`; missing any single one (in particular `nr_tag_k`) keeps the chain sealed.
- *Argument.* If V_k σ-quorum reaches at L_k, then by Pigeonhole 1 at L_k, NR-quorum at L_k does not reach. Decryption of L_{k+m}'s σ partials therefore fails (any prior unreached NR-quorum suffices), so V_{k+m} cannot reconstruct.
- *Symmetric direction.* If V_{k+m} reconstructs, NR-quorum at L_k must have reached (chained-decryption requirement), so by Pigeonhole 1 σ-quorum at L_k did not reach, so V_k does not reconstruct. ∎

Applied to every pair of layers, at most one V signature reconstructs cluster-wide across all K layers.

**Cryptographic primitive — chained IBE.** Layer-`k` σ partials are encrypted under `nr_tag_0 ∧ nr_tag_1 ∧ ... ∧ nr_tag_{k-1}`. Decryption requires NR-quorum at each prior layer. Implementation: each level of the chain is a standard threshold-IBE ciphertext (drand/tlock) using `nr_tag_j` as the tag. At K=2 the chain has only one level (single tag `nr_tag_0`); at K=3 there are two levels nested; etc.

The arguments above apply symmetrically to all K layers. **None of the proofs depends on honest operators excluding cross-signers from their aggregation** — they're properties of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule. Cross-phase exclusivity (σ XOR NR per layer) and single-σ-V (one V per operator per layer) are enforced cryptographically by EKM at signing time, not by aggregator-side filtering.

### Liveness (synchrony-conditional)

> ### ⚠️ In its current form, OBFT's liveness is conditional on two requirements per slot:
>
> **(a) [Assumption 2](#assumed) (within-budget partial-synchrony) holds for that slot.** Honest `KindCommit` messages emitted by `T_commit` (worst case; early-emit per §Phase 2 emission-timing is the typical case) reach a 2f+1 quorum of operators by `T_commit + Δ_2` (recommended `Δ_2 = 1 BTT`). This is the same partial-synchrony assumption QBFT operates under for its `PREPARE` / `COMMIT` phases.
>
> **(b) At each layer `L_k`, the leader's Phase-1 bundle must EITHER reach a 2f+1 quorum of operators by `T_commit` (so σ-quorum forms — output V at this layer), OR fail to reach by a wide-enough margin that the remaining ≥ qEnc operators can NR-quorum (chain falls through to `L_{k+1}`).** Partial propagation in between — `n − qEnc < receivers < qV`, where *receivers* counts operators with V (the leader counts trivially as a receiver of its own bundle); at f=1, n=4 this is exactly `r = 2` (= byz leader + 1 honest receiver, the `h_V=1` shape) — **deadlocks the layer with no in-protocol recovery**: σ-pool < qV, NR-pool < qEnc, and there is no flip / recovery mechanism to bridge the gap. Chain encryption stays sealed at `L_k` → deeper layers' partials remain undecryptable → no cluster-wide fall-through.
>
> **Caveat at f=1, n=4 with a dormant byzantine** (= byz silent / offline; the protocol counts dormant byz as silent, equivalent to a down honest): **the "fail to reach by a wide-enough margin" branch in (b) is unavailable**. Only 2 of 4 ops are honest non-leaders; even if both NR, NR-pool = 2 < qEnc = 3 because byz silence consumes the silent budget without contributing. **At this configuration, every layer succeeds only when its leader's Phase-1 bundle fully propagates to both honest non-leaders by `T_commit`. Any partial or zero propagation at any layer collapses the whole slot** — multi-layer K-fall-through doesn't help because NR-quorum can't form at the failing layer to unlock chain encryption.
>
> **Practical causes of (b) failing:**
> - Late leader broadcast (local fetch overran, slow disk, slow MEV-relay query)
> - Phase-1 mesh propagation tail past `B_k` (peer-score pruning, mesh churn, network spike), use sufficient [B_k <-> T_commit] buffer (eg. 2 BTT) to reduce the probability of these events
> - Clock skew beyond `δ` (operators read `T_commit` differently → effectively further propagation tail at peers on a different clock)
>
> Deployments with mesh-flakiness or honest leaders prone to processing tails should expect proportionally higher slot-miss rates from (b) failing.
>
> **Practical mitigations narrow (b) substantially:**
> - **Reflood-aware per-layer budgets** (`B_0 = 2·BTT + RefloodDelay`; backups at `B_k = T_commit` — see [§Setting](#setting)) absorb a full gossipsub IHAVE/IWANT cycle inside the per-layer budget, making partial propagation past `B_k` rare at L_0 and exceedingly rare at backups.
> - **Peer-reflood V via early Commit** (see §Phase 2 / Peer-reflood V via early commit) largely addresses the worst (b) sub-case — `h_V=1` selective Phase-1 delivery — at L_0: the in-time recipient's `KindCommit` carries V plaintext + σ_L^V witness, V-drop receivers σ on peer-V, σ-pool reaches qV.
>
> **Comparison to QBFT.** QBFT doesn't have an analogous per-layer (b) deadlock pattern; it round-changes when a round fails to converge and only misses the slot when cumulative round-change exceeds the slot budget. See [§Liveness comparison](#liveness-comparison-obft-vs-obftrr2-vs-qbft) (rows "Asymmetric propagation" and "Sustained partition") for the side-by-side.

OBFT's liveness is **partial-synchrony-conditional within the slot's relay-submission deadline** — the protocol's slot budget. Bundles arriving past `T_commit` at any honest receiver are not counted toward σ-quorum at that layer; the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time. View-divergence (equivocation, validity-divergence) is NOT recovered by the protocol; some splits succeed naturally as a side-effect of leader-σ-V counting toward whichever V reaches qV first, others slot-miss. Equivocation cases are slashable (leader pays the stake-based penalty); validity-divergence is not attributable.

If propagation between leader L_k's broadcast and any honest receiver's first-observation stays bounded by that layer's per-layer budget `B_k` (`B_0 = 2·BTT + RefloodDelay` for the primary, `B_{K-1} = T_commit` for the deepest backup; at K=2 Config A: `B_0 = 1100ms`, `B_1 = 3600ms`; see §Setting), the protocol terminates with a V signature on some layer (the first layer where σ-quorum reaches under cluster-wide V receipt by `T_commit`, or the deepest layer reachable via NR fall-through with a valid backup leader). If propagation exceeds `B_k` for layer L_k specifically, the cluster falls through to a deeper backup whose own `B_{k+1}` is wider. If all K layers fail to propagate in time (real propagation > `B_{K-1} = T_commit` at the deepest layer — i.e., no bundle reaches all honest before commit), the slot misses. **Safety holds in either case.**

**Best case (healthy at L_0)**: all honest receive V_{L_0} within `1 BTT`; all 3 honest σ-emit + leader's Phase-1 σ = 4 ≥ qV. Slot succeeds in 2 RTTs (Phase 1 + Phase 2).

**Asymmetric propagation past `T_commit`**: not recovered within-slot at the layer it affects. Honest who got V before T_commit σ-emit; honest who got V after T_commit treat the leader as silent and NR. If the σ-pool reaches qV (e.g., 2-of-3 honest + leader's σ_L^V), the slot still succeeds at this layer. If not, the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time at all honest. This is a deliberate trade-off: OBFT optimizes for spec simplicity at small clusters where asymmetric propagation is rare.

**Equivocation handling (no protocol-level recovery; outcome depends on byzantine timing)**: byzantine `L_0` equivocates, broadcasting distinct V's selectively. Case analysis at `f = 1, n = 4` (3 honest A, B, C; byz D = leader):

Recovered via σ-quorum at L_0 (when honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool):

- **D delivers V to {A, B}, V' to {C}.** σ-pool on V = A + B + D's σ_L^V(V) = 3 = qV. σ-quorum reaches. Slot outputs V.
- **D delivers V to {A}, V' to {B, C}.** σ-pool on V' = B + C + D's σ_L^V(V') = 3 = qV. Reaches. Slot outputs V'.

Recovered via NR-quorum at L_0 → fall-through to L_1 (when all honest retain ≥ 2 distinct V's by `T_commit` and emit NR per the equivocation-NR rule):

- **All-honest-NR outcome (byz delivers V's early enough that re-flood spreads conflicts before T_commit).** Each honest retains ≥ 2 distinct V's by `T_commit` → all 3 emit NR. σ-pools at L_0 ≤ byz partials per V < qV. NR-pool: 3 honest + 0 byz (σ-locked from Phase 1) = 3 = qEnc → NR-quorum reaches at L_0 → in the same Phase 3 reconstruction walk, advance to L_1; if L_1 honest, σ-quorum at L_1 reaches and slot succeeds at L_1.

Not recovered — slot misses at L_0 with no fall-through (σ-locked split patterns; slashable):

- **D delivers V to {A}, V' to {B}, both to C (or neither).** A σ-locks on V; B σ-locks on V'; C either retains both (NR per equivocation rule) or has nothing (NR per silent-leader rule). σ-pool on V = A + leader = 2 < qV. σ-pool on V' = B + leader = 2 < qV. NR-pool = 1 (C) < qEnc → no fall-through. Slot misses.
- **D delivers distinct V_1 to A, V_2 to B, V_3 to C (1-1-1 split).** Each honest σ-commits on their V_i on receipt before observing equivocation. All 3 honest σ-locked on different V's. **If byz triple-signs σ_L on all three V's** (worst case for slot-miss): σ-pool on each V_i = 1 honest + 1 byz σ_L^V = 2 < qV. NR-pool = 0 (all σ-locked, cross-phase exclusivity). Slot misses; equivocation evidence on all three (V_1, V_2), (V_2, V_3), (V_1, V_3) pairs is slashable. **If byz signs σ_L on only one V_i** (say V_1): σ-pool on V_1 = 2 (= recipient A + byz σ_L), σ-pool on V_2 = σ-pool on V_3 = 1 (just the recipient), all still < qV; same NR-pool = 0; same slot-miss outcome. Triple-signing maximizes the slashing surface but the slot-miss is invariant to byz σ_L choices once honest split 1-1-1.

**Byzantine timing controls which class fires — and an *adversarial* byzantine reliably picks the slot-miss class.** Delivering V's early in Phase 1 leaves time for gossipsub re-flood to spread conflicts → all-honest-NR outcome → slot succeeds at L_1. Delivering near end-of-Phase-1 leaves insufficient re-flood time → σ-locked split outcome → slot misses at L_0. **In expectation against an adversarial byz primary, these patterns slot-miss reliably.** The rational-byzantine deterrent (assumption 4) is what makes this tolerable across many slots — but the *evidence quality* for these patterns is the *behavioral* class (not the cryptographically-self-contained class), so single-observation slashing is not credible (see [§Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4)). Practical effect: byz can grief many slots before the pattern accumulates enough confidence for honest operators to act.

In all cases, Pigeonhole 2 ensures at most one V can reach qV cluster-wide regardless of how honest σ-emissions split — there is no two-output safety violation.

**Multi-failure fall-through (K ≥ 3)**: with `K > 2` layers, the cluster can fall through past multiple silent or non-validating layers **within Phase 3's single reconstruction walk** — the walk processes layers sequentially using local decryption (no per-layer RTT). E.g., at K=4 with L_0, L_1, L_2 all silent: NR-quorum reaches at L_0 in Phase 2 (silent-leader rule fires for all 3 honest cluster-wide); same at L_1 and L_2; σ-quorum at L_3 if L_3 leader honest. All happens in Phase 2 + Phase 3 windows. Recovery scope at K layers is achieved without round-change overhead — a structural difference from QBFT (which round-changes once per failed leader). At K = n = 4, every cluster member is a leader exactly once; pigeonhole guarantees ≥3 honest leaders at f=1, providing maximum K-fall-through depth within a single round.

**Adversarial scheduling within partial synchrony**: the network adversary can delay messages by up to `1 BTT`.

- *Safety unaffected.* Pigeonholes 1, 2, 3 are properties of the cluster-wide signed-message set, not of arrival times.
- *Liveness — adversary delays V to ≤ 1 honest past `T_commit`.* The other 2 honest σ-emit on time; σ-pool = 2 + leader = 3 = qV. **Quorum reaches without the delayed operator.**
- *Liveness — adversary delays V to ≥ 2 honest past `T_commit`.* σ-pool < qV at this layer. **Two sub-cases by NR-pool composition** (per the §Liveness fat warning (b)):
  - **NR-quorum reaches** (delayed honest treat the layer as silent and NR — typical when byz is silent or NR-emits, and the σ-recipient is the leader): chain unlocks at this layer; cluster falls through to a deeper backup whose bundle did propagate in time. ✓ if some deeper layer succeeds; ✗ slot-miss if all K layers exceed their `B_k`.
  - **NR-quorum fails** (the `h_V=1` shape: 1 honest receives V, 2 honest delayed; the recipient is σ-locked and can't NR; NR-pool = 2 < qEnc): under the post-§Phase 2 peer-reflood-V recovery, the σ-recipient's early commit redelivers V + σ_L^V to the V-drop receivers, who σ on peer-V — σ-pool reaches qV at L_0. ✓ Recovers in-protocol. (Pre-§Phase 2 baseline: chain stayed sealed at this layer with no fall-through; the deterrent (Assumption 4) was the only defense.) The post-§Phase 2 recovery requires the V-recipient's early commit to arrive at V-drop receivers before their T_commit fallback — comfortably the case under healthy mesh; under sender-side degraded mesh the receivers fall back to NR and the historical slot-miss path applies.

### Liveness comparison: OBFT vs OBFTR(R=2) vs QBFT

The table below puts OBFT, OBFTR(R=2), and QBFT side-by-side per scenario at the standard SSV proposer-duty configuration (n=4, f=1, **K=2 default**, ~4s relay cutoff). Timing assumes the SSV proposer-duty operating point (`BTT = 200ms` = P99=150ms + δ=50ms; primary-vs-backup `B_0 = 2·BTT + RefloodDelay = 1100ms`, `B_1 = T_commit = 3600ms` at K=2 — see [Timing budget](#timing-budget)). The Multi-failure fall-through row uses K=4 as an up-tier illustration of multi-silent-leader recovery (K=2 has only L_0 → L_1; multi-silent fall-through requires K ≥ 3). All counts at the unified tightened "1 BTT per emission" sizing (OBFT `Δ_2 = 1 BTT`; OBFTR per-round `Δ_2 = 1 BTT`; QBFT 4 BTT R1) — see [docs/BFT-comparison.md / Sizing convention](BFT-comparison.md#sizing-convention). For QBFT-SSV, `RT = 2s = 10 BTT`; QBFT-no-reflood uses per-round RT (R1 = 3·BTT, R≥2 = 4·BTT).

| Scenario | OBFT outcome | OBFTR(R=2) outcome | QBFT-SSV outcome |
|---|---|---|---|
| Healthy (all honest receive V_{L_0}) | σ-quorum reaches in 2 RTTs. ✓ at L_0 in ~400ms (2 BTT + ε_3). | Same recovery shape. ✓ at L_0 in ~1200ms (6 BTT). | PROPOSE→PREPARE→COMMIT + post-consensus (4 emissions × 2 BTT). ~1600ms (8 BTT). ✓ |
| Byzantine leader silent | 0 σ-emits → all honest NR (silent-leader rule) → NR-quorum at L_0 reaches in Phase 2 → automatic fall-through to L_1 in Phase 3 walk; σ-quorum at L_1 if leader honest. ✓ in ~400ms. | Same. ✓ in ~1200ms. | Round 1: no PROPOSE arrives; round timeout (~2s). Round 2: new leader proposes; succeeds in ~1.6s. ✓ in ~3.6s. |
| Asymmetric propagation (≤1 of 3 honest miss V at T_commit) | Other 2 honest σ-emit on time; σ-pool = 2 + leader = qV. ✓ at L_0 in ~400ms. The miss-honest's NR partial is unused. | Same (within OBFT's absorption). ✓ in ~1200ms. | Round 1: PREPARE-pool short; timeout (~2s). Round 2: re-fetch + propose; succeeds in ~1.6s. ✓ in ~3.6s. |
| Asymmetric propagation (≥2 of 3 honest miss V at T_commit, h_V=1 selective-Phase-1-delivery shape) | The 1 honest σ-recipient early-emits with V plaintext + σ_L^V witness; V-drop receivers harvest peer-V, σ on it → σ-pool = leader + 3 honest σ = 4 ≥ qV. **✓ at L_0 in ~600ms** (2 BTT + ε_3 + peer-V validation hop). Closes the h_V=1 deadlock in-protocol per §Phase 2 / Peer-reflood V via early commit. (Pre-§Phase 2 baseline was ✗ slot-miss with no fall-through — Class B byz grief vector deterred only via Assumption 4 across slots.) | Within OBFTR(R=2)'s wider absorption: round 2 re-flood may deliver V to the miss-honest; σ-quorum at L_0 reaches in round 2. ✓ in ~2.4s. | Round 1: timeout. Round 2: new leader; succeeds in ~1.6s. ✓ in ~3.6s. |
| Byzantine leader equivocates, 2-1 split where one V already has 2 honest + leader-σ_L^V | σ-pool on the 2-honest-V = 2 + leader = 3 = qV; reaches naturally. ✓ in ~400ms. | Same. ✓ in ~1200ms. | Round 1: PREPARE-pool split; timeout. Round 2: new leader proposes; succeeds. ✓ in ~3.6s. |
| Byzantine leader equivocates, σ-locked split pattern (1-1-1, etc.) | σ-pools split below qV; NR-pool below qEnc (σ-locked operators can't NR). **✗ slot misses at L_0;** no fall-through. Equivocation slashable. | Same exposure (R-invariant). ✗ slot misses. Equivocation slashable. | Round 1: PREPARE split; timeout. Round 2: new leader proposes a fresh V; honest converge; succeeds. ✓ in ~3.6s. **QBFT recovers what OBFT/OBFTR don't** via "new leader proposes fresh V" mode. |
| Byzantine leader equivocates, all-honest-NR outcome (byz delivers V's early; re-flood spreads conflicts before T_commit) | All 3 honest retained ≥ 2 V's by T_commit → all NR per equivocation rule → NR-quorum at L_0 → fall-through to L_1 in Phase 3 walk; if L_1 honest, ✓ at L_1 in ~400ms. Equivocation slashable. | Same recovery via Round-R force-NR. ✓ in ~1200ms (round 1) or ~2.4s (round 2). | Same recovery as σ-locked split row: round-2 new leader proposes fresh V. ✓ in ~3.6s. |
| Multi-failure fall-through (multiple silent leaders) | At K=4 with L_0, L_1, L_2 silent: NR-quorum reaches at each in Phase 2; Phase 3's walk decrypts down to L_3; σ-quorum at L_3 if honest. **All in single Phase 2 + Phase 3 windows** (Phase 3 ε_3 grows with K-layer decryption walks). ✓ in ~800ms (3 BTT consensus + ~200ms ε_3 × K) at tightened per-emission sizing. | Same. ✓ in ~800ms (3·BTT R1 + ε_3·K at tightened sizing). | Round 1: leader 1 silent → timeout (~2s). Round 2: leader 2 silent → timeout (~2s). Round 3: leader 3 silent → timeout (~2s). Round 4: succeeds in ~0.8s. ✓ in ~6.8s — past 4s cutoff. ✗ for proposer duty. **OBFT's K-layer parallel fall-through beats QBFT's serial round-change**. |
| Host-validity divergence (head-change mid-slot, strict host) | Out of scope (assumption 3 — host stabilizes verdict at Phase-1 acceptance). Same as OBFTR(R=2). | Same. | Round 1: validators with stale head don't PREPARE; timeout. Round 2: new leader fetches at moved head; if quorum agrees, succeeds. ✓ in ~3.6s. **QBFT recovers what OBFT-family doesn't** via fresh-V refetch on round-change. |
| Adversarial scheduling — adversary delays V at all K layers beyond their respective per-layer absorption | **Out of envelope** (Class A). ✗ Slot misses. The deepest layer's `B_{K-1} = T_commit = 3600ms` at K=2 SSV default operating point is the cluster-wide tolerance ceiling. | If delay ≤ OBFTR(R=2)'s cross-round retention (~1050ms at this operating point): in envelope at R=2; round 2 re-flood may resolve at L_0. ✓ in ~1.8s. Else: out of envelope. ✗ | Round 1: PREPARE delayed → timeout (~2s). Round 2: new leader; if adversary delays again → round 2 timeout. ✗ Within 4s, QBFT tolerates ≤ 1 round of adversarial delay. |
| Sustained partition (real propagation > all layers' absorption) | OBFT deepest-layer absorption `B_{K-1} = T_commit ≈ 3600ms` at K=2 SSV default operating point; exceeded → ✗ slot misses. Safety holds. | OBFTR(R=2) cross-round retention ~1050ms at this operating point (recovers at L_0 via re-flood, preserves MEV); exceeded → ✗ slot misses. Safety holds. | QBFT round-budget × RT exceeded; slot misses. ✗ Safety holds. |
| > f operators offline/byzantine | Standard 3f+1 violation; slot misses. ✗ Safety holds. | Same. ✗ | Same. ✗ |

**Summary of recovery-scope differences:**

- **OBFT and OBFTR(R=2) differ in *where* they recover, not just how much they tolerate.** OBFT's primary-vs-backup budgets (`B_0 = 2·BTT + RefloodDelay = 1100ms` at default RefloodDelay; `B_1..B_{K-1} = T_commit = 3600ms` at K=2 SSV default operating point) recover via K-layer fall-through — propagation up to the full slot budget is absorbed at any backup, but the slot succeeds at a backup (deepest-confirmed-parent V, no MEV freshness). OBFTR(R=2)'s ~1050ms cross-round retention (at tightened per-round Δ_2 = 1·BTT) recovers at L_0 specifically via round-2 re-flood (preserves MEV freshness) at the cost of an extra round of consensus.
- **OBFTR(R=2) > OBFT for slots where MEV preservation matters during partition tail**: round-2 re-flood at L_0 gets the freshest MEV V even when L_0's broadcast didn't propagate in round 1. OBFT loses one layer's worth of MEV per fall-through.
- **OBFT-family > QBFT in latency and multi-leader-failure**: OBFT's healthy path is ~400ms (vs ~1600ms QBFT-SSV at recommended sizing); K-layer parallel fall-through is in-round (vs QBFT's serial round-change at ~3.6s per round-change cycle, exceeding the 4s budget at K-1=3 silent leaders).
- **QBFT > OBFT-family in 1-1-1 equivocation and host-validity divergence**: QBFT's "round-change with fresh-V" handles these structurally; OBFT-family relies on assumption 3 and assumption 4.
- **All three fail equivalently** on sustained partition beyond their respective envelopes and on > f byzantine.

The choice between OBFT, OBFTR(R=2), and QBFT for SSV proposer duty depends on (a) MEV freshness sensitivity — OBFT's deeper-layer fall-through preserves liveness but uses backup-leader V's (less fresh MEV), OBFTR(R=2)'s round-2 re-flood preserves L_0's MEV at +1800ms latency cost (R1+R2 − R1 = 2.4s − 0.6s); (b) observed re-org rate; (c) the cluster's tolerance for 1-1-1 equivocation (handled by rational-byzantine deterrent in OBFT-family, recovered in QBFT). Detailed cost-side trade-offs (latency, bandwidth, cryptographic primitive maturity) are in [Appendix A.3](#a3--comparison-with-qbft). The QBFT-recovery wins above (1-1-1 σ-locked equivocation, host-validity-divergence) are partly an artifact of QBFT's larger 2-round consensus budget; pure all-honest network failures (jitter, transient outages, partition within absorption) recover identically across the three protocols at apples-to-apples budget.

### Equivocation handling

See "Phase 1 / Equivocation handling — detect and slash" for the operational rule. Summary: at `T_commit`, each operator decides per layer based on retained V's:

1. 0 V retained → NR (silent-leader rule).
2. Exactly 1 V retained, host validates → σ on that V.
3. Exactly 1 V retained, host returns not-valid → NV.
4. ≥ 2 distinct V's retained (equivocation observed pre-T_commit) → NR (the operator does not pick a winner; cross-phase exclusivity locks them out of σ on either V).
5. Gossip the equivocation evidence (the pair of equivocating Phase-1 bundles) for out-of-band slashing.

The leader is required to sign `σ_V` *exactly once per (slot, layer)*; refreshes in the host's fetch loop are pre-signing only (see "Application: SSV Ethereum proposer duty / Head-change handling") and don't surface multiple `σ_V` partials on the wire. Any second `σ_V` from the same leader is a protocol violation.

OBFT does not provide in-protocol equivocation recovery. Some equivocation patterns naturally reach qV on a single V (when honest happen to split such that 2-of-3 σ-emit on the same V; leader's σ_L^V on that V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. See "Liveness / Equivocation handling" for the full case analysis. Equivocation is treated as a slashable byzantine fault (Phase-1 bundles signed by leader's key are self-contained slashing evidence — see "Slashing evidence"); the rational-byzantine deterrent (assumption 4) is what makes the slot-miss tolerable across many slots.

**Cross-onion (operator-side) equivocation — counting and suppression.** If an operator `i` is observed with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different V, the dual partials are slashable evidence on the same logic as leader equivocation. For aggregation: by single-σ-V exclusivity (EKM-enforced — see "Slashing-protection scope"), an honest operator only ever emits σ on one V per layer, so any dual-V σ partials from the same operator are byzantine. The byzantine f-bound on per-V σ-pool contributions therefore holds without explicit suppression: byz contributes ≤ 1 partial per V regardless. Honest receivers MAY additionally elect to fully suppress `i`'s partials upon observing the equivocation evidence — this is not load-bearing for safety.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_k` is a slashable cross-signer. The pair is detected uniformly across phases:

- **σ from Phase 1 + NR/NV from Phase 2** — applies to a layer's leader specifically: a `σ_{L_k}^V(V_{L_k})` from the Phase-1 bundle paired with an `σ_{L_k}^{IBE}(nr_tag_k)` from the same operator at the same slot.
- **σ from Phase 2 onion + NR/NV from Phase 2** — any operator who included σ in their onion *and* broadcast a no-σ attestation.

Detection is straightforward — the dual partials are public. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it cryptographically — the EKM-enforced cross-phase exclusivity is what gives Pigeonhole 1 its load-bearing constraint, and a byzantine cross-signer simply consumes their f-bound contribution to one of σ-pool or NR-pool, not both effectively). The detection is purely for **attribution** and out-of-band punishment.

### Slashing evidence

Five rules surface byzantine fault evidence for *attribution and punishment*. Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to inform the surviving operators (and stakers) that a byzantine fault has occurred so the cluster can act on it — see [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the offline-equivalence model. The protocol-functional message flow (Phase-1 bundles, `KindCommit` partials) already carries the underlying signed messages on the wire; **honest operators MUST log observed evidence** per the rules below for later out-of-band aggregation. Log format and retention are implementation-defined; the manual-blacklist mechanism (planned protocol extension) is the canonical consumer. The surviving operators verify aggregated logs and decide whether to (a) blacklist the byzantine via cluster-wide config update (restoring `Byzantine ≡ Down`; planned protocol extension), (b) file a stake-slashing transaction via the SSV contract where the evidence is cryptographically self-contained, and/or (c) propagate the signal to stakers and to future cluster formation.

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
| 5. Fake plaintext σ at the plaintext layer (L_0; L_Bid in the L_Bid extension) | Immediate (partial vs retained V check) | Conditional on receiver retaining (or auth-only-retaining) V — covered by Phase-1 bundle re-flood under partial synchrony | Very low |

**Rule 4 has a structural detection limit.** The chained-encryption construction makes Rule 4 evidence cryptographically inaccessible when prior layers' NR-quorum doesn't reach: decryption requires `qEnc` partials on `nr_tag_{0..k-1}`, those partials don't exist if honest σ'd at those layers, and honest cannot preemptively sign IBE partials on layers they intend to σ-side-commit to (that violates cross-phase exclusivity = cross-signing). This is a **fundamental limit of the IBE primitive as used here**, not a fixable spec gap.

**The seal applies in BOTH slot-success and slot-miss outcomes**, not just slot-misses:

- **Slot succeeds at L_0** (σ-quorum reaches at L_0): per Pigeonhole 1, NR-quorum at L_0 does NOT reach (σ and NR mutually exclusive at the same layer). Hence the chained encryption at L_1, L_2, ... stays sealed, and any fake encrypted-presence at deeper layers in this slot is invisible. **This is the common case for healthy slots** — a byzantine that fakes encrypted-presence at L_2 in every slot pays no per-slot cost on healthy slots where the cluster succeeds at L_0. The fake-presence is essentially "rehearsing the attack with no consequences" until a slot-miss-at-L_0 path happens to unlock the relevant encryption.
- **Slot misses at L_0** (no quorum reaches): σ-locked split equivocation, validity-divergence deadlock, and h_V=1 selective Phase-1 delivery *under degraded mesh* (h_V=1 under healthy mesh is now closed in-protocol via the §Phase 2 peer-reflood-V mechanism; the degraded-mesh case is the residual where the in-time recipient's early commit doesn't reach V-drop receivers before their T_commit fallback) — each leaves the chained encryption sealed. Compounded with byz fake-presence at deeper layers, byz gets two grief actions per detection. Deeper-layer Rule-4 evidence stays sealed in these slot-miss patterns.

**Practical implication for deployments.** Rule 4 functions as a *probabilistic* deterrent rather than an unconditional one: a byzantine that fakes encrypted-presence at L_k>0 expects detection only with the probability that NR-quorum reaches at all prior layers in subsequent slots where the deterrent's coordination process can still act. Deployments relying on assumption 4 (rational-byzantine deterrent) for L_k>0 fake-presence should weight the deterrent's effective strength accordingly — Rule 4 is *best-effort, not guaranteed surface-able*. Rule 5 (Fake plaintext σ at the cluster's plaintext layer — L_0 in bare OBFT, L_Bid in the L_Bid extension) does not have this limitation since the plaintext layer's σ is unencrypted.

The five classes are all *cryptographically self-contained* (high-confidence, low false-positive risk against honest operators) once surfaced. The asymmetry above — Rule 4's slot-progress-conditional surface-ability — is a real limitation that adversarial byzantine can exploit by engineering slot-miss precisely to seal Rule-4 evidence. Behavioral-pattern grief (selective-delivery, σ-refusal coordinated with honest flakiness) leaves no on-wire cryptographic evidence at all and is correspondingly harder for humans to act on with confidence — see [Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4).

### Failure modes

The slot misses (no V signature is produced) under any of the following. The cases split into two classes by relationship to OBFT's operating assumptions:

- **Class A — assumption violations** (the listed condition violates one of OBFT's assumptions; the protocol does not promise liveness when an assumption is violated). These are out-of-scope for OBFT's recovery guarantees by construction.
- **Class B — permitted byzantine grief within the f-bound** (occurs *under* valid assumptions; one byzantine operator within the f-byzantine bound deliberately misbehaves to cause slot-miss). These are *permitted because they are eventually bounded* — every Class B grief leaves evidence on the wire (cryptographically self-contained for some classes, behavioral-pattern for others), and the rational-byzantine deterrent (assumption 4) bounds the byzantine's grief across slots via the eventual `Byzantine ≡ Down` collapse (manual blacklist by the surviving `n − f` operators; planned protocol extension) plus staker migration that collapses cluster-wide fee accrual. The boundedness is what makes Class B "permitted" rather than "fatal" — an attacker that griefs reliably ends up in the same fee position as if they had gone permanently offline (and worse, for cryptographically-self-contained faults: stake-slashable via the SSV contract).

The slot misses under any of:

- **[Class A]** **Asymmetric propagation past `T_commit` (real propagation from L_k's broadcast to first-observation > `B_k` at layer L_k)** — violates assumption 2 (partial synchrony) for that layer. Honest who first-observe V past `T_commit` treat the leader as silent and NR. If the resulting σ-pool falls below qV at this layer, the cluster falls through to a backup layer whose own `B_k = T_commit` provides maximally-wide absorption (per-layer budgets: `B_0 = 2·BTT + RefloodDelay` for the primary; `B_1..B_{K-1} = T_commit` for all backups at K=2 Config A (`B_1 = T_commit`; K=4 up-tier extends to `B_1..B_3 = T_commit`); see §Setting). If propagation also exceeds the backup-layer absorption ceiling (= the full slot's commit budget), slot misses cleanly. **No safety violation.**
- **[Class A]** **More than `f` faults** — violates assumption 1 (BFT trust bound). More than `f` operators offline/byzantine combined. Standard `3f+1` trust bound. Slot misses regardless of protocol structure.
- **[Class A]** **Assumption 3 violated (host validity diverges mid-slot)** — violates assumption 3 (host validity unanimous at decision time). See [Implications of validity-divergence not being recovered](#implications-of-validity-divergence-not-being-recovered-assumption-3). The host's stabilization workflow (validate-once-and-lock at Phase-1 acceptance) is the design's path to keeping this from occurring; if it does occur, the slot misses cleanly. Not slashable (re-orgs are real-world events, not protocol violations); rational-byzantine deterrent does not apply.
- **[Class B — cryptographically slashable]** **Equivocation σ-locked split patterns** (1-1-1 split, 1-1-NR at f=1 n=4 — patterns where some honest σ-commit on different V's before observing equivocation, leaving neither σ-quorum nor NR-quorum reachable at L_0 and no fall-through) — occurs under valid assumptions; one byzantine within f-bound equivocates to engineer the split. See [Implications of equivocation not being recovered](#implications-of-equivocation-not-being-recovered). **Equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained cryptographically slashable**; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots. The all-honest-NR case (every honest retains ≥ 2 V's by T_commit and emits NR per the equivocation-NR rule) actually recovers via L_1 fall-through and is not a failure mode at f=1 n=4 with L_1 honest.
- **[Mostly Class A]** **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates in non-recoverable patterns at every layer). At K ≥ f+1, byzantines alone (within f-bound) cannot cause this — pigeonhole guarantees ≥ 1 honest leader. So this typically requires either >f faults (Class A: violates assumption 1) or coincident non-byzantine independent failures (relay timeouts, host issues, etc. — also Class A in the sense that the cluster's operational assumptions about host availability are violated). Rare in practice. The K parameter controls the fall-through depth; **at K = n = 4 (recommended OBFT default for proposer duty), pigeonhole guarantees ≥ 3 honest leaders, providing maximum fall-through redundancy**.
- **[Class A]** **Late deepest-layer leader broadcast.** A deepest-layer leader L_{K-1} whose Phase-1 bundle's first cluster-observation arrives after `T_commit` — e.g., the leader's fetch loop overruns substantially due to slow beacon node, MEV-relay timeout, or head-change refresh — is not counted by any honest receiver. All 3 honest at L_{K-1} treat as silent-leader-NR, NR-quorum at L_{K-1} reaches → walk advances past L_{K-1}, but no L_K layer exists. **Slot misses.**

  **Why Class A?** None of the six explicit assumptions is violated: BFT trust bound holds, partial synchrony bounds *gossipsub* propagation but not the leader's *application-internal fetch latency*, validity is orthogonal, EKM and DKGs are orthogonal. The failure relies on an *implicit* operational assumption — that honest leaders broadcast by their per-layer deadline `T_broadcast_max_k = max(BFT_start, T_commit − B_k)` (with `B_k` sized so propagation completes before `T_commit`). When this implicit assumption fails (legitimate operational delay overruns even the deepest layer's wide budget `B_{K-1}`, no byzantine action), the protocol cannot fall through past the deepest-layer. Note that in the staggered model the deepest layer has the *largest* propagation budget (`B_{K-1} = T_commit` at the recommended default; the leader is expected to broadcast at BFT_start, giving the entire `T_commit` budget for propagation), so this failure requires the entire slot's propagation budget to be exceeded — i.e., the bundle doesn't reach all honest before commit.

  **Mitigation paths:**
  - **Use K ≥ f+2** (a deployment choice — see §Setting). Adds at least 2 honest leaders so a single late-broadcasting honest leader doesn't foreclose the slot — fall-through to a deeper honest leader recovers. At f=1 n=4 this means K ≥ 3 (with K = n = 4 providing maximum fall-through depth at minimal extra bandwidth ~3KB per onion). At f=2 n=7, K ≥ 4. **No protocol change**, just deployment configuration; this is the cleanest fix that actually recovers the slot. Deployments that select `K = f+1` accept exposure to this Class A failure mode as part of the deployment trade-off.
  - **Host-side hard deadline** (defense-in-depth; minor host-side discipline, no protocol change). Leader `L_k`'s fetch loop should abort and broadcast no bundle if it hasn't terminated by `T_broadcast_max_k` when that target is positive; for the deepest layer (where `B_{K-1} = T_commit` makes `T_broadcast_max_{K-1} = 0` by default) the host falls back to `T_commit` as the abort point (the protocol-wide acceptance gate, beyond which bundles are rejected anyway). Converts "late broadcast missed cutoff" into "silent leader" (handled cleanly via NR-quorum → fall-through). At K = f+1 this normalizes the late-broadcast path into the silent-leader path but doesn't recover the slot (silent + L_0-fail still falls through to non-existent layer). At K ≥ f+2 it complements the K choice by ensuring late broadcasts always fall into the silent-leader path.
- **[Class A]** **Validity-divergence deadlock (network-induced; no byzantine action required in the cleanest case).** A beacon-chain re-org landing inside the bundle-acceptance window can split honest operators between σ-eligibility (parent_root matched their pre-reorg head snapshot) and NV (parent_root didn't match their post-reorg head). The host's per-operator validate-once-and-lock workflow (§Head-change handling) preserves each operator's verdict but cannot reconcile cluster-wide divergence. At the boundary split (e.g., 2-σ vs 2-NV at f=1 n=4 with all 4 honest, where leader's Phase-1 σ_V counts as σ-side via cross-phase exclusivity): σ-pool = 2 < qV = 3; NR-pool = 2 < qEnc = 3. **No safety violation** — just no quorum on either side; slot misses cleanly. The host's stabilization workflow narrows the divergence window to ≈ `1 BTT` (the time between earliest possible bundle first-observation and `T_commit`), but doesn't eliminate it.

  **The deadlock zone widens when a byzantine within f-bound exercises f-budget passively** (silent or σ-on-V — neither is independently slashable; passive silence is the natural f-budget consumption pattern for an absent operator). At f=1 n=4 with leader honest, additional deadlock configurations include:

  - "1 non-leader σ + 1 non-leader NV + byz silent" (σ-pool = leader + 1 = 2 < qV; NR-pool = 1 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz silent" (σ-pool = leader = 1 < qV; NR-pool = 2 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz σ-on-V" (σ-pool = leader + byz = 2 < qV; NR-pool = 2 < qEnc).
  - Symmetric configs when leader's host-verdict is itself NV (Phase-1 σ_V locks leader σ-side regardless of leader's host's later opinion).

  The all-honest example captures the cleanest case; in real deployments with byz exercising f-budget, the practical slot-miss rate from validity-divergence is meaningfully higher than the all-honest example alone suggests. **The expected rate scales with `re-org rate × byz-passivity-rate`**, not re-org rate alone — i.e., a deployment with re-orgs in 1% of slots and a byzantine present and adopting passive grief in some fraction of slots compounds these probabilities into validity-divergence slot-misses. The host's stabilization workflow narrows the divergence window but does not eliminate it; byzantine passive f-budget consumption (silence or σ-on-V — neither cryptographically slashable individually) is essentially "free" within the f-bound, so byz can reliably contribute the passivity factor whenever exercising the deterrent's weak-attribution corner is favorable. See [Implications of validity-divergence not being recovered (assumption 3)](#implications-of-validity-divergence-not-being-recovered-assumption-3) for the full discussion.
- **[Class B — closed under healthy mesh via peer-reflood-V]** **Byzantine selective Phase-1 delivery (h_V=1).** A byzantine leader that unicasts Phase-1 to exactly one honest creates a pre-§Phase 2 algebraic deadlock at f=1, n=4: σ-pool = 2 (recipient + byz σ_L^V) and NR-pool = 2 (the two no-V honest; recipient is σ-locked, can't NR), with neither reaching qV/qEnc=3. **Bare OBFT closes this in-protocol under healthy mesh via the §Phase 2 / Peer-reflood V via early commit mechanism**: the σ-recipient's early KindCommit carries V plaintext + σ_L^V witness; V-drop receivers harvest V, σ on it; σ-pool reaches qV + leader σ_L^V = 4 ≥ 3 at L_0. Slot succeeds at L_0. (Pre-§Phase 2 baseline: slot missed at L_0 with no fall-through; rational-byzantine deterrent — Assumption 4 — was the only defense, with behavioral-pattern evidence quality.) Same in-protocol recovery applies when the cause is an honest leader's bundle hitting a propagation tail past `B_0` for a single receiver (the in-time recipient's early commit redelivers V to the tail-affected receivers). The recovery requires the in-time recipient's early commit to arrive at V-drop receivers before their T_commit fallback — comfortably the case under healthy mesh; under degraded mesh the receivers fall back to NR and the historical slot-miss outcome applies (Assumption 4 deterrent remains the residual defense).

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
| Termination (output guaranteed) | Conditional. **One-liner: consensus expected to complete by `slot_start + 3.85s` at SSV proposer-duty operating point (n = 4, f = 1, BTT = 200ms, K = 2, T_commit = 3.60s, recommended Δ_2 = 1 BTT = 200ms, ε_3 ≈ 50ms), with submission slack to `slot_start + 4.00s` for relay submit (`header_submit_headroom = 100ms` plus ~50ms additional jitter buffer); under conditions: (a) ≤ f operators byzantine/offline, (b) real propagation from leader L_k broadcast to any honest first-observation ≤ that layer's per-layer budget `B_k` (primary-vs-backup: `B_0 = 2·BTT + RefloodDelay`, `B_1..B_{K-1} = T_commit` — see §Setting), (c) host validity unanimous at decision time (assumption 3). At the K=2 default (= K=f+1) the cluster has no late-leader-resilience backstop (a late-broadcasting honest L_1 forecloses the slot); deployments preferring late-leader resilience may opt for `K ≥ f+2 = 3` (up-tier).** Single-round protocol; backup budgets give every backup layer the entire slot's commit budget for absorption (`B_k = T_commit` for k > 0, backups broadcast at BFT_start with deepest-confirmed-parent fetch); the primary L_0 has a tighter budget for MEV-fetch headroom. |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates form self-contained slashable evidence |
| Byzantine-leader-grief resistance | **Partial under non-adversarial byzantine; weaker against adversarial byz that deliberately engineers grief patterns.** Recovery via "natural" σ-quorum patterns (leader's σ_L^V completes a 2-of-3-honest pool at n=4) and via peer-reflood V (see §Phase 2 / Peer-reflood V via early commit) closes the historical `h_V=1` selective Phase-1 delivery grief vector in-protocol under healthy mesh (degraded-mesh residual remains; deterred via Assumption 4). **Remaining adversarial-byz grief** is σ-locked split equivocation at L_0 (1-1-1, 1-1-NR, etc.) — still slot-misses without fall-through. At f=1 n=4 with uniform leader rotation, an adversarial byz primary can deterministically grief slots via σ-locked equivocation (typically a subset of the ~25% slots where they're L_0; lower than the pre-§Phase 2 baseline because h_V=1 no longer counts). The rational-byzantine deterrent (assumption 4) is the only protocol-level defense for the residual σ-locked-equivocation patterns. Effective deterrent strength is deployment-specific (stake-to-grief-value ratio, governance responsiveness, slashability evidence quality — see [§Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4)). |
| Mesh-flakiness tolerance (honest operator with poor gossipsub mesh visibility) | **Limited.** A mesh-flaky honest operator who fails to observe peer σ-emits within the NR-decision window can NR-emit incorrectly, becoming a byzantine-equivalent f-budget consumer for that slot. Combined with byz σ-refusal, this creates a deadlock that the protocol cannot recover from within the slot. Per-layer `B_k` (reflood-aware: `(k+2)·BTT + RefloodDelay` for shallow layers) absorbs mesh-jitter at the Phase-1 boundary — the recommended `Δ_2 = 1 BTT` only covers one synchronous-fallback propagation cycle. Outliers wider than `B_k` for the layer that actually carries the slot's V slot-miss cleanly. QBFT's round-reset semantics handle this case better (a flaky operator's bad PREPARE doesn't lock them across rounds); OBFT enforces cross-phase exclusivity per slot. |
| Validity-divergence under strict host | **Out of scope** — see [Assumptions](#implications-of-validity-divergence-not-being-recovered-assumption-3); host stabilizes the verdict at Phase-1 acceptance |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (K-layer fall-through within Phase 3's reconstruction walk; K configurable, **`K = f+1` (BFT-min) recommended default** for SSV proposer duty at n=4 — see [§Application](#application-ssv-ethereum-proposer-duty). K up to `n` available as up-tier for deeper fall-through). |
| Round-change recovery | **No** — single-round design. Late re-flood within Phase 2's receiver acceptance window is the only within-slot partition-recovery mechanism. |
| Partial-synchrony absorption | Primary-vs-backup: `B_0` for the MEV-fresh primary L_0; `B_k = T_commit` for all backups L_1..L_{K-1}. At K=2 Config A with default RefloodDelay=700ms: `B_0 = 2·BTT + RefloodDelay = 1100ms`; `B_1 = T_commit = 3600ms` (backup broadcasts at BFT_start, deepest-confirmed-parent fetch). At RefloodDelay=0 (fully-meshed opt-out): `B_0 = 400ms`. K=4 up-tier extends backup absorption to L_2, L_3 with the same `B_k = T_commit` shape. Cluster falls through layers based on which one's bundle actually arrived by `T_commit`. |
| Recovery scope vs QBFT | Multi-leader fall-through is in-round (vs QBFT's serial round-change), so OBFT wins on K-leader-failure cases and healthy-path latency. View-divergence (validity-divergence and equivocation patterns that don't naturally reach qV) is out of scope — handled by assumptions 3 and 4. |

## Application: SSV Ethereum proposer duty

For SSV's proposer duty, the recommended OBFT configuration is **`K = f+1` (BFT-liveness minimum)** — at n=4 (f=1) this resolves to **`K = 2`**: one fall-through layer `L_0 → L_1`, motivated by production testing showing K > f+1 doesn't materially improve outcomes once peer-reflood-V closes the healthy-mesh `h_V=1` case at L_0 (see [§Phase 2 / Peer-reflood V via early commit](#phase-2--onion-broadcast-t_emit-t_emit--%CE%94_2-with-t_emit--t_commit)). Concretely at K=2: **`V_0`** is the slot's designated MEV proposer (fetches the freshest relay-bundle); **`V_1`** is the backup leader fetching a deepest-confirmed-parent vanilla beacon-node payload (lower re-org exposure; see §Head-change handling). K-up-tier (`K = f+2..n`; at n=4 up to K=4 where every cluster member leads exactly one layer with `f+1 = 3` honest leaders guaranteed by pigeonhole) is available for deployments preferring deeper fall-through at the cost of larger onion bandwidth and longer Phase-3 IBE walks. The K choice is per-cluster and deployment-dependent.

Cryptographic safety (`qEnc = qV` + chained encryption + EKM-enforced cross-phase / single-σ-V exclusivity) ensures only one block can ever get a valid validator signature, regardless of K. The single-round design simplifies the EKM coordinator (no cross-round atomicity).

**Blinded-block convention.** OBFT proposer duty conventionally operates on **blinded** beacon blocks (= `BeaconBlock` with `ExecutionPayloadHeader` instead of full execution payload), per the relay-MEV-Boost convention used by the existing SSV proposer flow. Operators sign blinded V; relay-revealed unblinding of the full execution payload happens **after** threshold reconstruction, outside the consensus protocol. Bare OBFT's wire format does not impose a blinded-only constraint at the protocol level (per-op `KindCommit` ≈ 1.5–2 KB at K=2 n=4 default — chained-encryption onion ~1 KB + NR partials + σ_L^V witness section ~290 B + auth envelope; cluster-wide ≈ 6–8 KB across 4 operators. K=4 up-tier: per-op ≈ 7 KB, cluster-wide ≈ 28 KB; the increase is dominated by the deeper chained-encryption onion). Non-proposer SSV duties (attestations, sync committee) operate on small `V` (~100 bytes) where the blinding question is moot.

### Proposer-duty terminology

| Term | Meaning |
|---|---|
| `V_0` | MEV-optimized block fetched late from the relay |
| `V_1` (at K=2 default; `V_1, V_2, ..., V_{K-1}` at K up-tier) | safe earlier-fetched blocks from vanilla beacon-node payloads, refreshed on head changes within each leader's pre-signing fetch loop |
| `BTT` | broadcast trip time = P99 + δ; one one-way gossipsub propagation cycle. **`BTT = 200ms`** (P99 ≈ 150ms + δ ≈ 50ms) at the operating point below |
| Slot start | t = 0 (anchored to consensus-layer slot start) |
| `RANDAO` | RANDAO-reveal completion; cluster-wide ≈ slot_start + 150ms — earliest possible Phase-1 fetch start |
| `T_broadcast_max_k` | per-layer leader broadcast target: `max(BFT_start, T_commit − B_k)`. Deeper layers broadcast earlier (wider `B_k`) to absorb wider propagation tails. Target, not hard runtime cap — see §Setting |
| `T_commit` | view-fix deadline: `Relay_cutoff − 2 BTT`. Receivers stop counting Phase-1 bundles past this point |
| `header_submit_headroom` | budget for cert broadcast + relay submit after Phase 3 completes; **100ms** |
| `Relay_cutoff` | slot_start + **4000ms** — slot's hard relay-submission deadline |

### Timing budget

**Operating point.** `BTT = 200ms`, `header_submit_headroom = 100ms`, `Δ_2 = 1 BTT = 200ms` recommended (KindCommit synchronous-fallback propagation cycle; reflood lives in `B_k` via `RefloodDelay`), `ε_3 ≈ 50ms` local CPU.

**Derived anchors.** `T_commit = Relay_cutoff − 2 BTT = 3600ms`. (The 2 BTT post-`T_commit` budget = `Δ_2 + ε_3 + header_submit_headroom + ~50ms jitter buffer` = 400ms; the `T_commit` anchor is BTT-rounded for design simplicity, leaving ~50ms unallocated as additional submit slack.)

The 400ms after `T_commit` decomposes as: `Δ_2` (1 BTT = 200ms; **scales with BTT** — propagation budget for the synchronous-fallback KindCommit fan-out) + `ε_3` (≈ 50ms; **absolute** — local CPU work, doesn't scale with BTT) + `header_submit_headroom` (100ms; **absolute** — cert broadcast + relay HTTP submit, doesn't scale with BTT) + ~50ms residual jitter buffer. At higher BTT the post-`T_commit` budget is dominated by `Δ_2` and the absolute components matter less in BTT terms. Sizing recommendation: keep `ε_3` and `header_submit_headroom` as absolute quantities and let `Δ_2` scale.

**Default RefloodDelay** = 700ms (matches SSV's gossipsub HeartbeatInterval per [network/topics/params/gossipsub.go](../network/topics/params/gossipsub.go)). Schedule rows below assume this default; lower RefloodDelay for fully-meshed clusters recovers MEV-fetch headroom (table values shift later by `default − chosen = ΔRefloodDelay`).

| t (ms) | Event | Targets / notes |
|---|---|---|
| 0 | Slot start; backup `V_1` broadcasts at `BFT_start` (`T_broadcast_max_1 = 0`) at K=2 default — backup leader fetches deepest-confirmed parent and broadcasts as soon as the bundle is ready; bounded by RANDAO_done in practice. (K=4 up-tier: all backups `V_1, V_2, V_3` broadcast at BFT_start.) | Backups target propagation tails up to **~T_commit** (entire commit budget); MEV-fetch budget ≈ 0 |
| 150 | `RANDAO` done | Earliest practical Phase-1 fetch / broadcast |
| 2500 | `V_0` broadcast (`T_commit − (2·BTT + RefloodDelay) = 3600 − 1100`) | Targets up to **1100ms** (2·BTT + RefloodDelay); MEV-fetch budget **~2350ms** (freshest relay-bundle) |
| 3600 | `T_commit` (= `Relay_cutoff − 2 BTT`) | View-fix deadline; receivers stop counting Phase-1 bundles. `KindCommit` worst-case emit (fallback when `T_L_0_observed > T_commit`, i.e., silent `L_0`); typical-healthy emit fires earlier at `T_L_0_observed ≈ 3600ms − 1·BTT` per §Phase 2 emission-timing |
| 3800 | `T_commit + Δ_2` | SOFT cluster-wide worst-case target; σ/NR pools stable; Phase 3 begins. Healthy case stabilizes earlier (KindCommit emitted at `T_L_0_observed`, σ-quorum often forms before this deadline) |
| 3850 | Phase 3 complete | Local IBE-walk + BLS aggregation + certificate; `ε_3 ≈ 50ms`. Worst-case anchor; healthy case completes earlier. **`ε_3 ≈ 50ms` is the L_0-success (single-layer-walk) value**; under multi-layer fall-through the IBE-decryption walk runs sequentially through each NR-quorum-unlocked layer and end-to-end Phase 3 cost grows roughly linearly with the number of fall-throughs (e.g., `~ε_3 × K` ≈ 100ms at K=2 default with K−1=1 silent layer (K=4 up-tier with 3 silent layers: ≈ 200ms) — see §Phase 3 / ε_3 sizing for the formula and the multi-failure fall-through row in [§Liveness comparison](#liveness-comparison-obft-vs-obftrr2-vs-qbft) for the catalog impact). The wider K-fall-through Phase 3 spills into submission slack rather than expanding this row's deadline. |
| 4000 | `Relay_cutoff` | Cert broadcast + relay submit fit in `header_submit_headroom = 100ms`; ~50ms residual jitter buffer |

**Recovery scope.** Within Phase 3's single reconstruction walk: at K=2 default, silent V_0 → fall through to V_1 (the only backup). K=4 up-tier extends fall-through: silent V_0 + V_1 → V_2; silent V_0 + V_1 + V_2 → V_3 (at K=4 every cluster member is a leader; pigeonhole guarantees ≥ 3 honest leaders at f=1). For `h_V=1` selective-Phase-1-delivery at L_0, **the slot recovers at L_0 via the §Phase 2 peer-reflood-V mechanism** under healthy mesh (V-drop receivers σ on V harvested from the in-time recipient's KindCommit + σ_L^V witness; pushes σ-pool past qV at L_0). Under degraded mesh where the in-time recipient's early commit doesn't reach V-drop receivers before their T_commit fallback, the pre-§Phase 2 behavior applies (slot misses at L_0 with no fall-through; the rational-byzantine deterrent — Assumption 4 — bounds it across slots). Per-layer budgets `B_k`: V_0 covers real propagation up to 1100ms (2·BTT + RefloodDelay); backups (V_1 at K=2; V_1, V_2, V_3 at K=4) all cover up to 3600ms (the entire commit budget — backups broadcast at BFT_start). Beyond the deepest backup's budget — i.e., real propagation > T_commit — is out-of-envelope and slot-misses cleanly.

**MEV-fetch-budget asymmetry.** V_0's ~2350ms (at default RefloodDelay=700ms) is the only MEV-fresh budget; backups all fetch deepest-confirmed-parent vanilla payloads at slot start. Per-leader budgets at this operating point (K=2 default): `[V_0: ~2350ms (MEV-fresh), V_1: ~0ms (deepest-confirmed parent)]`. K=4 up-tier: `[V_0: ~2350ms (MEV-fresh), V_1/V_2/V_3: ~0ms (deepest-confirmed parents)]`. At RefloodDelay=0 (fully-meshed cluster opt-out) V_0's budget widens to `~3050ms`; backups unchanged. The primary-vs-backup design lets V_0 capture maximum MEV under healthy propagation while all backups absorb the entire-slot tail when V_0's bundle doesn't reach in time.

### Comparison vs QBFT (RT = 2000ms, 2-round target)

QBFT-SSV under SSV's production round-timeout (`RT = 2s = 10 BTT`) at the same operating point (`BTT = 200ms`, `Relay_cutoff = 4000ms`, `header_submit_headroom = 100ms`), at unified tightened sizing (1 BTT per emission — see [docs/BFT-comparison.md / Sizing convention](BFT-comparison.md#sizing-convention)):

| t (ms) | Event | Notes |
|---|---|---|
| 0 | Slot start | |
| 150 | `RANDAO` done | |
| 1100 | `PROPOSE_1` | Round-1 leader's MEV-fetch budget = **950ms** (RANDAO + fetch must fit before PROPOSE_1; R2 fit constraint loosens substantially vs older 2·BTT/emission framing) |
| 1700 | Round-1 success target | `BFT_start_1 + 3 BTT` (3 phases × 1 BTT consensus) |
| 1900 | Round-1 + post-consensus done | `+1 BTT` for partial-sig aggregation |
| 3100 | `RT_1` fires | Round 1 timed out; round-change |
| 3100 | `PROPOSE_2` | Round-2 leader's MEV-fetch budget = **2950ms** (re-fetch can run during R1 timeout window) |
| 3700 | Round-2 consensus done | `BFT_start_2 + 3 BTT` |
| 3900 | Post-consensus done | `+1 BTT` for σ aggregation |
| 4000 | `Relay_cutoff` | Cert + submit fit in 100ms |

**MEV-freshness ranking** at this operating point (incl. partial-sigs-on-pre-agreed-V baseline) at tightened sizing throughout. Default RefloodDelay=700ms is the production schedule; the RefloodDelay=0 column gives the fully-meshed-cluster opt-out for comparison.

| Rank | Leader | MEV-fetch (default RefloodDelay=700ms) | MEV-fetch (RefloodDelay=0 opt-out) | Notes |
|---|---|---|---|---|
| 1 † | Partial-sigs on pre-agreed V | **3550ms** | 3550ms | Floor; only available if V is pre-agreed (no MEV / no V-disagreement) — not directly applicable to SSV proposer duty |
| 2 | QBFT R2 leader | 2950ms | 2950ms | (RT=2s; only after paying the R1-timeout gap; widens substantially vs older 2·BTT/emission framing) |
| 3 | OBFT V_0 | **~2350ms** | **3050ms** | The MEV-fresh primary; the only OBFT layer that competes on MEV-fetch |
| 4 | QBFT-no-reflood R1 leader | 2150ms | 2150ms | (per-round timer; recovers more R1 fetch than QBFT-SSV) |
| 5 | QBFT-SSV R1 leader | 950ms | 950ms | RT=2s still eats slot budget but R1 fetch grows ~6× vs older framing |
| 6 (last) | OBFT V_1 (K=2 default; V_1, V_2, V_3 at K=4 up-tier) | ~0ms | ~0ms | Backups — broadcast at BFT_start with deepest-confirmed-parent fetch; no MEV-fetch budget by design (safety nets, not MEV-fresh alternatives) |

† **Partial-sigs assumes V is pre-agreed across operators** — works for non-MEV duties (attestations, sync committee) where V is determined by beacon-spec computation, but not for proposer duty where V varies per operator. Listed as the no-consensus floor: BFT consensus protocols pay 500-2600ms over this baseline to resolve V-disagreement.

**Comparison vs partial-sigs floor**: OBFT V_0 pays a **~1200ms BFT-consensus tax** at default RefloodDelay (3550 − ~2350ms = ~6 BTT), or a **~500ms tax** at RefloodDelay=0 (3550 − 3050 = 2.5 BTT). The tax decomposes as: **`B_0` + Δ_2 − partial-sigs post-fetch overhead** = `(2·BTT + RefloodDelay) + 1·BTT − 1.5·BTT` (per-leader broadcast slack + the 1·BTT slot tail OBFT reserves for KindCommit fan-out, less the 1.5·BTT the partial-sigs floor reserves for emit + submit). At default RefloodDelay this evaluates to 1100 + 200 − 300 = 1000ms ≈ tax bound; the additional ~200ms reflects RANDAO timing. RefloodDelay accounts for one full IHAVE/IWANT cycle for mesh-flaky receivers; the 2·BTT shallow base covers 1 BTT P99 leader-broadcast propagation + 1 BTT IWANT round-trip. QBFT-SSV R1 pays a **2600ms tax** under tightened sizing — still meaningfully larger than OBFT at default RefloodDelay, but substantially smaller than the 3200ms tax under the older framing.

**Comparison OBFT vs QBFT (post-tighten reranking)**: under the older 2·BTT/emission framing, OBFT V_0 led QBFT R2 (~2350ms vs 2150ms at default RefloodDelay). Under tightened 1·BTT/emission, QBFT R2 now beats OBFT V_0 at default RefloodDelay (2950ms vs ~2350ms = 600ms ahead) — but OBFT V_0 is the *primary* leader (always tried first; no round-timeout gap), while QBFT R2 is only reachable after R1 fails (paying the ~2s round-change cost). At RefloodDelay=0 (fully-meshed opt-out), OBFT V_0 leads QBFT R2 by 100ms (3050 vs 2950). OBFT's backups (V_1..V_3) trade all MEV-fetch for maximally-wide propagation absorption (B_k = T_commit) — they're not MEV-fresh alternatives but safety nets that catch the slot when L_0's bundle doesn't reach in time. The structural tradeoff is unchanged: OBFT's primary always tries first; QBFT's R2 is conditional on R1 failure.

### Head-change handling

> **TL;DR for SSV proposer duty.** Two host-validation modes interact differently with re-orgs:
>
> - **Strict mode** (per-receiver `parent_root`-vs-head check inside `obftHostValidate`): re-orgs landing inside the validity-locking window can split honest verdicts → in-protocol slot-miss via validity-divergence deadlock (see [§Failure modes](#failure-modes)).
> - **Loose mode** (no `parent_root` / fork-domain check; rely on relay + beacon-node rejection at submit time): no in-protocol deadlock from re-orgs; instead, slot-miss surfaces at submit if the cluster-agreed V's parent ends up orphaned.
>
> **SSV picks loose mode** for both OBFT and QBFT in the SSV implementation (`obftHostValidate` / `ProposerValueCheckF`). Rationale: under mainnet re-org rates, strict per-receiver validation triggers assumption-3 violations more often than it prevents byzantine-fork acceptance, and the relay / beacon-node already enforces fork validity at submit time. Deployments with higher re-org rates or stricter byzantine-fork tolerance may opt into strict mode.
>
> The rest of this section walks through both modes — leader-side fetch loop semantics apply to both; the receiver-side validity-locking discussion is most directly relevant to strict-mode deployments.

For SSV's proposer duty under strict mode, the host application's `valid` / `not-valid` verdict on `V_k` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_k`. Under loose mode the verdict reduces to structural + slashing-protection checks and is stable across the consensus window. The protocol consumes whichever verdict the host produces; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_k)` *exactly once per slot/layer*, on the final `V_k` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_k, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_k` is locked on the originally-signed value. The layer's leader cannot subsequently emit NR/NV on `nr_tag_k` — the Phase-1 σ is their σ-side commitment per cross-phase exclusivity.

**Receiver-side validity stabilization — host best-effort toward assumption 3.** OBFT requires host validity to be unanimous at decision time (see [Assumptions](#assumed)). For SSV's proposer duty, the host narrows the divergence window via per-operator validity stabilization: each operator runs the validity check (including `parent_root`-vs-head) **once at Phase-1 acceptance** against a stable head snapshot, then locks the verdict for the remainder of the slot. Subsequent head movements within that operator do not flip the verdict.

**The validity-locking window is per-V, bounded by `B_k`.** Operators accept Phase-1 bundles in `[slot_start, T_commit]`. Each operator locks their verdict per V at first-observation of that V. For V_k, the cluster-wide spread of lock-times is at most `B_k` — the time from earliest possible first-observation (right after `T_broadcast_max_k`) to latest (just before `T_commit`). At the operating point above with default RefloodDelay=700ms: V_0's window ≈ 1100ms (= 2·BTT + RefloodDelay); backups (V_1 at K=2 default; V_1, V_2, V_3 at K=4 up-tier) all have window ≈ 3600ms (the entire commit budget — backups broadcast at BFT_start). At RefloodDelay=0 V_0's window shrinks to 400ms; backups unchanged. **For the practical case where the cluster reconstructs V_0 (healthy path), the relevant window is V_0's `B_0`.** Backups (V_k for k > 0) have the maximum possible validity-locking window (entire commit budget), but those layers are only reached on fall-through (silent/late primary leader), so the divergence-rate × fall-through-rate product is the practical metric — and backups fetch from deepest-confirmed parents which are re-org resistant by construction. A re-org landing inside V_0's window can split honest verdicts; the all-honest rate of validity-divergence slot-misses scales with re-org distribution within V_0's `B_0`. **In adversarial-byz deployments the operational rate is multiplicatively higher** — a byzantine within the f-bound exercising passive f-budget (silence or σ-on-V; neither cryptographically slashable individually) widens the deadlock zone beyond the all-honest case. See [§Failure modes / Validity-divergence deadlock](#failure-modes) for the `re-org rate × byz-passivity-rate` scaling.

**This per-operator locking does NOT guarantee cluster-wide convergence.** Operators accept Phase-1 bundles at different `t` within the gossipsub propagation window (operator-local first-observation timestamps, plus clock skew δ). If a re-org happens during this acceptance window, operator A may snapshot pre-reorg (verdict = valid) while operator B snapshots post-reorg (verdict = invalid). Both locks hold; cluster views diverge. The locking ensures *per-operator* stability across the slot, narrowing the divergence window to "re-org during gossipsub-acceptance window" — typical P99 ≈ 100–500ms — but does not eliminate it.

When the residual window fires (re-org lands inside the acceptance window AND operators split across the boundary), assumption 3 is violated and the slot may miss in-protocol — neither σ-quorum (capped at honest-σ-validators + leader's σ_L^V) nor NR-quorum (capped at honest-NV-count) reaches threshold under adversarial byz that withholds NR. This deadlock is the cost of strict re-validation. The looser alternative — **omit per-receiver `parent_root` validation entirely and rely on relay / beacon-node rejection at submit time** — avoids the in-protocol deadlock at the cost of committing cluster-wide on a V whose parent may become orphaned (slot miss surfaces at submit, not at consensus). Hosts pick between the two failure modes based on observed re-org rates.

**SSV's implementation choice** is the loose mode (per the TL;DR at the top of this section): `obftHostValidate` for OBFT and `ProposerValueCheckF` for QBFT both perform structural + duty/identity + slashing-protection checks only, omitting `parent_root` / fork-domain validation. The receiver-side validity-locking discussion above describes how strict mode would behave if a deployment opts into it; under SSV's actual loose-mode default, validity verdicts are stable across the consensus window and the in-protocol validity-divergence deadlock does not fire.

The "permit and slot-miss" framing parallels OBFT's equivocation handling: validity-divergence is a view-divergence pattern that the protocol does not recover from. Unlike equivocation, it's not attributable to anyone (re-orgs are real-world events, not protocol violations), so no slashing applies; it relies on assumption 3 being approximately true.

**Backup-leader re-org resistance.** Fetching `V_k` for k ≥ 1 from a deeper-confirmed parent (the asymmetric `T_broadcast_max_3 < ... < T_broadcast_max_0` schedule already accommodates this) reduces the likelihood that the backup's parent becomes orphaned. Backups are structurally re-org-resistant by construction.

Implementation notes:

- Each operator validates `parent_root` against a stable head snapshot taken at Phase-1 acceptance time; the verdict is locked into the operator's local state for the remainder of the slot.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" and "single V per operator per layer" — a second signing attempt with a different V at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing AND operator-side double-signing detectable cluster-wide.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same.

2. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. The deadlines:

   - **`T_broadcast_max_k = max(BFT_start, T_commit − B_k)`** per-layer: leader L_k aims to finish broadcasting by this target so its bundle propagates to all honest before `T_commit` under that layer's per-layer propagation budget `B_k`. See §Setting for the primary-vs-backup design (`B_0` < `B_1 = ... = B_{K-1} = T_commit`; backups clamp target to slot start).
   - **`T_commit`**: receiver acceptance cutoff. Bundles first-observed past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum at this layer.

   Phase-window minimums:

   - **`Δ_2 ≥ 1 BTT`**: `KindCommit` propagation budget — operators emit `KindCommit` by `T_commit` (typically earlier under §Phase 2 emission-timing), peers must receive it before Phase 3.
   - **`ε_3` ≈ 50ms**: Phase 3 is purely local reconstruction processing (BLS aggregation, IBE decryption walk, certificate construction).

   All are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

3. **Choosing K (layer count).** K is per-duty:

   - **`K = f+1`**: BFT-liveness minimum; **recommended default for SSV proposer duty at n=4** (= K=2). Motivated by production testing showing K > f+1 doesn't materially improve outcomes once peer-reflood-V closes the healthy-mesh `h_V=1` case at L_0 (see [§Phase 2 / Peer-reflood V via early commit](#phase-2--onion-broadcast-t_emit-t_emit--%CE%94_2-with-t_emit--t_commit)). Trade-off: only one fall-through layer (`L_0 → L_1` at K=2), so the late-deepest-layer-leader-broadcast Class A failure mode at L_{K-1} has no further backstop — acceptable given the deterrent and the (healthy-mesh) closure of the Class B `h_V=1` grief vector; deployments operating with sustained degraded mesh may prefer `K ≥ f+2`.
   - **`K = f+2..n`** (up-tier): provides additional fall-through layers within Phase 3's single reconstruction walk. At `n = 4`, max K is 4 (one layer per operator). Each extra layer adds a level of chained encryption (~0.5 KB per onion at K=2, ~1 KB at K=3, ~3 KB at K=4 — within practical bandwidth). Suited to deployments preferring deeper fall-through (high-MEV proposer slots, clusters with frequent late-deepest-layer-broadcast events).

   **K bound (see §Setting for the full discussion):**
   - **`K ≥ f+1`** — BFT-liveness minimum (≥ 1 honest leader exists by pigeonhole over the f-byz bound). This is the only K-floor the protocol mandates. `K ≥ f+2` additionally provides late-leader-resilience (≥ 2 honest leaders); whether to use it is a deployment choice (see [§Setting](#setting)).

4. **R is fixed at 1.** OBFT is single-round by design. The slot's hard wall is the relay submission deadline; bundles arriving past `T_commit` are not counted, and the cluster relies on K-layer fall-through rather than retry.

5. **Tag construction and replay.** Each NR tag must uniquely bind `(slot, cluster, layer)` to prevent replay across slots/layers/clusters. Structure: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`.

6. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one OBFT instance and assumes:
   - Single OBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between OBFT (`protocol_tag = "OBFT-v1"`) and any other path that signs against the V-signing share (other consensus protocols sharing the same V-keypair).
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`), not just submission.

7. **Equivocation is permitted, not recovered.** OBFT does not provide an in-protocol equivocation recovery mechanism. Some patterns reach qV naturally (when 2-of-3 honest happen to σ-commit on the same V at f=1 n=4; leader's σ_L^V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. Equivocation is treated as a slashable byzantine fault; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots.

## Where this came from

OBFT is the design point that asks: **what's the minimum machinery needed to get K-layer parallel fall-through, in a single round, without within-slot partition recovery?** The motivation is **spec/wire/EKM simplification** — the answer keeps K-layer fall-through, chained encryption, equivocation detect-and-slash, and the five slashing-evidence rules, and omits round-change, round-retry, cross-round σ-or-NR exclusivity, cross-round acceptance widening, the Defer state, and any Phase-2 sub-phasing. Each operator commits exactly once by `T_commit` on a 3-state (σ, NR, NV) lattice with a single `KindCommit` message; the EKM is a single signing event per (slot, layer) per operator backed by standard transactional sign-and-log.

The cost is no within-slot partition recovery — bundles past `T_commit` are not counted, and the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time. This is suited for small clusters where the gossipsub mesh is effectively full and asymmetric propagation is rare; the trade-off buys a smaller spec and fewer wire kinds than 2abOBFT, with a comparable EKM (both are a single signing event per (slot, layer)) and comparable high-P99 headroom now that 2abOBFT's async fire removed its former healthy-path RTT cost. 2abOBFT shares bare OBFT's per-layer leader σ-witness head-start; its Phase-2 split (the `KindNoValue` coordination state + dynamic NR commit) additionally recovers σ-locked-split leader equivocation at L_0 and decides a late L_0-leader at L_0 (see [Appendix A.2](#a2--comparison-with-2abobft)). h_V=1 selective-Phase-1-delivery is largely addressed in both protocols under healthy mesh via the peer-reflood-V mechanism (the degraded-mesh case remains a slot-miss pattern deterred via Assumption 4).

The σ-locked-split-equivocation miss specifically is a **simplicity choice, not a safety-forced one**. OBFT drops the `Defer` state (a V-drop operator hard-locks NR at `T_commit`), and that removal closed the withhold-then-fake-σ attack as a side-effect — but OBFT also retains the leader-witness byz-safety gate (peer-V usable only against a verified `σ_L^V`), which independently neutralizes that attack. So a deferred-NR state *could* be re-added to recover σ-locked-split at L_0 (the way 2abOBFT's `KindNoValue` does), at the cost of regrowing the 4-state lattice OBFT exists to shed; the lean 3-state lattice is kept deliberately, with the rational-byzantine deterrent (Assumption 4) absorbing the residual. See [Appendix A.2](#a2--comparison-with-2abobft).

## Appendix A — Protocol comparisons

This appendix gives high-level structural comparisons against the protocols OBFT relates to: [OBFTR](OBFTR.md) (the multi-round generalization), [2abOBFT](2abOBFT.md) (the Phase-2-split successor that recovers equivocation and validity-divergence in-protocol), and QBFT (SSV's existing consensus protocol).

### A.1 — Comparison with [OBFTR](OBFTR.md) (R ≥ 2)

OBFT is OBFTR with R fixed at 1 and the round-retry machinery stripped. They share Phase 1 / Phase 2 / Phase 3 structure, K-layer fall-through, chained encryption, the three commitment states (σ, NR, NV), and the five slashing-evidence rules. OBFT differs from OBFTR by what's *removed* from the spec rather than by adding anything.

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
| Wire format per operator | Separate `KindOnion` (σ-side, may emit multiple times) + `KindNR` (NR-side, end-of-window) | Single `KindCommit` emitted by `T_commit` (early on `T_L_0_observed`) carrying both σ and NR partials |
| Partial-synchrony envelope (Phase-1 bundle propagation) | `R · P99` cross-round retention (e.g., `2·P99` at R=2 — bundles arriving in `(P99, R·P99]` get a fresh chance to be σ-emitted on in round 2) | primary-vs-backup `B_k`: `B_0 = 2·BTT + RefloodDelay` for the MEV-fresh primary; `B_1..B_{K-1} = T_commit` for all backups (full commit budget, backup leaders broadcast at BFT_start); cluster falls through to whichever layer's bundle arrived in time. No cross-round retention (single round) |
| Partial-synchrony envelope (Phase-2 KindCommit propagation) | `Δ_2 = 1 BTT` per round (tightened — mesh-jitter absorbed by OBFTR's structural cross-round retention rather than per-round Δ_2 cushion) | **`Δ_2 = 1 BTT` recommended** — reflood absorption is structurally provided by `B_k` (reflood-aware schedule), so Δ_2 only covers the synchronous-fallback propagation cycle |
| MEV-fetch budget for primary leader (BTT=200ms, `header_submit_headroom = 100ms`; B_0 is K-independent) | ~1.45s (T_commit_1 = 2.00s constrained by R1+R2 fit within 4s slot) | **~2.35s** at default RefloodDelay=700ms (T_commit = 3.60s; B_0 = 2·BTT + RefloodDelay = 1100ms; single-round, no retry budget needed) — **+0.9s more MEV-fresh fetch**; at RefloodDelay=0 the budget widens to ~3.05s (+1.6s more) |
| Submission headroom (`header_submit_headroom`) | 100ms | 100ms |
| Consensus complete | slot_start + 3.90s | slot_start + 3.90s (same anchor; OBFT redirects the saved BTT-budget into the MEV-fetch window) |
| Bandwidth (healthy, n=4, K=2 default; cluster-wide totals) | ~8 KB across 2 emissions per round (`KindCommit_r` + `KindLCClaim_r`) — `KindLCClaim` is small (~64 B per emission × n = ~256 B total per round; just the operator's `L_C` view + auth signature, no σ partials) so it's negligible vs `KindCommit_r`'s ~1.5–2 KB/operator at K=2 | ~6–8 KB across 1 emission per operator (`KindCommit` ≈ 1.5–2 KB/op × 4 ops at K=2); both protocols' totals include the σ_L^V witness section ≈ +1.2 KB at 145 bytes/witness × 8 witnesses (cluster-wide at K=2; per-op ~290 B). K=4 up-tier: cluster-wide ≈ 28 KB across both protocols (~7 KB/op × 4 ops; witness section ≈ +2.3 KB at 16 witnesses). |
| Bandwidth (worst case at R=2 with round-1 failure) | ~52 KB across 4 emissions (2 rounds × 2 emissions) | n/a (no round 2) |
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

[2abOBFT](2abOBFT.md) is the Phase-2-split sibling of OBFT — same K-layer onion structure, same chained encryption, and (in the implemented design) the **same per-layer leader σ-witness head-start** (`LWitness` at Phase 1, the analog of OBFT's `σ_L^V`). What differs is the Phase-2 shape: 2abOBFT splits the single `KindCommit` into a `KindValue` (terminal σ-side emission, σ partial inline) / `KindNoValue` (non-binding coordination, **no L_0 lock**) / `KindCommit-NRDirect` triad, plus a dynamically-fired `KindCommit-NR` and the A1 upgrade. Because both protocols now carry the witness and fire asynchronously on L0Ready, their healthy paths and residual misses coincide; the split buys three incremental recoveries over bare OBFT — all from the same `KindNoValue` no-lock lever (table below) — at no healthy-path RTT cost.

| Aspect | OBFT | 2abOBFT |
|---|---|---|
| Phase-1 leader σ-witness | Yes — `σ_L^V`, per-layer (head-start of one partial; σ-locks the leader) | Yes — `LWitness`, per-layer (same head-start, same σ-lock) |
| Phase-2 shape | single `KindCommit` (σ+NR together); each operator commits once by `T_commit` (early on `T_L_0_observed`) | split: `KindValue` (σ inline) / `KindNoValue` (coordination, **no L_0 lock**) / `KindCommit-NRDirect`; dynamic `KindCommit-NR` (cannot-σ gate); A1 upgrade |
| Healthy-path latency | ~2 RTTs (Phase 1 + Phase 2) | ~2 RTTs — async fire emits `KindValue` ~1·BTT after the bundle arrives; no synchronized fire instant |
| σ-locked-split leader equivocation (f-f) | **Misses at L_0** — V-drop operators NR-lock at `T_commit` and cannot recover | **Recovers at L_0** — `KindNoValue` keeps V-drop operators harvest-eligible, so they A1-upgrade to σ on a forwarded witness |
| Late L_0-leader broadcast | Falls through to L_1 | Decides at L_0 (bundle absorbed in the σ-pool fill window) |
| Transient mesh-flakiness (operator would NR-lock before its mesh recovers) | May miss — cross-phase exclusivity locks the flaky NR | **Recovers** — `KindNoValue` no-lock state lets the operator A1-upgrade once its mesh delivers V |
| 1-1-1 leader equivocation | **Misses** (σ-locked, no NR pivot) | **Misses** (σ-locked, no NR pivot) — same |
| h_V=1 selective-delivery | Largely addressed at L_0 via peer-reflood-V (recovers under healthy mesh; degraded-mesh tail slot-misses, deterred via Assumption 4) | Same mechanism — peer-reflood-V harvest of forwarded witnesses + A1-upgrade (same degraded-mesh tail) |
| Validity-divergence (re-org) | honest-majority recovers (3-σ vs 1-NV → L_0; 1-σ vs 3-NV → L_1); 2-2 boundary and validity-div + passive-byz miss | Same — the σ-locked-leader limit is identical |
| Wire kinds | `Phase1Bundle`, `KindCommit`, `KindCertificate` | `Phase1Bundle`, `KindValue`, `KindNoValue`, `KindCommit`, `KindCertificate` |
| EKM | single signing event per (slot, layer) per operator | Same (lowest in the family) |

**The Phase-2 split's net delta over bare OBFT is narrow** now that both protocols carry the leader-witness head-start: it recovers **σ-locked-split leader equivocation at L_0** (via the `KindNoValue` no-lock state + A1-upgrade on a forwarded witness), decides a **late L_0-leader at L_0** rather than falling through to L_1, and recovers **transient mesh-flakiness** where bare OBFT's hard NR-lock at `T_commit` would foreclose the operator. All three trace to the same lever — the `KindNoValue` coordination state that defers the NR commitment instead of locking it. Otherwise the two protocols share the witness head-start, the ~2-RTT healthy path, the peer-reflood-V close of h_V=1, and the same residual misses (1-1-1 equivocation; 2-2 validity-divergence; validity-divergence + passive byzantine). The cost of the split is the extra wire kinds and the dynamic Phase-2b cascade; for deployments where the three recoveries above matter (high-MEV proposer slots, small adversarial-byz clusters, mesh-flakiness conditions), 2abOBFT closes them in-protocol rather than relying on assumption 4 alone. See [2abOBFT.md §Comparison](2abOBFT.md#comparison-with-the-obft-family-and-qbft) for the same comparison from 2abOBFT's side.

### A.3 — Comparison with QBFT

QBFT is SSV's existing consensus protocol. The key structural difference: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus partial-sig collection)**; OBFT (and the rest of the OBFT family) fuses the two by embedding partial signatures inside Phase-2 onions. Most observable trade-offs trace back to this structural difference.

For per-scenario liveness behavior (recovery scope, mechanism, outcome) see [Liveness comparison: OBFT vs OBFTR(R=2) vs QBFT](#liveness-comparison-obft-vs-obftrr2-vs-qbft). This appendix covers the structural / cost dimensions: protocol shape, latency, bandwidth, safety posture, primitive complexity, and deployment maturity.

| Aspect | QBFT | OBFT (K=2 default for proposer; K=4 up-tier) |
|---|---|---|
| Protocol shape | Multi-round (PROPOSE → PREPARE → COMMIT) with per-round leader rotation; round-change on timeout | Single round, K-layer onion fall-through |
| Consensus and signing | Decoupled: consensus reaches a decided value, then a separate post-consensus phase collects 2f+1 partial sigs | Fused: Phase-2 onions carry σ partials directly, Phase 3 reconstructs |
| RTTs to signed output (min, healthy) | 3 (consensus) + 1 (post-consensus) ≈ 4 | 2 (Phase 1 + Phase 2) |
| Termination guarantee | Eventually-terminating across rounds (under partial synchrony) | Conditional on `D ≥ real_propagation`; for larger envelope, use [OBFTR(R≥2)](OBFTR.md) |
| Safety posture | Honest-majority (`2f+1` honest) + correct quorum-certificate verification | Honest-majority cryptographic via `qEnc = qV = 2f+1` + chained IBE + EKM-enforced per-operator commitments. Same trust posture as QBFT — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). |
| Bandwidth (healthy n=4; cluster-wide totals) | ~14 KB across 4 emissions per round (PROPOSE + PREPARE + COMMIT + post-cons.) | ~6–8 KB across 1 emission per operator at K=2 default (`KindCommit` ≈ 1.5–2 KB/op × 4 ops; includes σ_L^V witness section ≈ +1.2 KB cluster-wide at 145 bytes/witness × 8 witnesses). K=4 up-tier: ~28 KB cluster-wide. |
| Latency (healthy, n=4, BTT=200ms) | ~800 ms | ~400 ms (Phase 1 + Phase 2 + Phase 3 = 2·BTT + ε_3 at Δ_2 = 1 BTT) |
| Latency (1 round failure, n=4) | ~3.0 s (round-1 timeout 2s + round-2 success ~750 ms) | n/a (single round; failure → slot miss) |
| Slashing-protection scope | Single block sig per slot, gated at submission time | Multiple per-share sigs per slot at candidate-signing (Phase-1 leader, Phase-2 onion) gated by EKM cross-keypair coordination — see [EKM coordination model](#ekm-coordination-model) |
| Cryptographic primitive | BLS threshold signatures only | BLS threshold signatures + threshold IBE / SWE (drand/tlock-style) |
| Application contract | Decides on any value the application proposes | K-layer with primary + backup-leader fetches |
| Production maturity | What SSV runs today; corner cases hit and fixed | New codebase; deployment confidence to be derived |

**Cost-side trade-off summary:**

- **Latency.** OBFT wins on healthy-path (~400ms vs ~1600ms QBFT-SSV at recommended sizing). On round-1 failure, QBFT can still recover via round-change (at ~3.6s total), while OBFT single-round failures are slot-misses; OBFTR(R=2) covers round-1-failure cases at ~2.4s within the same envelope. OBFT's recovery scope is narrower than QBFT's but available much faster within scope.
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

- **Healthy-path latency.** ~400ms vs ~1600ms QBFT-SSV at recommended sizing.
- **Multi-leader-failure recovery.** OBFT's K-layer parallel fall-through resolves K-1 silent layers within Phase 3's reconstruction walk (sequential local decryption, no per-layer RTT). For K=4 up-tier with 3 silent leaders, OBFT recovers in ~800ms (3·BTT consensus + ~200ms ε_3 × K at tightened per-emission sizing); QBFT round-changes 3 times serially, exceeding the 4s budget. (K=2 default has only one fall-through layer — multi-silent recovery requires K ≥ 3.)
- **All-honest-NR equivocation recovery.** When byz delivers V's early enough for re-flood to spread conflicts before T_commit, all 3 honest retain ≥ 2 V's and emit NR per the equivocation rule; NR-quorum at L_0 → fall-through to L_1. Same recovery as QBFT but in single round (~400ms vs ~3.6s).
- **Spec/EKM simplicity vs OBFTR(R≥2).** No cross-round atomicity, no L_C consensus, no per-round widening — see [§A.1](#a1--comparison-with-obftr-r--2).

**The operational bottom line:** QBFT covers more failure modes (its round-change-with-fresh-V handles validity-divergence and 1-1-1 equivocation that OBFT-family doesn't). OBFT wins on common-case latency and multi-leader-failure recovery. For SSV proposer duty under a 4s relay cutoff, the choice depends on observed re-org rate (favors QBFT), cluster's tolerance for the 1-1-1 equivocation case via the rational-byzantine deterrent (favors OBFT-family), and deployment complexity tolerance (favors OBFT over OBFTR(R≥2) in the family).

## Appendix B — L_Bid mini-consensus extension

> **Note on values:** This appendix's concrete numbers reflect the reflood-aware bare OBFT schedule (`B_0 = 2·BTT + RefloodDelay`; see [§Setting](#setting)). The L_Bid extension's *structural* design is unchanged from earlier drafts — `B_0_LBid = 0.5·BTT` typical-mesh-only at the bid layer, mini-consensus window between bundle arrival and `T_commit`, bare OBFT's reflood-buffer reused as mini-consensus headroom. Under the wider reflood-aware `B_0`, L_Bid's MEV-fetch cost collapses to zero at every named sizing under default RefloodDelay (= 700ms); see the per-sizing derivations in [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening).
>
> **Note on deep-layer deadlines under the primary-vs-backup schedule:** Bare OBFT's current schedule (see [§Setting](#setting)) is primary-vs-backup — `B_0 = 2·BTT + RefloodDelay` for the MEV-fresh primary; `B_1..B_{K-1} = T_commit` for all backups (broadcast at BFT_start with deepest-confirmed-parent fetch). The L_Bid mini-consensus framework below was originally drafted against an earlier per-layer staggered backup schedule (`B_k_shallow = (k+2)·BTT + RefloodDelay`) and includes per-layer deadline-shift formulas (`T_broadcast_max_k = T_0_arrival − B_k_LBid` for `k ≥ 1`) that assume deeper-layer deadlines can be moved earlier for mini-consensus headroom. Under the current schedule those deadlines are already pinned at BFT_start and cannot be shifted further; deep-layer bundles broadcast from BFT_start propagate via standard gossipsub and reach receivers well before any reasonable `T_0_arrival` anchor, so the mini-consensus operates over deep-layer bundles that arrived "for free" via standard backup propagation rather than via L_Bid-side deadline tightening. The L_0-side derivations (`B_0_LBid = 0.5·BTT` constraint, reflood-buffer reuse, per-sizing MEV-fetch costs) remain accurate. A full L_Bid re-derivation under the current schedule is a follow-up if/when the extension is implemented.

This appendix specifies an opportunistic bid-routing extension to OBFT. **L_Bid** is a bid-determined top layer prepended to OBFT's K rotation-determined layers (yielding `K' = K + 1`). The extension adds a **mini-consensus sub-phase between `T_0_arrival` and `T_commit`** that resolves L_Bid's identity cluster-wide before σ-commitment. `T_0_arrival` is the deterministic point by which an honest L_0 Phase-1 bundle broadcast at `T_0_broadcast_max` is expected to have reached all honest operators under the bid layer's typical-mesh budget (`B_0_LBid = 0.5·BTT`). The mini-consensus is a single round of all-to-all verdict broadcast with quorum-based binding — verdicts are op-identity-signed claims, not threshold partials, so it adds no new cryptographic primitives and does not change OBFT's safety analysis.

The extension closes three deadlock surfaces that any naive bid-routing extension would expose ([§Background — bid-layer deadlocks](#background--bid-layer-deadlocks)) and adds two adversarial-byz residual surfaces at L_Bid (2-1-byz-defect, verdict-equivocation) plus the standard 2-2 validity-divergence hard algebraic limit. Post-`T_commit` latency matches bare OBFT; the leader's broadcast deadline shifts earlier vs bare OBFT by `max(0, Δ_minicon − (B_0 − B_0_LBid))` — i.e., the leader broadcasts at whichever is earlier between L_Bid's mini-consensus requirement and bare OBFT's reflood-aware L_0 requirement. Under Config A with default RefloodDelay=700ms, `B_0 − B_0_LBid = 1.5·BTT + RefloodDelay = 1000ms`, large enough to absorb all three named `Δ_minicon` sizings (2·BTT / 1.5·BTT / 0.5·BTT) with zero MEV-fetch cost vs bare OBFT. At RefloodDelay=0 the buffer shrinks to `1.5·BTT = 300ms`; conservative `Δ_minicon = 2·BTT = 400ms` overshoots by 100ms and pays that as MEV-fetch cost. See [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening) for the full derivation. Rotation-layer recovery scope is unchanged; safety is identical to bare OBFT. L_Bid relies on one additional assumption beyond bare OBFT's threat model — **bid-value honesty** (see [§Additional assumption — bid-value honesty](#additional-assumption--bid-value-honesty)).

### Background — bid-layer deadlocks

Any bid-routing extension that gates σ-eligibility on per-operator local computation over a bid set ("did I see enough eligible Phase-1 bundles? what's the highest bid?") has three deadlock surfaces under f-byz adversarial behavior. The mini-consensus exists to close them.

- **C1 — Selective candidate withholding.** A byzantine rotation leader selectively withholds its L_Bid-eligible Phase-1 bundle (including bid metadata) from a subset of honest peers. Honest with incomplete bid sets cannot compute σ-eligibility correctly (their argmax may differ from cluster truth); honest with complete sets can. Honest σ-emit decisions fragment; the σ-pool fragments below `qV`; no V reaches quorum.

- **C2 — Candidate / bid equivocation.** A byzantine rotation leader sends different signed Phase-1 bundle views to different peers (V vs V', or the same V with different signed bid metadata). Each honest peer's argmax may yield a different winner; honest σ-commit on different V's; σ-pool fragments per V below `qV`.

- **C3 — Validity-divergence on the bid winner.** Some honest find the highest-bid V valid (parent-root match, fork/domain, etc.); others do not. Honest with valid view σ-commit; honest with invalid view NR. σ-pool below `qV` from one side; NR-pool below `qEnc` capped by σ-locked operators; deadlock with no fall-through.

Without a cluster-wide convergence step, all three lead to slot-miss. The mini-consensus replaces "each operator computes σ-eligibility from their local bid view" with "each operator broadcasts a verdict; the cluster binds when verdict-quorum reaches" — verdicts are observable, so honest peers converge on whether quorum reached even when underlying bid sets diverge.

### Additional assumption — bid-value honesty

L_Bid introduces one assumption beyond bare OBFT's: **eligible rotation leaders report their bid values truthfully**. Without this, a byzantine leader can claim arbitrary `bid_value` for its Phase-1 candidate and reliably win L_Bid argmax, routing the cluster to sign a low-MEV (potentially self-dealt) block. The grief leaves no on-wire evidence and produces no slot-miss, so none of [assumption 4's](#assumed) deterrent mechanisms (slashable evidence, slot-miss visibility to stakers, behavioral patterns visible to honest peers) discipline it per-slot. Safety and liveness are unaffected (Pigeonholes hold regardless); only L_Bid's MEV-value-capture motivation breaks.

The protocol does not enforce bid-value honesty cryptographically. Deployments satisfy it via one of:

- **Relay/builder attestation verification** (recommended for SSV proposer duty) — the Phase-1 bundle's bid section carries a `relay_attestation` field cryptographically binding `(V, bid_value)` to a cluster-recognized relay or builder; receivers verify before admitting the bundle into `bid_set_i`. Spec'd as an optional protocol extension — see [§Optional extension — relay/builder attestation verification](#optional-extension--relaybuilder-attestation-verification).
- **Institutional / permissioned operator set** — operators are vetted such that bid-value lying carries external (legal, reputational, business) consequences sufficient to discipline it. The cluster relies on those external mechanisms; in-protocol attestation is unnecessary.
- **Post-hoc payment reconciliation** — stakers monitor cluster MEV revenue against expected relay payouts and migrate validators from clusters where claimed bid-values systematically fail to materialize. Same evidence-quality character as OBFT's behavioral-pattern fault detection (slow, false-positive-prone) but viable when in-protocol attestation isn't available.

This assumption is L_Bid-specific — bare OBFT does not depend on it. Deployments choosing OBFT + L_Bid implicitly load this onto their threat model, satisfied by one of the paths above.

### When to use it

**Suited for**: deployments where MEV bid-routing upside among eligible rotation-layer candidates justifies (a) the `max(0, Δ_minicon − (B_0 − B_0_LBid))` MEV-fetch budget reduction — zero at every named sizing under Config A default RefloodDelay=700ms because the reflood-buffer `B_0 − B_0_LBid = 1.5·BTT + RefloodDelay = 1000ms` absorbs all three sizings free; only conservative (`Δ_minicon = 2·BTT`) pays 100ms at RefloodDelay=0 — see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening), and (b) the new adversarial-byz residual surfaces at L_Bid. For SSV proposer duty under Config A: high-MEV slots where bid-routed block value capture exceeds the slot-loss-rate cost from the new L_Bid failure modes.

**BTT regime guidance** (see [§Deployment envelope by BTT](#deployment-envelope-by-btt) for full table):
- **Under default RefloodDelay=700ms**, every named sizing matches bare OBFT V_0's broadcast deadline across the BTT range where bare OBFT itself fits (BTT ≤ 600ms — the reflood-aware `B_0 = 2·BTT + 700ms` consumes most of the pre-`T_commit` budget at higher BTT). The reflood buffer `1.5·BTT + RefloodDelay` absorbs `Δ_minicon` regardless of BTT — pick `Δ_minicon` purely on L_Bid success-rate vs implementation-complexity grounds, not MEV-fetch.
- **At RefloodDelay=0** (fully-meshed opt-out): bare OBFT extends to BTT ≤ 800ms. Conservative `Δ_minicon = 2·BTT` is the only sizing that pays MEV-fetch cost (0.5·BTT) anywhere in that range; standard and aggressive remain free. At BTT ≥ 1000ms bare OBFT itself doesn't fit so L_Bid is moot.
- **Adversarial residual still applies independently of MEV cost**: aggressive (`Δ_minicon = 0.5 BTT`, `Δ_verdict = 0.5 BTT`) exposes partial-propagation deadlock at sub-1·BTT verdict propagation (Class A; see [§New residual failure modes](#new-residual-failure-modes-at-l_bid)) — recommended only when production telemetry shows mesh propagation tighter than P99=1·BTT.

**Not suited for**: deployments prioritizing maximum MEV-fetch budget at high BTT, or where the L_Bid residuals (adversarial-byz + sub-1-BTT `Δ_verdict` partial-propagation deadlock) are a hard constraint (slot-miss without fall-through; see [§Liveness](#liveness)).

### Timing notation for L_Bid variants

This appendix and Appendix E use three arrival anchors with distinct meanings:

| Symbol | Meaning |
|---|---|
| `T_0_arrival` | Current L_Bid mini-consensus start: the point by which an honest L_0 Phase-1 bundle broadcast at `T_0_broadcast_max` is expected to have reached all honest operators. Current L_Bid sets `Δ_minicon = T_commit − T_0_arrival` and shifts all eligible rotation-layer deadlines to this anchor. |
| `T_deep_arrival` | L_Bid_New mini-consensus start: the point by which honest deep-layer Phase-1 bundles (`L_1..L_{K-1}`) broadcast at their L_Bid_New deadlines are expected to have reached all honest operators. L_Bid_New sets `Δ_minicon = T_commit − T_deep_arrival` and shifts only deep-layer deadlines to this anchor. |
| `T_broadcast_max_0^bare` | Bare OBFT primary broadcast deadline = `T_commit − B_0` (= `T_commit − (2·BTT + RefloodDelay)` = `T_commit − 1100ms` at Config A with default RefloodDelay=700ms; `T_commit − 400ms` at RefloodDelay=0). L_Bid_New preserves this deadline for bid_1. |
| `Δ_minicon` | Total mini-consensus interval, from the relevant arrival anchor (`T_0_arrival` or `T_deep_arrival`) to `T_commit`. |
| `Δ_verdict` | Sub-interval reserved for `KindBidVerdict` propagation: `T_verdict = T_commit − Δ_verdict`. |
| `Δ_select` | In-window bid-set settling budget before verdict broadcast: `Δ_minicon − Δ_verdict`. |

### Setting

Adds to OBFT's setting:

- **K' = K + 1 layers**: L_Bid (top, bid-determined) + OBFT's rotation-determined L_0, L_1, ..., L_{K-1}.
- **Bid data lives inside Phase-1 bundles**: there is no standalone `KindBid` wire message. Each rotation leader's Phase-1 bundle carries bid metadata for that same `V_{L_k}`. Only rotation-layer candidates are eligible for L_Bid; with `K = n` this still means every operator has one bid candidate, while with `K < n` L_Bid ranks only the selected `K` rotation leaders.
- **Mini-consensus window** `Δ_minicon`: `Δ_minicon = T_commit − T_0_arrival`. Mini-consensus starts at `T_0_arrival`, ends at `T_commit`, and all L_Bid timing derives from that interval. `T_0_broadcast_max = T_0_arrival − B_0_LBid` is the L_Bid-side constraint; the actual broadcast time is `T_commit − max(Δ_minicon + B_0_LBid, B_0)` — the leader broadcasts at whichever is earlier between L_Bid's mini-consensus requirement and bare OBFT's L_0 reflood-absorption requirement (so the same bundle remains in-envelope for L_0 fall-through if L_Bid mini-consensus fails). L_Bid uses tighter per-layer budgets `B_k_LBid` (typical-mesh propagation only, no reflood-tail budget; e.g., `B_0_LBid = 0.5 BTT` at Config A vs bare OBFT's `B_0 = 2·BTT + RefloodDelay = 1100ms` at default RefloodDelay); the rationale is that L_Bid is opportunistic and doesn't need bare OBFT's reflood-absorption guarantee at the bid layer — reflood-tail bundles miss `bid_set_i` but the same bundle still arrives in time for L_0 σ-pool aggregation via bare OBFT's wider `B_0` (see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening) below). The L_0..L_{K-1} broadcast deadlines shift earlier vs bare OBFT by `max(0, Δ_minicon − (B_0 − B_0_LBid))` (= `max(0, Δ_minicon − (1.5·BTT + RefloodDelay))`; zero at every named sizing under Config A default RefloodDelay). `T_commit` itself stays back-end-anchored to `T_relay_cutoff − submit_headroom − ε_3 − Δ_2` and is unchanged from bare OBFT.
- **Verdict propagation budget** `Δ_verdict`: `0 < Δ_verdict ≤ Δ_minicon`, with `T_verdict = T_commit − Δ_verdict`. Operators compute and broadcast `KindBidVerdict` at `T_verdict`; those verdicts propagate until `T_commit`. The remaining `Δ_select = Δ_minicon − Δ_verdict` is the in-window bid-set settling budget after `T_0_arrival` and before verdict broadcast.
- **L_Bid σ-eligibility**: determined cluster-wide by mini-consensus, not by per-operator local computation.
- **Bid visibility threshold**: `qBid = K − f` L_Bid-eligible Phase-1 bundles. At `K = n`, `qBid = n − f = qV`; for `K < n`, L_Bid intentionally ranks a smaller candidate universe.

`qV = qEnc = 2f+1` and the BLS+IBE keypair structure are unchanged from bare OBFT. The mini-consensus adds no new threshold cryptography.

### Wire kinds

In addition to OBFT's `Phase1Bundle`, `KindCommit`, `KindCertificate`:

- **`Phase1Bundle` bid section** — L_Bid extends the existing Phase-1 bundle envelope with `(bid_value, relay_attestation)`, signed by the leader's operator-identity key as part of the structured Phase-1 envelope. The bid section is not a separate message: it refers to exactly the same `V_{L_k}` carried by the bundle and signed by `σ_{L_k}^V(V_{L_k})`. The `relay_attestation` field is host-defined; verification is governed by an optional protocol extension (see [§Optional extension — relay/builder attestation verification](#optional-extension--relaybuilder-attestation-verification)). When the extension is disabled, the field MAY be empty or host-supplied.
- **`KindBidVerdict`** — operator `i`'s mini-consensus verdict. Payload `(protocol_tag = "OBFT-LBid-v1", message_kind = "minicon-verdict", cluster_id, slot, operator_id i, predicted_LBid_value_root_or_null)`, signed by `i`'s operator-identity key. `predicted_LBid_value_root` is set when `i` claims a specific V is the cluster's L_Bid winner; null when `i` claims no L_Bid (insufficient bid-set visibility, parent-root filter failure, or no consensus reachable).

### Per-layer windows and deadlines

Phase 1 layers operate under bare OBFT's primary-vs-backup schedule (primary `B_0 = 2·BTT + RefloodDelay`; backups `B_k = T_commit` for k ≥ 1, broadcast at BFT_start; see [§Setting](#setting)). The arrival anchor for the L_Bid extension is `T_0_arrival = T_commit − Δ_minicon`. The latest L_0 bundle broadcast deadline is `T_0_broadcast_max = T_0_arrival − B_0_LBid`; deeper leaders already broadcast at BFT_start and their bundles propagate to receivers well before `T_0_arrival` via standard gossipsub, so the L_Bid mini-consensus consumes deep-layer bids without imposing a separate deadline. (Earlier drafts of this appendix used `T_broadcast_max_k = T_0_arrival − B_k_LBid` for `k ≥ 1`; that per-layer-shift formula was load-bearing under the older staggered backup schedule and is moot now that backups are uniformly at BFT_start.) Mini-consensus starts at `T_0_arrival` and ends at `T_commit`:

| Phase | Window | Activity |
|---|---|---|
| Phase 1 candidate broadcast | `[slot_start, T_0_arrival]` | Primary `L_0` rotation leader broadcasts by `T_broadcast_max_0 = T_0_arrival − B_0_LBid` (the L_Bid-side constraint); deeper rotation leaders (`L_k`, `k ≥ 1`) broadcast at BFT_start per §2's primary-vs-backup schedule. Each bundle carries its own bid metadata. Receivers continue accepting bundles first-observed in `[slot_start, T_commit]` per bare OBFT, but L_Bid verdict computation uses only bundles first-observed by `T_verdict`. |
| Mini-consensus | `[T_0_arrival, T_commit]` | Operators compute predicted L_Bid (argmax over `bid_set_i` first-observed by `T_verdict = T_commit − Δ_verdict`) and broadcast `KindBidVerdict`; verdicts propagate by `T_commit`. |
| Phase 2 | `[T_commit, T_commit + Δ_2]` | σ-or-NR commit at all K' layers (L_Bid + L_0..L_{K-1}). |
| Phase 3 | (opportunistic from `T_commit` onward; SOFT target `T_commit + Δ_2 + ε_3`) | K'-layer reconstruction walk; observer-on-arrival per main §Phase 3. |

Sizing — `Δ_minicon` is the total mini-consensus interval; `Δ_verdict` is the portion reserved for verdict propagation; `Δ_select = Δ_minicon − Δ_verdict` is the bid-set settling buffer (post-`T_0_arrival` window during which late-arriving bundles still enter `bid_set_i`). Three named sizings, each step adding 0.5 BTT of robustness on top of the previous:

- **Conservative** `Δ_minicon = 2 BTT`, `Δ_verdict = 1 BTT`, `Δ_select = 1 BTT`: highest L_Bid success rate. P99 verdict propagation + 1 BTT post-`T_0_arrival` bid-set settling for late-arriving bundles. Under bare OBFT's reflood-aware schedule when L_0 reflood-absorption binds the broadcast, the bid_set effectively absorbs bundles in `[broadcast, T_verdict]` = `B_0 − Δ_verdict` post-broadcast — at Config A default RefloodDelay that's 900ms total absorption window (=  100ms typical-mesh propagation + 600ms pre-`T_0_arrival` reflood-buffer headroom + 200ms `Δ_select` post-`T_0_arrival`). At RefloodDelay=0 the broadcast shifts 100ms earlier (L_Bid constraint binds; see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening)) and the bid_set window shrinks to 300ms = 100ms typical-mesh + 200ms `Δ_select`.
- **Standard** `Δ_minicon = 1.5 BTT`, `Δ_verdict = 1 BTT`, `Δ_select = 0.5 BTT`: medium L_Bid success rate. P99 verdict propagation + 0.5 BTT post-`T_0_arrival` bid-set settling. Under L_0-binding (default RefloodDelay): bid_set window = `B_0 − Δ_verdict` = 900ms (= 100ms typical-mesh + 700ms pre-`T_0_arrival` headroom + 100ms `Δ_select`). At RefloodDelay=0 (regime boundary): bid_set window = 200ms (= 100ms typical-mesh + 0ms headroom + 100ms `Δ_select`).
- **Aggressive** `Δ_minicon = 0.5 BTT`, `Δ_verdict = 0.5 BTT`, `Δ_select = 0`: lowest L_Bid success rate. Sub-P99 verdict propagation (verdicts may not all arrive by `T_commit` under tail jitter — partial-propagation deadlock becomes a Class A residual, see [§New residual failure modes](#new-residual-failure-modes-at-l_bid)) + zero `Δ_select` post-`T_0_arrival` settling. Under L_0-binding (default RefloodDelay): bid_set window = `B_0 − Δ_verdict` = 1000ms — counterintuitively wider than conservative/standard because tighter `Δ_verdict` cedes 100ms back to bid_set. Suitable only when production telemetry shows mesh propagation tighter than the standard P99 assumption. Matches bare OBFT V_0's broadcast deadline exactly under both default RefloodDelay and RefloodDelay=0.

**Success-rate gradient.** As you go from conservative → standard → aggressive, the probability of L_Bid converging on a bid winner decreases monotonically:

- Two failure modes degrade under tighter sizings: (a) tail-arriving bundles excluded from `bid_set_i` (smaller `Δ_select`); (b) tail-arriving verdicts excluded from `verdict_pool` (smaller `Δ_verdict`).
- Conservative tolerates the widest tail on both. Standard tolerates a 0.5 BTT bundle-tail and a P99 verdict tail. Aggressive tolerates neither typical-mesh-tail bundles nor sub-P99 verdict jitter, and additionally exposes the cluster to all-honest partial-propagation deadlock at L_Bid (Class A).
- When L_Bid mini-consensus fails to converge, the cluster falls through to L_0 cleanly — bare OBFT's recovery scope at L_0 is unchanged. So "lower L_Bid success rate" means "more slots fall through to L_0's vanilla payload instead of the bid-routed payload" — not slot-misses (except at aggressive sizing's Class A residual).

`Δ_2`, `ε_3`, and `B_k` (bundle propagation budgets) unchanged from bare OBFT.

`T_commit` is back-end-anchored at `T_relay_cutoff − submit_headroom − ε_3 − Δ_2` and is **the same value for bare OBFT and OBFT+L_Bid** (e.g., `3600ms` at §Application's max-MEV anchor with Config A, BTT=200ms, tightened `Δ_2 = 1·BTT`, `ε_3 = 50ms`, `header_submit_headroom = 100ms`, `T_relay_cutoff = 4000ms`; with ~50ms residual jitter buffer).

What L_Bid changes is the **L_0..L_{K-1} broadcast deadlines** and the **MEV-fetch budget**: the L_Bid-side constraint is `T_0_broadcast_max = T_0_arrival − B_0_LBid = T_commit − Δ_minicon − B_0_LBid` (with tighter `B_0_LBid = 0.5 BTT` at Config A vs bare OBFT's `B_0 = 2·BTT + RefloodDelay = 1100ms` at default RefloodDelay; L_Bid budgets only typical-mesh propagation at the bid layer, leaving the reflood-tail to L_0 fall-through). The bundle must ALSO remain in-envelope for L_0 σ-pool aggregation if mini-consensus fails — so the leader broadcasts at the EARLIER of L_Bid's deadline and bare OBFT's `T_commit − B_0` deadline. Net broadcast-deadline shift vs bare OBFT is `max(0, Δ_minicon − (B_0 − B_0_LBid))`: bare OBFT's reflood-buffer beyond typical-mesh (`B_0 − B_0_LBid = 1.5·BTT + RefloodDelay = 1000ms` at Config A default RefloodDelay; `1.5·BTT = 300ms` at RefloodDelay=0) is reused as mini-consensus headroom for free, and any excess of `Δ_minicon` over that buffer is the actual MEV-fetch cost L_Bid pays. At Config A with **default RefloodDelay = 700ms** the reflood-buffer is 1000ms — wide enough to absorb every named sizing free:

- **Conservative** (`Δ_minicon = 2 BTT = 400ms`): shift = 0 (400 < 1000). L_0 broadcast stays at `2500ms` (= bare OBFT); MEV-fetch unchanged at ~2350ms. The bundle's effective bid-layer propagation budget is `B_0 − Δ_minicon = 700ms` (= 3.5·BTT, far more than `B_0_LBid = 0.5·BTT`), so L_Bid additionally absorbs reflood-tail bundles arriving up to that wider window for free.
- **Standard** (`Δ_minicon = 1.5 BTT = 300ms`): shift = 0. L_0 broadcast at `2500ms`; MEV-fetch ~2350ms; effective bid-layer budget `800ms`.
- **Aggressive** (`Δ_minicon = 0.5 BTT = 100ms`): shift = 0. L_0 broadcast at `2500ms`; MEV-fetch ~2350ms; effective bid-layer budget `1000ms`.

At **RefloodDelay = 0** (fully-meshed opt-out) the reflood-buffer shrinks to `1.5·BTT = 300ms` and only Conservative overshoots:

- **Conservative** (`Δ_minicon = 2 BTT = 400ms`): shift = 100ms. L_0 broadcast at `3100ms` (vs bare OBFT V_0 at `3200ms`); MEV-fetch ~2950ms (vs bare OBFT ~3050ms).
- **Standard** (`Δ_minicon = 1.5 BTT = 300ms`): shift = 0. L_0 broadcast at `3200ms`; MEV-fetch ~3050ms.
- **Aggressive** (`Δ_minicon = 0.5 BTT = 100ms`): shift = 0. L_0 broadcast at `3200ms`; MEV-fetch ~3050ms.

### L_Bid broadcast-deadline tightening

Bare OBFT's reflood-aware `B_0 = 2·BTT + RefloodDelay` (= 1100ms at Config A default RefloodDelay=700ms) decomposes as ~0.5 BTT typical-mesh propagation plus a wider **reflood-absorption buffer** — extra time between expected typical-mesh arrival and `T_commit` covering one full IHAVE/IWANT lazy-push cycle when initial eager-push fails to reach all honest peers (see [§Setting](#setting)). The reflood buffer is `B_0 − 0.5·BTT = 1.5·BTT + RefloodDelay = 1000ms` at default RefloodDelay; at `RefloodDelay = 0` (fully-meshed opt-out) it collapses to `1.5·BTT = 300ms`. The reflood buffer is **required at every layer** in bare OBFT because σ-or-NR commit is mandatory: each layer must reach σ-quorum or NR-quorum at `T_commit` for the cluster to make progress, so without it the reflood tail would leak past `T_commit` and operators could wrongly NR-emit on layers where the bundle was actually in flight.

L_Bid uses a tighter per-layer budget at the bid layer — `B_0_LBid = 0.5 BTT` (typical-mesh propagation only, no reflood-tail budget) — because bid-routing is **opportunistic, not mandatory**:

- Late bundles excluded from one operator's `bid_set_i` only mean that operator predicts a different argmax (or null). The cluster converges via `verdict_quorum` on whichever V enough operators saw on time.
- If no `verdict_quorum` forms, the cluster falls through to L_0 — which is bare OBFT's L_0 with its full reflood-aware `B_0 = 2·BTT + RefloodDelay`. So the bid layer doesn't need its own reflood-absorption; bare OBFT's L_0 covers the case.
- The post-`T_0_arrival` budget `Δ_select = Δ_minicon − Δ_verdict` (nonzero at standard or conservative sizing) provides bid-set settling for late-arriving bundles — a different role from bare OBFT's reflood buffer (settling improves bid-routing rate; reflood buffer prevents deadlock).

**Why the broadcast is constrained by L_0, not L_Bid, at typical sizings.** The same Phase-1 bundle serves both purposes (L_Bid bid metadata + L_0 candidate `V_{L_0}`), so the leader broadcasts at the EARLIER of the two deadlines:

- L_Bid-side deadline: `T_commit − Δ_minicon − B_0_LBid` (bundle reaches `bid_set_i` by `T_0_arrival`).
- L_0-side deadline: `T_commit − B_0` (bundle reaches L_0 σ-pool by `T_commit` even under reflood-tail).

The L_0 deadline binds whenever `Δ_minicon + B_0_LBid ≤ B_0` — equivalently, `Δ_minicon ≤ B_0 − B_0_LBid = 1.5·BTT + RefloodDelay`. At Config A default RefloodDelay=700ms this means every `Δ_minicon ≤ 1000ms` clamps L_Bid's broadcast at bare OBFT's `2500ms` deadline; at RefloodDelay=0 the threshold drops to 300ms and conservative `Δ_minicon = 400ms` overshoots by 100ms.

In the L_0-binding regime the bundle's *effective* bid-layer propagation budget is `B_0 − Δ_minicon`, which exceeds the spec's nominal `B_0_LBid = 0.5·BTT` — providing additional reflood-tail absorption at the bid layer "for free." Under default RefloodDelay this is 600-1000ms of headroom depending on sizing; under RefloodDelay=0 it shrinks to 0-300ms.

**Aggressive sizing's MEV-fetch ceiling.** Under bare OBFT's reflood-aware schedule, L_Bid's leader broadcast is bounded above by bare OBFT V_0's deadline (the L_0 constraint binds at every named sizing under default RefloodDelay). Aggressive L_Bid therefore matches bare OBFT V_0 in broadcast time and MEV-fetch budget; conservative and standard sizings at default RefloodDelay do the same (they fit inside the 1000ms reflood-buffer for free). At `RefloodDelay = 0` the buffer shrinks to 300ms; conservative `Δ_minicon = 400ms` is the only sizing that overshoots — paying 100ms (= 0.5·BTT) MEV-fetch cost. Standard and aggressive remain free at RefloodDelay=0.

**Timeline comparison** (Config A, BTT=200ms, **default RefloodDelay=700ms**, L_0 layer; deeper layers broadcast earlier and are off-table):

| Time (ms) | Bare OBFT | L_Bid conservative | L_Bid standard | L_Bid aggressive |
|---|---|---|---|---|
| 2500 | **leader broadcast** (`T_broadcast_max_0`) | **leader broadcast** (`T_0_broadcast_max`; L_0 constraint binds) | **leader broadcast** | **leader broadcast** |
| 2600 | **typical-mesh arrival** | **typical-mesh arrival** (bundle in `bid_set_i` early) | **typical-mesh arrival** | **typical-mesh arrival** |
| 3200 | (reflood window idle) | **`T_0_arrival`** (mini-consensus begins) | bid-set settling | bid-set settling |
| 3300 | (reflood window idle) | bid-set settling | **`T_0_arrival`** | bid-set settling |
| 3400 | (reflood window idle) | **verdict broadcast** (`T_verdict`; `Δ_verdict = 1·BTT`) | **verdict broadcast** | bid-set settling |
| 3500 | (reflood window idle) | verdict propagation | verdict propagation | **`T_0_arrival = T_verdict`** (`Δ_verdict = 0.5·BTT`) |
| 3600 | **σ/NR commit** (`T_commit`); reflood-tail cutoff | **σ/NR commit** (`T_commit`) | **σ/NR commit** | **σ/NR commit** |

Post-broadcast budget allocation (broadcast → `T_commit`, at default RefloodDelay=700ms; total = `B_0 = 1100ms` across all variants since L_0 constraint binds):

| Variant | Total | Breakdown |
|---|---|---|
| Bare OBFT | 1100ms (= 2·BTT + RefloodDelay) | 100ms typical-mesh propagation + 1000ms reflood-absorption buffer (idle in healthy case) |
| L_Bid aggressive | 1100ms | 100ms typical-mesh propagation + 900ms free reflood-buffer headroom + 100ms `Δ_verdict` (sub-P99 verdict propagation) |
| L_Bid standard | 1100ms | 100ms typical-mesh propagation + 700ms free reflood-buffer headroom + 100ms `Δ_select` settling + 200ms `Δ_verdict` (1·BTT P99 verdict propagation) |
| L_Bid conservative | 1100ms | 100ms typical-mesh propagation + 600ms free reflood-buffer headroom + 200ms `Δ_select` settling + 200ms `Δ_verdict` |

At **RefloodDelay = 0** (fully-meshed opt-out) the post-broadcast budget shrinks to 400ms (= `2·BTT`); conservative's `Δ_select + Δ_verdict = 400ms` matches the available budget exactly (zero free headroom), so its broadcast shifts 100ms earlier (paying 100ms MEV-fetch). Standard and aggressive still fit free.

Reading the gradient column-by-column (default RefloodDelay):

- **Aggressive** matches bare OBFT V_0 in broadcast time and post-broadcast layout — the post-arrival window is split between free reflood-buffer headroom (900ms) and `Δ_verdict` propagation (100ms). Zero MEV-fetch cost vs bare OBFT V_0; lowest L_Bid success rate (no settling, sub-P99 verdict propagation).
- **Standard** also matches bare OBFT V_0 in broadcast time at default RefloodDelay (the reflood buffer absorbs its 300ms `Δ_minicon` for free). Zero MEV-fetch cost vs bare OBFT V_0; medium L_Bid success rate (0.5·BTT settling + P99 verdict propagation).
- **Conservative** matches bare OBFT V_0 in broadcast time at default RefloodDelay (the reflood buffer absorbs its 400ms `Δ_minicon` for free). Zero MEV-fetch cost vs bare OBFT V_0; highest L_Bid success rate (1·BTT settling + P99 verdict propagation). At RefloodDelay=0 it's the only sizing that pays MEV-fetch cost (100ms).

### Protocol

#### Phase 1 — Rotation-leader broadcast with bid metadata

Each rotation leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Fetches a candidate `V_{L_k}` from the host (e.g., MEV-Boost relay or vanilla beacon-node-built block) with `bid_value` (relay-attested or 0).
2. Constructs its normal `Phase1Bundle`, extended with bid metadata `(bid_value, relay_attestation)` for that same `V_{L_k}`.
3. Signs the Phase-1 envelope with its operator-identity key and gossips the bundle via gossipsub. The V-keypair partial `σ_{L_k}^V(V_{L_k})` remains exactly the same Phase-1 leader contribution as in bare OBFT.

Non-rotation operators do not publish independent bids in this design. The bid universe is the set of L_Bid-eligible Phase-1 bundles.

Bid retention is the existing Phase-1 bundle retention keyed by `(slot, layer k, leader_id)`. For L_Bid argmax computation, honest operators count the **first-observed** valid bid metadata for each retained eligible bundle. Subsequent signed bundles from the same `(slot, layer, leader)` with either a distinct V or distinct bid metadata are dropped from convergence input but retained as slashing evidence (base leader-equivocation for distinct V; Rule 7 for bid-metadata equivocation on the same V).

Because the bid data is carried inside the Phase-1 bundle, bid/bundle coherence is structural: the bid is for the bundle's own `V_{L_k}`.

#### Mini-consensus — verdict broadcast

Each operator `i`, by `T_verdict = T_commit − Δ_verdict`:

1. Computes `bid_set_i` = set of received-and-validated L_Bid-eligible Phase-1 bundles first-observed before `T_verdict` — Phase-1 cryptographic checks pass, bid metadata is well-formed, and host-validity verdict = valid. Bundles first-observed *after* `T_verdict` do not contribute to `i`'s `bid_set_i` for verdict computation, but are still retained for Phase-1 processing and slashing-evidence purposes. Host-validity follows the same locking semantics as bare OBFT's Phase-1 bundle validation (see [§Head-change handling](#head-change-handling)): on first-observation of `V_{L_k}`, the host validates `V_{L_k}` against a stable head snapshot taken at that observation moment and **locks the verdict (valid/not-valid) for the remainder of the slot**. Subsequent host-validity checks on `V_{L_k}` (in particular, the L_Bid Phase-2 emit-time check below) are reads of this locked verdict, never re-evaluations against a moved head. Bundles whose locked verdict is not-valid are excluded from `bid_set_i`. Optional host-supplied filters MAY further restrict the set: parent-root within cluster-recognized set (see [Application: SSV Ethereum proposer duty](#application-ssv-ethereum-proposer-duty) for the SSV-specific filter); relay/builder attestation verification (see [§Optional extension — relay/builder attestation verification](#optional-extension--relaybuilder-attestation-verification)). Bundles that fail any enabled filter are excluded from `bid_set_i`.
2. Computes `predicted_LBid_i`:
   - If `|bid_set_i| ≥ qBid = K − f` AND optional parent-root filter passes: `predicted_LBid_i = argmax_{V in bid_set_i} bid_value`, with `(layer, leader_id)` tiebreak on equal bids.
   - Else: `predicted_LBid_i = null` (insufficient visibility or no consensus reachable from `i`'s view).
3. Constructs `KindBidVerdict` binding the prediction. Signs with operator-identity key; gossips.

Operators broadcast verdict as late as possible while preserving the configured verdict propagation budget: at `T_verdict = T_commit − Δ_verdict`. Larger `Δ_select` (= `Δ_minicon − Δ_verdict`) gives more time for post-`T_0_arrival` bundle visibility before verdicting; smaller values preserve MEV-fetch budget but rely more heavily on mesh propagation being tight.

A second distinct `KindBidVerdict` from `i` for the same slot is verdict-equivocation, slashable via Rule 8. Honest receivers count `i`'s first-observed verdict toward convergence; subsequent verdicts are dropped from convergence input but recorded for slashing.

#### Convergence rule for L_Bid

At mini-consensus end (`T_commit`), each operator computes:

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

L_Bid preserves OBFT's per-layer commitment model and removes the old bid/bundle coherence problem by construction.

**Coherence — bid data is part of the bundle.** A rotation leader L_k commits to a single V per slot:

- The bid metadata is carried inside the Phase-1 bundle whose V is signed as `σ_{L_k}^V(V_{L_k})`; there is no separate bid message that can point at a different V.
- EKM enforces single-σ-V per `(slot, layer)`, so signing two distinct V's at the same `(slot, L_k)` is rejected for honest leaders and slashable for byzantine leaders under the base leader-equivocation rule.
- Distinct signed bid metadata for the same `(slot, layer, leader, V)` is L_Bid-specific bid-metadata equivocation, covered by Rule 7 below.

**Independence — cross-layer commitments:**

- Non-rotation operators have no independent L_Bid candidate in this design. They still participate in mini-consensus by validating retained eligible Phase-1 bundles and broadcasting `KindBidVerdict`.
- An operator's σ-or-NR commitment at one layer does not constrain another layer. A rotation leader L_k whose bid loses L_Bid still σ-emits at L_k on their (matching) bundle V if rotation-layer σ-eligibility holds; the L_Bid outcome and the L_k σ-decision are independent at the operator level.
- L_Bid σ-quorum on `V_X` reconstructs `(V_X, S)`; rotation-layer signatures are not used in that case. Conversely, L_Bid NR-quorum unlocks rotation-layer decryption; whichever rotation layer reaches σ-quorum supplies the output.

### Why L_Bid is the outermost chained-encryption gate

Chained encryption wraps L_0..L_{K-1} σ partials under `nr_tag_LBid` (outermost) plus the existing `nr_tag_0..nr_tag_{k-1}` chain. Decrypting any rotation-layer σ partial requires L_Bid NR-quorum first. This is a deliberate design choice with a concrete trade-off:

- **Benefit — cryptographic enforcement of L_Bid priority.** If the cluster reaches L_Bid σ-quorum on `V_X`, no rotation layer can produce an output (their σ partials remain encrypted). Pigeonhole 3's induction extends to K' layers: at most one V signature reconstructs cluster-wide, even when honest operators σ-commit at multiple layers. The bid-routed value is preferred when consensus reaches at L_Bid; rotation values are only accessible after L_Bid NR-quorum signals "no bid winner".
- **Cost — no fall-through if L_Bid deadlocks.** Adversarial-byz patterns at L_Bid (2-1-byz-defect, verdict-equivocation; see [§New residual failure modes](#new-residual-failure-modes-at-l_bid)) can produce σ-pool < `qV` AND NR-pool < `qEnc` simultaneously. Without NR-quorum, the rotation-layer chained encryption stays sealed, and rotation-layer σ-pools — even if they would otherwise reach `qV` — cannot reconstruct. The slot misses.

The alternative (L_Bid as a non-gating layer with rotation layers reachable independently) would close the no-fall-through gap but would also let rotation outputs fire whenever an honest operator σ-emits at their rotation layer regardless of L_Bid state, defeating bid-routing priority and creating split-output races. The chosen design preserves bid-routing semantics at the cost of L_Bid adversarial-byz exposure.

### Safety

Identical to bare OBFT.

The mini-consensus does not bind threshold partials. Phase-1 bundle bid metadata and `KindBidVerdict` envelopes contribute zero to either σ-pool or NR-pool — they only influence which Phase-2 emission an operator chooses. EKM enforces single-σ-V per `(slot, layer)` per operator at Phase-2 sign time exactly as in bare OBFT.

Pigeonholes 1, 2, 3 hold unchanged at K' = K + 1 layers. The additional `nr_tag_LBid` gate is structurally a deeper chained `nr_tag` and falls under Pigeonhole 3's inductive argument. Byzantine verdict misbehavior is slashable (Rule 8) but cannot violate cryptographic safety.

### Slashing-evidence rules

Inherits OBFT's 5 rules unchanged. Rule 5's "plaintext layer" binds to L_Bid in this extension (since L_0 is no longer the plaintext layer here — its σ is encrypted under `nr_tag_LBid`); a fake plaintext σ at L_Bid that doesn't verify against any retained eligible Phase-1 bundle in `bid_set` is slashable on the same construction as L_0 in bare OBFT. Two new rules cover L_Bid-specific surfaces:

- **Rule 7 — Bid-metadata equivocation.** Two distinct signed Phase-1 bundle envelopes from the same `(slot, layer, leader)` that carry the same V but different bid metadata (`bid_value` and/or `relay_attestation`). Self-contained slashable evidence — both envelopes are signed by the leader's operator-identity key. If the two bundles carry different V's, the base leader-equivocation rule already applies.
- **Rule 8 — Verdict equivocation / verdict-vs-action equivocation.** Operator `i` either broadcasts two distinct `KindBidVerdict` envelopes for the same slot, OR broadcasts `KindBidVerdict(σV(V_X))` and emits Phase-2 NR partial on `nr_tag_LBid`. Self-contained slashable evidence — both signed messages exist on the wire. The reverse pattern (null verdict + Phase-2 σ on `V_X`) is **not** Rule-8 slashable: it's permitted honest behavior when an operator's `bid_set` was incomplete at the verdict-broadcast deadline but late re-flood completes the view before Phase-2 emit (cluster gains an extra σ-pool contribution; no defection).

**Evidence quality** (paralleling [§Implications of the rational-byzantine deterrent (assumption 4)](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the main spec's rules):

| Fault class | Evidence type | False-positive risk |
|---|---|---|
| Phase-1 bundle equivocation, bid-metadata equivocation, verdict equivocation, verdict-vs-action equivocation (verdict claims `σV(V_X)` and Phase-2 emits NR partial on `nr_tag_LBid`) | Cryptographic, self-contained — single signed message-pair conclusively demonstrates the action | Very low |
| Candidate silence (C1 — withholding own L_Bid-eligible Phase-1 bundle without any alternate emission) | Behavioral pattern — no signed message proves the leader failed to broadcast; observable only via aggregate honest reception failure across slots | Higher — same character as silence-grief in bare OBFT |

The same asymmetry as in bare OBFT applies: high-evidence-quality faults (clear equivocation) are also the ones the protocol handles cleanly within the slot (verdict-quorum reaches or doesn't, cluster falls through); low-evidence-quality faults (silence, including the candidate-silence component of C1's withholding-then-defect residual) are the load-bearing adversarial-byz attacks that engineer slot-miss-without-fall-through. The rational-byzantine deterrent's strength is correspondingly weakest where adversarial grief is most damaging — same structural property as bare OBFT, surfaced at L_Bid as well.

### Liveness

#### Recovery scope at L_Bid

The mini-consensus addresses the three deadlock classes from [§Background — bid-layer deadlocks](#background--bid-layer-deadlocks). C3 closes cleanly (3-of-4 majority); C1 and C2 close only when no V reaches `verdict_quorum`, otherwise fold into [2-1-byz-defect](#new-residual-failure-modes-at-l_bid):

- **C1 — Selective candidate withholding.** Byz withholds its L_Bid-eligible Phase-1 bundle from a subset of honest peers. **Closed when no V reaches `verdict_quorum`** (byz withholds widely enough that affected peers verdict null or on non-byz candidates → pool fragments below `qV` → fall-through). **Not closed when byz withholds from exactly the minority** — at `K = n = 4`, minority still has `|bid_set| = qBid = 3` and verdicts on its non-`V_X` argmax; byz adds its own verdict on `V_X` to reach `qV`, then withholds σ at Phase 2 (NR-emit or silent). Folds into [2-1-byz-defect](#new-residual-failure-modes-at-l_bid).
- **C2 — Candidate / bid equivocation.** Byz sends conflicting signed Phase-1 bundle views to different peers. **Closed when no V reaches `verdict_quorum`** (verdicts split widely enough that no V reaches `qV` even with byz's own verdict → fall-through). **Not closed when byz aligns its verdict with the majority-honest first-observation** to push `verdict_pool` to `qV`, then withholds σ at Phase 2 (NR-emit or silent). Folds into [2-1-byz-defect](#new-residual-failure-modes-at-l_bid).
- **C3 — Validity-divergence majority on V_LBid (3-of-4 at f=1, n=4).** 3 honest verdict `σV(V_X)`, 1 honest verdict NV/null. `verdict_pool[V_X] = 3 = qV` → cluster σ-binds on `V_X`. The dissenting honest NRs at L_Bid; the σ-pool reaches `qV` from the V_X-side honest. **Closed for 3-of-4 majority.**

#### Recovery scope at rotation layers L_0, ..., L_{K-1}

Identical to bare OBFT. Mini-consensus failure cleanly falls through to L_0 via NR-quorum at L_Bid. Bare OBFT's K-layer fall-through and three-state commitment model apply unchanged at rotation layers.

#### New residual failure modes at L_Bid

Four failure modes at the bid layer, all resulting in slot-miss without fall-through (chained encryption to L_0 stays sealed when L_Bid has neither σ-quorum nor NR-quorum). The first two are **Class B** (permitted byzantine grief within the f-bound; same taxonomy as bare OBFT's [§Failure modes](#failure-modes)) — slot-miss per-slot, mixed evidence quality (cryptographic for some trigger/action combos, behavioral for silent variants), deterred across slots by [assumption 4](#implications-of-the-rational-byzantine-deterrent-assumption-4). The last two are **Class A** (assumption violations) — partial verdict propagation under assumption 2, and 2-2 validity-divergence under assumption 3:

- **[Class B] 2-1-byz-defect.** Byz engineers `verdict_quorum` on `V` that some honest don't retain at Phase 2, casts its own verdict on `V` to make the quorum, then withholds σ at Phase 2 (NR-emit or silent). Same algebraic deadlock either way; evidence varies by trigger × Phase-2 action:

  | Trigger \ Phase 2 | NR-emit | Silent |
  |---|---|---|
  | **Candidate/bid equivocation** (V to majority, V' or different bid metadata to minority) | base leader-equivocation or Rule 7 + Rule 8 (crypto) | base leader-equivocation or Rule 7 only (action behavioral) |
  | **Candidate withholding** (V to majority, withhold from minority; minority verdicts non-V argmax) | Rule 8 only (candidate-silence behavioral) | fully behavioral |

  At f=1, n=4: σ-pool[V] = 2 (majority); NR-pool = 1 (minority) + (1 if byz NR-emit, 0 if silent) ≤ 2 < `qEnc`. Deadlock.
- **[Class B — cryptographically slashable] Verdict equivocation at L_Bid.** Byz issues different verdicts to different peers, fragmenting per-peer verdict-pool views. Some honest see `qV`-quorum on `V_X` (and σ-bind); others don't (and NR). σ-pool < `qV`; NR-pool < `qEnc`. Same deadlock shape. Rule 8 fires on the two-distinct-verdict-envelopes pair regardless of Phase-2 action — cryptographic.
- **[Class A — assumption 2 violation] Partial verdict propagation at sub-1-BTT `Δ_verdict`.** When `Δ_verdict < 1 BTT` (aggressive sizing), verdict propagation operates in P50-P99 territory rather than guaranteed P99. All-honest network with mesh jitter can produce per-receiver verdict-pool divergence: some operators see `verdict_pool[V_X] ≥ qV` and σ-emit on `V_X`; others see `< qV` and NR. At f=1, n=4 with 2 σ-emitters / 2 NR-emitters: σ-pool = 2 < `qV`, NR-pool = 2 < `qEnc`, deadlock at L_Bid blocks L_0 fall-through. Same algebraic shape as 2-1-byz-defect but caused by network timing, not byz action — no slashing applies. Rate scales with the gap between actual mesh propagation P-percentile and the chosen `Δ_verdict`. Eliminated by sizing `Δ_verdict ≥ 1 BTT` (standard or conservative).
- **[Class A — assumption 3 violation] 2-2 validity-divergence at L_Bid.** Hard algebraic limit: when honest split 2-2 on `V_X` validity (and byz aligns to extend the split), no `verdict_pool` reaches `qV` and the NR-pool may also fall short under adversarial byz alignment. The same hard limit applies symmetrically in bare OBFT at L_0 — no protocol decides 2-2 validity divergence at f=1, n=4 without breaking BFT bound symmetry. Not attributable to byz (re-orgs are real-world events); no slashing applies. (See [§Implications of validity-divergence not being recovered (assumption 3)](#implications-of-validity-divergence-not-being-recovered-assumption-3).)

#### Trigger frequency

Candidate-withholding and candidate/bid-equivocation triggers require the byz to be one of the slot's K rotation leaders. Under uniform leader selection that is roughly `K/n` slots (and every slot when `K = n`). Verdict-equivocation remains available to any byz operator every slot because every operator broadcasts a `KindBidVerdict`. Compared with bare OBFT's L_0-only adversarial-byz surface (`1/n` slots under uniform rotation), L_Bid raises trigger frequency when `K > 1`, but no longer assumes a standalone all-operators-bid model.

### Slot timing

Measured from `T_commit` (start of Phase 2 σ-emit; mini-consensus already complete). At Config A (P99=150ms, δ=50ms, tightened `Δ_2 = 200ms`, `ε_3 = 50ms`). The same scenarios apply to L_Bid_New (Appendix E) with V_early replacing V_X at L_Bid and bid_1 replacing V_{L_0} at L_0; rotation-layer scenarios are unchanged since post-`T_commit` timing is identical across bare OBFT, current L_Bid, and L_Bid_New:

| Scenario | Time | Mechanism |
|---|---|---|
| L_Bid σ-quorum reaches early in Phase 2 (early-reconstruct path) | ~`1 BTT ≈ 200ms` | σ-emit propagation completes 1 RTT into Phase 2; operator reconstructs at L_Bid plaintext |
| L_Bid σ-quorum reaches at end of Phase 2 (canonical) | ~`Δ_2 + ε_3 ≈ 250ms` | Full Phase 2 + Phase 3 walk |
| Mini-consensus failed at end of Phase 1 → fall-through to L_0 | ~`Δ_2 + ε_3 ≈ 250ms` | NR-quorum at L_Bid (already determined pre-T_commit) + Phase-3 walk decrypts L_0; L_0 σ-quorum |
| Multi-layer fall-through after L_Bid | ~`Δ_2 + ε_3 ≈ 250ms` | K'-layer walk in Phase 3 (sequential local decryption, no extra RTT per layer) |
| L_Bid 2-1-byz-defect, verdict-equivocation, or partial-propagation deadlock | slot misses | Deadlock at L_Bid blocks fall-through |

Post-`T_commit` timing **matches bare OBFT** since mini-consensus runs pre-`T_commit`. Under the reflood-aware schedule, L_Bid's pre-`T_commit` cost is `max(0, Δ_minicon − (B_0 − B_0_LBid))` of MEV-fetch budget — `Δ_minicon`'s overlap with bare OBFT's reflood buffer (`B_0 − B_0_LBid = 1.5·BTT + RefloodDelay`) doesn't cost extra; only the excess does. At Config A default RefloodDelay=700ms the reflood buffer is 1000ms wide — all three named sizings fit free, so the L_Bid extension is zero-cost on MEV-fetch under default RefloodDelay.

### Deployment envelope by BTT

`T_commit` is back-end-anchored and invariant across bare OBFT and OBFT+L_Bid. What L_Bid changes is the **L_0..L_{K-1} broadcast deadline**, which shifts earlier by `max(0, Δ_minicon − (B_0 − B_0_LBid))` = `max(0, Δ_minicon − (1.5·BTT + RefloodDelay))` to fit mini-consensus pre-`T_commit`. Under the reflood-aware schedule, the primary leader's MEV-fetch budget shrinks only by the excess of `Δ_minicon` over bare OBFT's reflood buffer — zero whenever `Δ_minicon ≤ 1.5·BTT + RefloodDelay`.

The table below shows L_0 broadcast deadline (= MEV-fetch budget at `slot_start = 0`) across BTT regimes at **default RefloodDelay = 700ms** (`T_relay_cutoff = 4.0s`, `submit_headroom = 100ms`, `ε_3 = 50ms`, tightened `Δ_2 = 1·BTT`, `B_0 = 2·BTT + 700ms` for bare OBFT, `B_0_LBid = 0.5 BTT` for L_Bid). `T_commit` scales with BTT as `≈ 3800 − BTT` (= `Relay_cutoff − Δ_2 − ε_3 − submit_headroom − ~50ms jitter`); `T_broadcast_max_0` for bare OBFT = `T_commit − B_0 = 3800 − 3·BTT − RefloodDelay`. Bare OBFT row included for comparison:

| BTT | Bare OBFT | Δ_minicon=2 BTT (conservative) | Δ_minicon=1.5 BTT (standard) | Δ_minicon=0.5 BTT (aggressive) |
|---|---|---|---|---|
| 200ms | 2500ms ✓ | 2500ms ✓ | 2500ms ✓ | 2500ms ✓ |
| 400ms | 1900ms ✓ | 1900ms ✓ | 1900ms ✓ | 1900ms ✓ |
| 600ms | 1300ms ✓ | 1300ms ✓ | 1300ms ✓ | 1300ms ✓ |
| 800ms | 700ms ✓ tight | 700ms ✓ tight | 700ms ✓ tight | 700ms ✓ tight |
| 1000ms | 100ms ✓ tight | 100ms ✓ tight | 100ms ✓ tight | 100ms ✓ tight |
| 1200ms | **−500ms ✗** | **−500ms ✗** | **−500ms ✗** | **−500ms ✗** |

(At default RefloodDelay=700ms the reflood buffer `B_0 − B_0_LBid = 1.5·BTT + 700ms` exceeds every named `Δ_minicon` at every BTT in the table, so L_Bid's broadcast deadline matches bare OBFT V_0 exactly across the board — L_Bid imposes zero MEV-fetch cost under default RefloodDelay. Bare OBFT itself fits up to BTT ≤ 1000ms under default RefloodDelay post-tighten — the unified 1·BTT-per-emission convention extends the envelope by 200ms at every BTT vs the older 2·BTT/emission framing. Deployments at BTT ≥ 1200ms must either use a lower RefloodDelay or accept that the slot doesn't fit.)

**At RefloodDelay = 0** (fully-meshed opt-out) the reflood buffer collapses to `1.5·BTT`, so the L_Bid extension's MEV-fetch cost surfaces — most prominently at conservative sizing:

| BTT | Bare OBFT | Δ_minicon=2 BTT (conservative) | Δ_minicon=1.5 BTT (standard) | Δ_minicon=0.5 BTT (aggressive) |
|---|---|---|---|---|
| 200ms | 3200ms ✓ | 3100ms ✓ | 3200ms ✓ | 3200ms ✓ |
| 400ms | 2600ms ✓ | 2400ms ✓ | 2600ms ✓ | 2600ms ✓ |
| 600ms | 2000ms ✓ | 1700ms ✓ | 2000ms ✓ | 2000ms ✓ |
| 800ms | 1400ms ✓ | 1000ms ✓ | 1400ms ✓ | 1400ms ✓ |
| 1000ms | 800ms ✓ | 300ms ✓ tight | 800ms ✓ | 800ms ✓ |
| 1200ms | 200ms ✓ tight | **−400ms ✗** | 200ms ✓ tight | 200ms ✓ tight |

(At RefloodDelay=0 the L_Bid loss vs bare OBFT = `max(0, Δ_minicon − 1.5·BTT)`. Only conservative `Δ_minicon = 2·BTT` overshoots the buffer by `0.5·BTT`. Negative MEV-fetch means L_0 broadcast deadline is before slot_start; the slot doesn't fit.)

**Net for deployment selection.** At production-typical BTT (200-400ms) with default RefloodDelay, every L_Bid sizing matches bare OBFT V_0's MEV-fetch budget exactly — the trade reduces to "L_Bid success rate vs implementation complexity," not MEV-fetch. The reflood-buffer absorption means L_Bid is essentially free under default RefloodDelay; conservative gives the highest L_Bid success rate (1·BTT settling) at no MEV cost. Under the tightened per-emission sizing, bare OBFT fits up to BTT ≤ 1000ms under default RefloodDelay — the reflood-aware `B_0 = 2·BTT + 700ms` consumes the slot budget but the tighter Δ_2 = 1·BTT recovers 200ms vs the older 2·BTT/emission framing; deployments running BTT ≥ 1200ms must reduce RefloodDelay (the RefloodDelay=0 envelope reaches BTT ≤ 1200ms tight). At RefloodDelay=0 (denser meshes / mesh-friendly deployments), conservative `Δ_minicon = 2·BTT` is the only sizing that pays MEV-fetch cost (0.5·BTT); standard and aggressive remain free. The L_Bid trade has two knobs: `Δ_minicon` controls L_Bid success rate (larger Δ_minicon → more bundle tail-absorption) and — only when it exceeds the reflood buffer — MEV-fetch budget; `Δ_verdict` controls verdict-propagation safety (≥ 1·BTT for P99 guarantees). Choose both from production telemetry. Bare OBFT (no `Δ_minicon`) remains available when the trade isn't favorable.

### Optional extension — relay/builder attestation verification

The protocol-level mitigation for [§Additional assumption — bid-value honesty](#additional-assumption--bid-value-honesty). When enabled, the cluster cryptographically rejects bids with unattested `bid_value` claims.

**Wire format.** The Phase-1 bundle's bid section carries the relay/builder's signature over `(V, bid_value)` plus the relay/builder identity (or a key reference into a cluster-recognized identity set). When the extension is disabled, the field MAY be empty or host-supplied; the wire format is unchanged across enable/disable, so clusters can toggle the check without coordinating on schema.

**Validation rule.** During mini-consensus bid-set computation, each receiver additionally checks: `relay_attestation` verifies against a cluster-recognized relay/builder identity AND binds the same `(V, bid_value)` pair carried in the Phase-1 bundle's bid section. Bundles that fail are excluded from `bid_set_i` and contribute nothing to argmax.

**What it closes:**

- **Bid-value inflation** — byzantine cannot claim higher `bid_value` than a recognized relay actually committed to.
- **Permanent L_Bid hijack via fake-bid spam** — byzantine leaders' bids are capped by what cluster-recognized relays actually offer them.

**What it does not close:**

- **Selective candidate withholding** (C1) — protocol handling is conditional (see [§Recovery scope at L_Bid](#recovery-scope-at-l_bid)): falls through only when no V reaches `verdict_quorum`. The residual case folds into [2-1-byz-defect](#new-residual-failure-modes-at-l_bid); attestation does not close it.
- **Candidate / bid equivocation** — distinct V equivocation is covered by the base leader-equivocation rule; same-V bid-metadata equivocation is covered by Rule 7.
- **Leader selecting a low-MEV bid from a recognized relay** — within the leader's discretion; not a protocol concern.

**Cost.** One signature verification per L_Bid-eligible Phase-1 bundle per receiver (small relative to overall cryptographic work) plus the bandwidth of carrying the attestation. Verification must complete before `T_verdict`; sign-check throughput should be confirmed for the deployment's relay-set size.

**When to enable.** SSV proposer duty under realistic adversarial conditions (operators may run builders or collude with builders for self-dealing). Default ON for SSV's deployment.

**When safe to disable.** Deployments where bid-value honesty holds via the [institutional / permissioned operator set or post-hoc payment reconciliation paths](#additional-assumption--bid-value-honesty). Bandwidth and CPU savings are modest; the trade-off is the explicit assumption-shift documented in the additional-assumption section.

### Comparison with bare OBFT

| Aspect | Bare OBFT | OBFT + L_Bid mini-consensus |
|---|---|---|
| Slot structure | Phase 1 → Phase 2 → Phase 3 | Phase 1 (with mini-consensus sub-phase at tail) → Phase 2 → Phase 3 |
| Layers | K (rotation-determined) | K' = K + 1 (L_Bid + K rotation-determined) |
| Wire kinds | `Phase1Bundle`, `KindCommit`, `KindCertificate` | `Phase1Bundle` gains bid metadata; + `KindBidVerdict` |
| Slashing-evidence rules | 5 | 7 (+ Rule 7 bid-metadata equivocation, + Rule 8 verdict equivocation/verdict-vs-action) |
| `T_commit` anchor | back-end: `T_relay_cutoff − submit_headroom − ε_3 − Δ_2` | **Same** (`T_commit` invariant across bare OBFT and OBFT+L_Bid; cross-family T_commit anchors differ — see [BFT-comparison.md](BFT-comparison.md#scope-and-assumptions)) |
| Best-case latency post-`T_commit` (early reconstruct) | ~200ms (`1 BTT`) | **Same** (~200ms; mini-consensus runs pre-`T_commit`) |
| Canonical latency post-`T_commit` (full Phase 2 + Phase 3) | ~250ms (`Δ_2 + ε_3` at tightened Δ_2 = 1·BTT) | **Same** (~250ms) |
| Time-to-completion spread (best → canonical) | ~1.25× | **Same** |
| Bandwidth (n=4, K=2 default healthy; cluster-wide totals) | ~6–8 KB across 1 emission per operator (~1.5–2 KB/op × 4 ops at K=2; K=4 up-tier: ~28 KB) | Base bandwidth + K bid-metadata sections + n verdicts + 1 chained encryption layer (no standalone bid envelopes) — 2 emissions per operator (`KindCommit` + `KindBidVerdict`) |
| L_0 broadcast deadline | `T_broadcast_max_0 = T_commit − B_0` (e.g., 2500ms at Config A with tightened Δ_2 = 1·BTT and `B_0 = 2·BTT + RefloodDelay = 1100ms` at default RefloodDelay=700ms) | `T_0_broadcast_max = T_commit − max(Δ_minicon + B_0_LBid, B_0)` — at default RefloodDelay the L_0 constraint binds, so deadline coincides with bare OBFT V_0 (2500ms at Config A) for every named `Δ_minicon`. See [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening). |
| MEV-fetch budget (4s cutoff, `header_submit_headroom = 100ms`, §Application's max-MEV anchor) | ~2350ms (V_0; T_commit = 3.60s) at default RefloodDelay; ~3050ms at RefloodDelay=0 | At default RefloodDelay: ~2350ms (V_X) at every named sizing (= bare OBFT V_0; reflood buffer fully absorbs `Δ_minicon`). At RefloodDelay=0: ~2950ms at conservative `Δ_minicon = 2·BTT` (vs bare OBFT 3050ms); ~3050ms at standard and aggressive (= bare OBFT V_0) |
| Cryptographic primitives | BLS threshold + threshold IBE/SWE | Same (no new primitives) |
| **Safety** | Cryptographic via Pigeonholes 1, 2, 3 | **Same** |
| Rotation-layer (L_0/.../L_{K-1}) liveness | OBFT base recovery scope | **Same** (mini-consensus failure falls through cleanly; rotation layers unchanged) |
| L_Bid liveness — C1 selective candidate withholding | n/a (no bid layer) | **Closed only when verdict-quorum doesn't form** (withhold from > f honest → fall-through). Otherwise (withhold from exactly the minority + byz pushes verdict via own verdict + defects) falls under 2-1-byz-defect (Class B). |
| L_Bid liveness — C2 candidate / bid equivocation | n/a | **Closed only when verdict-quorum doesn't form** (verdicts split widely → fall-through). Otherwise (byz aligns its verdict with majority-honest + defects) falls under 2-1-byz-defect (Class B). |
| L_Bid liveness — C3 validity-divergence majority (3-of-4) | n/a | **Closed** (verdict-quorum reaches on majority; minority NR) |
| L_Bid liveness — 2-1-byz-defect | n/a | **Class B**: slot-miss-without-fall-through; mixed evidence (base leader-equivocation or Rule 7 under candidate/bid equivocation, Rule 8 under NR-emit, behavioral for silent variants); assumption-4 deterred across slots |
| L_Bid liveness — verdict-equivocation | n/a | **Class B** (slashable Rule 8; assumption-4 deterred across slots): slot-miss-without-fall-through |
| L_Bid liveness — partial verdict propagation (sub-1-BTT `Δ_verdict`) | n/a | **Class A** (assumption 2 violation): all-honest partial-propagation deadlock at L_Bid; rate scales with mesh propagation distribution; eliminated by `Δ_verdict ≥ 1 BTT` |
| L_Bid liveness — 2-2 validity split | shared hard algebraic limit at L_0 (BFT-theoretical, not protocol-specific) | **Class A** (assumption 3 violation): same hard limit at L_Bid; not attributable, no slashing |
| Bid-routing value capture | n/a | Highest-bid eligible rotation-layer block on healthy path |
| Adversarial-byz trigger frequency at the bid layer | n/a | Candidate withholding/equivocation when byz is among K rotation leaders (`K/n` under uniform selection, every slot at `K=n`); verdict-equivocation every slot |

**Net trade vs bare OBFT**: pays `max(0, Δ_minicon − (B_0 − B_0_LBid))` = `max(0, Δ_minicon − (1.5·BTT + RefloodDelay))` of MEV-fetch budget — zero at every named sizing under Config A default RefloodDelay (the reflood buffer absorbs all three sizings free); 0.5·BTT only at conservative `Δ_minicon = 2·BTT` under RefloodDelay=0 (fully-meshed opt-out). Plus additional adversarial-byz residual surface at L_Bid (slot-miss-without-fall-through; mixed evidence quality) and an all-honest Class A residual at sub-1-BTT `Δ_verdict`, in exchange for bid-routing value capture on the healthy path. The L_0/.../L_{K-1} layers' recovery scope is unchanged. Under the reflood-aware schedule the MEV-fetch lever has collapsed — whether favorable depends primarily on bid-routing value capture vs slot-loss cost (byzantine and partial-propagation residuals) at the chosen `Δ_minicon` / `Δ_verdict`.

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

- **No additional defense against h_V=1 byz selective-delivery deadlock**. The withhold-then-fake-σ variant is closed by the absence of the Defer state (a `T_commit` receiver without V immediately NR's, so the withholding byz produces a clean NR-quorum and fall-through to L_1; see [§Where this came from](#where-this-came-from)). The selective-Phase-1-delivery variant remains an algebraic limit at f=1, n=4. Re-broadcast can't help with the latter — byz only delivered Phase-1 to one honest, and that's already what gossipsub auto-forward delivers from.
- **No additional defense against σ-locked equivocation 1-1-1 splits**. These failures are about cross-phase exclusivity locking honest into different σ commits, not about V propagation speed. Re-broadcast doesn't change the algebra.
- **No defense against silent leader**. If the leader doesn't broadcast at all, no non-leader has the bundle to re-broadcast. Recovery is via NR-quorum fall-through to L_1 (in-protocol).
- **No defense against sustained partition** (real propagation > absorption window). Re-broadcast can't deliver what no honest peer has received.

### Costs

- **Bandwidth**: O(n) multiplier per bundle. At n=4 K=2 default, leader-only broadcast is 2 publishes per slot (one per layer's leader); non-leader re-broadcast adds 6 more publishes (each non-leader publishes each leader's bundle). At Config A's ~6–8 KB cluster-wide onion bandwidth at K=2, this adds a few KB. K=4 up-tier: 4 publishes leader-only + 12 non-leader re-broadcast, ~30 KB onion bandwidth. Modest at small n; meaningful at n=13 where it becomes ~10× the bundle bandwidth.
- **Gossipsub deduplication overhead**: each peer receives the same auth-signed bundle from multiple sources. Gossipsub deduplicates by message ID (content hash for self-validating messages), so this is mostly CPU/memory cost on each peer. Minor.

### σ_L^V re-inclusion is part of the core protocol

The σ_L^V witness re-inclusion mechanism — each operator including byte-for-byte copies of received Phase-1 σ_L^V partials, paired with a 32-byte `value_root`, in their own `KindCommit` — is part of the core protocol; see the Wire format paragraph in §Phase 2. Bandwidth is modest (per-witness ≈ 145 bytes = Layer + Leader + ValueRoot + σ partial + length-prefix overhead; cluster-wide at K=2 n=4 default ≈ 1.2 KB; K=4 up-tier ≈ 2.3 KB); it adds no new EKM event, no new signing obligation, and no new cryptographic primitive (operators forward bytes they already received); Pigeonholes 1, 2, 3 are unchanged. The mechanism protects σ_L^V against Phase-1 bundle drop at peer receivers who DID receive V (via the `value_root` cross-reference into their σ-pool), but does **not** address V-drop — receivers without V locally rely on `KindCertificate` gossip (see §Final-certificate gossip), or, if the deployment opts into it, the full-bundle re-broadcast described elsewhere in this appendix.

### Recommendations by cluster size

- **n = 4**: skip explicit re-broadcast. Gossipsub auto-forward is single-hop; mesh is effectively full. The bandwidth cost has no offsetting benefit.
- **n = 7**: marginal. Mesh sampling adds 1-2 hops; explicit re-broadcast may compress P99 propagation by 50-100ms. Worth measuring; not a clear win.
- **n ≥ 10-13**: explicit re-broadcast becomes more useful as mesh sampling adds more hops. Score-based pruning and peer-score variance also become more significant. Consider explicit re-broadcast as defensive engineering, especially in deployments with adversarial peer behavior or aggressive scoring config.

### Conclusions

1. **OBFT's spec relies on gossipsub auto-forwarding**, not on explicit application-layer re-broadcast by non-leaders. The cluster-wide propagation guarantee comes from gossipsub's standard mesh-forwarding behavior under partial synchrony.
2. **Explicit non-leader re-broadcast is a latency/bandwidth trade-off, not a safety/liveness change**. The OBFT protocol's stated guarantees don't depend on whether non-leaders explicitly re-broadcast — they're property of gossipsub's propagation under partial synchrony.
3. **Explicit re-broadcast addresses gossipsub-layer issues** (mesh sparsity, score-pruning, multi-hop latency) but does not address OBFT's residual adversarial-byz failure modes (σ-locked equivocation, validity-divergence). Those are structural and not fixable at the propagation level. (h_V=1 selective Phase-1 delivery, previously also in this list, is now closed in-protocol via the §Phase 2 peer-reflood-V mechanism.)
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

- OBFT: K is configurable; default K=2 (1 fall-through, L_0 → L_1); K=4 up-tier (3 fall-throughs, L_0 → L_1 → L_2 → L_3). Fixed per slot.
- OBFT-replenish: K grows with rounds, fall-through depth = 2R. At R=3 rounds (typical fit in 4s slot budget), 6 layers ≈ 5 fall-throughs.

OBFT-replenish has **deeper fall-through** at the cost of round-by-round retry. Useful if multi-leader-silent patterns are observed.

#### Adversarial-byz failure modes

- OBFT: σ-locked equivocation 1-1-1 and validity-divergence — slot-miss patterns deterred via Assumption 4 across slots (see §Liveness, §Failure modes). `h_V=1` selective Phase-1 delivery is closed in-protocol under healthy mesh via the §Phase 2 peer-reflood-V mechanism; under degraded mesh it remains a slot-miss pattern with the Assumption-4 deterrent as residual defense.
- OBFT-replenish: **identical exposure to bare OBFT.** New layers introduced in later rounds are subject to the same residual byzantine patterns (equivocation, validity-divergence; degraded-mesh h_V=1). If byz exercises σ-locked split at L_0 in round 1, the L_0 σ-locks block fall-through; later layers' chained encryption stays sealed under L_0/L_1's nr_tags. Replenishment does not close any of these patterns.

R-invariant: replenishment changes neither the cross-phase σ-or-NR algebra nor the leader's Phase-1 σ_V locking, so the same residual patterns persist regardless of layer count or staging.

#### Slot timing complexity

- OBFT: single set of phase deadlines (`T_commit`, relay-submission deadline); single Phase 1/2/3 cycle.
- OBFT-replenish: per-round deadlines (`T_commit_r` plus a round-duration); round-transition signaling needed.

OBFT-replenish's slot scheduling is closer to OBFTR's complexity than to bare OBFT's.

### When OBFT-replenish is worth the complexity over bare OBFT

- **Healthy-dominated production environments** (most slots succeed at L_0 or L_1): bandwidth saving is real; freshness advantage materializes on late-MEV slots; multi-round complexity is the cost.
- **High-MEV proposer duty with late-moving MEV**: round-r leaders' freshness advantage (vs OBFT's all-in-round-1 fetch) captures MEV that bare OBFT misses regardless of K.
- **Clusters with intermittent operator unreliability**: deeper fall-through depth (2R vs OBFT's K=4 up-tier; K=2 default has only 1 fall-through) provides more recovery against silent-rotation operators.

### When bare OBFT is preferable

- **Implementation simplicity is valuable**: OBFT has one round, one set of phase deadlines, fixed K, no cross-round atomicity. Replenish needs variable K, cross-round σ-locks, round-transition coordination — closer to OBFTR territory.
- **Failure rate is uncertain or stress-test environments**: OBFT's flat K=2 default bandwidth (~6–8 KB cluster-wide) is predictable; replenish's bandwidth grows with rounds and can exceed OBFT past round 2 (especially at K=4 up-tier).
- **Adversarial-byz-heavy deployments**: replenish doesn't add adversarial-byz coverage; extra rounds are wasted under byz patterns.
- **Late BFT-start (e.g., BFT_start ≥ 2s, slot budget ≤ 1.25s)**: tight budget doesn't admit multi-round retries. Replenish's recovery depth is gated by round count, which is gated by budget. At late BFT-start, replenish reduces to roughly OBFT-with-K=2 with no recovery advantage.

### Conclusions

1. **OBFT-replenish is positioned as a multi-round enhancement of OBFT**, but its operational complexity profile (cross-round σ-locks, round-transition signaling, per-round Phase 1) is closer to OBFTR than to bare OBFT. The naming "enhancement of OBFT" is structurally fair — it keeps OBFT's chained-encryption + per-layer commitments machinery — but the protocol implementation effort is comparable to OBFTR.
2. **The genuine OBFT-replenish-specific advantage is V freshness at later rounds**, which neither OBFT (any K) nor OBFTR (re-flood, no fresh fetches) provides. For late-MEV proposer slots, this is structurally meaningful.
3. **Healthy-case bandwidth saving is real but conditional** — OBFT-replenish wins in round-1-success scenarios (replenish's K=2 instead of OBFT's K=4 up-tier bandwidth) and breaks even at round 2; past round 2, bandwidth exceeds OBFT. Whether favorable depends on observed round-1-success rate. (Note: OBFT's default is now also K=2; this comparison is against the K=4 up-tier baseline.)
4. **Adversarial-byz exposure is unchanged from bare OBFT.** Replenish does not provide additional protection against the residual patterns σ-locked equivocation, validity-divergence, or degraded-mesh `h_V=1` selective Phase-1 delivery; closing those would require structural changes orthogonal to replenishment. (Healthy-mesh `h_V=1` is closed in bare OBFT via the §Phase 2 peer-reflood-V mechanism, so neither bare OBFT nor OBFT-replenish needs additional protection there.)
5. **Healthy-path latency is identical to OBFT**. Reduced layer count in round 1 doesn't compress consensus time — the Phase 1/2/3 cycle is the bottleneck.
6. **OBFT-replenish vs OBFT trade summary**:
   - **+** Bandwidth saving on healthy slots (real if round-1-success dominates).
   - **+** V freshness at later rounds (genuine MEV upside for late-resolving slots).
   - **+** Deeper fall-through at the cost of multi-round retry.
   - **−** Multi-round implementation complexity comparable to OBFTR.
   - **−** Bandwidth grows past OBFT in multi-round failure cases.
   - **=** Same adversarial-byz exposure as OBFT (R-invariant patterns unfixed).
   - **=** Same healthy-path latency.

Treat OBFT-replenish as a research direction worth specifying further if the late-MEV freshness motivation is significant for the target deployment. If the priority is simplicity and predictable behavior, bare OBFT (K configurable; K=2 default, K=4 up-tier) remains the cleaner choice. Replenishment is orthogonal to the structural changes that would be needed for adversarial-byz coverage.

## Appendix E — OBFT + L_Bid_New (deep-bid mini-consensus)

> **Note on current-schedule compatibility:** L_Bid_New was drafted against an earlier per-layer staggered backup schedule, where deep-layer leaders had positive `B_k` budgets that could be tightened (`T_broadcast_max_k = T_deep_arrival − B_k_LBid`) to fit a deep-layer mini-consensus window before `T_commit`. Under bare OBFT's current primary-vs-backup schedule (see [§Setting](#setting)), deep-layer leaders broadcast at BFT_start (`B_k = T_commit`) and have no MEV-fetch budget to preserve — so L_Bid_New's structural premise ("preserve primary L_0 MEV by tightening only deep-layer deadlines") loses its motivation: under the current schedule there's no MEV cost being saved at deep layers, since deep layers already fetch deepest-confirmed-parent. The design is preserved here as a candidate framing in case future spec revisions reintroduce per-deep-layer MEV-fetch budgets (e.g., a partial reversal of the primary-vs-backup shape for specific deployment profiles). A full L_Bid_New re-derivation under the current schedule is a follow-up if/when the extension is implemented; the formulas below reference `T_deep_arrival`, `T_broadcast_max_k = T_deep_arrival − B_k_LBid` for `k ≥ 1`, and `Δ_minicon` shifts on deep-layer deadlines that no longer make sense as written.
>
> **Note on stale primary-MEV-fetch comparison:** The motivational claim below — that L_Bid_New gains 300ms (conservative) / 200ms (standard) / 0 (aggressive) of primary MEV-fetch over current L_Bid — reflects an older L_Bid sizing where Δ_minicon directly compressed the primary's broadcast deadline. Under bare OBFT's current reflood-aware schedule (see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening)), current L_Bid's reflood-buffer absorbs Δ_minicon for free at every named sizing under default RefloodDelay (= 700ms), so current L_Bid *already* matches bare OBFT V_0 (~2350ms) — leaving L_Bid_New with **zero** primary-MEV gain to offer at default RefloodDelay. At RefloodDelay=0, current L_Bid pays 100ms only at conservative; L_Bid_New's gain there is 100ms (not 300ms). Treat the 200-300ms figures throughout this appendix as historical motivation, not as current operating-point numbers.

This appendix specifies **L_Bid_New** as a candidate alternative to the L_Bid mini-consensus design ([Appendix B](#appendix-b--l_bid-mini-consensus-extension)). The two extensions share the same goal — opportunistic bid-routing across eligible rotation-layer Phase-1 candidates — but L_Bid_New trades a different set of properties. Where L_Bid puts the bid-routing winner V_X on the outermost (plaintext) onion layer and reaches it via cluster-wide convergence on **all K rotation-layer bids**, L_Bid_New restricts cluster-wide convergence to **deep-layer bids only** (V_early) and incorporates the primary's late bid (bid_1) at an inner onion layer, gated by chained encryption.

The structural trade vs current L_Bid: L_Bid_New preserves bare OBFT V_0's primary MEV-fetch budget (~3050ms) at every sizing by excluding bid_1 from mini-consensus, at the cost of exposing bid_1 to bare-OBFT-style asymmetric-delivery patterns at the bid layer (where current L_Bid uses mini-consensus to converge on bid_1's status). The primary-MEV-fetch gain vs current L_Bid is sizing-dependent: 300ms at conservative, 200ms at standard, **0 at aggressive** (where current L_Bid already matches bare OBFT V_0 because `Δ_minicon = 0.5 BTT`). Post-`T_commit` latency is the same order as current L_Bid because both variants run mini-consensus before `T_commit`. The timing distinction is pre-`T_commit`: current L_Bid starts mini-consensus at `T_0_arrival` and shifts every eligible rotation-layer deadline earlier; L_Bid_New starts mini-consensus at `T_deep_arrival` and shifts only deep-layer deadlines earlier.

L_Bid_New is documented here as a candidate design point. Whether it is preferable to current L_Bid in production is deployment-dependent — see [§E.6 Comparison with current L_Bid](#e6--comparison-with-current-l_bid).

### E.1 — Setting

L_Bid_New extends OBFT's setting in the same way L_Bid does — bid metadata inside Phase-1 bundles, mini-consensus convergence on a bid winner — with these structural differences:

- **K' = K + 1 layers** (same as L_Bid): an additional bid-layer prepended to OBFT's K rotation-determined layers.
- **Mini-consensus window = deep-arrival to commit**: `Δ_minicon = T_commit − T_deep_arrival`. Mini-consensus starts at `T_deep_arrival`, ends at `T_commit`, and all L_Bid_New mini-consensus timing derives from that interval. `T_deep_arrival` is the deterministic point by which honest deep-layer Phase-1 bundles (`L_1..L_{K-1}`) broadcast at their L_Bid_New deadlines are expected to have reached all honest operators under their propagation budgets. Deep leaders use `T_broadcast_max_k = T_deep_arrival − B_k_LBid` for `k ≥ 1` (same opportunistic tighter-budget shape as current L_Bid — bid-routing is non-mandatory; see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening)); the primary `L_0` uses the bare OBFT broadcast deadline and is not shifted by `Δ_minicon`.
- **Mini-consensus scope = deep bids only**: the mini-consensus runs over `{V_i : operator i is L_k for k ≥ 1}` — i.e., the bids associated with the deeper-layer rotation leaders. The primary's bid (operator at L_0) is *not* in the mini-consensus bid set. The deep visibility threshold is `qBid_deep`, normally `(K − 1) − f` when the deep candidate set has enough layers to tolerate f byz omissions; deployments with smaller deep candidate sets must configure this threshold explicitly.
- **Verdict propagation budget** `Δ_verdict`: `0 < Δ_verdict ≤ Δ_minicon`, with `T_verdict = T_commit − Δ_verdict`. Operators compute and broadcast `KindBidVerdict` at `T_verdict`; those verdicts propagate until `T_commit`. The remaining `Δ_select = Δ_minicon − Δ_verdict` is the in-window deep-bid settling budget after `T_deep_arrival` and before verdict broadcast.
- **Primary-bid placement** (vs current L_Bid): V_early (the mini-consensus winner over deep bids) is the **outermost** plaintext layer; bid_1 (= the primary's bid = V_{L_0}) is at the inner L_0 layer encrypted under `nr_tag_LBid`. The encryption layout is *structurally identical* to current L_Bid (both have L_Bid plaintext outer, L_0 wrapped under `nr_tag_LBid`, deeper layers chained via `nr_tag_0..k-1`); the change is in the *content* of the mini-consensus — current L_Bid's V_X is argmax over all eligible bids (including primary's bid_1), while L_Bid_New's V_early is argmax over deep bids only. Primary's bid is removed from mini-consensus and lives only at L_0.

The key intuition: under L_Bid_New, the cluster outputs `argmax(V_early, bid_1)` via the chained-encryption fall-through structure rather than via a single mini-consensus over all K rotation-layer bids. Operators σ-emit on V_early at L_Bid when V_early ≥ bid_1 (in their local view); they NR at L_Bid otherwise (preferring fall-through to L_0 where bid_1's σ-pool aggregates). The chained encryption gates L_0 reconstruction on L_Bid NR-quorum — preserving the cluster's single-output guarantee (Pigeonhole 3).

### E.2 — Wire kinds

L_Bid_New uses the same wire framing as L_Bid ([Appendix B](#appendix-b--l_bid-mini-consensus-extension)):

- **`Phase1Bundle` bid section**: bid metadata carried by each eligible rotation leader's Phase-1 bundle. There is no standalone `KindBid` message.
- **`KindBidVerdict`**: mini-consensus verdict, broadcast over deep-layer Phase-1 bundle bids only.
- **`KindCommit`**: Phase-2 onion + NR-partials + σ_L^V witness section (inherits from base OBFT).
- **`KindCertificate`**: final certificate.

The mini-consensus verdict (`KindBidVerdict`) carries a verdict on V_early specifically — not on the full argmax over all K rotation-layer bids. This is a semantic difference from L_Bid, which verdicts on V_X = argmax over the full eligible rotation-layer bid set.

### E.3 — Per-layer windows and deadlines

L_Bid_New's Phase 1 has two structural elements with different timing anchors:

1. **Deep-layer candidate broadcast**: deep-layer rotation leaders `L_1..L_{K-1}` broadcast their Phase-1 bundles with bid metadata by `T_broadcast_max_k = T_deep_arrival − B_k`. `T_deep_arrival = T_commit − Δ_minicon` is the start of the mini-consensus window, so the deep candidates that can influence V_early are expected to have reached all honest receivers before mini-consensus begins.

2. **Primary Phase-1 broadcast**: primary `L_0` uses the bare OBFT primary deadline (`T_broadcast_max_0` unchanged). The primary bundle carries `bid_1 = V_{L_0}`, `σ_{L_0}^V(V_{L_0})`, and op-identity auth exactly as in bare OBFT. **`Δ_minicon` does not shift the primary deadline.**

The mini-consensus runs over deep-layer bids only during `[T_deep_arrival, T_commit]`. Operators compute and broadcast verdicts at `T_verdict = T_commit − Δ_verdict`, leaving `Δ_verdict` for verdict propagation. Primary's bid_1 is broadcast in parallel and may arrive before or after `T_verdict`; it is intentionally excluded from `bid_set_i_deep` and is evaluated locally at Phase 2.

| Phase | Window | Activity |
|---|---|---|
| Phase 1 (deep candidates) | `[slot_start, T_deep_arrival]` | Deep-layer leaders `L_1..L_{K-1}` broadcast Phase-1 bundles with bid metadata by `T_broadcast_max_k = T_deep_arrival − B_k`. |
| Mini-consensus (deep bids) | `[T_deep_arrival, T_commit]` | Operators compute V_early = argmax over `bid_set_i_deep` first-observed by `T_verdict = T_commit − Δ_verdict` and broadcast `KindBidVerdict`; verdicts propagate by `T_commit`. |
| Phase 1 (primary) | `[T_broadcast_max_0^bare, T_commit]` | Primary broadcasts Phase-1 bundle (bid_1 + `σ_{L_0}^V`); broadcast happens at the bare-OBFT primary deadline and overlaps the deep mini-consensus. |
| Phase 2 | `[T_commit, T_commit + Δ_2]` | σ-or-NR commit at K' layers (L_Bid + L_0..L_{K-1}). |
| Phase 3 | `[T_commit + Δ_2, T_soft_end]` | K'-layer reconstruction walk. (`T_soft_end = T_commit + Δ_2 + ε_3` — the soft per-operator target for reconstruction completion; the slot's relay-submission cutoff at `T_relay_cutoff − T_submit` is the only hard wall.) |

Sizing — `Δ_minicon` is the total deep mini-consensus interval, `Δ_verdict` is the portion reserved for verdict propagation, and `Δ_select = Δ_minicon − Δ_verdict` is the deep-bid settling buffer. Same shape and meaning as in current L_Bid, applied only to deep bids:

- **Conservative** `Δ_minicon = 2 BTT`, `Δ_verdict = 1 BTT`, `Δ_select = 1 BTT`: highest deep-bid success rate; P99 verdict propagation + 1 BTT deep-bid tail-absorption.
- **Standard** `Δ_minicon = 1.5 BTT`, `Δ_verdict = 1 BTT`, `Δ_select = 0.5 BTT`: medium deep-bid success rate; P99 verdict propagation + 0.5 BTT deep-bid tail-absorption.
- **Aggressive** `Δ_minicon = 0.5 BTT`, `Δ_verdict = 0.5 BTT`, `Δ_select = 0`: lowest deep-bid success rate; sub-P99 verdict propagation + zero deep-bid tail-absorption. Partial-propagation deadlock becomes a Class A residual, same as current L_Bid's aggressive sizing.

`T_commit` is the same back-end anchor as bare OBFT (`Relay_cutoff − 2 BTT = 3600ms` at Config A post-tighten). L_Bid_New pays the `max(0, Δ_minicon − 0.5 BTT)` pre-`T_commit` cost only on deep-layer candidate deadlines; the primary L_0 MEV-fetch budget remains `T_broadcast_max_0^bare − slot_start` ≈ 3050ms at RefloodDelay=0 (= bare OBFT's primary budget; ≈ 2350ms at default RefloodDelay=700ms). Post-`T_commit` timing matches current L_Bid and bare OBFT.

### E.4 — Protocol

#### E.4.1 — Phase 1: Deep bids + primary bundle

Each deep-layer rotation leader broadcasts its Phase-1 bundle with bid metadata by `T_broadcast_max_k = T_deep_arrival − B_k` for `k ≥ 1`. The primary additionally broadcasts its Phase-1 bundle (bid_1 + `σ_{L_0}^V` + op-auth) at `T_broadcast_max_0^bare` per bare OBFT.

As in L_Bid, bid/bundle coherence is structural: each bid is the bid metadata carried by the same Phase-1 bundle whose V is signed by the layer leader. Deep bundles serve as mini-consensus candidates; the primary's bundle serves as bid_1 and as the L_0 candidate, but not as a mini-consensus input.

#### E.4.2 — Mini-consensus: verdict on V_early

Each operator `i`, by `T_verdict = T_commit − Δ_verdict`:

1. Computes `bid_set_i_deep` = received-and-validated deep-layer Phase-1 bundles with bid metadata, first-observed before `T_verdict`. Deep bundles first-observed after `T_verdict` remain retained for ordinary rotation-layer processing and slashing evidence, but do not affect this verdict. **The primary's bid (bid_1) is NOT in `bid_set_i_deep`** — primary's bid is treated separately at L_0.
2. Computes `predicted_LBid_i`: `argmax over bid_set_i_deep` if `|bid_set_i_deep| ≥ qBid_deep`; else null, where `qBid_deep` is the configured visibility threshold for the deep candidate set.
3. Broadcasts `KindBidVerdict` carrying their prediction.

At mini-consensus end (`T_commit`):

- `verdict_pool[V] = | { distinct ops j broadcasting first-observed KindBidVerdict(j, slot, hash(V)) } |`
- `verdict_quorum_V_early ≡ ∃V : verdict_pool[V] ≥ qV`

If `verdict_quorum_V_early` reaches on some V, that V is the cluster's V_early. Otherwise no V_early; cluster falls through L_Bid via NR-quorum.

#### E.4.3 — Phase 2: σ-or-NR at K' = K + 1 layers

Each operator constructs a K'-layer onion. **L_Bid is the outermost plaintext layer; L_0 is encrypted under `nr_tag_LBid`** (encryption layout structurally same as current L_Bid; difference is content — V_early at L_Bid, bid_1 at L_0):

```
layer L_Bid (V_early, plaintext):  σ_i^V(V_early)
layer L_0 (bid_1, encrypted):       E_{nr_tag_LBid}( σ_i^V(bid_1) )
layer L_k (k ≥ 1, chained):         E_{nr_tag_LBid}( E_{nr_tag_0}( ... σ_i^V(V_{L_k}) ) )
```

Honest σ-or-NR commitment per layer:

- **L_Bid**:
  - σ on V_early IF: `verdict_quorum_V_early` reached AND operator retains V_early locally AND host validity-locked verdict on V_early is valid AND **(operator does NOT have bid_1, OR bid_1 ≤ V_early)**.
  - NR on `nr_tag_LBid` otherwise. Specifically NR if: mini-consensus failed OR operator doesn't have V_early OR (operator has bid_1 AND bid_1 > V_early).

  The conjunction `(no bid_1 OR bid_1 ≤ V_early)` is the critical rule: operators σ on V_early when they're certain V_early is the cluster-preferred winner; NR otherwise. Operators without bid_1 default to σ on V_early (rather than NR-when-uncertain), since they cannot rule out V_early winning.

  **Note**: An alternative rule is "NR-when-uncertain" (NR if no bid_1, regardless). The two rules differ in their failure modes — see [§E.5 Failure modes](#e5--failure-modes). The σ-when-uncertain variant is recommended; NR-when-uncertain has a complementary failure mode at a different honest-split configuration. Formal analysis of the choice is in `docs/OBFT-formal-verif.md`.

- **L_0**: σ on bid_1 if received and host-valid (standard bare OBFT rule for L_0); NR otherwise. As the onion diagram above shows, L_0's σ partial is wrapped under `nr_tag_LBid` — so reconstructing at L_0 (whether σ-quorum on bid_1 or fall-through to L_1) requires L_Bid NR-quorum to unlock the chained encryption (same gating as current L_Bid; see [§Why L_Bid is the outermost chained-encryption gate](#why-l_bid-is-the-outermost-chained-encryption-gate)).
- **L_k for k ≥ 1**: same as bare OBFT.

Cross-phase exclusivity per `(slot, layer)` and single-σ-V per `(slot, layer)` per operator continue to hold across all K' layers; EKM enforces.

#### E.4.4 — Phase 3: K'-layer reconstruction walk

```
1. L_Bid: σ-pool[V_early] plaintext. If ≥ qV: reconstruct (V_early, S); halt.
2. Else: NR-pool[L_Bid] check. If ≥ qEnc: aggregate decryption key; unlock L_0.
3. L_0: σ-pool[bid_1] (decrypted). If ≥ qV: reconstruct (bid_1, S); halt.
4. Else: NR-pool[L_0] check. If ≥ qEnc: unlock L_1; continue.
5. Walk L_1..L_{K-1} as bare OBFT.
```

If L_Bid reaches neither σ-quorum nor NR-quorum, the chained encryption to L_0 stays sealed; no fall-through; slot misses. Same structural property as current L_Bid (chained-encryption priority enforces single-V output).

### E.5 — Failure modes

L_Bid_New's failure modes split into (a) modes inherited from bare OBFT at L_0 and below, and (b) modes specific to the L_Bid layer's σ-or-NR commitment under asymmetric bid_1 delivery or deep mini-consensus divergence.

#### E.5.1 — Inherited from bare OBFT

When L_Bid NR-quorums and falls through to L_0, L_0 behaves as bare OBFT's L_0 (with bid_1 as V_{L_0} and the primary's σ_{L_0}^V as the leader's contribution). Inherits all bare-OBFT L_0 failure modes:

- **σ-locked equivocation at L_0** (Class B): byz primary equivocates bid_1; honest σ-lock on different V's; deadlock at L_0 with chained-encryption seal preventing fall-through.
- **h_V=1 selective Phase-1 delivery at L_0** (Class B): byz primary broadcasts bid_1 to one honest only; σ-pool fragments.
- **Asymmetric propagation of primary's bundle past `T_commit`** (Class A): violates assumption 2 for L_0; cluster falls through to L_1 if propagation symmetric there.

These are bare OBFT's failure modes restated; same Class A / Class B framing.

#### E.5.2 — Specific to L_Bid_New's L_Bid layer

The L_Bid layer's σ-or-NR commitment depends on each honest's local view of (V_early, bid_1, V_early-vs-bid_1), and on whether honest operators converge on the same V_early verdict set by `T_commit`. Asymmetric bid_1 delivery (one honest doesn't have bid_1) creates an honest-split at L_Bid:

- **Asymmetric bid_1 delivery + V_early > bid_1**: honest with bid_1 σ on V_early (per the σ-when-uncertain rule's `bid_1 ≤ V_early` clause); honest without bid_1 σ on V_early (per the rule's `no bid_1` clause). All `n−f` honest σ at L_Bid → σ-pool[V_early] = n−f = 3 = qV at f=1, n=4 → σ-quorum reaches → output V_early. **No deadlock at f=1, n=4 under either non-grief or adversarial byz** (byz σ adds; byz NR or silence doesn't subtract from honest σ-pool). This case is exactly what σ-when-uncertain handles cleanly — the rule's purpose is to keep operators-without-bid_1 in the σ-camp on V_early. NR-when-uncertain would 2-1-split honest here (honest with bid_1 σ; honest without bid_1 NR) and produce the complementary residual.

- **Asymmetric bid_1 delivery + bid_1 > V_early**: honest with bid_1 NR at L_Bid (prefer bid_1); honest without bid_1 σ on V_early (per the σ-when-uncertain rule). Mixed split. Under non-grief at f=1, n=4 with 2 honest having bid_1, 1 honest without: σ-pool = 1, NR-pool = 2. Neither reaches qV/qEnc=3. **In-assumption deadlock under σ-when-uncertain — Class A or B depending on the asymmetric-delivery cause** (network-tail propagation past `T_commit` → Class A; byzantine-crafted targeted delivery → Class B; the protocol cannot distinguish them). See [§E.5.3](#e53--comparison-which-asymmetric-delivery-deadlocks-are-class-a-vs-class-b) for the detailed split.

  - Under the NR-when-uncertain rule, this case recovers cleanly (all 3 honest NR → NR-quorum at L_Bid → fall-through to L_0 → σ-pool[bid_1] = 2 + primary's σ_{L_0}^V = 3 = qV). NR-when-uncertain has its complementary residual at V_early > bid_1 (where it would 2-1-split honest at L_Bid).
  - **The choice of σ-vs-NR-when-uncertain trades which asymmetric-delivery configuration deadlocks**. Neither rule eliminates the residual at f=1, n=4 (this is an algebraic limit per [§E.7](#e7--algebraic-floor)); the choice picks which corner case is the residual.

- **Verdict-equivocation by byz on V_early**: byz emits different verdicts to different honest peers; honest local-verdict-views diverge; some honest σ at L_Bid on V_early, others NR. Same algebraic deadlock pattern. Class B.

- **Selective deep-candidate withholding / equivocation**: same mini-consensus residual shape as current L_Bid, but scoped to `L_1..L_{K-1}` only. If no V_early reaches verdict quorum, L_Bid NR-quorums and falls through to L_0 cleanly. If byz helps form a verdict quorum and then NR-defects or stays silent in Phase 2, L_Bid can deadlock before L_0 unlocks. Evidence quality mirrors current L_Bid: cryptographic when the trigger/action includes signed equivocation or signed NR-after-verdict, behavioral for silence variants.

- **Partial verdict propagation under aggressive `Δ_verdict`**: if `Δ_verdict` is sized below the deployment's honest-message propagation bound, honest operators may not receive the same `KindBidVerdict` set by `T_commit`. This is the same Class A residual as current L_Bid's sub-1-BTT verdict-propagation mode, now over deep-bid verdicts only.

#### E.5.3 — Comparison: which asymmetric-delivery deadlocks are Class A vs Class B

Asymmetric bid_1 delivery can arise from:

- Network mesh failure (broken link, mesh sparsity): violates assumption 2 if propagation exceeds `B_0`. **Class A.** Slot-miss is out of the protocol's recovery scope by design.
- Byzantine primary signing-and-broadcasting bid_1 selectively (sends to gossipsub but only to a subset of peers): if byz uses standard gossipsub, this is a network-level outcome (peers see what gossipsub delivers). If byz crafts targeted delivery, this counts as active byz grief (deviation from honest broadcast). **Class B** in the latter case.

Per the threat model in `docs/OBFT-formal-verif.md`, selective delivery via standard gossipsub is treated as a network-level property (not byz grief); the protocol cannot distinguish byz-induced from network-induced asymmetry, and both are out of scope for the Class A closure property.

### E.6 — Comparison with current L_Bid

The two extensions are structurally distinct points in the L_Bid design space.

| Aspect | Current L_Bid | L_Bid_New |
|---|---|---|
| Mini-consensus interval | `[T_0_arrival, T_commit]`, where `Δ_minicon = T_commit − T_0_arrival` | `[T_deep_arrival, T_commit]`, where `Δ_minicon = T_commit − T_deep_arrival` |
| Candidate broadcast deadlines affected by `Δ_minicon` | All eligible rotation-layer leaders `L_0..L_{K-1}` use `T_broadcast_max_k = T_0_arrival − B_k_LBid` (tighter `B_k_LBid` than bare OBFT's `B_k`) | Only deep leaders `L_1..L_{K-1}` use `T_broadcast_max_k = T_deep_arrival − B_k_LBid`; primary `L_0` uses bare OBFT deadline |
| Verdict deadline | `T_verdict = T_commit − Δ_verdict`; verdict over full eligible bid set | `T_verdict = T_commit − Δ_verdict`; verdict over deep bid set only |
| Mini-consensus scope | All K rotation-layer bids (incl. primary) | Deep rotation-layer bids only; primary bid_1 is evaluated locally at Phase 2 |
| Healthy-path bid routing | V_X = argmax over eligible rotation-layer bids | argmax(V_early, bid_1) via σ/NR choice at L_Bid then chained fall-through |
| Onion priority | L_Bid plaintext OUTER (V_X); L_0..L_{K-1} chained INNER | L_Bid plaintext OUTER (V_early); L_0..L_{K-1} chained INNER (L_0 carries bid_1) |
| Primary MEV-fetch budget | ~2750ms at conservative `Δ_minicon = 2 BTT`; ~2850ms at standard `Δ_minicon = 1.5 BTT`; ~3050ms at aggressive (= bare OBFT V_0; convergence buffer repurposed as `Δ_verdict`) | ~3050ms (= bare OBFT V_0; no `Δ_minicon` shift on primary at any sizing) |
| Deep-layer MEV-fetch budget | Deep deadlines shift earlier by `max(0, Δ_minicon − 0.5 BTT)` | Same deep-deadline cost as current L_Bid for `L_1..L_{K-1}` |
| Post-`T_commit` consensus budget | ~250ms (Δ_2 + ε_3 at tightened Δ_2 = 1·BTT; mini-consensus is pre-`T_commit`) | ~250ms (Δ_2 + ε_3 at tightened Δ_2 = 1·BTT) |
| Submission headroom (4s cutoff) | Same post-`T_commit` headroom as bare OBFT (primary MEV-fetch budget broken down in row above) | Same post-`T_commit` headroom as bare OBFT; primary MEV-fetch budget preserved at every sizing |
| Bid_1 protection from asymmetric delivery | ✓ (mini-consensus convergence on bid_1) | ✗ (bid_1 exposed to bare-OBFT-style asymmetric attacks) |
| Deep-bid protection from asymmetric delivery | Mini-consensus convergence applies | Mini-consensus convergence applies to deep bids only |
| Adversarial-byz exposure surface at the bid layer | C1/C2 conditional closure, 2-1-byz-defect, verdict-equivocation, sub-1-BTT `Δ_verdict` residual | bare-OBFT-style L_0 exposure for bid_1 + deep mini-consensus residuals + verdict-equivocation on deep bids |
| Trigger frequency | Candidate withholding/equivocation when byz is among K rotation leaders (`K/n` under uniform selection, every slot at `K=n`); verdict-equivocation every slot | Primary bid_1 exposure when byz is L_0 (`1/n`); deep candidate withholding/equivocation when byz is among `K−1` deep leaders; verdict-equivocation every slot |
| Cryptographic primitives | BLS threshold + threshold IBE/SWE | Same |
| **Safety** | Cryptographic via Pigeonholes 1, 2, 3 | **Same** |
| Number of onion encryption layers | K + 1 (L_Bid plaintext + L_0..L_{K-1} chained, with L_Bid + L_0 gated by `nr_tag_LBid`) | K + 1 (same encryption layout) |

**Net trade**: L_Bid_New shifts the bid-layer Class B exposure from "mini-consensus residuals at f=1" to "bare-OBFT-style asymmetric primary delivery at f=1". Both have Class B residuals at f=1, n=4 — the choice is which residual is more acceptable in the deployment. L_Bid_New's pre-`T_commit` advantage on primary MEV-fetch is sizing-dependent (300ms at conservative, 200ms at standard, 0 at aggressive); post-`T_commit` latency is identical between the variants.

**Whether L_Bid_New is preferable to current L_Bid is deployment-dependent**:

- Deployments with strong relay-attestation enforcement and reliable primary mesh propagation, choosing standard or conservative sizing: L_Bid_New favorable (recovers 200-300ms of primary MEV-fetch budget; bid_1's Class B exposure is narrower in practice when primary's relay attestation is enforced and primary's mesh is reliable). At aggressive sizing the variants are equivalent on primary MEV-fetch budget, so this driver doesn't apply.
- Deployments with adversarial primary or unreliable primary mesh: current L_Bid favorable (mini-consensus convergence on bid_1 protects against more bid_1-asymmetry patterns).
- Deployments at higher cluster sizes (n ≥ 7): bare OBFT's L_0-only Class B exposure decreases proportionally with `n`; current L_Bid's candidate-withholding/equivocation exposure scales with the selected K leaders (`K/n` under uniform selection, every slot at `K=n`), while verdict-equivocation remains every slot. The relative gain of L_Bid_New's MEV-fetch budget recovery is preserved at larger n.

### E.7 — Algebraic floor

Both L_Bid and L_Bid_New are subject to the same algebraic deadlock floor at f=1, n=4: any non-unanimous honest commitment split at any layer is exploitable by adversarial byz to engineer a 2-2 deadlock (σ-pool ≤ 2, NR-pool ≤ 2, neither reaching qV = qEnc = 3). The floor scales uniformly across n ∈ {4, 7, 10, 13}: the deadlock condition is "(h_σ < qV) AND (h_NR < qEnc)" and adversarial byz can engineer this from any non-unanimous honest split.

The protocols differ in *which* adversarial scenarios produce non-unanimous splits, not in whether the algebraic floor exists. Formal verification of this floor and per-protocol-variant exposure is the subject of `docs/OBFT-formal-verif.md`.

### E.8 — When to use L_Bid_New vs L_Bid

**Use L_Bid_New** when:
- Primary MEV-fetch budget is the binding constraint (high-MEV slots, late-discovery relay queries).
- Primary's mesh reliability is high and relay-attestation extension is enforced.

**Use current L_Bid** when:
- Bid_1's Class B exposure must be minimized (adversarial primary likely; primary mesh unreliable).
- MEV-fetch headroom is generous; the `Δ_minicon` cost before `T_commit` is acceptable.
- Deployment prefers protocol-level convergence on every eligible rotation-layer bid (rather than per-operator argmax computation on the late bid).

**Either is acceptable** for production-typical mesh + institutional/permissioned operator sets where primary equivocation is unlikely. The choice is then driven by primary MEV-fetch budget vs which residual surface the deployment prefers; it is not a post-`T_commit` latency choice, because both variants run mini-consensus before `T_commit`.

**Note on aggressive sizing.** At aggressive sizing (`Δ_minicon = 0.5 BTT`), current L_Bid's primary MEV-fetch budget already matches bare OBFT V_0 (~3050ms) — equal to L_Bid_New's. The two variants then differ only in which bids are subject to mini-consensus (current L_Bid: all eligible rotation-layer bids; L_Bid_New: deep bids only) and which Class B residual surface the bid layer exposes. The primary-MEV-fetch advantage L_Bid_New offers vanishes at aggressive sizing.
