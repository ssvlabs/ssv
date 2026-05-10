# OBFT — Single-Round Onion BFT

A single-round agreement protocol for SSV clusters that produces one collective threshold-signed value per "slot" against a hard deadline. OBFT achieves agreement *cryptographically* — a unique cluster-wide output via threshold cryptography over EKM-enforced per-operator commitments — over a configurable K-layer onion structure with parallel leader fall-through. Single-round only: agreement runs once per slot against a single hard deadline; there is no round-change and no cross-round re-flood.

OBFT runs K layers (configurable, `max(2, f+1) ≤ K ≤ n`) in a single agreement round. The K-layer reconstruction walk in Phase 3 is the load-bearing fall-through mechanism. Each operator commits exactly once per (slot, layer) at `T_commit` — σ, NR, or NV — and emits a single combined `KindCommit` message carrying that decision.

OBFT's recovery scope is intentionally bounded. The protocol's absorption is **per-layer staggered**: the primary L_0 absorbs real propagation up to `B_0 = 1 BTT = 200ms` at Config A (= optimistic); deeper backups absorb progressively wider tails, with the deepest layer at K=4 absorbing up to `B_{K-1} = 5.5 BTT = 1100ms` (= last-resort) — see [§Setting](#setting) for the per-layer budget `B_k`. Within these bounds, OBFT recovers all in-envelope cases via K-layer fall-through: healthy path, silent leader, multi-leader fall-through within Phase 3's reconstruction walk (no per-layer RTT). The protocol does NOT recover from **view-divergence** patterns: equivocation (byzantine leader sending different V's to different honest) and validity-divergence (e.g., re-org causing some honest to validate V and others to NV). View-divergence cases either succeed naturally (when the split happens to leave 2-of-3 honest σ-committed on the same V at f=1 n=4, allowing leader's σ_L^V to push the pool to qV) or slot-miss. Equivocation in non-naturally-recovered patterns is **slashable** (the byzantine leader pays the stake-based penalty); validity-divergence is not attributable to anyone and just slot-misses. See [Assumptions and implications](#assumptions-and-implications) for the explicit list.

OBFT explicitly does not provide within-slot partition recovery for asymmetric propagation — bundles arriving past `T_commit` at any honest receiver simply don't count, and the cluster falls through to a deeper layer whose bundle did propagate. This trades partition tolerance for spec simplicity, suited to small clusters (n=4, the SSV proposer-duty default) where the gossipsub mesh is effectively full and asymmetric propagation is rare.

The protocol description below targets `n = 4` (`f = 1`) as the running example, with `K = 4` (i.e., K = n) as the recommended default for SSV proposer duty (every cluster member is a leader at exactly one layer; pigeonhole guarantees ≥3 honest leaders at f=1). K is tunable per duty within `max(2, f+1) ≤ K ≤ n`. SSV's Ethereum proposer duty is used as the running application example. Generalizations to `n ≥ 7` are noted inline where the algebra changes.

## When to use it

**Suited for:** SSV proposer duty under healthy-network partial synchrony (`P99` ≈ 150ms cluster gossipsub P99/P999, i.e. `1 BTT ≈ 200ms`; see [§Setting](#setting)), where OBFT's 2-RTT healthy-path latency plus K-layer parallel leader fall-through is sufficient and round-change machinery is not desired. Also well-suited for high-P99 networks (`P99` ≈ 300–500ms) where multi-round protocols do not fit the 4s relay cutoff but a single round still does. Generally: deployments that prioritize spec/EKM simplicity, more submission headroom, and high-P99 fit.

**Not suited for:** deployments where the gossipsub propagation tail commonly delays L_0's bundle past `T_commit` (L_0's per-layer budget is `B_0 = 1 BTT` at K=4 Config A) AND the cluster needs to preserve V_0's MEV freshness (rather than fall through to a deeper backup's vanilla payload). OBFT's K-layer fall-through still absorbs propagation tails up to `B_{K-1} = 5.5 BTT` (deepest layer; loses MEV freshness on each fall-through), so OBFT alone is fine when the tail rarely exceeds the deepest-layer absorption or when MEV-preservation isn't the priority. This typically becomes load-bearing at n ≥ 10 where mesh sparsity makes asymmetric propagation common. Also not suited for: scenarios requiring host-validity-divergence recovery within a slot (OBFT assumes host validity is unanimous at decision time, see [Assumptions](#assumptions-and-implications); QBFT is the appropriate choice when validity is unstable across the consensus window).

**Also not suited for:** general-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. OBFT gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n = 3f+1` (the BFT-tight setting; matches SSV's deployment configurations `n ∈ {4, 7, 10, 13}`). The running example is `n = 4, f = 1`. The threshold formula `qV = qEnc = 2f+1` below equals `n − f` exactly at this setting; this equality is what makes the Pigeonhole arguments in [§Safety](#safety-cryptographic-honest-majority) hold.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) as the threshold-signing scheme for no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety". At `n = 4`, `qEnc = 3`.

> **Implementation note (Option A reuse).** Implementations may reuse the V-keypair shares as IBE-keypair shares with cryptographic separation achieved via distinct domain-separation tags (DSTs) in the BLS primitive — saving a second DKG. The Pigeonhole 1 algebraic argument depends only on the SHARED threshold (`qEnc = qV = 2f+1`), which is preserved under DST reuse; the "two distinct keypairs" framing above is normative for the safety argument but a single keypair with DST separation is operationally equivalent. SSV's reference implementation defaults to this Option A; see `docs/IBE-INTEGRATION.md`. Implementations that prefer cryptographic key separation (Option B) run the second DKG and key the IBE primitive separately.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts and protocol-level claims, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- **K layers** (`max(2, f+1) ≤ K ≤ n`, configurable; **K ≥ f+2 strongly recommended** — see below) with deterministically-derived leaders: layer 0 with **primary leader** `L_0`, layer 1 with **backup leader** `L_1`, ..., layer `K-1` with `L_{K-1}`. Leaders must be distinct (`L_i ≠ L_j` for `i ≠ j`). For `K = n`, every cluster member is a leader at exactly one layer per slot.

  **Two distinct K bounds:**
  - **`K ≥ f+1` is the BFT-liveness minimum** — ensures at least one layer has an honest leader (by pigeonhole over the f-byz bound). At K < f+1, all leaders could be byzantine and no σ-quorum reaches at any layer.
  - **`K ≥ f+2` is the late-leader-resilience minimum** — ensures at least *two* honest leaders exist, so a single late-broadcasting honest leader doesn't foreclose the slot via the deepest-layer NR-lock pathology (see §Failure modes / Late deepest-layer leader broadcast). At K = f+1 with the single honest leader running late, the slot misses; at K ≥ f+2 a second honest leader provides fall-through redundancy.

  Concrete minimums by f: at `f = 1`, BFT-min `K = 2` but **late-leader-resilient `K = 3` recommended**, with **`K = n = 4` as the OBFT default** for SSV proposer duty (every cluster member leads exactly one layer; maximum honest-leader probability via pigeonhole); at `f = 2, n = 7`, BFT-min `K = 3` but resilient `K = 4` recommended; at `f = 3, n = 10`, resilient `K = 5`.
- **Single agreement round.** OBFT fixes `R = 1`: one Phase 1 → Phase 2 → Phase 3 sequence per slot, no retry, no re-flood across rounds. The slot's reconstruction deadline is the only deadline. Each operator commits exactly once per (slot, layer) at `T_commit` based on what they observed by then; bundles arriving past `T_commit` do not contribute to that layer's σ-pool.
- Per-layer leader-fetch deadlines `T_{K-1} < T_{K-2} < ... < T_1 < T_0`, plus a single cluster deadline `T_commit`. (`T_commit` is the *view-fix point*: each operator commits its stance based on what it observed by `T_commit`. Reconstruction and submission happen after.) The asymmetric fetch times let primary leaders fetch high-MEV values late (`T_0` close to `T_commit`) while deeper layers' leaders fetch safe early values from deeper-confirmed parents (`T_{K-1}` well before `T_0`).
- **Time unit `BTT` (broadcast trip time)** — `P99` is the propagation budget at the deployment's chosen tail percentile (the variable name `P99` is shorthand for the high-percentile propagation latency; deployments may use P99, P999, P9999 etc. as the actual percentile depending on tail tolerance). `δ` is the cluster's clock-skew bound. We define `1 BTT = P99 + δ` — the time needed for one one-way message to propagate from a sender to all honest receivers under partial-synchrony assumptions. This unit is used throughout for time-budget formulas; the underlying `P99` and `δ` are kept distinct only in §Trust model (where partial synchrony is defined) and in safety arguments (Pigeonhole proofs). Concrete sizing at Config A: `P99 = 150ms, δ = 50ms, 1 BTT = 200ms`.

- **Per-layer leader broadcast deadlines `T_broadcast_max_k`** — OBFT uses **asymmetric per-layer broadcast budgets**: the primary `L_0` broadcasts latest with the smallest propagation budget (= freshest MEV); each backup broadcasts progressively earlier with a progressively wider propagation budget. The cluster falls through to whichever layer's bundle actually arrived by `T_commit`.

  General form: `T_broadcast_max_k = T_commit − B_k`, where `B_k` is layer `k`'s propagation budget — sized to cover typical-mesh propagation from `L_k`'s broadcast plus a small buffer for cluster-wide first-observation convergence before any operator commits at `T_commit`. The convergence buffer matters because Phase-1 emits are staggered per-layer; without it, a slow-tail receiver of L_0's bundle could NR-emit while peers σ-emit on the same bundle, splitting the layer's quorum. (Phase 2 doesn't need a comparable buffer: `KindCommit` emits are synchronous at `T_commit`, so view-convergence is not at issue. `Δ_2`'s recommended widening serves a different purpose — jitter cushion for synchronous fan-out propagation; see §Phase 2.) Sizing: `B_0 ≥ 1 BTT` heuristic at Config A (≈0.5 BTT typical-mesh propagation + 0.5 BTT convergence buffer; tunable per deployment); `B_k ≥ B_{k-1}` for `k > 0` (deeper layers have ≥ predecessor's budget); each leader `L_k` broadcasts by `T_broadcast_max_k`. Bundles whose first-observation time is past `T_commit` at any honest receiver are not counted toward σ-quorum at this layer; the cluster relies on K-layer fall-through to a backup layer whose bundle did propagate in time.

  **Sizing intuition.** `L_0`'s budget covers the optimistic case (nominal propagation, maximum MEV fetch); deeper backups absorb progressively wider propagation tails. Concrete K=4 sizing for SSV proposer duty (in §Application): `B_0 = 1 BTT`, `B_1 = 1.5 BTT`, `B_2 = 2.5 BTT`, `B_3 = 5.5 BTT`. The trade-off: `B_0`'s tighter budget gives the primary the longest fetch window (best MEV) but the least propagation safety margin; if real propagation from L_0's broadcast exceeds `B_0` (= bundle arrives past `T_commit`), the cluster falls through to L_1 (whose `B_1` is wider), and so on.
- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

## Assumptions and implications

OBFT's claims hold conditional on a small set of explicit assumptions about the deployment environment. They are consolidated here; the rest of the spec assumes them and refers back to this section rather than re-deriving the trade-offs each time.

### Assumed

1. **Standard BFT trust bound at the tight setting.** `n = 3f + 1`, up to `f` operators may be byzantine. At `n = 4`, `f = 1`. SSV deploys at the BFT-tight bound (`n ∈ {4, 7, 10, 13}` for `f ∈ {1, 2, 3, 4}`); the threshold formula `qV = qEnc = 2f+1` below requires this tightness to give `2f+1 = n − f`, which is what the Pigeonhole arguments depend on. Honest operators run protocol-conformant software (correct EKM rule enforcement, correct host application, correct gossipsub behavior). Byzantine operators may deviate arbitrarily within their f-bound.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `P99` (cluster gossipsub P99/P999) and clock skew `δ`. Safety is unconditional on timing; only liveness depends on this. **OBFT's per-layer absorption is staggered: `B_k = T_commit − T_broadcast_max_k`** — `B_0 = 1 BTT` for the primary up to `B_{K-1} = 5.5 BTT` for the deepest backup at Config A K=4 (see [§Setting](#setting) for the staggered design). Bundles arriving past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum. Real propagation from leader L_k's broadcast to any honest first-observation that exceeds `B_k` causes that layer to fail at that receiver; the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time (deeper layers have wider `B_k`).

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

Phase 1 has K per-layer windows (driven by the asymmetric fetch times): `[T_{K-1}, T_{K-1} + Δ_1]` for the deepest backup, then progressively `[T_{K-2}, T_{K-2} + Δ_1]`, ..., ending at `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "OBFT-v2", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates OBFT Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, other OBFT message kinds, other consensus protocols sharing the same identity key). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, and check the first-observation timestamp — accept bundles whose first-observation time is in `[slot_start, T_commit]`. Bundles first-observed past `T_commit` are not counted toward σ-quorum at this layer (no late acceptance: each operator commits once at `T_commit` based on what they observed by then). Bundles failing cryptographic-auth are silently dropped. (A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.)
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. A `not-valid` verdict turns the operator's commitment into NV (operationally identical to NR — see "Operator commitments — σ, NR, NV" below).

If a leader `L_k` fails to broadcast at all (or broadcasts so late that its bundle arrives past `T_commit` at every honest receiver), that layer is unavailable; the cluster falls through to deeper layers via NR-quorum. If all K leaders fail, the slot is missed.

**Bundle propagation.** Honest receivers re-flood every bundle via standard gossipsub on first observation. In addition, every honest operator re-attaches the full Phase-1 bundle `(V, σ_L^V, σ_L^op)` for every layer they retained as a `bundle_witnesses` entry in their Phase-2 `KindCommit` (see §Phase 2 / Wire format). This provides cluster-wide V availability under within-budget partial-synchrony: any single retainer's `KindCommit` carries the bundle to all peers via gossipsub, so non-retainers recover both V and the leader's threshold partial — closing V-drop in addition to σ_L^V-drop. Honest leaders broadcasting by their per-layer deadline `T_broadcast_max_k = T_commit − B_k` reach all honest within partial-synchrony assumptions for that layer's propagation budget `B_k` (see §Setting); if any single honest receives, the bundle is cluster-wide-available by `T_commit + Δ_2` via the re-flood section.

**Retention bounds.** Retention state for accepted Phase-1 bundles is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **one auth-valid `(V, σ_{L_k}^V, σ_{L_k}^{op})` tuple** — the first auth-valid bundle observed. If a second auth-valid bundle with a *distinct* `value_root` is later observed, the operator records both bundles in their slashing-protection store as **Rule 2 leader-equivocation evidence** (filed locally, not added to retention or re-broadcast in protocol messages — see §Recovery and failure-mode analysis / Slashing evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Bundles whose cryptographic-auth checks fail are dropped without retention. Retention lifetime: until the operator's local end of Phase 3 (i.e., until reconstruction halts or the slot is declared missed); slot state is then cleared. Memory bound: `O(K)` bundles per slot per operator.

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with honest threshold partials produced in Phase 2 by operators who received the leader's bundle, the cluster reaches `qV` real partials on `V_{L_k}` whenever ≥ qV-1 honest received the bundle. With `bundle_witnesses` re-flood (above), V-drop and σ_L^V-drop are both addressed under within-budget honest propagation: any single retainer's `KindCommit` makes V and σ_L^V cluster-wide visible by `T_commit + Δ_2`, so non-retainers' snapshots include them at trigger-evaluation time (§Phase 2.5).

**Equivocation handling — detect and slash.** If a participant observes two distinct `σ_V` partials from the same leader at the same slot/layer, that's leader equivocation. The pair of signed bundles is self-contained slashable evidence, gossipped for out-of-band slashing.

Local protocol response, by current state at T_commit:

- **Retained 0 V**: the operator has no V to σ on → NR (silent-leader rule).
- **Retained exactly 1 V** (only one bundle reached this operator before T_commit): σ on that V if host validates; otherwise NV.
- **Retained ≥ 2 distinct V** (equivocation observed pre-T_commit): NR. The operator does not attempt to pick a winner; the slot may still succeed if other honest happened to retain only one V and σ on it (Pigeonhole 2 still ensures at most one V reaches qV cluster-wide).

The leader is required to sign `σ_V` exactly once per slot/layer (refreshes in the host's fetch loop are pre-signing only — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance); any second `σ_V` from the same leader is a protocol violation regardless of intent.

**Equivocation is permitted as a slashable byzantine fault.** OBFT does not provide an in-protocol equivocation recovery mechanism. Some equivocation patterns naturally reach σ-quorum on one V (e.g., 2-of-3 honest σ-commit on the same V plus the leader's σ_L^V on that V = 3 = qV at f=1 n=4) — the slot succeeds in those cases as a side-effect, not via a specific protocol mechanism. Other patterns (1-1-1 split where each honest σ-commits on a different V before observing equivocation, or asymmetric-retention patterns where honest see different V-pairs under byz-controlled delivery order) do not reach σ-quorum and the slot misses.

**Practically, an adversarial byzantine controls which pattern occurs.** A byzantine that times equivocation deliveries near the end of Phase 1 (leaving insufficient time for cross-honest gossipsub re-flood to spread the conflict before T_commit) reliably engineers σ-locked split patterns (1-1-1, etc.) that don't reach qV. The natural-recovery cases (2-1 split where 2 honest happen to σ-commit on the same V) only fire when the byzantine fumbles the timing — delivers V's early enough that re-flood converges honest views before σ-emit. **In expectation, byzantine-leader equivocation slot-misses; the rational-byzantine deterrent (assumption 4) is the practical defense, not natural recovery.** A byzantine indifferent to the deterrent (e.g., last-slot-before-exit) can grief reliably; a byzantine that values future participation pays once and stops.

In all cases, the byzantine leader pays the stake-based slashing penalty — equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained on each pair of conflicting bundles.

**Operator commitments — σ, NR, NV.** Each operator commits exactly once per (slot, layer) at `T_commit`, based on what they observed by then. Three states:

- **σ (sign-on-V)**: the operator received the leader's bundle by `T_commit`, both protocol-level and application-level checks passed, and the operator did not retain ≥ 2 distinct V's at this layer (no equivocation observed). Materializes as a σ partial in the operator's `KindCommit` message at this layer (or as the leader's Phase-1 σ for the layer's own leader). Once committed, the operator is σ-locked at this layer until Phase 2.5 finalization; an honest leader who σ'd may emit `KindNRFlip` (additive) post-finalize iff their snapshot satisfies `NRFlipTriggered` (see §Phase 2.5).
- **NR (non-receipt)**: by `T_commit`, the operator did not receive an auth-valid Phase-1 bundle for this layer (the leader is treated as silent from this operator's perspective). A non-leader honest who NR'd may emit `KindSigmaFlip` (additive) post-finalize iff their snapshot satisfies `SigmaFlipTriggered` (see §Phase 2.5).
- **NV (non-validity)**: host application returned `not valid` for `V_{L_k}`.

NR and NV are operationally interchangeable on the wire: both materialize as a partial `σ_i^{IBE}(nr_tag_k)` on the layer's NR tag, carried in the operator's `KindCommit` message. The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to as "NR-quorum" throughout). The distinction is local-only diagnostic. References to "NR" elsewhere in this document encompass NR-silent + NV unless stated otherwise.

Equivocation observed pre-T_commit (≥ 2 distinct V's retained) collapses to NR per the rule above — there is no Defer state. The operator emits NR; cross-phase exclusivity locks them out of σ on either V at this layer (with the narrow Phase-2.5 σ-flip exception under the snapshot-based trigger condition — see §Phase 2.5).

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

**Δ_2 sizing.** `Δ_2 ≥ 1 BTT` minimum (propagation budget for `KindCommit` messages emitted at `T_commit` to reach all honest by start of Phase 3). **Recommended for production: `Δ_2 = 2 BTT`** to absorb mesh-jitter and per-operator processing variance — one full propagation cycle of slack on top of the P99 propagation budget. At Config A (P99=150ms, δ=50ms): minimum `Δ_2 = 200ms`, recommended `Δ_2 = 400ms`. Concrete tables and downstream timing throughout this document use the recommended sizing. (`Δ_2`'s widening absorbs propagation jitter for synchronous KindCommit fan-out — distinct purpose from Phase 1's per-layer convergence buffer inside `B_k`; see [§Setting](#setting).)

The Phase-2.5 σ-flip / NR-flip mechanism (see [§Phase 2.5](#phase-25--σ-flip--nr-flip-flips)) does **not** require an additional sub-window in `Δ_2`: flips are a Phase-3 reconstruction sub-mechanism, emitted by operators whose snapshot satisfies the corresponding trigger at `T_commit + Δ_2`. The emitter includes their own flip partial in their local pool aggregation immediately and reconstructs locally — propagation to other operators is best-effort (via gossipsub + final-certificate gossip) and not load-bearing for cluster slot success.

**Δ_3 sizing.** `Δ_3 ≥ ε_3` (BLS aggregation + IBE decryption walk + certificate construction). At Config A: `Δ_3 ≈ 100ms`. Phase 3 begins when all expected `KindCommit` messages have arrived (i.e., at `T_commit + Δ_2`); reconstruction is local-CPU work. The slot's hard wall is the relay submission deadline `T_relay_cutoff − T_submit`, not a fixed Phase-3-end deadline — a slow operator's reconstruction can spill into submission slack, and `KindCertificate` gossip from a faster peer (see §Final-certificate gossip) lets an operator that hasn't completed local reconstruction submit `(V, S)` directly.

**Wire format: a single auth-wrapped message kind.** Each operator emits exactly one `KindCommit` per (slot, operator) at `T_commit`, carrying:

- The K-layer onion of σ partials (plaintext at L_0, chained-encrypted at deeper layers) for layers where the operator is σ-state.
- NR/NV partials `σ_i^{IBE}(nr_tag_k)` for layers where the operator is NR-state.
- **`bundle_witnesses` section (mandatory leader-bundle re-flood).** For every Phase-1 bundle the operator has retained at this point (per §Phase 1's retention rules — at most one bundle per `(slot, layer, leader_id)` key, since equivocation evidence is logged locally rather than retained), a `(layer k, Phase1Bundle_k)` entry where `Phase1Bundle_k` is the byte-for-byte received Phase-1 bundle (= `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` plus envelope metadata). These are byte-for-byte copies of the leader's signed bundle — **not** new signings by `i` (no EKM event, no new signing obligation, no new cryptographic primitive). The section closes both V-drop and σ_L^V-drop under within-budget honest propagation: any single retainer's `KindCommit` carries the bundle to all peers via gossipsub, so non-retainers recover both V and the leader's threshold partial by `T_commit + Δ_2` (the snapshot point); receivers verify each `Phase1Bundle_k` against the leader's pubkey + envelope re-derivation independently. **Inclusion rule**: include `bundle_witnesses[k]` iff the operator retained an auth-valid bundle for layer `k` by the time of `KindCommit` emission; skip layers where the operator did not retain.

  **Bandwidth**. At Config A (blinded V), per-entry size ≈ V_size + 2 sigs + envelope overhead ≈ V_size + 200 B. With blinded `BeaconBlock` typically ~5–15 KB and worst case ~50 KB on mainnet:

  | n=4, K=4 | Per-op `KindCommit` | Cluster outbound | Per-op ingress |
  |---|---|---|---|
  | Blinded V typical (15 KB) | ~88 KB | ~352 KB | ~264 KB |
  | Blinded V worst (50 KB) | ~228 KB | ~912 KB | ~684 KB |

  See [Appendix C](#appendix-c--leader-bundle-re-flood) for the full design rationale. **OBFT proposer duty operates on blinded blocks only** (see §Application: SSV Ethereum proposer duty); unblinding via relay reveal happens after threshold reconstruction.

Auth-envelope binding: `(protocol_tag = "OBFT-v2", message_kind = "commit", cluster_id, slot, operator_id i, onion_payload, nr_partials, bundle_witnesses)` signed by `i`'s operator-identity key. Emitted at most once per operator per slot. Receivers reject any `KindCommit` whose envelope auth fails verification. (An operator may additionally emit at most one `KindSigmaFlip` or `KindNRFlip` per `(slot, layer)` during Phase 2.5 if the corresponding flip trigger fires from their snapshot — see §Phase 2.5 for the wire format.)

The K-layer onion construction (chained encryption depth `k` at layer `k`) is exactly as defined above.

Each operator includes per layer based on the three-state model from Phase 1:

- **σ-state**: include the (encrypted) σ partial in the onion at this layer.
- **NR-state** (silent-leader, equivocation-observed, or NV): include a partial `σ_i^{IBE}(nr_tag_k)` in the NR-partials section. These IBE partials are the witnesses that unlock the next layer's chained encryption.

**Per-operator commitment is exclusive across phases.** OBFT enforces cross-phase exclusivity. The commitment is *one decision per operator per layer, spanning Phase 1 and Phase 2*, with two narrow Phase-2.5 exceptions (σ-flip for non-leaders, NR-flip for honest leader — see [§Phase 2.5](#phase-25--σ-flip--nr-flip-flips)):

- An operator who emitted `σ_i^V(V_{L_k})` at layer `k` on any value `V` has σ-side committed at this layer; they may **not** σ on a different `V'` at the same `(slot, layer)` — see Pigeonhole 2 in §Recovery analysis / Safety. **Phase-2.5 NR-flip exception**: if the operator is the honest layer-`k` leader and their snapshot at `T_commit + Δ_2` satisfies `NRFlipTriggered`, they may emit one `KindNRFlip` adding an NR partial (additive — the prior σ_L^V remains valid). Without a valid trigger, NR-after-σ is grief / Rule-1 cross-signing.
- An operator who emitted an NR/NV partial on `nr_tag_k` has NR-side committed at this layer; they may **not** subsequently emit σ on any V at L_k. **Phase-2.5 σ-flip exception**: if the operator is a non-leader honest at layer `k` and their snapshot satisfies `SigmaFlipTriggered`, they may emit one `KindSigmaFlip` adding a σ partial on a V observed in their snapshot (additive — the prior NR partial remains valid). Without a valid trigger, σ-after-NR is grief / Rule-1 cross-signing.
- The layer-`k` **leader**'s Phase-1 σ_V counts as their σ-side commitment at layer `k`. The leader's only post-Phase-1 commitment exception is the Phase-2.5 NR-flip above (honest-leader-only).
- **Single-flip-per-layer**: each operator emits at most one of `{KindSigmaFlip, KindNRFlip}` per `(slot, layer)`.
- Across layers, commitments are **independent**: an operator's σ-or-NR commitment at layer `k` does not constrain their commitment at layer `j ≠ k`. Hedging across layers is preserved (an operator may σ at multiple layers if they validated multiple V's).

EKM enforces these exclusivities cryptographically (slashing-protection log keyed on `(slot, layer, side)` with `side ∈ {"σ", "NR", "σ-flip", "NR-flip"}`); see "Preconditions on the host application / Slashing-protection scope" and [EKM coordination model](#ekm-coordination-model).

A byzantine operator that publishes both σ and NR on the same `(slot, layer)` *without a valid Phase-2.5 trigger satisfied from honest receivers' snapshots* is publicly attributable (see §Recovery analysis / Cross-signing detection — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For each layer's leader specifically, "cross-signing" includes any Phase-1 σ + later NR pair on the same slot — except trigger-validated NR-flips per §Phase 2.5.

### Phase 2.5 — σ-flip / NR-flip flips

Phase 2.5 specifies two flip rules — **σ-flip** (any non-leader honest who NR'd) and **NR-flip** (honest leader only) — that allow a single per-(operator, layer) cross-sign emission post-snapshot when narrow trigger conditions are met. Both flips are **additive**: the original σ or NR partial stays in the cluster signed-message-set; the flip adds a partial on the other side. Emission is integrated into Phase 3 reconstruction (no separate phase budget); see [§Phase 3](#phase-3--local-decryption-and-reconstruction-from-t_commit--%CE%94_2) for the walk integration. **What this mechanism recovers and what it does not is described in [§Liveness](#liveness-synchrony-conditional) and [§Failure modes](#failure-modes).**

**Snapshot semantics.** At `T_commit + Δ_2` ("Phase 2 finalization"), each operator finalizes a per-layer **snapshot** of σ/NR partials observed locally from incoming `KindCommit` messages. All flip-trigger evaluation operates against this frozen snapshot — not against the live cluster pool. By design, all honest operators finalize at the same cluster-wide moment, so flips emitted post-finalize do not appear in any other honest's snapshot (no flip-cascade).

**σ-flip rule (any non-leader honest who NR'd).** Eligible: honest operator `i` who emitted NR at `(slot, layer k)` pre-finalize (= NR partial in `i`'s `KindCommit`) and is **not** the leader at `L_k`. Trigger (evaluated against `i`'s own snapshot):

```
SigmaFlipTriggered(i, k) ≡  snap_NR_nl(i, k) < f + 1
                         ∧  snap_S_post(i, k) ≥ A(i, k) + 2f
```

where (all from `i`'s snapshot):
- `snap_NR_nl(i, k)` = non-leader NRs (incl. `i`'s own NR);
- `snap_S_nl(i, k)` = non-leader σ partials (any V);
- `snap_S_post(i, k) = snap_S_nl(i, k) + 1` (post-flip non-leader σ count);
- `A(i, k)` = silent operators (no σ AND no NR in `i`'s snapshot at `k`).

If triggered, `i` emits a single `KindSigmaFlip` adding a σ partial on a `V` they observed in their snapshot. The V-availability precondition (= some operator has signed σ on `V` at `L_k` in `i`'s snapshot) is `VAvailable(i, k, V)` — under within-budget honest propagation + mandatory `bundle_witnesses` re-flood (see [Appendix C](#appendix-c--leader-bundle-re-flood)), `VAvailable` is satisfied cluster-wide for `V_{L_k}` whenever any single honest operator retained the layer-`k` Phase-1 bundle:

```
KindSigmaFlip(slot, layer k, σ_i^V(V), trigger_evidence)
```

The σ partial is verifiable against `V` using the cluster's V-keypair pubkey; receivers also verify `i`'s emission carries valid `trigger_evidence` (= the snapshot pool composition `i` observed) consistent with the receiver's own snapshot. The original NR partial in `i`'s `KindCommit` remains valid and counts toward NR-pool unchanged.

**NR-flip rule (HONEST LEADER only).** Eligible: honest operator `i` who is the leader at `L_k` and emitted `σ_{L_k}^V(V_{L_k})` in Phase 1 (= σ-side committed at `k`). Trigger:

```
NRFlipTriggered(i, k) ≡  i = leader_of(k)
                      ∧  snap_S_nl(i, k) < f
                      ∧  snap_NR_nl(i, k) ≥ A(i, k) + 2f
```

If triggered, `i` emits:

```
KindNRFlip(slot, layer k, σ_i^IBE(nr_tag_k), trigger_evidence)
```

The IBE partial is verifiable against `nr_tag_k` using the cluster's IBE-keypair pubkey. The leader's original `σ_{L_k}^V` from Phase 1 remains valid and counts toward σ-pool unchanged.

**Single-flip-per-layer.** Each operator emits at most one of `{KindSigmaFlip, KindNRFlip}` per `(slot, layer)`. EKM-enforced.

**EKM amendment.** The cross-phase exclusivity rule of [§Phase 2](#phase-2--onion-broadcast-t_commit-t_commit--%CE%94_2) is amended with two narrow exceptions:
- σ-after-NR is allowed at `(slot, layer)` for a non-leader honest operator iff their snapshot satisfies `SigmaFlipTriggered(i, k)`. EKM-recorded as `(slot, layer, "σ-flip")` with trigger evidence.
- NR-after-σ is allowed at `(slot, layer)` for the honest leader iff their snapshot satisfies `NRFlipTriggered(i, k)`. EKM-recorded as `(slot, layer, "NR-flip")` with trigger evidence.

See [§EKM coordination model](#ekm-coordination-model) for the per-request check details.

**Phase-3 sub-mechanism (no extra budget).** Flip emission and local re-aggregation happen during Phase 3 reconstruction at the operator whose snapshot satisfies the trigger. At `n=4, f=1`, the lone-reconstructor path always suffices and cluster slot success doesn't depend on flip propagation, so no extension to `Δ_2` is needed. (For `n ≥ 7` deployments, σ-er-to-σ-er flip propagation may be needed in `k ≥ 2` deadlock cases — see "Caveat at n ≥ 7" below.)

**Reception and aggregation.** Receivers verify each `KindSigmaFlip` / `KindNRFlip` by:
1. Verifying the threshold partial (V-side BLS for σ-flip; IBE for NR-flip) against the emitter's keypair share.
2. Verifying `trigger_evidence` against the receiver's own snapshot for consistency (= the snapshot the emitter claims is consistent with the partials the receiver observed).
3. Confirming the appropriate trigger condition (`SigmaFlipTriggered` or `NRFlipTriggered`) holds in the receiver's snapshot — i.e., the flip is protocol-allowed from the receiver's view.

If all checks pass, the flip's partial enters the appropriate pool (σ-pool[V] for σ-flip; NR-pool for NR-flip) at reconstruction time. If any check fails, the flip is dropped and may surface as Rule-1 cross-signing evidence (see [§Slashing evidence](#slashing-evidence)).

**Healthy-slot cost: zero.** Flip emissions are conditional on the trigger predicates, evaluated locally post-finalize. Healthy slots (where σ-quorum reaches naturally at some layer) elapse Phase 3 without any flip emission. The mechanism is lazy; only fires when a Class B byz pattern would otherwise produce a deadlock recoverable by the narrow trigger conditions.

**Caveat at n ≥ 7.** Local-only flip-and-reconstruct sufficiency is `n=4`-specific. The general algebraic condition is `h_partial + 1 ≥ qEnc / qV`, i.e., `h_partial ≥ 2f`. At `n=4, f=1`, this always holds; at `n ≥ 7`, the `k ≥ 2` deadlock cases require σ-er-to-σ-er flip propagation. Deployments at `n ≥ 7` should account for this in `Δ_2` sizing (one practical option: `Δ_2 ≥ 3 BTT` for inter-flip propagation slack), or rely on `KindCertificate` gossip for slowest-path reconstruction. Concrete timing tables in this document are at `n=4, f=1`; n ≥ 7 deployments require independent timing analysis. The protocol mechanics (commitment lattice, slashing rules, EKM amendment) are unchanged across cluster sizes.

**Per-layer scope.** Flips are evaluated independently per layer. Multi-layer flips at K ≥ 3 are sequenced by chain-unlock order (deadlock detection at deeper layers requires chain unlock at preceding layers; chained-IBE encryption gates `L_{k > 0}`'s σ-pool until NR-quorum at all `L_0..L_{k-1}` is reached). At K=2 the chain has only one level; multi-layer cascade verification at K ≥ 3 is future work.

**Verification status.** The σ-flip / NR-flip rules with snapshot semantics + per-operator views were formally verified for safety (Pigeonholes 1, 2, 3) at `n=4, f=1, K=1` under full byz grief (incl. selective Phase-1 delivery, KindCommit selective delivery, post-snap byz late-publish). Liveness (Class A closure under non-grief byz broadcast) was verified at `n=4, f=1, K=4`. See [docs/OBFT-formal-verif.md §7](OBFT-formal-verif.md) for full results, including the resolution of CE-1 (the safety counterexample that motivated the leader-only restriction on NR-flip).

### Phase 3 — Local decryption and reconstruction (from `T_commit + Δ_2`)

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches on some V (output produced), or NR-quorum reaches (advance to next layer), or neither (slot misses).

**Δ_3 sizing.** Phase 3 is purely local CPU work — BLS aggregation, IBE decryption walk across K layers, certificate construction. So `Δ_3 ≥ ε_3` ≈ 100ms at Config A. (`KindCommit` propagation is already covered by `Δ_2`; by the start of Phase 3, all expected commits have arrived at all honest receivers.) **`ε_3` scales with the number of layers actually walked**: `ε_3 ≈ 100ms` characterizes single-layer reconstruction (σ-quorum reaches at L_0); under multi-layer fall-through, the IBE-decryption walk runs sequentially through each NR-quorum-unlocked layer, so end-to-end Phase 3 cost grows roughly linearly with the number of fall-throughs (e.g., ~`ε_3 × K` ≈ 400ms at K=4 with K−1 silent layers; see [§Liveness comparison / Multi-failure fall-through](#liveness-comparison-obft-vs-obftrr2-vs-qbft)). This isn't a hard sizing constraint because Phase 3 has no fixed end (the slot's wall is the relay submission deadline, not `T_commit + Δ_2 + Δ_3`); a slow operator's reconstruction spills into submission slack — see §Phase 3.

```
L_C = 0                                        # current frontier; advances per loop iteration
sigs[k] for k in [0, K)                        # σ pools, indexed by layer (initially empty)
nrs[k]  for k in [0, K)                        # NR pools, indexed by nr_tag_k (initially empty)

for k in [0, K):
    # Aggregate σ pool at layer k.
    sigs[k] = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
            ∪ {σ_{L_k}^V(V_{L_k}) from peer KindCommit bundle_witnesses sections at layer k, if valid}
            ∪ {σ_j^V(V) from received layer-k onion contents on any value V}
              (decrypted via accumulated NR-decryption keys at layers 0..k-1, if k > 0)
            ∪ {σ_j^V(V) from Phase-2.5 KindSigmaFlip emissions at this layer}
            # deduplicated per operator: leader's Phase-1 σ, bundle_witnesses-section
            # copies of σ_L^V (which collapse to identical bytes across peer
            # KindCommits), onion σ, and KindSigmaFlip σ from the same operator
            # all collapse to one partial per (operator, V).
            # σ-flip's σ partial is on a V the flipper observed in their snapshot
            # and is cryptographically indistinguishable from a Phase-2 σ partial.
            # Per Pigeonhole 2, at most one V can have qV partials cluster-wide,
            # so partition sigs[k] by V and check each.

    nrs[k]  = {σ_j^{IBE}(nr_tag_k) partials from KindCommits AND any Phase-2.5
                KindNRFlip emissions at this layer, deduplicated per operator.
                NR-flip partials and Phase-2 NR partials are indistinguishable
                cryptographically — both are valid IBE shares on nr_tag_k.}

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

**Slot's hard wall: relay submission deadline.** Reconstruction runs from `T_commit + Δ_2` until either σ-quorum reaches (output `V`, halt) or the operator's local relay-submission deadline (`T_relay_cutoff − T_submit`) is reached without a result (slot misses for that operator). Expected reconstruction completion is `T_commit + Δ_2 + ε_3` (`≈ 1 BTT + 100ms` after `T_commit` at Config A under recommended sizing `Δ_2 = 2 BTT`); a slow operator overrunning this can still complete inside the submission slack `[T_commit + Δ_2 + ε_3, T_relay_cutoff − T_submit]`. A faster peer's `KindCertificate` broadcast (see §Final-certificate gossip) lets an operator that hasn't completed local reconstruction submit `(V, S)` directly. Phase-2.5 σ-flip / NR-flip emissions (per §Phase 2.5) fit within `Δ_3`'s local-CPU budget; the emitter aggregates their own flip locally and reconstructs without waiting for propagation.

**Re-running on late `KindCommit` / flip arrivals.** Under nominal partial synchrony, all `KindCommit` and flip messages (`KindSigmaFlip`, `KindNRFlip`) arrive within `Δ_2` and the reconstruction walk above runs once on the stable snapshot at `T_commit + Δ_2`. If messages arrive late (out-of-envelope, after `T_commit + Δ_2`), the operator may re-run the reconstruction walk to incorporate the new partials. This can salvage slots where:

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

A participant that hasn't received `j`'s `KindCommit` at decryption time treats `j` as not having contributed at any layer: no σ partial, no NR partial. Standard threshold cryptography — only signed messages count. Within Phase 2's hard cluster-wide deadline (`Δ_2 ≥ 1 BTT`), gossipsub propagation is expected to deliver all honest `KindCommit` messages to all honest receivers before Phase 3 starts. Phase-2.5 flip emissions (`KindSigmaFlip` / `KindNRFlip`) issued post-finalize propagate best-effort: receivers that observe a flip in time include it in their own pool aggregation; receivers that don't, fall back to `KindCertificate` gossip from the emitter for cluster-level slot success (see §Phase 2.5 / §Final-certificate gossip).

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within the slot's relay-submission deadline (see "Fault tolerance / Liveness"). If more than `f` operators are offline or byzantine combined, neither σ nor NR quorums reach their thresholds and the slot is missed. Within the f-bound, OBFT covers in-envelope cases via K-layer fall-through: healthy path, silent-leader fall-through, multi-leader fall-through (sequential within Phase 3 reconstruction). View-divergence cases — equivocation σ-locked splits and host-validity divergence — are explicitly out of recovery scope (some splits succeed naturally as a side-effect of leader-σ-V counting; rest slot-miss). Asymmetric propagation past `T_commit` is also out of recovery scope (a deeper backup whose bundle did propagate in time is the only recovery path). See [Assumptions and implications](#assumptions-and-implications).

### Slot structure

OBFT runs a single agreement round per slot. The slot proceeds as follows:

1. **Phase 1**: each leader `L_k` broadcasts its Phase-1 bundle by its per-layer deadline `T_broadcast_max_k = T_commit − B_k` (with `B_k` ordered `B_0 < B_1 < ... < B_{K-1}` so deeper layers have wider propagation budgets — see §Setting). Receivers accept bundles first-observed in `[slot_start, T_commit]`.
2. **Phase 2** `[T_commit, T_commit + Δ_2]`: each operator emits a single `KindCommit` message at `T_commit` carrying their per-layer σ partials (for σ-state layers) and NR partials (for NR-state layers). The window is sized for `KindCommit` propagation to all honest peers.
3. **Phase 3** (from `T_commit + Δ_2`): each operator runs the K-layer reconstruction walk. Operators whose snapshot satisfies a flip trigger (per [§Phase 2.5](#phase-25--σ-flip--nr-flip-flips)) emit `KindSigmaFlip` or `KindNRFlip` (as applicable) and immediately include their own flip partial in their local pool aggregation, reconstructing locally. If σ-quorum reaches on some V at any layer, output the V; halt. If NR-quorum reaches (with or without flip contributions) at some layer `L_C < K-1`, advance L_C and continue the walk. If neither, the slot misses (re-running may incorporate late `KindCommit` / flip arrivals — see §Phase 3).

**Slot timing**: Phase 1 fetch occupies `[slot_start, T_commit]`. The slot's consensus budget (Phase 2 + Phase 3) is `Δ_2 + Δ_3 ≈ 1 BTT + ε_3` ≈ 250ms at Config A (recommended sizing `Δ_2 = 2 BTT`); consensus is expected to complete at `T_commit + Δ_2 + Δ_3`, leaving the rest of the slot as submission slack to `T_relay_cutoff`. Phase-2.5 σ-flip / NR-flip is a Phase-3 sub-mechanism with no separate budget; the emitter's local reconstruction completes within `Δ_3`.

## Preconditions on the host application

OBFT is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at the moment the operator considers committing σ to it. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be signed (the σ_V locks it). See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` before committing σ. A `not-valid` verdict turns the operator's commitment into NV (see "Phase 1 / Operator commitments — σ, NR, NV").

The protocol-level checks (cryptographic auth, envelope re-derivation, timing cutoff at `T_commit`) are protocol-internal and do not depend on the application.

For SSV's Ethereum proposer duty, application-level checks include slot match, proposer index match, fork/domain match, parent root match against the operator's view of the head, relay metadata validity, doppelganger / slashing-protection checks, and block encoding well-formedness. See the Application section for details. The protocol body does not enumerate or interpret these — they're host concerns.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot (potentially up to K, one per layer they validated). Constraints per (slot, layer):

- **Each layer's leader signs σ_V exactly once per (slot, layer)** — on the final V they commit to, after any pre-signing refreshes during the host's fetch loop. Refreshes update V via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this single-σ-V-per-(slot, layer) constraint cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational workflow.
- **Each operator commits σ-or-NR exclusively per (slot, layer), across phases — with two Phase-2.5 exceptions.** Honest who include σ on any V at layer `k` may not σ on a different V' at the same layer (single-σ-V per operator per layer). Cross-phase exclusivity is symmetric with two narrow Phase-2.5 trigger-gated allowances:
  - **σ-flip exception** (non-leader honest, σ-after-NR): a non-leader honest who emitted NR/NV at L_k may emit one `KindSigmaFlip` (σ partial on a V observed in their snapshot) iff their Phase-2-finalize snapshot satisfies `SigmaFlipTriggered` (see §Phase 2.5). Without a valid trigger, σ-after-NR is grief / Rule-1 cross-signing.
  - **NR-flip exception** (honest leader only, NR-after-σ): the layer's honest leader who emitted `σ_{L_k}^V` at Phase 1 may emit one `KindNRFlip` (NR partial on `nr_tag_k`) iff their snapshot satisfies `NRFlipTriggered` (see §Phase 2.5). Without a valid trigger, NR-after-σ is grief / Rule-1 cross-signing. **Non-leader honest cannot NR-flip.**

  EKM enforces these exclusivities by coordinating across the operator's V-signing and IBE-signing shares (distinct keys, but slashing-protection log keys on `(slot, layer, side)` with `side ∈ {"σ", "NR", "σ-flip", "NR-flip"}`): an unconstrained NR partial on `nr_tag_k` is rejected if the same operator has previously signed σ at L_k (only the trigger-validated NR-flip path, restricted to honest leader, bypasses this); an unconstrained σ partial on V at L_k is rejected if the same operator has previously signed any IBE partial at L_k (only the trigger-validated σ-flip path, restricted to non-leader, bypasses this); a σ partial on V' at L_k is rejected if the same operator has previously signed σ on V ≠ V' at L_k. Pigeonhole 1 and 2 below rely on these rules.

  **Single-flip-per-layer**: each operator emits at most one of `{KindSigmaFlip, KindNRFlip}` per `(slot, layer)`.
- Every operator signs each layer's V_{L_k} they consider valid (host returns `valid`) in their Phase-2 onion, provided they retained exactly one V at that layer (no equivocation observed). Operators with no V retained at T_commit emit NR; operators with ≥ 2 V's retained also emit NR. See "Phase 1 / Equivocation handling — detect and slash".

EKM/slashing-protection must permit the operator's per-layer Phase-2 σ signings (one σ per layer per slot) plus the leader's Phase-1 σ_V (exactly one per slot/layer the operator is leading), without flagging duplicates — the cluster's safety property collapses these to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating points are **candidate signing** (Phase-1 leader and Phase-2 onion alike) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`).

### EKM coordination model

The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log. Production EKMs (Web3Signer, Dirk, etc.) typically expose per-key signing checks; OBFT requires a coordination layer above them.

**Log schema.** One row per signing event: `(slot, layer, side, value_root, trigger_evidence)` where `side ∈ {"σ", "NR", "σ-flip", "NR-flip"}`; `value_root` is set on σ-side entries (`"σ"`, `"σ-flip"`), null on NR-side (`"NR"`, `"NR-flip"`); `trigger_evidence` is set only on flip entries (= the operator's snapshot composition justifying the flip per §Phase 2.5). No round dimension (single-round protocol).

**Per-request checks.**

- **Sign σ on V at (slot, layer)** (V-keypair share, primary path): rejected if any prior row at `(slot, layer)` exists. On success, log `(slot, layer, "σ", value_root(V), null)`. The single-σ-V rule means a second signing attempt with `V' ≠ V` at the same `(slot, layer)` is rejected — the existing row already contains `value_root(V)`.
- **Sign NR on `nr_tag_k` at (slot, layer)** (IBE-keypair share, primary path): rejected if any prior row at `(slot, layer)` exists. On success, log `(slot, layer, "NR", null, null)`.
- **Sign σ-flip on V at (slot, layer)** (V-keypair share, Phase-2.5 path; non-leader operators only): permitted iff the operator has a prior `(slot, layer, "NR", null, null)` row (= operator is NR-side committed) AND no prior `(slot, layer, "σ", _, _)` row exists AND no prior flip row at `(slot, layer)` exists AND the operator is **not** the leader at L_k AND the request includes valid `trigger_evidence` (= the operator's snapshot composition that satisfies `SigmaFlipTriggered`; EKM verifies the included partials' signatures against operator pubkeys cached at cluster init and checks the trigger arithmetic). On success, log `(slot, layer, "σ-flip", value_root(V), trigger_evidence)`. **This is the only path that emits a σ partial after a same-layer NR**; the strict σ rule above blocks all other σ-after-NR requests.
- **Sign NR-flip on `nr_tag_k` at (slot, layer)** (IBE-keypair share, Phase-2.5 path; honest leader only): permitted iff the operator has a prior `(slot, layer, "σ", value_root(V_{L_k}), null)` row (= operator signed σ_L^V at Phase 1) AND no prior `(slot, layer, "NR", _, _)` row exists AND no prior flip row at `(slot, layer)` exists AND the operator **is** the leader at L_k AND the request includes valid `trigger_evidence` (= the operator's snapshot composition that satisfies `NRFlipTriggered`; same EKM verification as σ-flip above). On success, log `(slot, layer, "NR-flip", null, trigger_evidence)`. **This is the only path that emits an IBE partial after a same-layer σ**; the strict NR rule above blocks all other NR-after-σ requests.

The single-round model means no cross-round atomicity, no persistent partial-sig cache, and no deterministic re-signing fallback are required: each `(slot, layer, operator)` signing event happens at most once per side, and at most one flip per `(slot, layer)` — i.e., the legitimate co-occurrence patterns are `{σ}`, `{NR}`, `{σ, NR-flip}` (honest leader), and `{NR, σ-flip}` (non-leader honest).

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
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `P99` (propagation P99/P999) and clock skew `δ`. Per-layer leader broadcast deadlines `T_broadcast_max_k = T_commit − B_k` (with `B_k` increasing monotonically from primary to deepest backup — see §Setting); bundles first-observed past `T_commit` are not counted. Phase 2's `T_commit + Δ_2` is a **hard cluster-wide deadline**; Phase 3 has no fixed end — reconstruction runs until σ-quorum reaches or the relay-submission deadline forces termination. Late `KindCommit` arrivals can be incorporated by re-running the reconstruction walk (see §Phase 3). Safety holds against arbitrary network adversaries; only liveness depends on synchrony.

  **OBFT's per-layer absorption is staggered**: per-layer budget `B_k` for layer `L_k`, with `B_0 = 1 BTT = 200ms` for the primary up to `B_{K-1} = 5.5 BTT = 1100ms` for the deepest backup at Config A K=4 (see [§Setting](#setting) for the staggered design). Bundles arriving past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum at this layer; the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time (deeper layers' wider `B_k` means slower propagation is still absorbed there).

### Safety (cryptographic, honest-majority)

**Claim:** at most one full `V` signature is ever produced per OBFT instance per slot — across any layer, on any value, across any combination of σ sources — cluster-wide, against an offline-aggregating byzantine within the f-bound, regardless of which honest aggregation rules are followed. (See [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic) for what this scope does and doesn't cover.)

The proof rests on three pigeonhole arguments — Pigeonholes 1 and 2 (single-layer) plus Pigeonhole 3 (chained encryption at `K > 2`). All three are properties of cluster-wide signed messages, enforced cryptographically by [EKM coordination model](#ekm-coordination-model) rules at the operator level — no honest-aggregation rule required.

The pool definitions used in the arguments below:

- **σ-pool on V at L_k**: `{σ_i^V(V) partials at L_k}`, deduplicated per operator. Includes the leader's Phase-1 σ_V when valid. Once a partial is emitted, it stays on the wire — no "revocation" semantics.
- **NR-pool at L_k**: `{σ_i^{IBE}(nr_tag_k) partials}`, deduplicated per operator.

**Pigeonhole 1 — σ-vs-NR at the same layer.** σ-quorum on `V` (any V) at `L_k` and NR-quorum on `nr_tag_k` cannot both reach. The argument is an **algebraic-cardinality mutex** over honest-non-leader contributions, valid in three cases by which Phase-2.5 flip (if any) fired at `L_k`. The mutex relies on within-budget partial-synchrony for honest broadcasts (Assumption 2), which makes honest-side contributions visible cluster-wide — byz selective delivery only varies byz contributions across honest views, and byz contributions are bounded by f.

Honest non-leader cardinality at `L_k` (with leader honest) is `n − f − 1 = 2f` at `n = 3f+1`. Let `s_h` = honest non-leader σ count, `nr_h` = honest non-leader NR count. Cross-phase exclusivity at signing time (EKM-enforced; pre-snap σ XOR NR per honest non-leader): `s_h + nr_h = 2f`. (If leader is byzantine, NR-flip is unavailable a priori and the argument simplifies — handled implicitly in case A below.)

**Case A: no flip fired at L_k** (the bare-OBFT case).

- σ-quorum on V: `h_σ + byz_σ ≥ qV = 2f+1` (where `h_σ` counts honest with σ partials on V at L_k from any phase, deduplicated per operator).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Cross-phase exclusivity: `h_σ + h_NR ≤ n − f = 2f+1` (equality at `n = 3f+1`). Each honest commits σ-or-NR per layer at most once, EKM-enforced.
- Leader-counting: if leader is honest, their Phase-1 σ_V counts toward `h_σ` on the V they signed; cross-phase exclusivity then forbids them from NR/NV on `nr_tag_k` (in case A, by definition no NR-flip fired). If leader is byzantine and equivocates, each per-V partial they publish counts toward `byz_σ_V` (capped at 1 per byz per V by deduplication).
- Byzantine cross-signing: `byz_σ + byz_NR ≤ 2f` (each byz contributes at most 1 to each pool).
- If both quorums reached: `h_σ + h_NR ≥ (2f+1 − byz_σ) + (2f+1 − byz_NR) ≥ (4f+2) − 2f = 2f+2`. But `h_σ + h_NR ≤ 2f+1`. Contradiction. ∎

**Case B: σ-flip fired at L_k from honest non-leader X.**

- σ-flip trigger at X (snap-based): `snap_NR_nl_X < f+1`, i.e., X observes ≤ f non-leader NR partials in their snapshot. Within-budget propagation makes honest non-leader NRs visible cluster-wide — X (themselves a non-leader NR-er) is in `snap_NR_nl_X` and sees all other honest non-leader NRs. So `snap_NR_nl_X ≥ nr_h`, hence `nr_h ≤ f`. Combined with `s_h + nr_h = 2f`: **s_h ≥ f**.
- **NR-flip from honest leader cannot fire at L_k:** NR-flip's trigger requires `snap_S_nl_leader < f`. The leader (within-budget) sees all honest non-leader σ partials, so `snap_S_nl_leader ≥ s_h ≥ f`. Trigger blocked. ∎ (mutex of σ-flip vs NR-flip per snapshot)
- **NR-pool bound at L_k.** Under σ-flip path, no honest NR-flip fires; honest non-leader NR-pool is frozen at the snapshot (HonestNR is pre-snap-only; honest leaders cannot NR pre-snap by the protocol's NR rule). NR-pool ≤ honest_NR_at_snap + byz_NR_total ≤ nr_h + f ≤ 2f < 2f+1 = qEnc. ∎ (no NR-quorum reaches in Case B)
- σ-pool may grow via σ-flip emissions, but NR-quorum cannot reach. P1 holds.

**Case C: NR-flip fired at L_k from honest leader.**

- NR-flip trigger (snap-based): `snap_S_nl_leader < f`. Leader sees all honest non-leader σ partials, so `s_h ≤ snap_S_nl_leader < f`, hence **s_h ≤ f − 1**. Combined with `s_h + nr_h = 2f`: `nr_h ≥ f + 1`.
- **σ-flip from any honest non-leader X cannot fire at L_k:** σ-flip's trigger requires `snap_NR_nl_X ≤ f`. X sees `nr_h` honest non-leader NRs (within-budget; ≥ f+1 from above), so `snap_NR_nl_X ≥ nr_h ≥ f+1`. Trigger blocked. ∎ (mutex symmetric)
- **σ-pool bound at L_k.**
  - σ-pool[V_L]: honest non-leader σ on V_L (= `s_h` at most) + leader's σ_L^V on V_L (= 1) + byz σ on V_L (≤ f) = (f − 1) + 1 + f = 2f < 2f+1 = qV.
  - σ-pool[V ≠ V_L]: honest non-leader σ on V (= 0 since each honest signs at most one V; if any honest σ'd on V ≠ V_L it's still bounded by `s_h ≤ f − 1`) + leader's σ on V (= 0; leader σ_L^V is on V_L) + byz σ on V (≤ f) = f − 1 + 0 + f = 2f − 1 < qV.
  - So σ-quorum on any V at L_k cannot reach. ∎

**The mutex is algebraic.** At `n = 3f+1`, honest non-leader cardinality `2f` and `s_h + nr_h = 2f` jointly preclude `s_h ≥ f ∧ s_h < f`. Cases A/B/C cover the three protocol situations (no flip / σ-flip / NR-flip), and in each case at most one of {σ-quorum, NR-quorum} can reach at L_k.

**Verification.** The new design (σ-flip + leader-only NR-flip + snapshot semantics + per-operator views modeling byz selective delivery) was formally verified at `n=4, f=1, K=1, |V|=2` under full byz grief: 56,014 distinct states explored; no Pigeonhole-1, 2, or 3 counterexample. See [docs/OBFT-formal-verif.md §7.1](OBFT-formal-verif.md). At K=2 (partial coverage, ~80M distinct states explored), no counterexample either. The algebraic argument generalizes to all K via Pigeonhole 3 induction (below) and to all `n = 3f+1` via the cardinality identity; n=7, f=2 verification is deferred but the mutex argument's structure is identical.

**Pigeonhole 2 — two σ-quorums on different values at the same layer.** Two distinct `V`'s cannot both reach σ-quorum at the same layer (e.g., via leader equivocation that some honest σ-commit on early before observing evidence):

- `(h_σ_V + h_σ_V') + (byz_σ_V + byz_σ_V') ≥ 2 · qV = 2(2f+1)`.
- Honest sign at most one V per layer (single-σ-V exclusivity, EKM-enforced — see "Slashing-protection scope"): `h_σ_V + h_σ_V' ≤ 2f+1`. The layer's leader counts here: they sign σ_V exactly once per (slot, layer), contributing to one V's pool.
- Byzantine can sign multiple V's: `byz_σ_V + byz_σ_V' ≤ 2f`.
- Bound: `(2f+1) + 2f = 4f+1 < 4f+2`. Contradiction. ∎

This is the key safety constraint underlying OBFT's "permit equivocation, slot-miss on view-divergence" framing: regardless of which V's honest σ-commit on under equivocation, at most one V can reach qV cluster-wide. There is no two-output safety failure even when honest operators split across V's; the cluster either reaches qV on a single V (some patterns recover naturally) or no V reaches qV (slot misses).

**Pigeonhole 3 — cross-layer safety under chained encryption.** Two distinct V signatures (V_k and V_{k+m} for any `m ≥ 1`) cannot both be reconstructed cluster-wide. Proof by induction on `m`, applying Pigeonhole 1 at every L_j with `j ∈ [k, k+m−1]`.

- *Decryption requirement.* V_{k+m} σ partials at L_{k+m} are encrypted under `nr_tag_k ∧ nr_tag_{k+1} ∧ … ∧ nr_tag_{k+m−1}`. Decryption requires NR-quorum on every `nr_tag_j` for `j ∈ [k, k+m−1]` (chained-IBE oracle).
- *Inductive step.* For each such `j`, Pigeonhole 1 applied at L_j gives: σ-quorum at L_j ⇒ NR-quorum at L_j does not reach. Therefore if V_k σ-quorum reaches at L_k, NR-quorum at L_k fails, the chain at L_k stays sealed, and V_{k+m}'s σ partials are inaccessible. The induction proceeds at every `j` from `k` to `k+m−1`.
- *Symmetric direction.* If V_{k+m} reconstructs, NR-quorum at L_k must have reached (chained-decryption requirement), so by Pigeonhole 1 σ-quorum at L_k did not reach, so V_k does not reconstruct. ∎

Applied to every pair of layers, at most one V signature reconstructs cluster-wide across all K layers.

**Cryptographic primitive — chained IBE.** Layer-`k` σ partials are encrypted under `nr_tag_0 ∧ nr_tag_1 ∧ ... ∧ nr_tag_{k-1}`. Decryption requires NR-quorum at each prior layer. Implementation: each level of the chain is a standard threshold-IBE ciphertext (drand/tlock) using `nr_tag_j` as the tag. At K=2 the chain has only one level (single tag `nr_tag_0`); at K=3 there are two levels nested; etc.

The arguments above apply symmetrically to all K layers. **None of the proofs depends on honest operators excluding cross-signers from their aggregation** — they're properties of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule. Cross-phase exclusivity (σ XOR NR per layer) and single-σ-V (one V per operator per layer) are enforced cryptographically by EKM at signing time, not by aggregator-side filtering.

### Liveness (synchrony-conditional)

OBFT's liveness is **partial-synchrony-conditional within the slot's relay-submission deadline** — the protocol's slot budget. Bundles arriving past `T_commit` at any honest receiver are not counted toward σ-quorum at that layer; the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time. View-divergence (equivocation, validity-divergence) is NOT recovered by the protocol; some splits succeed naturally as a side-effect of leader-σ-V counting toward whichever V reaches qV first, others slot-miss. Equivocation cases are slashable (leader pays the stake-based penalty); validity-divergence is not attributable.

If propagation between leader L_k's broadcast and any honest receiver's first-observation stays bounded by that layer's per-layer budget `B_k` (`B_0 = 1 BTT` for the primary up to `B_{K-1} = 5.5 BTT` for the deepest backup at K=4 Config A; see §Setting), the protocol terminates with a V signature on some layer (the first layer where σ-quorum reaches under cluster-wide V receipt by `T_commit`, or the deepest layer reachable via NR fall-through with a valid backup leader). If propagation exceeds `B_k` for layer L_k specifically, the cluster falls through to a deeper backup whose own `B_{k+1}` is wider. If all K layers fail to propagate in time (real propagation > `B_{K-1}` at the deepest layer), the slot misses. **Safety holds in either case.**

**Best case (healthy at L_0)**: all honest receive V_{L_0} within `1 BTT`; all 3 honest σ-emit + leader's Phase-1 σ = 4 ≥ qV. Slot succeeds in 2 RTTs (Phase 1 + Phase 2).

**Asymmetric propagation past `T_commit`**: depending on the split, recovered either at the affected layer or at a deeper backup. Honest who got V before T_commit σ-emit; honest who got V after T_commit treat the leader as silent and NR. Three sub-cases:

- **σ-pool reaches qV at this layer** (e.g., 2-of-3 honest received V + leader's σ_L^V = 3 = qV at f=1, n=4): the slot succeeds at this layer naturally.
- **σ-pool sub-qV at L_k, byz selective Phase-1 delivery with `h_V=1` shape** (= byz leader unicasts to one honest; 2 honest don't retain): **slot-misses at L_k with no fall-through.** σ-flip's trigger `snap_NR_nl < f+1 = 2` doesn't fire at f=1 because the 2 honest NR-ers count themselves in `snap_NR_nl` (= 2). NR-flip is honest-leader-only and the leader is byz. This is a Class B byz grief vector (or Class A if caused by honest leader's bundle hitting an asymmetric-propagation tail past `B_k`); deterred via Assumption 4 (rational-byz deterrent + planned blacklist + staker migration). Phase-2.5 σ-flip / NR-flip do NOT close this case at f=1, n=4 — see §Recovery scope below.
- **σ-pool sub-qV with all honest NR (= 0 honest σ-ers, leader silent or fully partitioned away)**: NR-quorum reaches at the affected layer naturally (3 honest NR ≥ qEnc at f=1); chain unlocks via the standard NR-quorum path, no flip needed.
- **σ-pool sub-qV but only 1 honest non-leader NR (asymmetric propagation hits 1 honest, with byz σ-equivocating onto V')**: σ-flip from the 1 honest NR-er's snapshot satisfies the trigger (`snap_NR_nl = 1 < 2`; `snap_S_post ≥ A + 2`). σ-flip pushes σ-pool[V_L] to qV; slot succeeds at L_k. (One of three narrow R1/R2/R3 σ-flip recoveries — see §Recovery scope.)

**Phase-2.5 σ-flip / NR-flip recovery scope at f=1, n=4** is narrow — three specific Class B byz patterns where σ-flip's trigger fires:

- **R1**: honest leader + 1 honest σ-er + 1 honest NR-er + byz σ-equivocates onto V' (≠ V_L).
- **R2**: byz leader + 2 honest σ-ers + 1 honest NR-er + byz Phase-2 silent.
- **R3**: byz leader + 2 honest σ-ers + 1 honest NR-er + byz Phase-2 σ on V' (≠ V_L).

NR-flip provides no new recovery at f=1 (its trigger only fires when NR-quorum already reaches naturally). Phase-2.5 is **not** an h_V=1 closure mechanism — it's a safety-preservation mechanism that also covers these three narrow patterns. Verification at `n=4, f=1, K=1` under full byz grief confirms safety preservation; liveness at K=4 holds under non-grief byz broadcast (selective Phase-1 delivery is grief). See [docs/OBFT-formal-verif.md §7](OBFT-formal-verif.md).

**Equivocation handling (no protocol-level recovery; outcome depends on byzantine timing)**: byzantine `L_0` equivocates, broadcasting distinct V's selectively. Case analysis at `f = 1, n = 4` (3 honest A, B, C; byz D = leader):

Recovered via σ-quorum at L_0 (when honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool):

- **D delivers V to {A, B}, V' to {C}.** σ-pool on V = A + B + D's σ_L^V(V) = 3 = qV. σ-quorum reaches. Slot outputs V.
- **D delivers V to {A}, V' to {B, C}.** σ-pool on V' = B + C + D's σ_L^V(V') = 3 = qV. Reaches. Slot outputs V'.

Recovered via NR-quorum at L_0 → fall-through to L_1 (when all honest retain ≥ 2 distinct V's by `T_commit` and emit NR per the equivocation-NR rule):

- **All-honest-NR outcome (byz delivers V's early enough that re-flood spreads conflicts before T_commit).** Each honest retains ≥ 2 distinct V's by `T_commit` → all 3 emit NR. σ-pools at L_0 ≤ byz partials per V < qV. NR-pool: 3 honest + 0 byz (σ-locked from Phase 1) = 3 = qEnc → NR-quorum reaches at L_0 → in the same Phase 3 reconstruction walk, advance to L_1; if L_1 honest, σ-quorum at L_1 reaches and slot succeeds at L_1.

Not recovered — slot misses at L_0 with no fall-through (σ-locked split patterns; slashable):

- **D delivers V to {A}, V' to {B}, both to C (or neither).** A σ-locks on V; B σ-locks on V'; C either retains both (NR per equivocation rule) or has nothing (NR per silent-leader rule). σ-pool on V = A + leader = 2 < qV. σ-pool on V' = B + leader = 2 < qV. NR-pool = 1 (C) < qEnc → no fall-through. Slot misses.
- **D delivers distinct V_1 to A, V_2 to B, V_3 to C (1-1-1 split).** Each honest σ-commits on their V_i on receipt before observing equivocation. All 3 honest σ-locked on different V's. σ-pool on each V_i = 1 honest + leader's σ_L^V = 2 < qV. NR-pool = 0 (all σ-locked, cross-phase exclusivity). Slot misses.

**Byzantine timing controls which class fires — and an *adversarial* byzantine reliably picks the slot-miss class.** Delivering V's early in Phase 1 leaves time for gossipsub re-flood to spread conflicts → all-honest-NR outcome → slot succeeds at L_1. Delivering near end-of-Phase-1 leaves insufficient re-flood time → σ-locked split outcome → slot misses at L_0. **In expectation against an adversarial byz primary, these patterns slot-miss reliably.** The rational-byzantine deterrent (assumption 4) is what makes this tolerable across many slots — but the *evidence quality* for these patterns is the *behavioral* class (not the cryptographically-self-contained class), so single-observation slashing is not credible (see [§Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4)). Practical effect: byz can grief many slots before the pattern accumulates enough confidence for honest operators to act.

In all cases, Pigeonhole 2 ensures at most one V can reach qV cluster-wide regardless of how honest σ-emissions split — there is no two-output safety violation.

**Multi-failure fall-through (K ≥ 3)**: with `K > 2` layers, the cluster can fall through past multiple silent or non-validating layers **within Phase 3's single reconstruction walk** — the walk processes layers sequentially using local decryption (no per-layer RTT). E.g., at K=4 with L_0, L_1, L_2 all silent: NR-quorum reaches at L_0 in Phase 2 (silent-leader rule fires for all 3 honest cluster-wide); same at L_1 and L_2; σ-quorum at L_3 if L_3 leader honest. All happens in Phase 2 + Phase 3 windows. Recovery scope at K layers is achieved without round-change overhead — a structural difference from QBFT (which round-changes once per failed leader). At K = n = 4, every cluster member is a leader exactly once; pigeonhole guarantees ≥3 honest leaders at f=1, providing maximum K-fall-through depth within a single round.

**Adversarial scheduling within partial synchrony**: the network adversary can delay messages by up to `1 BTT`.

- *Safety unaffected.* Pigeonholes 1, 2, 3 are properties of the cluster-wide signed-message set, not of arrival times.
- *Liveness — adversary delays V to ≤ 1 honest past `T_commit`.* The other 2 honest σ-emit on time; σ-pool = 2 + leader = 3 = qV. **Quorum reaches without the delayed operator.**
- *Liveness — adversary delays V to ≥ 2 honest past `T_commit`.* σ-pool < qV at this layer. **Two sub-cases by NR-pool:** (a) all 3 honest delayed (= leader-effectively-silent from cluster view): NR-pool = 3 honest = qEnc, naturally reaches; chain unlocks via standard fall-through. (b) Exactly 2 honest delayed (= "h_V=1" selective-delivery shape): NR-pool = 2 honest < qEnc; **slot misses at this layer with no fall-through** (Phase-2.5 σ-flip's trigger requires `snap_NR_nl < f+1 = 2`, but each honest NR-er sees `snap_NR_nl = 2` from their snapshot — trigger blocked; NR-flip is honest-leader-only and doesn't apply). Class A or B depending on cause; deterred via Assumption 4 across slots. See [§Phase 2.5](#phase-25--σ-flip--nr-flip-flips) for the recovery scope.

### Liveness comparison: OBFT vs OBFTR(R=2) vs QBFT

The table below puts OBFT, OBFTR(R=2), and QBFT side-by-side per scenario at the standard SSV proposer-duty configuration (n=4, f=1, K=4, ~4s relay cutoff). Timing assumes the SSV proposer-duty operating point (`BTT = 200ms` = P99=150ms + δ=50ms; staggered K=4 with `B_3 = 5.5 BTT = 1100ms` deepest-layer budget — see [Timing budget](#timing-budget)). All counts at recommended sizing (2 BTT per emission cycle — see [docs/BFT-comparison.md / Sizing convention](BFT-comparison.md#sizing-convention)). For QBFT-SSV, `RT = 2s = 10 BTT`; QBFT-optimal RT = 6 BTT.

| Scenario | OBFT outcome | OBFTR(R=2) outcome | QBFT-SSV outcome |
|---|---|---|---|
| Healthy (all honest receive V_{L_0}) | σ-quorum reaches in 2 RTTs. ✓ at L_0 in ~600ms (3 BTT). | Same. ✓ at L_0 in ~1200ms (6 BTT). | PROPOSE→PREPARE→COMMIT + post-consensus (4 emissions × 2 BTT). ~1600ms (8 BTT). ✓ |
| Byzantine leader silent | 0 σ-emits → all honest NR (silent-leader rule) → NR-quorum at L_0 reaches in Phase 2 → automatic fall-through to L_1 in Phase 3 walk; σ-quorum at L_1 if leader honest. ✓ in ~600ms. | Same. ✓ in ~1200ms. | Round 1: no PROPOSE arrives; round timeout (~2s). Round 2: new leader proposes; succeeds in ~1.6s. ✓ in ~3.6s. |
| Asymmetric propagation (≤1 of 3 honest miss V at T_commit) | Other 2 honest σ-emit on time; σ-pool = 2 + leader = qV. ✓ at L_0 in ~600ms. The miss-honest's NR partial is unused. | Same (within OBFT's absorption). ✓ in ~1200ms. | Round 1: PREPARE-pool short; timeout (~2s). Round 2: re-fetch + propose; succeeds in ~1.6s. ✓ in ~3.6s. |
| Asymmetric propagation (≥2 of 3 honest miss V at T_commit, h_V=1 selective-Phase-1-delivery shape) | σ-pool < qV at L_0; **slot misses at L_0 with no fall-through** — Phase-2.5 σ-flip / NR-flip don't close this case at f=1, n=4 (σ-flip's `snap_NR_nl < f+1` doesn't hold; NR-flip is honest-leader-only). Class B byz grief vector (or Class A under honest-network-tail cause); deterred via Assumption 4 across slots. ✗ slot-miss. | Within OBFTR(R=2)'s wider absorption: round 2 re-flood may deliver V to the miss-honest; σ-quorum at L_0 reaches in round 2. ✓ in ~2.4s. | Round 1: timeout. Round 2: new leader; succeeds in ~1.6s. ✓ in ~3.6s. |
| Byzantine leader equivocates, 2-1 split where one V already has 2 honest + leader-σ_L^V | σ-pool on the 2-honest-V = 2 + leader = 3 = qV; reaches naturally. ✓ in ~600ms. | Same. ✓ in ~1200ms. | Round 1: PREPARE-pool split; timeout. Round 2: new leader proposes; succeeds. ✓ in ~3.6s. |
| Byzantine leader equivocates, σ-locked split pattern (1-1-1, etc.) | σ-pools split below qV; NR-pool below qEnc (σ-locked operators can't NR; Phase-2.5 trigger doesn't fire because NR-pool < f+1). **✗ slot misses at L_0;** no fall-through. Equivocation slashable. | Same exposure (R-invariant). ✗ slot misses. Equivocation slashable. | Round 1: PREPARE split; timeout. Round 2: new leader proposes a fresh V; honest converge; succeeds. ✓ in ~3.6s. **QBFT recovers what OBFT/OBFTR don't** via "new leader proposes fresh V" mode. |
| Byzantine leader equivocates, all-honest-NR outcome (byz delivers V's early; re-flood spreads conflicts before T_commit) | All 3 honest retained ≥ 2 V's by T_commit → all NR per equivocation rule → NR-quorum at L_0 → fall-through to L_1 in Phase 3 walk; if L_1 honest, ✓ at L_1 in ~600ms. Equivocation slashable. | Same recovery via Round-R force-NR. ✓ in ~1200ms (round 1) or ~2.4s (round 2). | Same recovery as σ-locked split row: round-2 new leader proposes fresh V. ✓ in ~3.6s. |
| Multi-failure fall-through (multiple silent leaders) | At K=4 with L_0, L_1, L_2 silent: NR-quorum reaches at each in Phase 2; Phase 3's walk decrypts down to L_3; σ-quorum at L_3 if honest. **All in single Phase 2 + Phase 3 windows** (Phase 3 ε_3 grows with K-layer decryption walks). ✓ in ~1000ms (3 BTT consensus + ~400ms ε_3 × K). | Same. ✓ in ~1200ms. | Round 1: leader 1 silent → timeout (~2s). Round 2: leader 2 silent → timeout (~2s). Round 3: leader 3 silent → timeout (~2s). Round 4: succeeds in ~1.6s. ✓ in ~7.6s — past 4s cutoff. ✗ for proposer duty. **OBFT's K-layer parallel fall-through beats QBFT's serial round-change**. |
| Host-validity divergence (head-change mid-slot, strict host) | Out of scope (assumption 3 — host stabilizes verdict at Phase-1 acceptance). Same as OBFTR(R=2). | Same. | Round 1: validators with stale head don't PREPARE; timeout. Round 2: new leader fetches at moved head; if quorum agrees, succeeds. ✓ in ~3.6s. **QBFT recovers what OBFT-family doesn't** via fresh-V refetch on round-change. |
| Adversarial scheduling — adversary delays V at all K layers beyond their respective per-layer absorption | **Out of envelope** (Class A). ✗ Slot misses. The deepest layer's `B_{K-1} = 5.5 BTT = 1100ms` at K=4 SSV operating point is the cluster-wide tolerance ceiling. | If delay ≤ OBFTR(R=2)'s cross-round retention (~1500ms at this operating point): in envelope at R=2; round 2 re-flood may resolve at L_0. ✓ in ~1.8s. Else: out of envelope. ✗ | Round 1: PREPARE delayed → timeout (~2s). Round 2: new leader; if adversary delays again → round 2 timeout. ✗ Within 4s, QBFT tolerates ≤ 1 round of adversarial delay. |
| Sustained partition (real propagation > all layers' absorption) | OBFT deepest-layer absorption `B_{K-1} ≈ 1100ms` at K=4 SSV operating point; exceeded → ✗ slot misses. Safety holds. | OBFTR(R=2) cross-round retention ~1500ms at this operating point (recovers at L_0 via re-flood, preserves MEV); exceeded → ✗ slot misses. Safety holds. | QBFT round-budget × RT exceeded; slot misses. ✗ Safety holds. |
| > f operators offline/byzantine | Standard 3f+1 violation; slot misses. ✗ Safety holds. | Same. ✗ | Same. ✗ |

**Summary of recovery-scope differences:**

- **OBFT and OBFTR(R=2) differ in *where* they recover, not just how much they tolerate.** OBFT's per-layer staggered budgets (`B_0 = 1 BTT` up to `B_{K-1} = 5.5 BTT = 1100ms` at K=4 SSV operating point) recover via K-layer fall-through — propagation up to 1100ms is absorbed, but the slot succeeds at a deeper layer (different leader's V, less MEV freshness). OBFTR(R=2)'s ~1500ms cross-round retention recovers at L_0 specifically via round-2 re-flood (preserves MEV freshness) at the cost of an extra round of consensus.
- **OBFTR(R=2) > OBFT for slots where MEV preservation matters during partition tail**: round-2 re-flood at L_0 gets the freshest MEV V even when L_0's broadcast didn't propagate in round 1. OBFT loses one layer's worth of MEV per fall-through.
- **OBFT-family > QBFT in latency and multi-leader-failure**: OBFT's healthy path is ~600ms (vs ~1600ms QBFT-SSV at recommended sizing); K-layer parallel fall-through is in-round (vs QBFT's serial round-change at ~3.6s per round-change cycle, exceeding the 4s budget at K-1=3 silent leaders). Phase-2.5 σ-flip / NR-flip recoveries (R1/R2/R3 narrow Class B patterns) happen within Phase 3's local-CPU budget (no `Δ_2` extension, healthy-path latency unchanged); does NOT close h_V=1 (deterred via Assumption 4).
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

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_k` — *without* valid Phase-2.5 trigger evidence — is a slashable cross-signer. The pair is detected uniformly across phases:

- **σ from Phase 1 + NR/NV from Phase 2** — applies to a layer's leader specifically: a `σ_{L_k}^V(V_{L_k})` from the Phase-1 bundle paired with an `σ_{L_k}^{IBE}(nr_tag_k)` from the same operator at the same slot.
- **σ from Phase 2 onion + NR/NV from Phase 2** — any operator who included σ in their onion *and* broadcast a no-σ attestation.

**Phase-2.5 flip exemption.** When the σ-then-NR pair is `(σ_L^V from honest leader, KindNRFlip)` with valid `trigger_evidence` per [§Phase 2.5](#phase-25--σ-flip--nr-flip-flips), the cross-signing is protocol-allowed and **not slashable**. Symmetrically, when the NR-then-σ pair is `(NR/NV from non-leader honest, KindSigmaFlip)` with valid trigger_evidence, the cross-signing is also protocol-allowed and not slashable. Receivers verifying a (σ, IBE-on-nr_tag_k) pair MUST first check for an accompanying valid flip emission at the same `(slot, layer)` from the same emitter (NR-flip if leader, σ-flip if non-leader); only if no valid flip exists does the pair surface as Rule-1 slashable evidence.

Detection is straightforward — the dual partials are public, the trigger evidence is verifiable. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it cryptographically — the EKM-enforced cross-phase exclusivity is what gives Pigeonhole 1 its load-bearing constraint, and a byzantine cross-signer simply consumes their f-bound contribution to one of σ-pool or NR-pool, not both effectively). The detection is purely for **attribution** and out-of-band punishment.

### Slashing evidence

Five rules surface byzantine fault evidence for *attribution and punishment*. Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to inform the surviving operators (and stakers) that a byzantine fault has occurred so the cluster can act on it — see [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the offline-equivalence model. The protocol surfaces the evidence; the surviving operators verify it and decide whether to (a) blacklist the byzantine via cluster-wide config update (restoring `Byzantine ≡ Down`; planned protocol extension), (b) file a stake-slashing transaction via the SSV contract where the evidence is cryptographically self-contained, and/or (c) propagate the signal to stakers and to future cluster formation.

- **Self-contradiction (σ + NR/NV at the same layer, without valid Phase-2.5 trigger).** If operator `i` emitted `σ_i^V(V)` at some layer AND `σ_i^{IBE}(nr_tag_k)` at the same layer, the dual partials are slashable evidence (cross-signing). **Exemption:** if the pair corresponds to one of the two protocol-allowed Phase-2.5 flip patterns — `(σ_L^V from honest leader, KindNRFlip)` or `(NR/NV from non-leader honest, KindSigmaFlip)` — with `trigger_evidence` consistent with the receiver's local snapshot composition (i.e., `NRFlipTriggered` or `SigmaFlipTriggered` evaluates to true from the receiver's view, see [§Phase 2.5](#phase-25--σ-flip--nr-flip-flips)), the cross-signing is protocol-allowed and **not slashable**. Receivers verifying the dual partials MUST first check for an accompanying valid flip emission with consistent trigger before treating the pair as slashable. The exemption applies per `(slot, layer)`; cross-signings at any other `(slot, layer)` without a valid flip remain Rule-1 slashable.
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

**Rule 4 has a structural detection limit.** The chained-encryption construction makes Rule 4 evidence cryptographically inaccessible when prior layers' NR-quorum doesn't reach: decryption requires `qEnc` partials on `nr_tag_{0..k-1}`, those partials don't exist if honest σ'd at those layers, and honest cannot preemptively sign IBE partials on layers they intend to σ-side-commit to (that violates cross-phase exclusivity = cross-signing). This is a **fundamental limit of the IBE primitive as used here**, not a fixable spec gap. Phase-2.5 NR-flip narrows this limit slightly when an honest leader's snapshot satisfies `NRFlipTriggered` (their post-finalize NR-flip generates an `nr_tag_k` partial, unlocking deeper-layer decryption), but this fires only in narrow Class B patterns and does not generally close Rule-4 evidence-sealing under adversarial-byz slot-miss patterns at L_0.

**The seal applies in BOTH slot-success and slot-miss outcomes**, not just slot-misses:

- **Slot succeeds at L_0** (σ-quorum reaches at L_0): per Pigeonhole 1, NR-quorum at L_0 does NOT reach (σ and NR mutually exclusive at the same layer). Hence the chained encryption at L_1, L_2, ... stays sealed, and any fake encrypted-presence at deeper layers in this slot is invisible. **This is the common case for healthy slots** — a byzantine that fakes encrypted-presence at L_2 in every slot pays no per-slot cost on healthy slots where the cluster succeeds at L_0. The fake-presence is essentially "rehearsing the attack with no consequences" until a slot-miss-at-L_0 path happens to unlock the relevant encryption.
- **Slot misses at L_0** (no quorum reaches): σ-locked split equivocation, validity-divergence deadlock, h_V=1 selective Phase-1 delivery — each leaves the chained encryption sealed. Compounded with byz fake-presence at deeper layers, byz gets two grief actions per detection. Deeper-layer Rule-4 evidence stays sealed in these slot-miss patterns.

**Practical implication for deployments.** Rule 4 functions as a *probabilistic* deterrent rather than an unconditional one: a byzantine that fakes encrypted-presence at L_k>0 expects detection only with the probability that NR-quorum reaches at all prior layers in subsequent slots where the deterrent's coordination process can still act. Deployments relying on assumption 4 (rational-byzantine deterrent) for L_k>0 fake-presence should weight the deterrent's effective strength accordingly — Rule 4 is *best-effort, not guaranteed surface-able*. Rule 5 (Fake plaintext σ at the cluster's plaintext layer — L_0 in bare OBFT, L_Bid in the L_Bid extension) does not have this limitation since the plaintext layer's σ is unencrypted.

The five classes are all *cryptographically self-contained* (high-confidence, low false-positive risk against honest operators) once surfaced. The asymmetry above — Rule 4's slot-progress-conditional surface-ability — is a real limitation that adversarial byzantine can exploit by engineering slot-miss precisely to seal Rule-4 evidence. Behavioral-pattern grief (selective-delivery, σ-refusal coordinated with honest flakiness) leaves no on-wire cryptographic evidence at all and is correspondingly harder for humans to act on with confidence — see [Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4).

### Failure modes

The slot misses (no V signature is produced) under any of the following. The cases split into two classes by relationship to OBFT's operating assumptions:

- **Class A — assumption violations** (the listed condition violates one of OBFT's assumptions; the protocol does not promise liveness when an assumption is violated). These are out-of-scope for OBFT's recovery guarantees by construction.
- **Class B — permitted byzantine grief within the f-bound** (occurs *under* valid assumptions; one byzantine operator within the f-byzantine bound deliberately misbehaves to cause slot-miss). These are *permitted because they are eventually bounded* — every Class B grief leaves evidence on the wire (cryptographically self-contained for some classes, behavioral-pattern for others), and the rational-byzantine deterrent (assumption 4) bounds the byzantine's grief across slots via the eventual `Byzantine ≡ Down` collapse (manual blacklist by the surviving `n − f` operators; planned protocol extension) plus staker migration that collapses cluster-wide fee accrual. The boundedness is what makes Class B "permitted" rather than "fatal" — an attacker that griefs reliably ends up in the same fee position as if they had gone permanently offline (and worse, for cryptographically-self-contained faults: stake-slashable via the SSV contract).

The slot misses under any of:

- **[Class A]** **Asymmetric propagation past `T_commit` (real propagation from L_k's broadcast to first-observation > `B_k` at layer L_k)** — violates assumption 2 (partial synchrony) for that layer. Honest who first-observe V past `T_commit` treat the leader as silent and NR. If the resulting σ-pool falls below qV at this layer, the cluster falls through to a deeper backup whose own `B_{k+1}` is wider (per-layer budgets are staggered: `B_0 = 1 BTT` for the primary up to `B_{K-1} = 5.5 BTT` for the deepest at K=4 Config A; see §Setting). If propagation also exceeds the deepest layer's absorption ceiling, slot misses cleanly. **No safety violation.**
- **[Class A]** **More than `f` faults** — violates assumption 1 (BFT trust bound). More than `f` operators offline/byzantine combined. Standard `3f+1` trust bound. Slot misses regardless of protocol structure.
- **[Class A]** **Assumption 3 violated (host validity diverges mid-slot)** — violates assumption 3 (host validity unanimous at decision time). See [Implications of validity-divergence not being recovered](#implications-of-validity-divergence-not-being-recovered-assumption-3). The host's stabilization workflow (validate-once-and-lock at Phase-1 acceptance) is the design's path to keeping this from occurring; if it does occur, the slot misses cleanly. Not slashable (re-orgs are real-world events, not protocol violations); rational-byzantine deterrent does not apply.
- **[Class B — cryptographically slashable]** **Equivocation σ-locked split patterns** (1-1-1 split, 1-1-NR at f=1 n=4 — patterns where some honest σ-commit on different V's before observing equivocation, leaving neither σ-quorum nor NR-quorum reachable at L_0 and no fall-through) — occurs under valid assumptions; one byzantine within f-bound equivocates to engineer the split. See [Implications of equivocation not being recovered](#implications-of-equivocation-not-being-recovered). **Equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained cryptographically slashable**; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots. The all-honest-NR case (every honest retains ≥ 2 V's by T_commit and emits NR per the equivocation-NR rule) actually recovers via L_1 fall-through and is not a failure mode at f=1 n=4 with L_1 honest.
- **[Mostly Class A]** **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates in non-recoverable patterns at every layer). At K ≥ f+1, byzantines alone (within f-bound) cannot cause this — pigeonhole guarantees ≥ 1 honest leader. So this typically requires either >f faults (Class A: violates assumption 1) or coincident non-byzantine independent failures (relay timeouts, host issues, etc. — also Class A in the sense that the cluster's operational assumptions about host availability are violated). Rare in practice. The K parameter controls the fall-through depth; **at K = n = 4 (recommended OBFT default for proposer duty), pigeonhole guarantees ≥ 3 honest leaders, providing maximum fall-through redundancy**.
- **[Class A]** **Late deepest-layer leader broadcast.** A deepest-layer leader L_{K-1} whose Phase-1 bundle's first cluster-observation arrives after `T_commit` — e.g., the leader's fetch loop overruns substantially due to slow beacon node, MEV-relay timeout, or head-change refresh — is not counted by any honest receiver. All 3 honest at L_{K-1} treat as silent-leader-NR, NR-quorum at L_{K-1} reaches → walk advances past L_{K-1}, but no L_K layer exists. **Slot misses.**

  **Why Class A?** None of the six explicit assumptions is violated: BFT trust bound holds, partial synchrony bounds *gossipsub* propagation but not the leader's *application-internal fetch latency*, validity is orthogonal, EKM and DKGs are orthogonal. The failure relies on an *implicit* operational assumption — that honest leaders broadcast by their per-layer deadline `T_broadcast_max_k = T_commit − B_k` (with `B_k` sized so propagation completes before `T_commit`). When this implicit assumption fails (legitimate operational delay overruns even the deepest layer's wide budget `B_{K-1}`, no byzantine action), the protocol cannot fall through past the deepest-layer. Note that in the staggered model the deepest layer has the *largest* propagation budget (e.g., `B_3 = 5.5 BTT = 1100ms` at K=4 Config A), so this failure requires either an extreme operational delay or a leader that hasn't pre-fetched a bundle in time for the very early `T_broadcast_max_{K-1}` deadline.

  **Mitigation paths (in order of recommendation):**
  - **Use K ≥ f+2** (the recommended config; see §Setting). Adds at least 2 honest leaders so a single late-broadcasting honest leader doesn't foreclose the slot — fall-through to a deeper honest leader recovers. At f=1 n=4 this means K ≥ 3 (with K = n = 4 as the OBFT default for proposer duty, providing maximum fall-through depth at minimal extra bandwidth ~3KB per onion). At f=2 n=7, K ≥ 4. **No protocol change**, just deployment configuration; this is the cleanest fix that actually recovers the slot. **The OBFT default `K = n = 4` already satisfies this.**
  - **Host-side hard deadline** (defense-in-depth on top of K ≥ f+2; minor host-side discipline, no protocol change). Leader `L_k`'s fetch loop should abort and broadcast no bundle if it hasn't terminated by `T_broadcast_max_k`. Converts "late broadcast missed cutoff" into "silent leader" (handled cleanly via NR-quorum → fall-through). At K = f+1 (BFT-min) this *cleans up the spec-tension* but doesn't recover the slot (silent + L_0-fail still falls through to non-existent layer). At K ≥ f+2 it complements the K choice by ensuring late broadcasts always fall into the silent-leader path.
- **[Class A]** **Validity-divergence deadlock (network-induced; no byzantine action required in the cleanest case).** A beacon-chain re-org landing inside the bundle-acceptance window can split honest operators between σ-eligibility (parent_root matched their pre-reorg head snapshot) and NV (parent_root didn't match their post-reorg head). The host's per-operator validate-once-and-lock workflow (§Head-change handling) preserves each operator's verdict but cannot reconcile cluster-wide divergence. At the boundary split (e.g., 2-σ vs 2-NV at f=1 n=4 with all 4 honest, where leader's Phase-1 σ_V counts as σ-side via cross-phase exclusivity): σ-pool = 2 < qV = 3; NR-pool = 2 < qEnc = 3. **No safety violation** — just no quorum on either side; slot misses cleanly. The host's stabilization workflow narrows the divergence window to ≈ `1 BTT` (the time between earliest possible bundle first-observation and `T_commit`), but doesn't eliminate it.

  **The deadlock zone widens when a byzantine within f-bound exercises f-budget passively** (silent or σ-on-V — neither is independently slashable; passive silence is the natural f-budget consumption pattern for an absent operator). At f=1 n=4 with leader honest, additional deadlock configurations include:

  - "1 non-leader σ + 1 non-leader NV + byz silent" (σ-pool = leader + 1 = 2 < qV; NR-pool = 1 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz silent" (σ-pool = leader = 1 < qV; NR-pool = 2 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz σ-on-V" (σ-pool = leader + byz = 2 < qV; NR-pool = 2 < qEnc).
  - Symmetric configs when leader's host-verdict is itself NV (Phase-1 σ_V locks leader σ-side regardless of leader's host's later opinion).

  The all-honest example captures the cleanest case; in real deployments with byz exercising f-budget, the practical slot-miss rate from validity-divergence is meaningfully higher than the all-honest example alone suggests. **The expected rate scales with `re-org rate × byz-passivity-rate`**, not re-org rate alone — i.e., a deployment with re-orgs in 1% of slots and a byzantine present and adopting passive grief in some fraction of slots compounds these probabilities into validity-divergence slot-misses. The host's stabilization workflow narrows the divergence window but does not eliminate it; byzantine passive f-budget consumption (silence or σ-on-V — neither cryptographically slashable individually) is essentially "free" within the f-bound, so byz can reliably contribute the passivity factor whenever exercising the deterrent's weak-attribution corner is favorable. See [Implications of validity-divergence not being recovered (assumption 3)](#implications-of-validity-divergence-not-being-recovered-assumption-3) for the full discussion.
- **[Class B — behavioral evidence]** **Byzantine selective Phase-1 delivery (h_V=1).** A byzantine leader that unicasts Phase-1 to exactly one honest creates an algebraic deadlock at f=1, n=4: σ-pool = 2 (recipient + byz σ_L^V) and NR-pool = 2 (the two no-V honest; recipient is σ-locked, can't NR), with neither reaching qV/qEnc=3. **Phase-2.5 σ-flip / NR-flip do NOT close this case at f=1, n=4**: σ-flip's trigger requires `snap_NR_nl < f+1 = 2`, but the 2 honest NR-ers each see `snap_NR_nl = 2` (themselves + the other NR-er) — trigger blocked. NR-flip is honest-leader-only and the leader is byz. Slot misses at L_0 with no fall-through (chain stays sealed). Class B: the deterrent is rational-byzantine (assumption 4 — per-slot fee equivalence to offline + planned blacklist + staker migration); evidence quality is behavioral-pattern (no on-wire cryptographic proof of selective delivery). Same outcome applies if the cause is honest leader's bundle hitting a propagation tail past `B_k` (Class A — Assumption-2 violation; protocol does not promise recovery; same algebraic deadlock; same Phase-2.5 ineffectiveness; same dependence on Assumption 4 across slots). Phase-2.5 σ-flip *does* close the related "1 honest σ + 1 honest NR + byz σ-equivocates onto V'" pattern (R1) and two byz-leader 2-σ patterns (R2, R3) — see §Liveness for the recovery-scope enumeration.

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
| Termination (output guaranteed) | Conditional. **One-liner: consensus expected to complete by `slot_start + 3.90s` at SSV proposer-duty operating point (n = 4, f = 1, BTT = 200ms, K = 4, T_commit = 3.40s, recommended Δ_2 = 2 BTT = 400ms, Δ_3 = ε_3 = 100ms), with submission slack to `slot_start + 4.00s` for relay submit (`header_submit_headroom = 100ms`); under conditions: (a) ≤ f operators byzantine/offline, (b) real propagation from leader L_k broadcast to any honest first-observation ≤ that layer's per-layer budget `B_k` (staggered: B_0 = 1 BTT, ..., B_3 = 5.5 BTT at K=4 — see §Setting), (c) host validity unanimous at decision time (assumption 3), (d) `K ≥ 3` (late-leader resilience).** Single-round protocol; Phase-2.5 σ-flip / NR-flip is a Phase-3 sub-mechanism providing narrow Class B recovery (R1/R2/R3 — see §Liveness) with zero MEV cost (no `Δ_2` extension at n=4); n ≥ 7 deployments require independent timing analysis per §Phase 2.5's "Caveat at n ≥ 7" subsection. Per-layer staggered budgets give narrower absorption at the deepest layer (~1100ms at L_3 = `B_3`), trading absorption width for spec simplicity and submission headroom. |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates form self-contained slashable evidence |
| Byzantine-leader-grief resistance | **Bare-OBFT-baseline + narrow Phase-2.5 σ-flip recovery; still partial under adversarial byz.** Phase-2.5 σ-flip closes 3 specific Class B byz patterns at f=1, n=4 (R1/R2/R3 — see §Liveness): asymmetric-propagation with 1 honest σ-er + 1 honest NR-er and byz σ-equivocates onto V'; byz-leader 2-honest-σ + 1-honest-NR with byz silent or byz σ on V'. Recovery via "natural" σ-quorum patterns (leader's σ_L^V completes a 2-of-3-honest pool at n=4) handles symmetric splits. **Adversarial byzantine still reliably engineers slot-miss when L_0** via σ-locked split equivocation (1-1-1, 1-1-NR), `h_V=1` selective Phase-1 delivery, and other patterns where σ-flip's `snap_NR_nl < f+1` doesn't hold. At f=1 n=4 with uniform leader rotation, an adversarial byz primary can deterministically grief ~25% of slots (whenever they're L_0) via these residual patterns; the rational-byzantine deterrent (assumption 4) is the only protocol-level defense for those, and it works *across slots in expectation*, not per-slot. Effective deterrent strength is deployment-specific (stake-to-grief-value ratio, governance responsiveness, slashability evidence quality — see [§Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4)). |
| Mesh-flakiness tolerance (honest operator with poor gossipsub mesh visibility) | **Limited.** A mesh-flaky honest operator who fails to observe peer σ-emits within the NR-decision window can NR-emit incorrectly, becoming a byzantine-equivalent f-budget consumer for that slot. Combined with byz σ-refusal, this creates a deadlock that the protocol cannot recover from within the slot. The recommended `Δ_2 ≥ 2 BTT` absorbs typical mesh-jitter (up to one full `1 BTT` of additional slack on top of P99 propagation) but doesn't cover wider mesh outliers. QBFT's round-reset semantics handle this case better (a flaky operator's bad PREPARE doesn't lock them across rounds); OBFT enforces cross-phase exclusivity per slot. |
| Validity-divergence under strict host | **Out of scope** — see [Assumptions](#implications-of-validity-divergence-not-being-recovered-assumption-3); host stabilizes the verdict at Phase-1 acceptance |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (K-layer fall-through within Phase 3's reconstruction walk; K configurable, K = n recommended for proposer duty) |
| Round-change recovery | **No** — single-round design. Late re-flood within Phase 2's receiver acceptance window is the only within-slot partition-recovery mechanism. |
| Partial-synchrony absorption | Per-layer staggered: `B_k` for layer L_k. At K=4 Config A: `B_0 = 1 BTT = 200ms` for primary L_0 (tightest, freshest MEV) up to `B_{K-1} = 5.5 BTT = 1100ms` for deepest L_3 (last-resort tail). Cluster falls through layers based on which one's bundle actually arrived by `T_commit`. |
| Recovery scope vs QBFT | Multi-leader fall-through is in-round (vs QBFT's serial round-change), so OBFT wins on K-leader-failure cases and healthy-path latency. View-divergence (validity-divergence and equivocation patterns that don't naturally reach qV) is out of scope — handled by assumptions 3 and 4. |

## Application: SSV Ethereum proposer duty

For SSV's proposer duty, the recommended OBFT configuration is **`K = 4 = n`** — every cluster member leads exactly one layer, providing maximum K-layer fall-through depth (`f+1 = 3` honest leaders guaranteed by pigeonhole at f=1). Concretely, **`V_0`** is the slot's designated MEV proposer (fetches the freshest relay-bundle); **`V_1, V_2, V_3`** are backup leaders fetching progressively safer / earlier vanilla beacon-node payloads (deeper-confirmed parents → lower re-org exposure; see §Head-change handling).

Cryptographic safety (`qEnc = qV` + chained encryption + EKM-enforced cross-phase / single-σ-V exclusivity) ensures only one block can ever get a valid validator signature, regardless of K. The single-round design simplifies the EKM coordinator (no cross-round atomicity).

**Blinded-block-only constraint.** OBFT proposer duty operates on **blinded** beacon blocks only (= `BeaconBlock` with `ExecutionPayloadHeader` instead of full execution payload). Operators sign blinded V; relay-revealed unblinding of the full execution payload happens **after** threshold reconstruction, outside the consensus protocol. The blinded constraint is what makes the leader-bundle re-flood in `KindCommit`'s `bundle_witnesses` section (see [§Phase 2 / Wire format](#phase-2--onion-broadcast-t_commit-t_commit--%CE%94_2)) bandwidth-tractable: blinded V is typically 5–15 KB on mainnet (worst case ~50 KB with full attestations + slashings), giving per-op `KindCommit` ~88 KB typical / ~228 KB worst case at K=4. Full-payload OBFT proposer would push per-op KindCommit into the megabyte range and is not supported. Non-proposer SSV duties (attestations, sync committee) operate on small `V` (~100 bytes) where the blinded constraint is moot.

### Proposer-duty terminology

| Term | Meaning |
|---|---|
| `V_0` | MEV-optimized block fetched late from the relay |
| `V_1, V_2, V_3` | safe earlier-fetched blocks from vanilla beacon-node payloads, refreshed on head changes within each leader's pre-signing fetch loop |
| `BTT` | broadcast trip time = P99 + δ; one one-way gossipsub propagation cycle. **`BTT = 200ms`** (P99 ≈ 150ms + δ ≈ 50ms) at the operating point below |
| Slot start | t = 0 (anchored to consensus-layer slot start) |
| `RANDAO` | RANDAO-reveal completion; cluster-wide ≈ slot_start + 150ms — earliest possible Phase-1 fetch start |
| `T_broadcast_max_k` | per-layer leader broadcast deadline: `T_commit − B_k`. Deeper layers broadcast earlier (wider `B_k`) to absorb wider propagation tails |
| `T_commit` | view-fix deadline: `Relay_cutoff − 3 BTT`. Receivers stop counting Phase-1 bundles past this point |
| `header_submit_headroom` | budget for cert broadcast + relay submit after Phase 3 completes; **100ms** |
| `Relay_cutoff` | slot_start + **4000ms** — slot's hard relay-submission deadline |

### Timing budget

**Operating point.** `BTT = 200ms`, `header_submit_headroom = 100ms`, `Δ_2 = 2 BTT = 400ms` recommended (KindCommit propagation + jitter), `Δ_3 = ε_3 ≈ 100ms` local CPU.

**Derived anchors.** `T_commit = Relay_cutoff − 3 BTT = 3400ms`.

The 600ms after `T_commit` decomposes as: `Δ_2` (2 BTT = 400ms; **scales with BTT** — propagation budget for KindCommit fan-out) + `Δ_3` (`ε_3 ≈ 100ms`; **absolute** — local CPU work, doesn't scale with BTT) + `header_submit_headroom` (100ms; **absolute** — cert broadcast + relay HTTP submit, doesn't scale with BTT). At Config A this happens to total 3 BTT, but only `Δ_2` is BTT-proportional; at higher BTT the post-`T_commit` budget is dominated by `Δ_2` and the absolute components matter less in BTT terms. Sizing recommendation: keep `Δ_3` and `header_submit_headroom` as absolute quantities and let `Δ_2` scale.

| t (ms) | Event | Targets / notes |
|---|---|---|
| 0 | Slot start | |
| 150 | `RANDAO` done | Earliest Phase-1 fetch start |
| 2300 | `V_3` broadcast (`T_commit − 5.5 BTT`) | Targets propagation tails up to **1100ms**; MEV-fetch budget **2150ms** (last-resort tail; pre-fetched at deepest-confirmed parent) |
| 2900 | `V_2` broadcast (`T_commit − 2.5 BTT`) | Targets up to **500ms**; MEV-fetch budget **2750ms** |
| 3100 | `V_1` broadcast (`T_commit − 1.5 BTT`) | Targets up to **300ms**; MEV-fetch budget **2950ms** |
| 3200 | `V_0` broadcast (`T_commit − 1 BTT`) | Targets up to **200ms**; MEV-fetch budget **3050ms** (freshest relay-bundle) |
| 3400 | `T_commit` (= `Relay_cutoff − 3 BTT`) | View-fix deadline; receivers stop counting Phase-1 bundles. `KindCommit` broadcast |
| 3800 | `T_commit + Δ_2` | Hard cluster-wide deadline; σ/NR pool snapshots taken; Phase 3 begins (incl. any Phase-2.5 σ-flip / NR-flip emissions whose trigger fires from the snapshot — see §Phase 2.5) |
| 3900 | Phase 3 complete | Local IBE-walk + BLS aggregation + certificate; `Δ_3 ≈ 100ms` |
| 4000 | `Relay_cutoff` | Cert broadcast + relay submit fit in `header_submit_headroom = 100ms` |

**Recovery scope.** Within Phase 3's single reconstruction walk: silent V_0 → fall through to V_1; silent V_0 + V_1 → V_2; silent V_0 + V_1 + V_2 → V_3 (always honest at f=1 by pigeonhole). For `h_V=1` selective-Phase-1-delivery at any layer, **the slot misses at that layer with no fall-through** (Phase-2.5 σ-flip / NR-flip don't close this case at f=1, n=4 — see §Phase 2.5 / §Liveness); the rational-byzantine deterrent (Assumption 4) bounds it across slots. For narrower Class B asymmetric-byz patterns (R1/R2/R3 in §Liveness), Phase-2.5 σ-flip provides in-protocol recovery within `Δ_3`'s local-CPU budget. Per-layer budgets `B_k` (= the broadcast offset before `T_commit` shown in the rows above): V_0 covers real propagation up to 200ms (1 BTT), V_1 up to 300ms (1.5 BTT), V_2 up to 500ms (2.5 BTT), V_3 up to 1100ms (5.5 BTT). Beyond V_3's budget is out-of-envelope and slot-misses cleanly.

**MEV-fetch-budget asymmetry.** V_0's 3050ms is the freshest budget; deeper layers trade fetch time for wider propagation budget. Per-leader budgets at this operating point: `[V_3: 2150ms, V_2: 2750ms, V_1: 2950ms, V_0: 3050ms]`. The staggered design lets V_0 capture maximum MEV under healthy propagation while deeper backups absorb tails when V_0's bundle doesn't reach in time.

**Phase-2.5 has zero MEV cost.** Phase-2.5 σ-flip / NR-flip are Phase-3 sub-mechanisms, not a separate phase with their own time budget. An emitter whose snapshot satisfies a flip trigger emits the flip and reconstructs locally within `Δ_3`'s existing CPU budget; no extension to `Δ_2` or shift in `T_commit` is needed. V_0's MEV-fetch budget remains 3050ms (unchanged from bare OBFT). Other operators that don't receive a flip in time fall back to `KindCertificate` gossip from the emitter — the existing OBFT lone-reconstructor mechanism for the slot's cluster-level success.

### Comparison vs QBFT (RT = 2000ms, 2-round target)

QBFT-SSV under SSV's production round-timeout (`RT = 2s = 10 BTT`) at the same operating point (`BTT = 200ms`, `Relay_cutoff = 4000ms`, `header_submit_headroom = 100ms`), at recommended sizing (2 BTT per emission cycle — see [docs/BFT-comparison.md / Sizing convention](BFT-comparison.md#sizing-convention)):

| t (ms) | Event | Notes |
|---|---|---|
| 0 | Slot start | |
| 150 | `RANDAO` done | |
| 300 | `PROPOSE_1` | Round-1 leader's MEV-fetch budget = **150ms** (RANDAO + fetch must fit before PROPOSE_1; tight under 2-round target with RT=2s) |
| 1500 | Round-1 success target | `BFT_start_1 + 6 BTT` (3 phases × 2 BTT consensus) |
| 1900 | Round-1 + post-consensus done | `+2 BTT` for partial-sig aggregation |
| 2300 | `RT_1` fires | Round 1 timed out; round-change |
| 2300 | `PROPOSE_2` | Round-2 leader's MEV-fetch budget = **2150ms** (re-fetch can run during R1 timeout window) |
| 3500 | Round-2 consensus done | `BFT_start_2 + 6 BTT` |
| 3900 | Post-consensus done | `+2 BTT` for σ aggregation |
| 4000 | `Relay_cutoff` | Cert + submit fit in 100ms |

**MEV-freshness ranking** at this operating point (incl. partial-sigs-on-pre-agreed-V baseline) at recommended sizing throughout:

| Rank | Leader | MEV-fetch budget | Notes |
|---|---|---|---|
| 1 † | Partial-sigs on pre-agreed V | **3350ms** | Floor; only available if V is pre-agreed (no MEV / no V-disagreement) — not directly applicable to SSV proposer duty |
| 2 | OBFT V_0 | **3050ms** | Best BFT-consensus leader for proposer duty |
| 3 | OBFT V_1 | 2950ms | |
| 4 | OBFT V_2 | 2750ms | |
| 5 (tie) | OBFT V_3 / QBFT R2 leader | 2150ms | QBFT R2 only after paying the R1-timeout gap |
| 7 | QBFT-optimal R1 leader | 950ms | (RT = 6 BTT variant; tighter RT recovers some R1 fetch) |
| 8 | QBFT-SSV R1 leader | 150ms | RT=2s eats most of slot budget under 2-round target |

† **Partial-sigs assumes V is pre-agreed across operators** — works for non-MEV duties (attestations, sync committee) where V is determined by beacon-spec computation, but not for proposer duty where V varies per operator. Listed as the no-consensus floor: BFT consensus protocols pay 300-3200ms over this baseline to resolve V-disagreement.

**Comparison vs partial-sigs floor**: OBFT V_0 pays a **300ms BFT-consensus tax** over the partial-sigs floor (3350 − 3050ms) — this is the structural cost of resolving V-disagreement in a single round at this operating point. The 300ms = 1.5 BTT decomposes as: **1 BTT V_0 leader-broadcast propagation** (OBFT has a single leader source V; partial-sigs assumes V is independently agreed at each operator and skips this step) + **0.5 BTT B_0 convergence buffer** (the part of `B_0 = 1 BTT` beyond pure typical-mesh propagation). Both protocols at recommended 2 BTT per emission. QBFT-SSV R1 pays a **3200ms tax** (10.7× larger), structurally constrained by needing PROPOSE_1 to fire early enough that consensus + post-consensus + R2 retry (with RT=2s) fit the slot.

**Comparison OBFT vs QBFT**: OBFT's V_0 captures **900ms more** MEV-fresh fetch time than QBFT's R2 leader (3050 vs 2150ms), and **2900ms more** than QBFT-SSV's R1 leader (3050 vs 150ms). All four OBFT leaders beat QBFT-SSV R1 by ≥2s. OBFT V_3 ties QBFT R2 at 2150ms — but OBFT V_3 is reached via in-round K-layer fall-through (no round-change cost), while QBFT R2 requires committing to R1's failure first. The MEV-fetch asymmetry is structural: OBFT's K-layer fall-through avoids the round-timeout gap that gates QBFT's R2 fetch.

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_k` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_k`. The protocol consumes the verdict; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_k)` *exactly once per slot/layer*, on the final `V_k` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_k, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_k` is locked on the originally-signed value. The layer's leader cannot subsequently emit NR/NV on `nr_tag_k` — the Phase-1 σ is their σ-side commitment per cross-phase exclusivity.

**Receiver-side validity stabilization — host best-effort toward assumption 3.** OBFT requires host validity to be unanimous at decision time (see [Assumptions](#assumed)). For SSV's proposer duty, the host narrows the divergence window via per-operator validity stabilization: each operator runs the validity check (including `parent_root`-vs-head) **once at Phase-1 acceptance** against a stable head snapshot, then locks the verdict for the remainder of the slot. Subsequent head movements within that operator do not flip the verdict.

**The validity-locking window is per-V, bounded by `B_k`.** Operators accept Phase-1 bundles in `[slot_start, T_commit]`. Each operator locks their verdict per V at first-observation of that V. For V_k, the cluster-wide spread of lock-times is at most `B_k` — the time from earliest possible first-observation (right after `T_broadcast_max_k`) to latest (just before `T_commit`). At the operating point above: V_0's window ≈ 200ms (= 1 BTT), V_1's ≈ 300ms (= 1.5 BTT), V_2's ≈ 500ms, V_3's ≈ 1100ms. **For the practical case where the cluster reconstructs V_0 (healthy path), the relevant window is V_0's 200ms.** Validity-divergence at deeper layers (V_k for k > 0) has a proportionally wider window, but those layers are only reached on fall-through (silent/late primary leader), so the divergence-rate × fall-through-rate product is the practical metric. A re-org landing inside V_0's window can split honest verdicts; the all-honest rate of validity-divergence slot-misses scales with re-org distribution within V_0's `B_0`. **In adversarial-byz deployments the operational rate is multiplicatively higher** — a byzantine within the f-bound exercising passive f-budget (silence or σ-on-V; neither cryptographically slashable individually) widens the deadlock zone beyond the all-honest case. See [§Failure modes / Validity-divergence deadlock](#failure-modes) for the `re-org rate × byz-passivity-rate` scaling.

**This per-operator locking does NOT guarantee cluster-wide convergence.** Operators accept Phase-1 bundles at different `t` within the gossipsub propagation window (operator-local first-observation timestamps, plus clock skew δ). If a re-org happens during this acceptance window, operator A may snapshot pre-reorg (verdict = valid) while operator B snapshots post-reorg (verdict = invalid). Both locks hold; cluster views diverge. The locking ensures *per-operator* stability across the slot, narrowing the divergence window to "re-org during gossipsub-acceptance window" — typical P99 ≈ 100–500ms — but does not eliminate it.

When the residual window fires (re-org lands inside the acceptance window AND operators split across the boundary), assumption 3 is violated and the slot may miss in-protocol — neither σ-quorum (capped at honest-σ-validators + leader's σ_L^V) nor NR-quorum (capped at honest-NV-count) reaches threshold under adversarial byz that withholds NR. This deadlock is the cost of strict re-validation. The looser alternative — **omit per-receiver `parent_root` validation entirely and rely on relay / beacon-node rejection at submit time** — avoids the in-protocol deadlock at the cost of committing cluster-wide on a V whose parent may become orphaned (slot miss surfaces at submit, not at consensus). Hosts pick between the two failure modes based on observed re-org rates.

**SSV's implementation choice (proposer duty, OBFT and QBFT).** Both protocols' value-check paths in SSV (`obftHostValidate` for OBFT, `ProposerValueCheckF` for QBFT) take the looser approach: structural validation + duty/identity match + slashing protection, **no `parent_root` or fork-domain check**. The trade-off is deliberate — strict per-receiver `parent_root` validation triggers assumption-3 violations on common honest re-orgs more often than it prevents byzantine-fork acceptance, and the relay/beacon-node already enforces the latter at submit time. Deployments with much-higher re-org rates than mainnet (or much-stricter byzantine-fork tolerance requirements) may choose to wire strict validation in their host instead.

The "permit and slot-miss" framing parallels OBFT's equivocation handling: validity-divergence is a view-divergence pattern that the protocol does not recover from. Unlike equivocation, it's not attributable to anyone (re-orgs are real-world events, not protocol violations), so no slashing applies; it relies on assumption 3 being approximately true.

**Backup-leader re-org resistance.** Fetching `V_k` for k ≥ 1 from a deeper-confirmed parent (the asymmetric `T_broadcast_max_3 < ... < T_broadcast_max_0` schedule already accommodates this) reduces the likelihood that the backup's parent becomes orphaned. Backups are structurally re-org-resistant by construction.

Implementation notes:

- Each operator validates `parent_root` against a stable head snapshot taken at Phase-1 acceptance time; the verdict is locked into the operator's local state for the remainder of the slot.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" and "single V per operator per layer" — a second signing attempt with a different V at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing AND operator-side double-signing detectable cluster-wide.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same.

2. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. The deadlines:

   - **`T_broadcast_max_k = T_commit − B_k`** per-layer: leader L_k must finish broadcasting by this deadline so its bundle propagates to all honest before `T_commit` under that layer's per-layer propagation budget `B_k`. See §Setting for the staggered-budget design (`B_0 < B_1 < ... < B_{K-1}`).
   - **`T_commit`**: receiver acceptance cutoff. Bundles first-observed past `T_commit` at any honest receiver are not counted by that receiver toward σ-quorum at this layer.

   Phase-window minimums:

   - **`Δ_2 ≥ 1 BTT`**: `KindCommit` propagation budget — operators emit `KindCommit` at `T_commit`, peers must receive it before Phase 3. (Phase-2.5 `KindNRFlip` emissions are part of Phase 3, not Phase 2; they don't extend `Δ_2` — see §Phase 2.5.)
   - **`Δ_3 ≥ ε_3`**: Phase 3 is purely local reconstruction processing (BLS aggregation, IBE decryption walk, certificate construction, plus any Phase-2.5 σ-flip / NR-flip emission + local pool re-aggregation); ≈ 100ms at Config A.

   All are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

3. **Choosing K (layer count).** K is per-duty:

   - `K = 2`: BFT-min at f=1; **not recommended for OBFT** — exposes the late-deepest-layer-leader-broadcast Class A failure mode (see §Failure modes).
   - `K = 3..n`: provides multiple fall-through layers within Phase 3's single reconstruction walk. At `n = 4`, max K is 4 (one layer per operator). Each extra layer adds a level of chained encryption (~1 KB per onion at K=3, ~3 KB at K=4 — within practical bandwidth).

   **Two K bounds (see §Setting for the full discussion):**
   - **`K ≥ f+1`** — BFT-liveness minimum (≥ 1 honest leader exists by pigeonhole over the f-byz bound).
   - **`K ≥ f+2`** — late-leader-resilience recommendation (≥ 2 honest leaders, so a single late-broadcasting honest leader doesn't foreclose the slot via the deepest-layer NR-lock pathology — see §Failure modes / Late deepest-layer leader broadcast).

   Recommended for OBFT proposer duty: **`K = n = 4`** (maximum fall-through depth at f=1, every cluster member leads exactly one layer). `K = f+2 = 3` is also viable at slightly lower bandwidth.

4. **R is fixed at 1.** OBFT is single-round by design. The slot's hard wall is the relay submission deadline; bundles arriving past `T_commit` are not counted, and the cluster relies on K-layer fall-through rather than retry.

5. **Tag construction and replay.** Each NR tag must uniquely bind `(slot, cluster, layer)` to prevent replay across slots/layers/clusters. Structure: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`.

6. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one OBFT instance and assumes:
   - Single OBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between OBFT (`protocol_tag = "OBFT-v2"`) and any other path that signs against the V-signing share (other consensus protocols sharing the same V-keypair).
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`), not just submission.

7. **Equivocation is permitted, not recovered.** OBFT does not provide an in-protocol equivocation recovery mechanism. Some patterns reach qV naturally (when 2-of-3 honest happen to σ-commit on the same V at f=1 n=4; leader's σ_L^V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. Equivocation is treated as a slashable byzantine fault; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots.

## Where this came from

OBFT is the design point that asks: **what's the minimum machinery needed to get K-layer parallel fall-through, in a single round, without within-slot partition recovery?** The motivation is **spec/wire/EKM simplification** — the answer keeps K-layer fall-through, chained encryption, equivocation detect-and-slash, and the five slashing-evidence rules, and omits round-change, round-retry, cross-round σ-or-NR exclusivity, cross-round acceptance widening, and the Defer state. Each operator's primary commitment is exactly one decision at `T_commit` on the 3-state (σ, NR, NV) lattice with a single `KindCommit` message; the EKM is a single signing event per (slot, layer) per operator backed by standard transactional sign-and-log.

**Phase 2.5 σ-flip / NR-flip — safety preservation + narrow Class B recovery.** Earlier OBFT iterations attempted to close the `h_V=1` selective Phase-1 delivery limit via an asymmetric NR-flip-from-σ-er design. Formal verification (TLC at `n=4, f=1, K=2`) surfaced 12 counterexamples (CE-1..CE-12 in `docs/OBFT-formal-verif.md`) showing that asymmetric design unsafe under grief-byz σ-withholding (cluster double-sign on the validator's BLS pubkey — a stake-slashable event). The current design replaces this with a symmetric **σ-flip / NR-flip** mechanism plus snapshot semantics + per-operator-view safety basis + leader-only restriction on NR-flip; it preserves safety against full byz grief at the cost of giving up h_V=1 in-protocol closure (h_V=1 reverts to bare-OBFT slot-miss + Assumption 4 deterrent). The design adds:

- **Two new message kinds** (`KindSigmaFlip`, `KindNRFlip`) carrying threshold partials + trigger evidence (= the emitter's snapshot composition).
- **Mandatory leader-bundle re-flood** in `KindCommit` (`bundle_witnesses` section) replacing the prior σ_L^V witness section: full Phase-1 bundles are byte-for-byte attached for every retained layer, ensuring cluster-wide V availability under within-budget honest propagation. Bandwidth: ~88-228 KB per `KindCommit` at K=4 with blinded V (5-15 KB typical / 50 KB worst on mainnet).
- **Snapshot semantics**: per-operator σ/NR pool snapshots taken at `T_commit + Δ_2` finalize simultaneously cluster-wide; flip triggers evaluate against the actor's snapshot. No-flip-cascade by design: a flip emitted post-finalize doesn't appear in any other honest's snapshot.
- **Two EKM amendments** (σ-flip allows σ-after-NR for non-leader honest; NR-flip allows NR-after-σ for honest leader; both gated by trigger evidence consistent with the receiver's snapshot).
- **Slashing-rule clarifications** (Rule 1 cross-signing exempts trigger-validated flip pairs; detection-at-receipt + local logging for equivocation evidence — see §Slashing evidence).
- **`protocol_tag` bump** to `"OBFT-v2"` for the wire-format change.
- **Zero slot-budget items**: flip emission and local re-aggregation fit within `Δ_3`'s existing local-CPU budget; no `Δ_2` extension needed (the emitter reconstructs locally; propagation to other operators is best-effort via gossipsub + KindCertificate fallback).

Both safety (Pigeonholes 1, 2, 3 under full byz grief incl. selective delivery) and liveness (Class A closure under non-grief byz broadcast) are mechanically verified — see [docs/OBFT-formal-verif.md §7](OBFT-formal-verif.md). At f=1, n=4, σ-flip provides narrow Class B recovery for 3 specific patterns (R1/R2/R3 in §Liveness); NR-flip adds no new recovery at this size. Phase-2.5 is **not** an h_V=1 closure mechanism in the new design — selective Phase-1 delivery slot-misses with no fall-through and is deterred via Assumption 4 (rational-byz + planned blacklist + staker migration).

The remaining cost is no within-slot partition recovery for *equivocation σ-locked splits, validity-divergence, h_V=1 selective Phase-1 delivery* — bundles past `T_commit` are not counted at the affected layer, the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time, and these grief patterns are bounded by Assumption 4 (rational-byzantine deterrent). This is suited for small clusters where the gossipsub mesh is effectively full and adversarial-byz grief is acceptable per the deterrent; the trade-off buys a smaller spec, a simpler EKM, more submission headroom, and a wider deployment envelope at high P99 than 2abOBFT (which closes equivocation and h_V=1 in-protocol at +1 RTT cost).

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
| Bandwidth (healthy, n=4, K=4) | ~28 KB across 2 emissions per round (`KindCommit_r` + `KindLCClaim_r`) | ~28 KB across 1 emission (`KindCommit`) — both include the σ_L^V witness section ≈ +2.3 KB at 145 bytes/witness × 16 witnesses |
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

[2abOBFT](2abOBFT.md) is the Phase-2a/2b successor to OBFT — same K-layer onion structure and chained encryption, but with σ-commitment deferred to a Phase-2b after a Phase-2a observation window. The Phase-2 split adds +1 RTT per slot but recovers in-protocol the equivocation and validity-divergence patterns OBFT cannot.

| Aspect | OBFT | 2abOBFT |
|---|---|---|
| Phase 1 σ_V | Yes — leader signs σ_V at Phase 1 (cryptographic head-start) | **No** — leader broadcasts only `(V, σ^op)` at Phase 1; σ-commitment happens at Phase 2b alongside non-leaders |
| Phase 2 split | Single Phase 2 (operators emit one `KindCommit` at `T_commit`) | 2a (verdict broadcast / σ-eligibility observation) + 2b (σ-emit on cluster-converged V) |
| Equivocation σ-locked split (1-1-1, etc.) | **Slot misses at L_0** — slashable but no in-slot recovery | **Recovered** via convergence rule (verdict-quorum-short → NR fall-through to L_1) |
| h_V=1 selective-delivery deadlock | **Class B grief vector — not closed in-protocol.** Phase-2.5 σ-flip / NR-flip do NOT close h_V=1 at f=1, n=4 (σ-flip's `snap_NR_nl < f+1` blocked; NR-flip honest-leader-only). Slot misses; deterred via Assumption 4 (rational-byz deterrent + planned blacklist + staker migration). Withhold-then-fake-σ variant remains closed as side-effect of Defer removal (see Appendix E). | **Closed** via Phase-2a verdict pool |
| Validity-divergence (re-org during acceptance) | **Out of scope** (assumption 3) — slot misses cleanly | **Recovered within f-bound** (3-of-4 majorities at f=1 n=4); 2-2 splits still slot-miss |
| Mesh-flakiness deadlock | Slot misses (cross-phase exclusivity locks flaky NR) | **Recovered** (Phase-2a defers commitment past mesh outliers) |
| New regression vs OBFT | n/a | **2-1-byz-defect** — byz leader equivocates V/V', verdict-claims σV(V), withholds σ at Phase-2b (NR-emit or silent). Slot misses; [Rule 6b](2abOBFT.md#slashing-evidence) (in 2abOBFT's numbering — verdict-vs-action equivocation) cryptographic only under NR-emit, behavioral under silent. Bare OBFT succeeded here via Phase-1 σ_V lock. |
| Healthy-path latency | 2 RTTs | 3 RTTs (Phase 1 + Phase 2a + Phase 2b) |

**2abOBFT remains OBFT's natural recovery-scope extension** at +1 RTT cost for the patterns OBFT (with Phase-2.5) still does not close — equivocation σ-locked split (1-1-1 at L_0), validity-divergence beyond the host's stabilization window, and mesh-flakiness deadlocks under cross-phase exclusivity. For deployments where these residual patterns matter (high-MEV proposer slots, small adversarial-byz clusters, frequent re-org rates, mesh-flakiness conditions), 2abOBFT closes them in-protocol rather than relying on assumption 4 alone.

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
| Bandwidth (healthy n=4) | ~14 KB across 4 emissions per round (PROPOSE + PREPARE + COMMIT + post-cons.) | ~28 KB across 1 emission at K=4 (`KindCommit`; includes σ_L^V witness section ≈ +2.3 KB at 145 bytes/witness × 16 witnesses) |
| Latency (healthy, n=4, BTT=200ms) | ~800 ms | ~600 ms (Phase 2 + Phase 3 with Δ_2 = 2 BTT) |
| Latency (1 round failure, n=4) | ~3.0 s (round-1 timeout 2s + round-2 success ~750 ms) | n/a (single round; failure → slot miss) |
| Slashing-protection scope | Single block sig per slot, gated at submission time | Multiple per-share sigs per slot at candidate-signing (Phase-1 leader, Phase-2 onion) gated by EKM cross-keypair coordination — see [EKM coordination model](#ekm-coordination-model) |
| Cryptographic primitive | BLS threshold signatures only | BLS threshold signatures + threshold IBE / SWE (drand/tlock-style) |
| Application contract | Decides on any value the application proposes | K-layer with primary + backup-leader fetches |
| Production maturity | What SSV runs today; corner cases hit and fixed | New codebase; deployment confidence to be derived |

**Cost-side trade-off summary:**

- **Latency.** OBFT wins on healthy-path (~600ms vs ~1600ms QBFT-SSV at recommended sizing). On round-1 failure, QBFT can still recover via round-change (at ~3.6s total), while OBFT single-round failures are slot-misses; OBFTR(R=2) covers round-1-failure cases at ~2.4s within the same envelope. OBFT's recovery scope is narrower than QBFT's but available much faster within scope.
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

- **Healthy-path latency.** ~600ms vs ~1600ms QBFT-SSV at recommended sizing.
- **Multi-leader-failure recovery.** OBFT's K-layer parallel fall-through resolves K-1 silent layers within Phase 3's reconstruction walk (sequential local decryption, no per-layer RTT). For K=4 with 3 silent leaders, OBFT recovers in ~600ms; QBFT round-changes 3 times serially, exceeding the 4s budget.
- **All-honest-NR equivocation recovery.** When byz delivers V's early enough for re-flood to spread conflicts before T_commit, all 3 honest retain ≥ 2 V's and emit NR per the equivocation rule; NR-quorum at L_0 → fall-through to L_1. Same recovery as QBFT but in single round (~600ms vs ~3.6s).
- **Spec/EKM simplicity vs OBFTR(R≥2).** No cross-round atomicity, no L_C consensus, no per-round widening — see [§A.1](#a1--comparison-with-obftr-r--2).

**The operational bottom line:** QBFT covers more failure modes (its round-change-with-fresh-V handles validity-divergence and 1-1-1 equivocation that OBFT-family doesn't). OBFT wins on common-case latency and multi-leader-failure recovery. For SSV proposer duty under a 4s relay cutoff, the choice depends on observed re-org rate (favors QBFT), cluster's tolerance for the 1-1-1 equivocation case via the rational-byzantine deterrent (favors OBFT-family), and deployment complexity tolerance (favors OBFT over OBFTR(R≥2) in the family).

## Appendix B — L_Bid mini-consensus extension

This appendix specifies an opportunistic bid-routing extension to OBFT. **L_Bid** is a bid-determined top layer prepended to OBFT's K rotation-determined layers (yielding `K' = K + 1`). The extension adds a **mini-consensus sub-phase between `T_0_arrival` and `T_commit`** that resolves L_Bid's identity cluster-wide before σ-commitment. `T_0_arrival` is the deterministic point by which an honest L_0 Phase-1 bundle broadcast at `T_0_broadcast_max` is expected to have reached all honest operators under the L_0 propagation budget. The mini-consensus is a single round of all-to-all verdict broadcast with quorum-based binding — verdicts are op-identity-signed claims, not threshold partials, so it adds no new cryptographic primitives and does not change OBFT's safety analysis.

The extension closes three deadlock surfaces that any naive bid-routing extension would expose ([§Background — bid-layer deadlocks](#background--bid-layer-deadlocks)) and adds two adversarial-byz residual surfaces at L_Bid (2-1-byz-defect, verdict-equivocation) plus the standard 2-2 validity-divergence hard algebraic limit. Post-`T_commit` latency matches bare OBFT; the cost is paid before `T_commit` by moving `T_0_broadcast_max` earlier by `max(0, Δ_minicon − 0.5 BTT)` (~0-300ms at Config A under named sizings; 0 at aggressive, 100ms at standard, 300ms at conservative — see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening)). Rotation-layer recovery scope is unchanged; safety is identical to bare OBFT. L_Bid relies on one additional assumption beyond bare OBFT's threat model — **bid-value honesty** (see [§Additional assumption — bid-value honesty](#additional-assumption--bid-value-honesty)).

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

**Suited for**: deployments where MEV bid-routing upside among eligible rotation-layer candidates justifies (a) the `max(0, Δ_minicon − 0.5 BTT)` MEV-fetch budget reduction (zero at aggressive sizing; `Δ_minicon − 0.5 BTT` at standard or conservative — see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening)), and (b) the new adversarial-byz residual surfaces at L_Bid. For SSV proposer duty under Config A: high-MEV slots where bid-routed block value capture exceeds the slot-loss-rate cost from the new L_Bid failure modes.

**BTT regime guidance** (see [§Deployment envelope by BTT](#deployment-envelope-by-btt) for full table):
- **BTT ≤ 400ms** (production-typical mesh): conservative sizing (`Δ_minicon = 2 BTT`) fits with comfortable MEV-fetch budget (2000ms+).
- **BTT 600-800ms** (degraded mesh): conservative sizing's MEV-fetch budget shrinks aggressively or stops fitting; standard (`Δ_minicon = 1.5 BTT`, `Δ_verdict = 1 BTT`) recovers some MEV-fetch budget at slightly lower L_Bid success rate (P99 verdict prop, 0.5 BTT bundle tail-absorption); aggressive (`Δ_minicon = 0.5 BTT`, `Δ_verdict = 0.5 BTT`) recovers maximum MEV-fetch but introduces partial-propagation deadlock risk and zero bundle tail-absorption.
- **BTT ≥ 1000ms** (severely degraded): only aggressive sizing fits L_Bid (tightly at BTT=1200ms). Bare OBFT (no `Δ_minicon`) remains the available alternative.

**Not suited for**: deployments prioritizing maximum MEV-fetch budget at high BTT, or where the L_Bid residuals (adversarial-byz + sub-1-BTT `Δ_verdict` partial-propagation deadlock) are a hard constraint (slot-miss without fall-through; see [§Liveness](#liveness)).

### Timing notation for L_Bid variants

This appendix and Appendix F use three arrival anchors with distinct meanings:

| Symbol | Meaning |
|---|---|
| `T_0_arrival` | Current L_Bid mini-consensus start: the point by which an honest L_0 Phase-1 bundle broadcast at `T_0_broadcast_max` is expected to have reached all honest operators. Current L_Bid sets `Δ_minicon = T_commit − T_0_arrival` and shifts all eligible rotation-layer deadlines to this anchor. |
| `T_deep_arrival` | L_Bid_New mini-consensus start: the point by which honest deep-layer Phase-1 bundles (`L_1..L_{K-1}`) broadcast at their L_Bid_New deadlines are expected to have reached all honest operators. L_Bid_New sets `Δ_minicon = T_commit − T_deep_arrival` and shifts only deep-layer deadlines to this anchor. |
| `T_broadcast_max_0^bare` | Bare OBFT primary broadcast deadline = `T_commit − B_0` (= `T_commit − 1 BTT` at Config A). L_Bid_New preserves this deadline for bid_1. |
| `Δ_minicon` | Total mini-consensus interval, from the relevant arrival anchor (`T_0_arrival` or `T_deep_arrival`) to `T_commit`. |
| `Δ_verdict` | Sub-interval reserved for `KindBidVerdict` propagation: `T_verdict = T_commit − Δ_verdict`. |
| `Δ_select` | In-window bid-set settling budget before verdict broadcast: `Δ_minicon − Δ_verdict`. |

### Setting

Adds to OBFT's setting:

- **K' = K + 1 layers**: L_Bid (top, bid-determined) + OBFT's rotation-determined L_0, L_1, ..., L_{K-1}.
- **Bid data lives inside Phase-1 bundles**: there is no standalone `KindBid` wire message. Each rotation leader's Phase-1 bundle carries bid metadata for that same `V_{L_k}`. Only rotation-layer candidates are eligible for L_Bid; with `K = n` this still means every operator has one bid candidate, while with `K < n` L_Bid ranks only the selected `K` rotation leaders.
- **Mini-consensus window** `Δ_minicon`: `Δ_minicon = T_commit − T_0_arrival`. Mini-consensus starts at `T_0_arrival`, ends at `T_commit`, and all L_Bid timing derives from that interval. `T_0_broadcast_max = T_0_arrival − B_0_LBid`, and generally `T_broadcast_max_k = T_0_arrival − B_k_LBid` — note L_Bid uses tighter per-layer budgets `B_k_LBid` (typical-mesh propagation only, no convergence buffer; e.g., `B_0_LBid = 0.5 BTT` at Config A vs bare OBFT's `B_0 = 1 BTT`); the rationale is that L_Bid is opportunistic and doesn't need bare OBFT's convergence guarantee at the bid layer (see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening) below). The L_0..L_{K-1} broadcast deadlines shift earlier vs bare OBFT by `max(0, Δ_minicon − 0.5 BTT)`. `T_commit` itself stays back-end-anchored to `T_relay_cutoff − submit_headroom − Δ_3 − Δ_2` and is unchanged from bare OBFT.
- **Verdict propagation budget** `Δ_verdict`: `0 < Δ_verdict ≤ Δ_minicon`, with `T_verdict = T_commit − Δ_verdict`. Operators compute and broadcast `KindBidVerdict` at `T_verdict`; those verdicts propagate until `T_commit`. The remaining `Δ_select = Δ_minicon − Δ_verdict` is the in-window bid-set settling budget after `T_0_arrival` and before verdict broadcast.
- **L_Bid σ-eligibility**: determined cluster-wide by mini-consensus, not by per-operator local computation.
- **Bid visibility threshold**: `qBid = K − f` L_Bid-eligible Phase-1 bundles. At `K = n`, `qBid = n − f = qV`; for `K < n`, L_Bid intentionally ranks a smaller candidate universe.

`qV = qEnc = 2f+1` and the BLS+IBE keypair structure are unchanged from bare OBFT. The mini-consensus adds no new threshold cryptography.

### Wire kinds

In addition to OBFT's `Phase1Bundle`, `KindCommit`, `KindCertificate`:

- **`Phase1Bundle` bid section** — L_Bid extends the existing Phase-1 bundle envelope with `(bid_value, relay_attestation)`, signed by the leader's operator-identity key as part of the structured Phase-1 envelope. The bid section is not a separate message: it refers to exactly the same `V_{L_k}` carried by the bundle and signed by `σ_{L_k}^V(V_{L_k})`. The `relay_attestation` field is host-defined; verification is governed by an optional protocol extension (see [§Optional extension — relay/builder attestation verification](#optional-extension--relaybuilder-attestation-verification)). When the extension is disabled, the field MAY be empty or host-supplied.
- **`KindBidVerdict`** — operator `i`'s mini-consensus verdict. Payload `(protocol_tag = "OBFT-LBid-v1", message_kind = "minicon-verdict", cluster_id, slot, operator_id i, predicted_LBid_value_root_or_null)`, signed by `i`'s operator-identity key. `predicted_LBid_value_root` is set when `i` claims a specific V is the cluster's L_Bid winner; null when `i` claims no L_Bid (insufficient bid-set visibility, parent-root filter failure, or no consensus reachable).

### Per-layer windows and deadlines

Phase 1 inherits bare OBFT's per-layer staggered broadcast schedule, but the arrival anchor for the L_Bid extension is `T_0_arrival = T_commit − Δ_minicon`. The latest L_0 bundle broadcast deadline is `T_0_broadcast_max = T_0_arrival − B_0_LBid`; deeper leaders broadcast by `T_broadcast_max_k = T_0_arrival − B_k_LBid`. (Tighter per-layer budgets than bare OBFT — see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening).) Mini-consensus starts at `T_0_arrival` and ends at `T_commit`:

| Phase | Window | Activity |
|---|---|---|
| Phase 1 candidate broadcast | `[slot_start, T_0_arrival]` | Rotation leaders broadcast Phase-1 bundles per per-layer windows (`T_broadcast_max_k = T_0_arrival − B_k`). Each bundle carries its own bid metadata. Receivers continue accepting bundles first-observed in `[slot_start, T_commit]` per bare OBFT, but L_Bid verdict computation uses only bundles first-observed by `T_verdict`. |
| Mini-consensus | `[T_0_arrival, T_commit]` | Operators compute predicted L_Bid (argmax over `bid_set_i` first-observed by `T_verdict = T_commit − Δ_verdict`) and broadcast `KindBidVerdict`; verdicts propagate by `T_commit`. |
| Phase 2 | `[T_commit, T_commit + Δ_2]` | σ-or-NR commit at all K' layers (L_Bid + L_0..L_{K-1}). |
| Phase 3 | (from `T_commit + Δ_2`) | K'-layer reconstruction walk. |

Sizing — `Δ_minicon` is the total mini-consensus interval; `Δ_verdict` is the portion reserved for verdict propagation; `Δ_select = Δ_minicon − Δ_verdict` is the bid-set settling buffer (post-`T_0_arrival` window during which late-arriving bundles still enter `bid_set_i`). Three named sizings, each step adding 0.5 BTT of robustness on top of the previous:

- **Conservative** `Δ_minicon = 2 BTT`, `Δ_verdict = 1 BTT`, `Δ_select = 1 BTT`: highest L_Bid success rate. P99 verdict propagation + 1 BTT bundle tail-absorption (exceeds bare OBFT's convergence buffer on the bundle side — bundles arriving up to `B_0 + 1 BTT` after broadcast still enter `bid_set_i`).
- **Standard** `Δ_minicon = 1.5 BTT`, `Δ_verdict = 1 BTT`, `Δ_select = 0.5 BTT`: medium L_Bid success rate. P99 verdict propagation + 0.5 BTT bundle tail-absorption (matches bare OBFT's convergence buffer on the bundle side).
- **Aggressive** `Δ_minicon = 0.5 BTT`, `Δ_verdict = 0.5 BTT`, `Δ_select = 0`: lowest L_Bid success rate. Sub-P99 verdict propagation (verdicts may not all arrive by `T_commit` under tail jitter — partial-propagation deadlock becomes a Class A residual, see [§New residual failure modes](#new-residual-failure-modes-at-l_bid)) + zero bundle tail-absorption (bundles arriving past typical-mesh `B_0_LBid` are excluded from `bid_set_i` at the affected receivers). Suitable only when production telemetry shows mesh propagation tighter than the standard P99 assumption. Matches bare OBFT V_0's broadcast deadline exactly — bare OBFT's convergence buffer is fully reused as `Δ_verdict` propagation.

**Success-rate gradient.** As you go from conservative → standard → aggressive, the probability of L_Bid converging on a bid winner decreases monotonically:

- Two failure modes degrade under tighter sizings: (a) tail-arriving bundles excluded from `bid_set_i` (smaller `Δ_select`); (b) tail-arriving verdicts excluded from `verdict_pool` (smaller `Δ_verdict`).
- Conservative tolerates the widest tail on both. Standard tolerates a 0.5 BTT bundle-tail and a P99 verdict tail. Aggressive tolerates neither typical-mesh-tail bundles nor sub-P99 verdict jitter, and additionally exposes the cluster to all-honest partial-propagation deadlock at L_Bid (Class A).
- When L_Bid mini-consensus fails to converge, the cluster falls through to L_0 cleanly — bare OBFT's recovery scope at L_0 is unchanged. So "lower L_Bid success rate" means "more slots fall through to L_0's vanilla payload instead of the bid-routed payload" — not slot-misses (except at aggressive sizing's Class A residual).

`Δ_2`, `Δ_3`, and `B_k` (bundle propagation budgets) unchanged from bare OBFT.

`T_commit` is back-end-anchored at `T_relay_cutoff − submit_headroom − Δ_3 − Δ_2` and is **the same value for bare OBFT and OBFT+L_Bid** (e.g., `3400ms` at §Application's max-MEV anchor with Config A, BTT=200ms, `Δ_2 = 2 BTT`, `Δ_3 = ε_3`, `header_submit_headroom = 100ms`, `T_relay_cutoff = 4000ms`).

What L_Bid changes is the **L_0..L_{K-1} broadcast deadlines** and the **MEV-fetch budget**: `T_0_broadcast_max = T_0_arrival − B_0_LBid = T_commit − Δ_minicon − B_0_LBid` (with tighter `B_0_LBid = 0.5 BTT` at Config A vs bare OBFT's `B_0 = 1 BTT`, since L_Bid drops the convergence buffer at the bid layer). The net broadcast-deadline shift vs bare OBFT is `max(0, Δ_minicon − 0.5 BTT)`: bare OBFT's convergence buffer (~0.5 BTT) gets repurposed inside the mini-consensus window, and any excess of `Δ_minicon` over that 0.5 BTT is the actual MEV-fetch cost L_Bid pays. At Config A:

- **Conservative** (`Δ_minicon = 2 BTT = 400ms`): L_0 broadcast shifts from `3200ms` (bare OBFT) to `2900ms` (L_Bid); MEV-fetch reduces from ~3050ms to ~2750ms.
- **Standard** (`Δ_minicon = 1.5 BTT = 300ms`): L_0 broadcast shifts to `3000ms`; MEV-fetch ~2850ms.
- **Aggressive** (`Δ_minicon = 0.5 BTT`): shift collapses to zero — `T_0_broadcast_max = T_broadcast_max_0 = 3200ms`; MEV-fetch = ~3050ms (= bare OBFT V_0). The convergence buffer is fully reused as `Δ_verdict` propagation; the cluster does mini-consensus work in the same wall-clock as bare OBFT's bundle tail-absorption.

### L_Bid broadcast-deadline tightening

Bare OBFT sizes `B_0 = 1 BTT` at Config A as ~0.5 BTT typical-mesh propagation plus ~0.5 BTT **convergence buffer** — extra time between expected typical-mesh arrival and `T_commit` so all honest converge cluster-wide on which leaders broadcast before any operator commits (see [§Setting](#setting)). The convergence buffer is **required at every layer** in bare OBFT because σ-or-NR commit is mandatory: each layer must reach σ-quorum or NR-quorum at `T_commit` for the cluster to make progress, so without it honest operators could wrongly NR-emit on layers where the bundle was actually in flight, deadlocking the slot.

L_Bid uses a tighter per-layer budget at the bid layer — `B_0_LBid = 0.5 BTT` (typical-mesh propagation only, no convergence buffer) — because bid-routing is **opportunistic, not mandatory**:

- Late bundles excluded from one operator's `bid_set_i` only mean that operator predicts a different argmax (or null). The cluster converges via `verdict_quorum` on whichever V enough operators saw on time.
- If no `verdict_quorum` forms, the cluster falls through to L_0 — which is bare OBFT's L_0 with its full `B_0 = 1 BTT` (convergence buffer included). So the bid layer doesn't need a convergence guarantee of its own; bare OBFT's L_0 covers the case.
- The post-`T_0_arrival` budget `Δ_select = Δ_minicon − Δ_verdict` (nonzero at standard or conservative sizing) provides bid-set settling for late-arriving bundles — a different role from bare OBFT's convergence buffer (settling improves bid-routing rate; convergence buffer prevents deadlock).

**Aggressive sizing's natural floor.** Setting `Δ_minicon = 0.5 BTT` (= bare OBFT's convergence buffer at Config A) makes L_Bid's leader broadcast deadline coincide with bare OBFT V_0's. The post-broadcast 100ms window that bare OBFT uses as convergence buffer is relabeled as `Δ_verdict` propagation — same wall-clock budget, different role. Aggressive L_Bid pays zero MEV-fetch cost vs bare OBFT V_0; the cluster does extra work (mini-consensus) within the same time. Standard adds 1 BTT vs aggressive (paying 1 BTT MEV-fetch cost = 200ms) to upgrade verdict propagation from sub-P99 to P99 *and* gain 0.5 BTT of bundle tail-absorption. Conservative adds another 0.5 BTT of bundle tail-absorption on top of standard (paying another 100ms = 1.5 BTT total MEV-fetch cost vs bare OBFT V_0).

**Timeline comparison** (Config A, BTT=200ms, L_0 layer; deeper layers broadcast earlier and are off-table):

| Time (ms) | Bare OBFT | L_Bid conservative | L_Bid standard | L_Bid aggressive |
|---|---|---|---|---|
| 2900 | (fetching) | **leader broadcast** (`T_0_broadcast_max`) | (fetching) | (fetching) |
| 3000 | (fetching) | **bundle arrival** (`T_0_arrival`) | **leader broadcast** (`T_0_broadcast_max`) | (fetching) |
| 3100 | (fetching) | bid-set settling | **bundle arrival** (`T_0_arrival`) | (fetching) |
| 3200 | **leader broadcast** (`T_broadcast_max_0`) | **verdict broadcast** (`T_verdict`) | **verdict broadcast** (`T_verdict`) | **leader broadcast** (`T_0_broadcast_max`) |
| 3300 | **typical-mesh arrival** | verdict propagation | verdict propagation | **bundle arrival + verdict broadcast** (`T_0_arrival = T_verdict`) |
| 3400 | **σ/NR commit** (`T_commit`) | **σ/NR commit** (`T_commit`) | **σ/NR commit** (`T_commit`) | **σ/NR commit** (`T_commit`) |

Post-broadcast budget allocation (broadcast → `T_commit`):

| Variant | Total | Breakdown |
|---|---|---|
| Bare OBFT | 200ms | `B_0 = 1 BTT` = 100ms typical-mesh propagation + 100ms convergence buffer |
| L_Bid aggressive | 200ms | 100ms typical-mesh propagation + 100ms `Δ_verdict` (same layout as bare OBFT; convergence buffer relabeled as `Δ_verdict`) |
| L_Bid standard | 400ms | 100ms typical-mesh propagation + 100ms `Δ_select` settling + 200ms `Δ_verdict` (1 BTT P99 verdict propagation) |
| L_Bid conservative | 500ms | 100ms typical-mesh propagation + 200ms `Δ_select` settling + 200ms `Δ_verdict` |

Reading the gradient column-by-column:

- **Aggressive** matches bare OBFT V_0 exactly in broadcast time and post-broadcast layout — only the role label on the 100ms post-arrival window changes (convergence buffer → `Δ_verdict` propagation). Zero MEV-fetch cost vs bare OBFT V_0; lowest L_Bid success rate (no settling, sub-P99 verdict prop).
- **Standard** broadcasts 200ms (1 BTT) earlier than bare OBFT V_0, gaining a 0.5 BTT bundle tail-absorption window plus 1 BTT P99 verdict propagation. Pays 1 BTT (200ms) MEV-fetch cost.
- **Conservative** broadcasts 300ms (1.5 BTT) earlier than bare OBFT V_0, gaining a full 1 BTT bundle tail-absorption window plus 1 BTT P99 verdict propagation. Pays 1.5 BTT (300ms) MEV-fetch cost; highest L_Bid success rate.

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

Measured from `T_commit` (start of Phase 2 σ-emit; mini-consensus already complete). At Config A (P99=150ms, δ=50ms, `Δ_2 = 400ms` recommended, `Δ_3 = ε_3`). The same scenarios apply to L_Bid_New (Appendix F) with V_early replacing V_X at L_Bid and bid_1 replacing V_{L_0} at L_0; rotation-layer scenarios are unchanged since post-`T_commit` timing is identical across bare OBFT, current L_Bid, and L_Bid_New:

| Scenario | Time | Mechanism |
|---|---|---|
| L_Bid σ-quorum reaches early in Phase 2 (early-reconstruct path) | ~`1 BTT ≈ 200ms` | σ-emit propagation completes 1 RTT into Phase 2; operator reconstructs at L_Bid plaintext |
| L_Bid σ-quorum reaches at end of Phase 2 (canonical) | ~`Δ_2 + Δ_3 ≈ 500ms` | Full Phase 2 + Phase 3 walk |
| Mini-consensus failed at end of Phase 1 → fall-through to L_0 | ~`Δ_2 + Δ_3 ≈ 500ms` | NR-quorum at L_Bid (already determined pre-T_commit) + Phase-3 walk decrypts L_0; L_0 σ-quorum |
| Multi-layer fall-through after L_Bid | ~`Δ_2 + Δ_3 ≈ 500ms` | K'-layer walk in Phase 3 (sequential local decryption, no extra RTT per layer) |
| L_Bid 2-1-byz-defect, verdict-equivocation, or partial-propagation deadlock | slot misses | Deadlock at L_Bid blocks fall-through |

Post-`T_commit` timing **matches bare OBFT** since mini-consensus runs pre-`T_commit`. L_Bid's cost is paid in the pre-`T_commit` budget (`max(0, Δ_minicon − 0.5 BTT)` of MEV-fetch budget — `Δ_minicon`'s overlap with bare OBFT's convergence buffer doesn't cost extra; only the excess does), not in post-`T_commit` slot consumption.

### Deployment envelope by BTT

`T_commit` is back-end-anchored and invariant across bare OBFT and OBFT+L_Bid. What L_Bid changes is the **L_0..L_{K-1} broadcast deadline**, which shifts earlier by `max(0, Δ_minicon − 0.5 BTT)` to fit mini-consensus pre-`T_commit`. The primary leader's MEV-fetch budget shrinks by the same amount vs bare OBFT — zero at aggressive sizing (`Δ_minicon = 0.5 BTT`), `Δ_minicon − 0.5 BTT` at standard or conservative sizing.

The table below shows L_0 broadcast deadline (= MEV-fetch budget at `slot_start = 0`) across BTT regimes (`T_relay_cutoff = 4.0s`, `submit_headroom = 100ms`, `Δ_3 = ε_3 = 100ms`, `Δ_2 = 2 BTT`, `B_0 = 1 BTT` for bare OBFT, `B_0_LBid = 0.5 BTT` for L_Bid). Bare OBFT row included for comparison:

| BTT | Bare OBFT | Δ_minicon=2 BTT (conservative) | Δ_minicon=1.5 BTT (standard) | Δ_minicon=0.5 BTT (aggressive) |
|---|---|---|---|---|
| 200ms | 3200ms ✓ | 2900ms ✓ | 3000ms ✓ | 3200ms ✓ |
| 400ms | 2600ms ✓ | 2000ms ✓ | 2200ms ✓ | 2600ms ✓ |
| 600ms | 2000ms ✓ | 1100ms ✓ | 1400ms ✓ | 2000ms ✓ |
| 800ms | 1400ms ✓ | 200ms ✓ tight | 600ms ✓ | 1400ms ✓ |
| 1000ms | 800ms ✓ | **−700ms ✗** | **−200ms ✗** | 800ms ✓ |
| 1200ms | 200ms ✓ tight | **−1600ms ✗** | **−1000ms ✗** | 200ms ✓ tight |

(L_Bid loss vs bare OBFT = `max(0, Δ_minicon − 0.5 BTT)`. At aggressive sizing where `Δ_minicon = 0.5 BTT`, L_Bid's broadcast deadline matches bare OBFT exactly — bare OBFT's convergence buffer is repurposed as `Δ_verdict` propagation; see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening). Negative MEV-fetch means L_0 broadcast deadline is before slot_start; the slot doesn't fit.)

**Net for deployment selection.** At production-typical BTT (200-400ms), conservative sizing fits with comfortable MEV-fetch budget (2000-2900ms) and the highest L_Bid success rate. At degraded mesh (BTT 600-800ms), conservative shrinks aggressively or starts to stop fitting; standard recovers some MEV-fetch budget at slightly lower L_Bid success rate (still P99 verdict propagation, slightly tighter bundle tail-absorption); aggressive recovers maximum MEV-fetch (matches bare OBFT V_0) but at the cost of partial-propagation deadlock risk (see [§New residual failure modes](#new-residual-failure-modes-at-l_bid)) and zero bundle tail-absorption. At BTT ≥ 1000ms, only aggressive sizing fits L_Bid (tightly at BTT=1200ms); standard and conservative drop out. The L_Bid trade has two knobs: `Δ_minicon` controls MEV-fetch budget *and* L_Bid success rate (larger Δ_minicon → more bundle tail-absorption); `Δ_verdict` controls verdict-propagation safety (≥ 1 BTT for P99 guarantees). Choose both from production telemetry. Bare OBFT (no `Δ_minicon`) remains available when the trade isn't favorable.

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
| `T_commit` anchor | back-end: `T_relay_cutoff − submit_headroom − Δ_3 − Δ_2` | **Same** (`T_commit` invariant across bare OBFT and OBFT+L_Bid; cross-family T_commit anchors differ — see [BFT-comparison.md](BFT-comparison.md#scope-and-assumptions)) |
| Best-case latency post-`T_commit` (early reconstruct) | ~200ms (`1 BTT`) | **Same** (~200ms; mini-consensus runs pre-`T_commit`) |
| Canonical latency post-`T_commit` (full Phase 2 + Phase 3) | ~500ms (`Δ_2 + Δ_3`) | **Same** (~500ms) |
| Time-to-completion spread (best → canonical) | ~2.5× | **Same** |
| Bandwidth (n=4, K=4 healthy) | ~28 KB across 1 emission | Base bandwidth + K bid-metadata sections + n verdicts + 1 chained encryption layer (no standalone bid envelopes) — 2 emissions (`KindCommit` + `KindBidVerdict`) |
| L_0 broadcast deadline | `T_broadcast_max_0 = T_commit − B_0` (e.g., 3200ms at Config A max-MEV anchor with `B_0 = 1 BTT`) | `T_0_broadcast_max = T_0_arrival − B_0_LBid = T_commit − Δ_minicon − B_0_LBid` (with tighter `B_0_LBid = 0.5 BTT` — see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening); e.g., 2900ms at conservative `Δ_minicon = 2 BTT`) |
| MEV-fetch budget (4s cutoff, `header_submit_headroom = 100ms`, §Application's max-MEV anchor) | ~3050ms (V_0; T_commit = 3.40s) | ~2750ms (V_X) at conservative `Δ_minicon = 2 BTT`; ~2850ms at standard `Δ_minicon = 1.5 BTT`; ~3050ms at aggressive `Δ_minicon = 0.5 BTT` (= bare OBFT V_0; convergence buffer repurposed as `Δ_verdict`) |
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

**Net trade vs bare OBFT**: pays `max(0, Δ_minicon − 0.5 BTT)` of MEV-fetch budget (L_0 broadcast deadline shifts earlier; `T_commit` and post-`T_commit` consumption unchanged; aggressive sizing where `Δ_minicon = 0.5 BTT` pays zero MEV-fetch cost) and additional adversarial-byz residual surface at L_Bid (slot-miss-without-fall-through; mixed evidence quality) plus an all-honest Class A residual at sub-1-BTT `Δ_verdict`, in exchange for bid-routing value capture on the healthy path. The L_0/.../L_{K-1} layers' recovery scope is unchanged. Whether favorable depends on MEV-value-capture upside vs slot-loss cost (byzantine and partial-propagation residuals) at the chosen `Δ_minicon` / `Δ_verdict`.

## Appendix C — Leader-bundle re-flood (`bundle_witnesses`)

Mandatory leader-bundle re-flood — every operator's `KindCommit` carries a `bundle_witnesses` section with byte-for-byte copies of every Phase-1 bundle the operator retained — is **core protocol** under OBFT-v2 (not optional defensive engineering). This appendix gives the design rationale, costs, and what the mechanism does vs doesn't address.

### Why core (vs optional)

The mechanism closes both V-drop and σ_L^V-drop under within-budget honest propagation:

- Under byz selective Phase-1 delivery (or honest-leader propagation tail), the leader's bundle may reach only a subset of honest receivers by `T_commit`. Without re-flood, non-receivers don't have V locally and can't σ-emit even after gossipsub eventually propagates the bundle past `T_commit`.
- With mandatory re-flood, **any single honest retainer's `KindCommit` carries the bundle** to all peers via gossipsub. By `T_commit + Δ_2` (the snapshot point), all honest who receive the retainer's `KindCommit` have V locally and can:
  - Verify σ_L^V against V from the bundle (closes σ_L^V-drop).
  - Reference V in `VAvailable(viewer, k, v)` checks for σ-flip evaluation (cluster-wide V-availability — necessary precondition for σ-flip's V-availability check, even though σ-flip's other trigger conditions may not hold at h_V=1 — see [§Phase 2.5](#phase-25--σ-flip--nr-flip-flips)).
  - Be eligible for σ-flip on V if their snapshot satisfies `SigmaFlipTriggered` (R1/R2/R3 narrow recoveries — see §Liveness).

The key design property: **cluster-wide V availability under within-budget honest propagation, given any single honest retainer**. Combined with the f-bound on byz contributions, this makes the algebraic-cardinality mutex argument (Pigeonhole 1) valid for the per-operator-view safety basis.

### Wire format

`bundle_witnesses` is a section in `KindCommit` carrying a list of `(layer_k, Phase1Bundle_k)` entries. `Phase1Bundle_k` is the byte-for-byte received Phase-1 bundle (= `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` plus envelope metadata) — the same SSZ type as the bundle the leader broadcast at Phase 1.

**Inclusion rule.** Include `bundle_witnesses[k]` iff the emitter retained an auth-valid bundle for `(slot, layer k, leader_id)` by the time of `KindCommit` emission (= the first auth-valid bundle observed; per §Phase 1 / Retention bounds). Skip layers where the operator did not retain.

**Equivocation case.** If the emitter observed two distinct auth-valid bundles for the same `(slot, layer, leader_id)` (= leader equivocation), they retain the first one only and log the second locally as Rule 2 evidence; only the retained bundle goes into `bundle_witnesses`. **Slashing-implicating data does not travel in protocol messages** — equivocation evidence is detected at receipt by honest receivers (each sees both bundles via gossipsub initial flood) and logged locally, with out-of-band SSV slashing-contract submission as the remedy. See §Slashing evidence.

**Receiver verification.** Each `Phase1Bundle_k` in `bundle_witnesses` is verified against the leader's pubkey + envelope re-derivation independently; the relaying operator is just a transport — no trust required. A relay operator that fakes a bundle (signs something the leader didn't) cannot succeed (they don't have the leader's signing keys).

### Bandwidth

OBFT proposer duty is constrained to **blinded blocks only** (see §Application: SSV Ethereum proposer duty). Per-entry size ≈ V_size + 200 B (sigs + envelope overhead). With blinded V typically 5–15 KB on mainnet (worst case ~50 KB with full attestations + slashings):

| n=4, K=4 | Per-op `KindCommit` | Cluster outbound | Per-op ingress |
|---|---|---|---|
| Bare-OBFT-v1 (sigma_L_witnesses, value_root + σ_L^V only) | ~28 KB | ~112 KB | ~84 KB |
| OBFT-v2 (`bundle_witnesses`, blinded V typical 15 KB) | ~88 KB | ~352 KB | ~264 KB |
| OBFT-v2 (`bundle_witnesses`, blinded V worst 50 KB) | ~228 KB | ~912 KB | ~684 KB |

Sustained per-op outbound at worst case ≈ 19 KB/s over a 12 s slot — within commodity SSV operator network budgets. **Implementation note**: gossipsub mesh handling at ~228 KB messages may need configuration tuning (typical gossipsub mesh defaults are tuned for smaller messages); recommend deployment-time validation.

### What re-flood does NOT close

- **`h_V=1` selective Phase-1 delivery (Class B)**: re-flood ensures cluster-wide V availability post-snap, but at f=1, n=4 the σ-flip trigger `snap_NR_nl < f+1 = 2` doesn't hold (2 honest NR-ers see each other's NR partials in their snapshot — `snap_NR_nl = 2`, blocking σ-flip). NR-flip is honest-leader-only and the leader is byz. Slot misses; deterred via Assumption 4. Re-flood is necessary but not sufficient.
- **σ-locked equivocation 1-1-1 splits**: cross-phase exclusivity locks honest into different σ commits before re-flood propagates conflicts cluster-wide. Algebraic limit; not addressable at the propagation layer.
- **Silent leader**: no honest retains; `bundle_witnesses` for the silent layer is empty cluster-wide; standard NR-quorum fall-through path applies (no flip needed).
- **Sustained partition** (real propagation > absorption window for ALL layers): no honest receives anything in time; re-flood has nothing to relay. Class A.

### Cross-references

- §Phase 1 / Bundle propagation, §Phase 1 / Retention bounds: retention ≤ 1 bundle per `(slot, layer, leader_id)` keyed by first-auth-valid observation.
- §Phase 2 / Wire format: `bundle_witnesses` field structure and auth-envelope binding under `protocol_tag = "OBFT-v2"`.
- §Phase 2.5: σ-flip's `VAvailable` precondition, satisfied cluster-wide via `bundle_witnesses` re-flood.
- §Slashing evidence: equivocation (Rule 2) detection-at-receipt + local logging; not transmitted in protocol messages.
- §Liveness, §Failure modes: R1/R2/R3 σ-flip recovery scope; h_V=1 not closed; deterrent via Assumption 4.


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

- OBFT: σ-locked equivocation 1-1-1, validity-divergence, `h_V=1` selective Phase-1 delivery — slot-miss patterns at any layer; deterred via Assumption 4 across slots (see §Phase 2.5).
- OBFT-replenish: **identical exposure to bare OBFT.** New layers introduced in later rounds are subject to the same residual byzantine patterns (equivocation, validity-divergence, h_V=1 selective Phase-1 delivery). If byz exercises σ-locked split at L_0 in round 1, the L_0 σ-locks block fall-through; later layers' chained encryption stays sealed under L_0/L_1's nr_tags. Phase-2.5 σ-flip / NR-flip applies per-round (closing the same narrow Class B patterns it does in bare OBFT — see §Liveness) but does NOT close h_V=1.

R-invariant: replenishment changes neither the cross-phase σ-or-NR algebra nor the leader's Phase-1 σ_V locking, so the same residual patterns persist regardless of layer count or staging.

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
4. **Adversarial-byz exposure is unchanged from bare OBFT.** Replenish does not provide additional protection against the residual patterns σ-locked equivocation, validity-divergence, or `h_V=1` selective Phase-1 delivery; closing those would require structural changes orthogonal to replenishment. Phase-2.5 σ-flip / NR-flip carries over to OBFT-replenish per-round (closing the same narrow Class B patterns it does in bare OBFT — see §Liveness) but does NOT close h_V=1.
5. **Healthy-path latency is identical to OBFT**. Reduced layer count in round 1 doesn't compress consensus time — the Phase 1/2/3 cycle is the bottleneck.
6. **OBFT-replenish vs OBFT trade summary**:
   - **+** Bandwidth saving on healthy slots (real if round-1-success dominates).
   - **+** V freshness at later rounds (genuine MEV upside for late-resolving slots).
   - **+** Deeper fall-through at the cost of multi-round retry.
   - **−** Multi-round implementation complexity comparable to OBFTR.
   - **−** Bandwidth grows past OBFT in multi-round failure cases.
   - **=** Same adversarial-byz exposure as OBFT (R-invariant patterns unfixed).
   - **=** Same healthy-path latency.

Treat OBFT-replenish as a research direction worth specifying further if the late-MEV freshness motivation is significant for the target deployment. If the priority is simplicity and predictable behavior, bare OBFT (K=4 fixed) remains the cleaner choice. Replenishment is orthogonal to the structural changes that would be needed for adversarial-byz coverage.

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

Variant B (selective Phase-1 delivery — byz broadcasts Phase-1 to exactly one honest, not withholding) is an algebraic limit at f=1, n=4 that neither Defer nor Defer-removal addresses at the σ-commitment-timing layer, and that the current Phase-2.5 σ-flip / NR-flip mechanism does **not** close either (see [§Phase 2.5](#phase-25--σ-flip--nr-flip-flips)): σ-flip's `snap_NR_nl < f+1 = 2` doesn't hold (2 honest NR-ers each see 2 NR partials in their snapshot); NR-flip is honest-leader-only and the leader is byz. Variant B remains a Class B grief vector deterred via Assumption 4. So bare OBFT (Defer-removed + Phase-2.5 added) closes Variant A (via Defer removal) but **not** Variant B; the h_V=1 family is partially mitigated, not fully closed.

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

Treat OBFT+Defer as a candidate enhancement worth re-evaluating if deployment conditions change — wider partial-synchrony, higher f, stronger rational-byz deterrent infrastructure, or production telemetry showing aggressive-marginal partition slot-misses at non-trivial rates. Under SSV's current n=4 healthy-mesh proposer-duty profile, bare OBFT (3-state, single-emission) is the cleaner trade.

## Appendix F — OBFT + L_Bid_New (deep-bid mini-consensus)

This appendix specifies **L_Bid_New** as a candidate alternative to the L_Bid mini-consensus design ([Appendix B](#appendix-b--l_bid-mini-consensus-extension)). The two extensions share the same goal — opportunistic bid-routing across eligible rotation-layer Phase-1 candidates — but L_Bid_New trades a different set of properties. Where L_Bid puts the bid-routing winner V_X on the outermost (plaintext) onion layer and reaches it via cluster-wide convergence on **all K rotation-layer bids**, L_Bid_New restricts cluster-wide convergence to **deep-layer bids only** (V_early) and incorporates the primary's late bid (bid_1) at an inner onion layer, gated by chained encryption.

The structural trade vs current L_Bid: L_Bid_New preserves bare OBFT V_0's primary MEV-fetch budget (~3050ms) at every sizing by excluding bid_1 from mini-consensus, at the cost of exposing bid_1 to bare-OBFT-style asymmetric-delivery patterns at the bid layer (where current L_Bid uses mini-consensus to converge on bid_1's status). The primary-MEV-fetch gain vs current L_Bid is sizing-dependent: 300ms at conservative, 200ms at standard, **0 at aggressive** (where current L_Bid already matches bare OBFT V_0 because `Δ_minicon = 0.5 BTT`). Post-`T_commit` latency is the same order as current L_Bid because both variants run mini-consensus before `T_commit`. The timing distinction is pre-`T_commit`: current L_Bid starts mini-consensus at `T_0_arrival` and shifts every eligible rotation-layer deadline earlier; L_Bid_New starts mini-consensus at `T_deep_arrival` and shifts only deep-layer deadlines earlier.

L_Bid_New is documented here as a candidate design point. Whether it is preferable to current L_Bid in production is deployment-dependent — see [§F.6 Comparison with current L_Bid](#f6--comparison-with-current-l_bid).

### F.1 — Setting

L_Bid_New extends OBFT's setting in the same way L_Bid does — bid metadata inside Phase-1 bundles, mini-consensus convergence on a bid winner — with these structural differences:

- **K' = K + 1 layers** (same as L_Bid): an additional bid-layer prepended to OBFT's K rotation-determined layers.
- **Mini-consensus window = deep-arrival to commit**: `Δ_minicon = T_commit − T_deep_arrival`. Mini-consensus starts at `T_deep_arrival`, ends at `T_commit`, and all L_Bid_New mini-consensus timing derives from that interval. `T_deep_arrival` is the deterministic point by which honest deep-layer Phase-1 bundles (`L_1..L_{K-1}`) broadcast at their L_Bid_New deadlines are expected to have reached all honest operators under their propagation budgets. Deep leaders use `T_broadcast_max_k = T_deep_arrival − B_k_LBid` for `k ≥ 1` (same opportunistic tighter-budget shape as current L_Bid — bid-routing is non-mandatory; see [§L_Bid broadcast-deadline tightening](#l_bid-broadcast-deadline-tightening)); the primary `L_0` uses the bare OBFT broadcast deadline and is not shifted by `Δ_minicon`.
- **Mini-consensus scope = deep bids only**: the mini-consensus runs over `{V_i : operator i is L_k for k ≥ 1}` — i.e., the bids associated with the deeper-layer rotation leaders. The primary's bid (operator at L_0) is *not* in the mini-consensus bid set. The deep visibility threshold is `qBid_deep`, normally `(K − 1) − f` when the deep candidate set has enough layers to tolerate f byz omissions; deployments with smaller deep candidate sets must configure this threshold explicitly.
- **Verdict propagation budget** `Δ_verdict`: `0 < Δ_verdict ≤ Δ_minicon`, with `T_verdict = T_commit − Δ_verdict`. Operators compute and broadcast `KindBidVerdict` at `T_verdict`; those verdicts propagate until `T_commit`. The remaining `Δ_select = Δ_minicon − Δ_verdict` is the in-window deep-bid settling budget after `T_deep_arrival` and before verdict broadcast.
- **Primary-bid placement** (vs current L_Bid): V_early (the mini-consensus winner over deep bids) is the **outermost** plaintext layer; bid_1 (= the primary's bid = V_{L_0}) is at the inner L_0 layer encrypted under `nr_tag_LBid`. The encryption layout is *structurally identical* to current L_Bid (both have L_Bid plaintext outer, L_0 wrapped under `nr_tag_LBid`, deeper layers chained via `nr_tag_0..k-1`); the change is in the *content* of the mini-consensus — current L_Bid's V_X is argmax over all eligible bids (including primary's bid_1), while L_Bid_New's V_early is argmax over deep bids only. Primary's bid is removed from mini-consensus and lives only at L_0.

The key intuition: under L_Bid_New, the cluster outputs `argmax(V_early, bid_1)` via the chained-encryption fall-through structure rather than via a single mini-consensus over all K rotation-layer bids. Operators σ-emit on V_early at L_Bid when V_early ≥ bid_1 (in their local view); they NR at L_Bid otherwise (preferring fall-through to L_0 where bid_1's σ-pool aggregates). The chained encryption gates L_0 reconstruction on L_Bid NR-quorum — preserving the cluster's single-output guarantee (Pigeonhole 3).

### F.2 — Wire kinds

L_Bid_New uses the same wire framing as L_Bid ([Appendix B](#appendix-b--l_bid-mini-consensus-extension)):

- **`Phase1Bundle` bid section**: bid metadata carried by each eligible rotation leader's Phase-1 bundle. There is no standalone `KindBid` message.
- **`KindBidVerdict`**: mini-consensus verdict, broadcast over deep-layer Phase-1 bundle bids only.
- **`KindCommit`**: Phase-2 onion + NR-partials + σ_L^V witness section (inherits from base OBFT).
- **`KindCertificate`**: final certificate.

The mini-consensus verdict (`KindBidVerdict`) carries a verdict on V_early specifically — not on the full argmax over all K rotation-layer bids. This is a semantic difference from L_Bid, which verdicts on V_X = argmax over the full eligible rotation-layer bid set.

### F.3 — Per-layer windows and deadlines

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
| Phase 3 | `[T_commit + Δ_2, T_round_end]` | K'-layer reconstruction walk. |

Sizing — `Δ_minicon` is the total deep mini-consensus interval, `Δ_verdict` is the portion reserved for verdict propagation, and `Δ_select = Δ_minicon − Δ_verdict` is the deep-bid settling buffer. Same shape and meaning as in current L_Bid, applied only to deep bids:

- **Conservative** `Δ_minicon = 2 BTT`, `Δ_verdict = 1 BTT`, `Δ_select = 1 BTT`: highest deep-bid success rate; P99 verdict propagation + 1 BTT deep-bid tail-absorption.
- **Standard** `Δ_minicon = 1.5 BTT`, `Δ_verdict = 1 BTT`, `Δ_select = 0.5 BTT`: medium deep-bid success rate; P99 verdict propagation + 0.5 BTT deep-bid tail-absorption.
- **Aggressive** `Δ_minicon = 0.5 BTT`, `Δ_verdict = 0.5 BTT`, `Δ_select = 0`: lowest deep-bid success rate; sub-P99 verdict propagation + zero deep-bid tail-absorption. Partial-propagation deadlock becomes a Class A residual, same as current L_Bid's aggressive sizing.

`T_commit` is the same back-end anchor as bare OBFT (~`Relay_cutoff − 3 BTT` = 3400ms at Config A). L_Bid_New pays the `max(0, Δ_minicon − 0.5 BTT)` pre-`T_commit` cost only on deep-layer candidate deadlines; the primary L_0 MEV-fetch budget remains `T_broadcast_max_0^bare − slot_start` ≈ 3050ms (= bare OBFT's primary budget). Post-`T_commit` timing matches current L_Bid and bare OBFT.

### F.4 — Protocol

#### F.4.1 — Phase 1: Deep bids + primary bundle

Each deep-layer rotation leader broadcasts its Phase-1 bundle with bid metadata by `T_broadcast_max_k = T_deep_arrival − B_k` for `k ≥ 1`. The primary additionally broadcasts its Phase-1 bundle (bid_1 + `σ_{L_0}^V` + op-auth) at `T_broadcast_max_0^bare` per bare OBFT.

As in L_Bid, bid/bundle coherence is structural: each bid is the bid metadata carried by the same Phase-1 bundle whose V is signed by the layer leader. Deep bundles serve as mini-consensus candidates; the primary's bundle serves as bid_1 and as the L_0 candidate, but not as a mini-consensus input.

#### F.4.2 — Mini-consensus: verdict on V_early

Each operator `i`, by `T_verdict = T_commit − Δ_verdict`:

1. Computes `bid_set_i_deep` = received-and-validated deep-layer Phase-1 bundles with bid metadata, first-observed before `T_verdict`. Deep bundles first-observed after `T_verdict` remain retained for ordinary rotation-layer processing and slashing evidence, but do not affect this verdict. **The primary's bid (bid_1) is NOT in `bid_set_i_deep`** — primary's bid is treated separately at L_0.
2. Computes `predicted_LBid_i`: `argmax over bid_set_i_deep` if `|bid_set_i_deep| ≥ qBid_deep`; else null, where `qBid_deep` is the configured visibility threshold for the deep candidate set.
3. Broadcasts `KindBidVerdict` carrying their prediction.

At mini-consensus end (`T_commit`):

- `verdict_pool[V] = | { distinct ops j broadcasting first-observed KindBidVerdict(j, slot, hash(V)) } |`
- `verdict_quorum_V_early ≡ ∃V : verdict_pool[V] ≥ qV`

If `verdict_quorum_V_early` reaches on some V, that V is the cluster's V_early. Otherwise no V_early; cluster falls through L_Bid via NR-quorum.

#### F.4.3 — Phase 2: σ-or-NR at K' = K + 1 layers

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

  **Note**: An alternative rule is "NR-when-uncertain" (NR if no bid_1, regardless). The two rules differ in their failure modes — see [§F.5 Failure modes](#f5--failure-modes). The σ-when-uncertain variant is recommended; NR-when-uncertain has a complementary failure mode at a different honest-split configuration. Formal analysis of the choice is in `docs/OBFT-formal-verif.md`.

- **L_0**: σ on bid_1 if received and host-valid (standard bare OBFT rule for L_0); NR otherwise. As the onion diagram above shows, L_0's σ partial is wrapped under `nr_tag_LBid` — so reconstructing at L_0 (whether σ-quorum on bid_1 or fall-through to L_1) requires L_Bid NR-quorum to unlock the chained encryption (same gating as current L_Bid; see [§Why L_Bid is the outermost chained-encryption gate](#why-l_bid-is-the-outermost-chained-encryption-gate)).
- **L_k for k ≥ 1**: same as bare OBFT.

Cross-phase exclusivity per `(slot, layer)` and single-σ-V per `(slot, layer)` per operator continue to hold across all K' layers; EKM enforces.

#### F.4.4 — Phase 3: K'-layer reconstruction walk

```
1. L_Bid: σ-pool[V_early] plaintext. If ≥ qV: reconstruct (V_early, S); halt.
2. Else: NR-pool[L_Bid] check. If ≥ qEnc: aggregate decryption key; unlock L_0.
3. L_0: σ-pool[bid_1] (decrypted). If ≥ qV: reconstruct (bid_1, S); halt.
4. Else: NR-pool[L_0] check. If ≥ qEnc: unlock L_1; continue.
5. Walk L_1..L_{K-1} as bare OBFT.
```

If L_Bid reaches neither σ-quorum nor NR-quorum, the chained encryption to L_0 stays sealed; no fall-through; slot misses. Same structural property as current L_Bid (chained-encryption priority enforces single-V output).

### F.5 — Failure modes

L_Bid_New's failure modes split into (a) modes inherited from bare OBFT at L_0 and below, and (b) modes specific to the L_Bid layer's σ-or-NR commitment under asymmetric bid_1 delivery or deep mini-consensus divergence.

#### F.5.1 — Inherited from bare OBFT

When L_Bid NR-quorums and falls through to L_0, L_0 behaves as bare OBFT's L_0 (with bid_1 as V_{L_0} and the primary's σ_{L_0}^V as the leader's contribution). Inherits all bare-OBFT L_0 failure modes:

- **σ-locked equivocation at L_0** (Class B): byz primary equivocates bid_1; honest σ-lock on different V's; deadlock at L_0 with chained-encryption seal preventing fall-through.
- **h_V=1 selective Phase-1 delivery at L_0** (Class B): byz primary broadcasts bid_1 to one honest only; σ-pool fragments.
- **Asymmetric propagation of primary's bundle past `T_commit`** (Class A): violates assumption 2 for L_0; cluster falls through to L_1 if propagation symmetric there.

These are bare OBFT's failure modes restated; same Class A / Class B framing.

#### F.5.2 — Specific to L_Bid_New's L_Bid layer

The L_Bid layer's σ-or-NR commitment depends on each honest's local view of (V_early, bid_1, V_early-vs-bid_1), and on whether honest operators converge on the same V_early verdict set by `T_commit`. Asymmetric bid_1 delivery (one honest doesn't have bid_1) creates an honest-split at L_Bid:

- **Asymmetric bid_1 delivery + V_early > bid_1**: honest with bid_1 σ on V_early (per the σ-when-uncertain rule's `bid_1 ≤ V_early` clause); honest without bid_1 σ on V_early (per the rule's `no bid_1` clause). All `n−f` honest σ at L_Bid → σ-pool[V_early] = n−f = 3 = qV at f=1, n=4 → σ-quorum reaches → output V_early. **No deadlock at f=1, n=4 under either non-grief or adversarial byz** (byz σ adds; byz NR or silence doesn't subtract from honest σ-pool). This case is exactly what σ-when-uncertain handles cleanly — the rule's purpose is to keep operators-without-bid_1 in the σ-camp on V_early. NR-when-uncertain would 2-1-split honest here (honest with bid_1 σ; honest without bid_1 NR) and produce the complementary residual.

- **Asymmetric bid_1 delivery + bid_1 > V_early**: honest with bid_1 NR at L_Bid (prefer bid_1); honest without bid_1 σ on V_early (per the σ-when-uncertain rule). Mixed split. Under non-grief at f=1, n=4 with 2 honest having bid_1, 1 honest without: σ-pool = 1, NR-pool = 2. Neither reaches qV/qEnc=3. **In-assumption deadlock under σ-when-uncertain — Class B at f=1, n=4.**

  - Under the NR-when-uncertain rule, this case recovers cleanly (all 3 honest NR → NR-quorum at L_Bid → fall-through to L_0 → σ-pool[bid_1] = 2 + primary's σ_{L_0}^V = 3 = qV). NR-when-uncertain has its complementary residual at V_early > bid_1 (where it would 2-1-split honest at L_Bid).
  - **The choice of σ-vs-NR-when-uncertain trades which asymmetric-delivery configuration deadlocks**. Neither rule eliminates the residual at f=1, n=4 (this is an algebraic limit per [§F.7](#f7--algebraic-floor)); the choice picks which corner case is the residual.

- **Verdict-equivocation by byz on V_early**: byz emits different verdicts to different honest peers; honest local-verdict-views diverge; some honest σ at L_Bid on V_early, others NR. Same algebraic deadlock pattern. Class B.

- **Selective deep-candidate withholding / equivocation**: same mini-consensus residual shape as current L_Bid, but scoped to `L_1..L_{K-1}` only. If no V_early reaches verdict quorum, L_Bid NR-quorums and falls through to L_0 cleanly. If byz helps form a verdict quorum and then NR-defects or stays silent in Phase 2, L_Bid can deadlock before L_0 unlocks. Evidence quality mirrors current L_Bid: cryptographic when the trigger/action includes signed equivocation or signed NR-after-verdict, behavioral for silence variants.

- **Partial verdict propagation under aggressive `Δ_verdict`**: if `Δ_verdict` is sized below the deployment's honest-message propagation bound, honest operators may not receive the same `KindBidVerdict` set by `T_commit`. This is the same Class A residual as current L_Bid's sub-1-BTT verdict-propagation mode, now over deep-bid verdicts only.

#### F.5.3 — Comparison: which asymmetric-delivery deadlocks are Class A vs Class B

Asymmetric bid_1 delivery can arise from:

- Network mesh failure (broken link, mesh sparsity): violates assumption 2 if propagation exceeds `B_0`. **Class A.** Slot-miss is out of the protocol's recovery scope by design.
- Byzantine primary signing-and-broadcasting bid_1 selectively (sends to gossipsub but only to a subset of peers): if byz uses standard gossipsub, this is a network-level outcome (peers see what gossipsub delivers). If byz crafts targeted delivery, this counts as active byz grief (deviation from honest broadcast). **Class B** in the latter case.

Per the threat model in `docs/OBFT-formal-verif.md`, selective delivery via standard gossipsub is treated as a network-level property (not byz grief); the protocol cannot distinguish byz-induced from network-induced asymmetry, and both are out of scope for the Class A closure property.

### F.6 — Comparison with current L_Bid

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
| Post-`T_commit` consensus budget | ~500ms (Δ_2 + Δ_3; mini-consensus is pre-`T_commit`) | ~500ms (Δ_2 + Δ_3) |
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

### F.7 — Algebraic floor

Both L_Bid and L_Bid_New are subject to the same algebraic deadlock floor at f=1, n=4: any non-unanimous honest commitment split at any layer is exploitable by adversarial byz to engineer a 2-2 deadlock (σ-pool ≤ 2, NR-pool ≤ 2, neither reaching qV = qEnc = 3). The floor scales uniformly across n ∈ {4, 7, 10, 13}: the deadlock condition is "(h_σ < qV) AND (h_NR < qEnc)" and adversarial byz can engineer this from any non-unanimous honest split.

The protocols differ in *which* adversarial scenarios produce non-unanimous splits, not in whether the algebraic floor exists. Formal verification of this floor and per-protocol-variant exposure is the subject of `docs/OBFT-formal-verif.md`.

### F.8 — When to use L_Bid_New vs L_Bid

**Use L_Bid_New** when:
- Primary MEV-fetch budget is the binding constraint (high-MEV slots, late-discovery relay queries).
- Primary's mesh reliability is high and relay-attestation extension is enforced.

**Use current L_Bid** when:
- Bid_1's Class B exposure must be minimized (adversarial primary likely; primary mesh unreliable).
- MEV-fetch headroom is generous; the `Δ_minicon` cost before `T_commit` is acceptable.
- Deployment prefers protocol-level convergence on every eligible rotation-layer bid (rather than per-operator argmax computation on the late bid).

**Either is acceptable** for production-typical mesh + institutional/permissioned operator sets where primary equivocation is unlikely. The choice is then driven by primary MEV-fetch budget vs which residual surface the deployment prefers; it is not a post-`T_commit` latency choice, because both variants run mini-consensus before `T_commit`.

**Note on aggressive sizing.** At aggressive sizing (`Δ_minicon = 0.5 BTT`), current L_Bid's primary MEV-fetch budget already matches bare OBFT V_0 (~3050ms) — equal to L_Bid_New's. The two variants then differ only in which bids are subject to mini-consensus (current L_Bid: all eligible rotation-layer bids; L_Bid_New: deep bids only) and which Class B residual surface the bid layer exposes. The primary-MEV-fetch advantage L_Bid_New offers vanishes at aggressive sizing.
