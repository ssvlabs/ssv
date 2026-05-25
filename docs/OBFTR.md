# OBFTR — Onion BFT with R-Rounds

A multi-round agreement protocol for SSV clusters that produces one collective threshold-signed value per "slot" against a hard deadline. OBFTR achieves agreement *cryptographically* (cluster-wide unique output via threshold cryptography over EKM-enforced per-operator commitments) over a configurable K-layer onion structure, with up to R recovery rounds providing graceful degradation under network partitions.

OBFTR runs K layers (configurable, `f+1 ≤ K ≤ n` — `f+1` is the BFT-liveness minimum; see §Setting) over R rounds (configurable, `R ≥ 1`). Each round is a self-contained 3-phase OBFT-style instance with its own leaders and `T_commit_r` deadline; rounds 2..R fire on timeout when the prior round's reconstruction failed. Each round's commitments are independent (no cross-round σ-or-NR exclusivity) — late-arriving bundles from a prior round get a fresh chance to be σ-emitted on in the next round if they propagate by that round's `T_commit`. This extends partition tolerance to ~R·D propagation that bare [OBFT](OBFT.md) (R = 1) cannot reach.

OBFTR's recovery scope is intentionally bounded. The protocol recovers from **network-partition** patterns (gossipsub propagation > round-1 cutoff but ≤ R·D) via per-round retry with new leaders and gossipsub re-flood absorbing late bundles. The protocol does NOT recover from **view-divergence** patterns: equivocation (byzantine leader sending different V's to different honest) and validity-divergence (e.g., re-org during the bundle-acceptance window causing some honest to validate V and others to NV). View-divergence cases either succeed naturally (when the split happens to leave 2-of-3 honest σ-committed on the same V at f=1 n=4, allowing leader's σ_L^V to push the pool to qV) or slot-miss. Equivocation in non-naturally-recovered patterns is **slashable** (the byzantine leader pays the stake-based penalty); validity-divergence is not attributable to anyone and just slot-misses. See [Assumptions and implications](#assumptions-and-implications) for the explicit list.

The protocol description below targets `n = 4` (`f = 1`) as the running example, with K and R tunable per duty. SSV's Ethereum proposer duty is used as the running application example. Generalizations to `n ≥ 7` are noted inline where the algebra changes.

OBFTR is the multi-round generalization of [OBFT](OBFT.md): it adds R-round retry on top of OBFT's single-round K-layer onion structure. Readers unfamiliar with OBFT may want to read it first; this spec covers OBFTR-specific structure (round mechanics, cross-round atomicity, L_C signaling) on top of the shared base.

## When to use it

**Suited for:** any SSV duty (proposer, attestation, sync committee, DKG) where the 2-RTT healthy-path latency is desired plus tolerance for network partitions. The configurable R and RT let operators tune recovery aggression per duty's deadline budget. Particularly suited for proposer duty (4s relay cutoff) where the round-2 overhead is ~250ms vs QBFT's ~2s round-change.

**Not suited for:** scenarios requiring host-validity-divergence recovery within a slot — OBFTR assumes host validity is unanimous at decision time (see [Assumptions](#assumptions-and-implications)). QBFT is the appropriate choice when validity is unstable across the consensus window. Bare OBFTR (at any R) also does not defend against an adversarial byzantine that deliberately engineers σ-locked split equivocation, and — unlike bare [OBFT](OBFT.md) and [2abOBFT](2abOBFT.md) — does not close h_V=1 selective-delivery either (OBFTR's witness section re-broadcasts σ_L^V but not V; bare OBFT and 2abOBFT close h_V=1 under healthy mesh via peer-reflood-V). These patterns are R-invariant in OBFTR (per-round retry doesn't reconcile honest σ-locks made under selective Phase-1 delivery). The rational-byzantine deterrent (assumption 4) handles the residual across many slots; per-slot they cause clean slot-miss with weakly-slashable behavioral evidence. [2abOBFT](2abOBFT.md) is the appropriate choice when in-protocol h_V=1 closure (or σ-locked-split recovery) is required.

**Also not suited for:** general-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. OBFTR gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n = 3f+1` (the BFT-tight setting; matches SSV's deployment configurations `n ∈ {4, 7, 10, 13}`). The running example is `n = 4, f = 1`. The threshold formula `qV = qEnc = 2f+1` below equals `n − f` exactly at this setting; this equality is what makes the Pigeonhole arguments in [§Safety](#safety-cryptographic-honest-majority) hold.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) as the threshold-signing scheme for no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety". At `n = 4`, `qEnc = 3`.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts and protocol-level claims, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- **K layers** (`f+1 ≤ K ≤ n`, configurable) with deterministically-derived leaders: layer 0 with **primary leader** `L_0`, layer 1 with **backup leader** `L_1`, ..., layer `K-1` with `L_{K-1}`. Leaders must be distinct (`L_i ≠ L_j` for `i ≠ j`). For `K = n`, every cluster member is a leader at exactly one layer per slot.

  **Spec-level K bounds:**
  - **`K ≥ f+1` is the BFT-liveness minimum** — pigeonhole over the f-byz bound guarantees at least one honest leader. At `K < f+1`, all leaders could be byzantine and no σ-quorum reaches at any layer. This is the only K-floor the protocol mandates.
  - At `K = f+1`, the cluster has exactly one honest leader; a single late-broadcasting honest leader at the deepest layer can foreclose the slot — NR-quorum reaches at that layer but there is no deeper layer to fall through to at `K = f+1` (see §Failure modes / Late deepest-layer leader broadcast). `K ≥ f+2` guarantees ≥ 2 honest leaders, providing late-leader-resilience at the cost of one additional layer's leader-broadcast budget.

  Choice of K is **deployment-dependent** and left to the operator. Clusters with low-tail propagation, tight per-operator SLAs, or no expected Byzantine presence may prefer smaller K (fewer leaders, simpler reconstruction walk, smaller chained-encryption depth). Clusters operating closer to the partial-synchrony tail or treating adversarial-byz tolerance as a hard requirement may prefer `K ≥ f+2` or `K = n`. OBFTR's per-duty K choice trades off layer-count vs round-count within the duty's timing budget: at `K = f+1` more weight falls on the R-round retry; at `K ≥ f+2` more weight falls on within-round fall-through.
- **R rounds** (`R ≥ 1`, configurable) with **round timeout** `RT`. Round 1 runs the standard Phase 1 → Phase 2 → Phase 3 sequence (2-RTT healthy path). Rounds 2..R fire on timeout when prior round's reconstruction failed at all layers — they run with new leaders for new layer slots and gossipsub continues propagating any late bundles from prior rounds, which can now be σ-emitted on if received before the new round's `T_commit_r`. The R choice is per-duty: `R = 2` for proposer duty (one recovery round fits 4s budget); `R ≥ 3` for non-proposer duties.
- Per-layer leader-fetch deadlines `T_{K-1} < T_{K-2} < ... < T_1 < T_0` (within each round), plus a per-round cluster deadline `T_commit_r` for round `r`. (`T_commit_r` is a *view-fix point* for round `r`: each operator commits its stance based on what it observed by `T_commit_r`. Reconstruction and submission happen after.) The asymmetric fetch times let primary leaders fetch high-MEV values late (`T_0` close to `T_commit_1`) while deeper layers' leaders fetch safe early values from deeper-confirmed parents (`T_{K-1}` well before `T_0`).
- **Time unit `BTT` (broadcast trip time)** — `P99` is the propagation budget at the deployment's chosen tail percentile (the variable name `P99` is shorthand for the high-percentile propagation latency; deployments may use P99, P999, P9999 etc. as the actual percentile depending on tail tolerance). `δ` is the cluster's clock-skew bound. We define `1 BTT = P99 + δ` — the time needed for one one-way message to propagate from a sender to all honest receivers under partial-synchrony assumptions. This unit is used throughout for time-budget formulas; the underlying `P99` and `δ` are kept distinct only in §Trust model (where partial synchrony is defined) and in safety arguments (Pigeonhole proofs). Concrete sizing at Config A: `P99 = 150ms, δ = 50ms, 1 BTT = 200ms`.
- **`BFT_start` — slot-relative offset at which round 1's Phase 1 begins.** Pre-fetch and pre-consensus (block builder calls, partial-sig RANDAO aggregation, etc.) sit in `[slot_start, BFT_start]`; the BFT consensus phase runs in `[BFT_start, T_commit_R + Δ_2 + Δ_2.5 + ε_3]` across R rounds. In the spec's pure-timing model `BFT_start = 0` (round 1 Phase 1 fires at slot start with no pre-fetch overhead). SSV's proposer-duty application sets `BFT_start > 0` — see §Application for SSV-specific sizing. Subsequent rounds inherit the same anchor (each round's `T_commit_r` and `T_broadcast_max_r` are measured from `slot_start`); leader broadcasts cannot land before `BFT_start`.
- **Per-round leader broadcast deadline** `T_broadcast_max_r = T_commit_r − 1 BTT`. Each round's leaders must finish broadcasting by `T_broadcast_max_r` so that under P99 propagation, all honest first-observe by `T_commit_r`. Per-layer fetch windows are `[T_k_r, T_broadcast_max_r]`. Bundles first-observed past `T_commit_r` are not counted by that receiver toward σ-quorum at round r's layers; if reconstruction fails at round r, gossipsub may still deliver those bundles to that receiver before round r+1's `T_commit_{r+1}`, in which case round r+1 can σ-emit on them. This cross-round retention is OBFTR's structural mesh-jitter / reflood absorber (replacing the per-round jitter cushion that the older `T_broadcast_max_r = T_commit_r − 2 BTT` framing baked into Δ_2).

  This is OBFTR's structural difference vs single-round [OBFT](OBFT.md): late delivery is not a slot-miss-causing failure mode within the protocol's R-round budget — bundles late by one round's `T_commit_r` get a fresh chance to be σ-emitted on in round r+1.

  **Broadcast-schedule divergence vs [OBFT](OBFT.md) and [2abOBFT](2abOBFT.md).** All three siblings use a different per-layer broadcast shape. OBFT uses **primary-vs-backup** (`B_0 = 2·BTT + RefloodDelay`, `B_1..B_{K-1} = T_commit`) — the primary carries MEV, every backup absorbs up to the entire commit budget. 2abOBFT keeps the older **staggered** shape (`B_k_shallow = (k+2)·BTT + RefloodDelay`) because Phase-2a's re-flood window already provides uniform backup absorption on top of `B_k` — see [2abOBFT.md §Setting](2abOBFT.md). OBFTR uses a **flat per-round schedule** with no per-layer `B_k` at all: every layer's leader broadcasts by `T_broadcast_max_r = T_commit_r − 1·BTT`, and the structural absorber is the cross-round retention described above (a round-r-late bundle still propagates between rounds and gets a fresh σ-quorum chance in round r+1). The flat schedule is the natural shape once cross-round retention is doing the job that primary-vs-backup's wide `B_k` does for single-round OBFT — a per-layer staggered shape would be redundant on top of R-round re-flood. The trade-off is that within a single round OBFTR has no wider-than-`1·BTT` per-layer absorption: a single-round OBFTR(R=1) has narrower *per-layer* tail absorption than primary-vs-backup OBFT (which compensates with wide backup `B_k` and K-layer fall-through); OBFTR's advantage manifests once R≥2 turns cross-round retention on — its analogous compensation.
- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

## Assumptions and implications

OBFTR's claims hold conditional on a small set of explicit assumptions about the deployment environment. They are consolidated here; the rest of the spec assumes them and refers back to this section rather than re-deriving the trade-offs each time.

### Assumed

1. **Standard BFT trust bound at the tight setting.** `n = 3f + 1`, up to `f` operators may be byzantine. At `n = 4`, `f = 1`. SSV deploys at the BFT-tight bound (`n ∈ {4, 7, 10, 13}` for `f ∈ {1, 2, 3, 4}`); the threshold formula `qV = qEnc = 2f+1` below requires this tightness to give `2f+1 = n − f`, which is what the Pigeonhole arguments depend on. Honest operators run protocol-conformant software (correct EKM rule enforcement, correct host application, correct gossipsub behavior). Byzantine operators may deviate arbitrarily within their f-bound.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `P99` (cluster gossipsub P99/P999) and clock skew `δ`. Safety is unconditional on timing; only liveness depends on this.

3. **Host validity is unanimous at decision time** (best-effort assumption). OBFTR assumes the host application's `valid` / `not-valid` verdict on `V_{L_k}` is the same across all honest operators by the time they emit Phase 2. Operators may transiently diverge (e.g., a head change observed by some but not others). The host's job is to make divergence rare via per-operator stabilization — typically by validating against a stable head snapshot taken at Phase-1 acceptance time, then locking the verdict for the remainder of the slot. **This per-operator locking does not give cluster-wide convergence; it narrows the divergence window to events that land inside the receiver acceptance window** = R-round absorption window (per-round acceptance horizons + cross-round retention through round R; ≈ 1050ms at Config A R=2 with tightened Δ_2 = 1 BTT per round). When divergence does occur — a re-org during the acceptance window with operators accepting on either side of it — the assumption is violated: an honest-majority split still recovers (3-σ → L_0; 1-σ-3NV → L_1 via NR fall-through), but a 2-2 boundary split (or validity-divergence with a passive byzantine) deadlocks and the slot misses. **Note**: the per-round receiver acceptance horizon (= T_commit_r at tightened Δ_2 = 1·BTT — no within-round absorption) plus cross-round retention widens the validity-locking time spread proportionally with the absorption window. The wider absorption window trades partition recovery against validity-divergence exposure. See [Application: SSV Ethereum proposer duty / Head-change handling](#head-change-handling) for the SSV-specific stabilization workflow and the residual divergence window.

   The validity check exists to prevent the cluster from agreeing on a garbage / invalid V — it is not a divergence-recovery mechanism. NV is operationally identical to NR for protocol counting; it does not trigger any in-protocol divergence-handling path.

4. **Persistent operator set with rational-byzantine deterrent.** OBFTR operates within a stable SSV cluster running protocol instances over many slots. The deterrent is the same one that already disciplines an offline operator under SSV's network-wide threat model: per-validator operator fees flow continuously to all cluster operators regardless of per-slot contribution (the remaining `n − f` honest carry the work at zero ops cost to the silent/byzantine), and stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters, collapsing the silent/byzantine operator's fee accrual to zero. SSV is already designed for the operator-down case ("the cluster and stakers deal with it"); the rational-byzantine claim is that a byzantine operator gains nothing an offline operator wouldn't already get, and has reputation (persistent across slots) to lose.

   **Asymmetry — Byzantine vs Down — and what restores equivalence.** With QBFT, `Byzantine ≡ Down` automatically: round-change rotates past silent or malformed PROPOSE/PREPARE/COMMIT, so the worst a byzantine can do per-slot is silently going offline. OBFTR has no QBFT-style round-change escape valve, so byzantine is *significantly worse on latency than Down* — equivocation σ-locked splits, h_V=1 selective-delivery, fake-encrypted-presence, and behavioral σ-refusal can engineer per-slot grief above what equivalent offline behavior would produce. The expected mitigation is **manual blacklisting**: the cluster's surviving `n − f` operators agree out-of-band on the misbehaving operator's identity, push a config-file update to their nodes treating that operator's messages as silent for subsequent slots, and the byzantine's effective contribution becomes identical to offline — restoring the `Byzantine ≡ Down` guarantee. **The blacklist mechanism is a planned protocol extension; the current OBFTR spec does not specify it.** Until added, the byzantine's per-slot grief surface above offline behavior is bounded only by stakers eventually migrating validators away from the cluster.

   The on-wire byzantine-fault evidence ([§Slashing evidence](#slashing-evidence)) informs both (a) staker migration decisions and (b), once the extension lands, the cluster operators' blacklist trigger. Filing a stake-slashing transaction via the SSV contract is a complementary punitive action available where the evidence is cryptographically self-contained, but is not the primary deterrent — the primary deterrent is the economic equivalence to going offline plus the eventual `Byzantine ≡ Down` collapse via blacklisting. See [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the evidence-quality discussion and how it interacts with the blacklist's detection latency.

5. **Coordinated EKM across both keypair shares.** The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log keyed on `(slot, layer, side, value_root)`. See [EKM coordination model](#ekm-coordination-model).

6. **Independent V-keypair and IBE-keypair DKGs over the same operator set at the same threshold `qV = qEnc = 2f+1`.** Both DKGs are run once at cluster init with all `n` operators participating; the shared operator set and threshold is what makes Pigeonhole 1's algebra work (`h_σ + h_NR ≤ 2f+1` across the cross-keypair signing log).

### Implications of safety being honest-majority cryptographic, not 100% cryptographic

OBFTR's safety holds against an offline-aggregating byzantine *given the protocol rules are correctly enforced by honest operators*. Enforcement happens at two independent layers per operator — the **protocol layer** (operator software implementing the OBFTR state machine, deciding when to request σ vs NR) and the **EKM** (slashing-protection log that rejects bad signing requests as defense-in-depth). Together they ensure single-σ-V per (slot, layer) per operator and σ-XOR-NR per layer; these rules reduce the safety proof to pigeonhole arguments on the cluster-wide signed-message set.

An operator whose software actively misbehaves (maliciously compromised, or with compounding protocol+EKM bugs that produce byzantine-equivalent signing behavior) consumes f-budget directly. With `f = 1` byz at `n = 4`, one such operator is tolerated; two violate the trust bound and Pigeonhole arguments can fail. Single-layer bugs (protocol-only or EKM-only) do **not** produce byzantine behavior on their own — the other layer catches them. See [EKM coordination model](#ekm-coordination-model) for the full defense-in-depth analysis.

**This is the same trust posture as QBFT.** QBFT's safety also holds under f-byz with honest-majority correct code paths. A bug in `2f+1` honest operators (e.g., the post-consensus signing path signs both candidates from a split decision, or the prepared-certificate verification accepts conflicting commit certificates) would equally violate QBFT's safety guarantees. Neither protocol is "100% cryptographic" against operator-side software bugs; both rely on operator software correctness for honest operators.

Accordingly, "cryptographic safety" in this spec means: against a partially-synchronous byzantine network adversary plus up to `f` byzantine operators, honest operators following the protocol cannot produce two contradictory outputs. It does not mean: safety against arbitrary bugs in honest operator software.

### Implications of validity-divergence not being recovered (assumption 3)

Honest-majority validity-divergence recovers via the standard quorum mechanic, no R-round retry needed: a 3-σ vs 1-NV split reaches σ-quorum at L_0; a 1-σ vs 3-NV split reaches NR-quorum and falls through to L_1 (same as bare [OBFT](OBFT.md)). The residual is the **2-2 boundary** (and validity-divergence with a passive byzantine), which deadlocks — three structural causes: per-operator independent validity verdict, leader's Phase-1 σ_V locked, and cross-phase exclusivity. R-round retry does not help here (verdicts are locked at acceptance, so re-flood across rounds doesn't reconcile the split), and L_C consensus does not help (it coordinates the frontier layer, not the validity verdict).

If assumption 3 is violated beyond an honest majority — a 2-2 boundary split, or validity-divergence with a passive byzantine — OBFTR cannot recover within the slot (honest-majority splits recover, above). There is no fresh-V refetch mechanism. The leader's Phase-1 σ_V is locked; honest who NV cannot switch to σ; cluster deadlocks at L_k or falls through to L_{k+1} (where the same divergence pattern may repeat).

For SSV proposer duty, the host's stabilization workflow (validate parent_root once at acceptance, lock the verdict) is the design's path to satisfying assumption 3. If the host cannot guarantee unanimous validity (e.g., re-orgs are common enough that locking-at-acceptance leads to too many submission rejections), the available structural alternatives are:

- **Use a deterministic / finalized parent.** Validity criterion that doesn't depend on each operator's chain view at evaluation time — e.g., parent must be a finalized block (2 epochs old, all operators agree). Eliminates divergence by construction but loses late-MEV (you can only build on finalized parents).
- **QBFT.** Round-changes through with a new leader fetching at the moved head — covers validity-divergence as a side-effect of round-change recovery. Comes with QBFT's own ~2s round-change latency.

Smaller mitigations (relax cross-phase exclusivity, allow NV → σ flip via aggregator filtering, etc.) all break safety against an offline-aggregating byzantine — they let the byzantine concurrently aggregate σ on V and NR/NV at the same layer to reach two contradictory thresholds.

### Implications of equivocation not being recovered

OBFTR does not provide an in-protocol equivocation recovery mechanism. Outcomes split into three classes:

- **σ-quorum reaches at L_0 naturally** (slot succeeds): honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool to qV.
- **NR-quorum reaches at L_0 → fall-through to L_1** (slot succeeds at L_1 if L_1 honest): all 3 honest land in equivocation-NR (typical when byz delivers V's early enough for gossipsub re-flood to spread conflicts before Phase 2 σ-emit). Round-R force-NR produces qEnc-quorum, decryption unlocks L_1, σ-quorum at L_1 reaches.
- **σ-locked split patterns** (slot misses): honest σ-states split into mixed σ-locked + NR (1-1-1 split, 1-1-NR-C, 1-NR-NR at f=1 n=4 — an honest in `NR` here is either silent-leader-NR or equivocation-NR per the rules in [§Operator commitments](#protocol)). σ-pools split below qV; NR-pool capped by σ-locked operators below qEnc; no fall-through.

A byzantine controlling delivery timing picks the class. Delivering near end-of-Phase-1 (insufficient re-flood time) reliably engineers the σ-locked split slot-miss outcome. Equivocation evidence is slashable in all cases; the rational-byzantine deterrent (assumption 4) is what makes the slot-miss tolerable across many slots.

The rational-byzantine deterrent (assumption 4) is what makes this a tolerable failure mode in expectation: a byzantine that equivocation-griefs in slot N pays for it from slot N+1 onward via the eventual `Byzantine ≡ Down` collapse (manual blacklist by surviving operators; planned protocol extension) plus staker migration collapsing cluster-wide fee accrual; equivocation-evidence bundles additionally enable stake slashing via the SSV contract.

### Implications of the rational-byzantine deterrent (assumption 4)

The deterrent affects *liveness only*, not safety. Pigeonholes 1, 2, 3 hold cryptographically against any byzantine within the f-bound regardless of whether the byzantine is rational — a byzantine willing to absorb full reputation cost (e.g., last-slot-before-exit) cannot violate safety, only grief liveness.

Specifically:

- **Safety unaffected:** No matter how aggressively byzantine operators misbehave (1-1-1 equivocation, fake encrypted-presence, cross-signing), at most one V signature reconstructs cluster-wide per slot. This is a property of the cluster-wide signed-message set under EKM enforcement (assumptions 1, 5).
- **Liveness affected:** A short-horizon byzantine ignoring future-slot consequences may grief more slots than a rational byzantine; each affected slot misses cleanly (no safety violation). The deterrent therefore matters for *expected liveness across many slots*, not for per-slot correctness.

**The deterrent mechanism: SSV's existing offline-operator economics.** Per-validator operator fees on SSV are paid continuously to all cluster operators regardless of per-slot contribution — a byzantine that engineers slot-miss earns the same per-slot fee as an operator who is silently online or completely offline. Operations cost is ~zero (the other operators do the work). Stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters; once enough stakers migrate, the cluster's fee inflow drops to zero across all operators including the byzantine. This is the same mechanism that disciplines a permanently-offline operator. The byzantine's gain per slot is bounded above by what an offline operator would get; their loss (reputation, future cluster invitations, validator migration before the offending slot's fees materialize at scale) is real and persistent across slots.

The protocol surfaces every byzantine fault class as on-wire evidence ([§Slashing evidence](#slashing-evidence)) signed by the offender's own keys, verifiable in isolation by any observer. The evidence informs (a) staker migration decisions and (b) the cluster operators' blacklist trigger (next paragraph). Filing a stake-slashing transaction via the SSV contract is a complementary punitive action available where the evidence is cryptographically self-contained, but it is not the primary deterrent — the primary deterrent is the economic equivalence to going offline plus the eventual `Byzantine ≡ Down` collapse via blacklisting.

**Byzantine ≡ Down in QBFT, significantly worse on latency than Down in OBFT-family — manual blacklist is the equalizer.** QBFT's round-change makes any byzantine deviation functionally indistinguishable from operator silence: round 1 times out, round 2 succeeds with a different leader, byzantine pays the round-1-timeout latency and nothing more. OBFT-family has no round-change escape valve; byzantine grief vectors at L_0 (equivocation σ-locked splits, h_V=1 selective-delivery, fake-encrypted-presence, behavioral σ-refusal) can engineer reliable per-slot slot-miss when the byzantine is L_0 — typically ~25% of slots at f=1 n=4 with uniform leader rotation. The grief above offline behavior is the residual the deterrent must absorb.

The expected operational response is a **manual blacklist**: the surviving `n − f` operators, on observing sufficient evidence of byzantine behavior (whether cryptographically self-contained or behavioral-pattern accumulated across slots), push a config-file update treating the byzantine operator's messages as silent for subsequent slots. The protocol must support this — message-level dropping/discarding by operator identity, plus duty-scheduling that excludes the blacklisted operator's leader rotation — as a planned protocol extension. **The current OBFTR spec does not specify the blacklist mechanism**; once added, the byzantine's residual grief surface above offline behavior is bounded by detection latency + cluster governance reaction time (the same window that disciplines an offline operator who hasn't yet been migrated away from by stakers).

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

**An asymmetry to keep in mind: the deterrent is weakest where per-slot grief is most damaging.** Faults with cryptographically self-contained evidence (equivocation, cross-signing, cross-onion partial-sig equivocation, fake plaintext σ at L_0) are single-observation blacklistable, but these are also the faults the protocol *already* defends against at the slot level (natural σ-recovery on equivocation 2-1 splits; detect-and-reject for fake plaintext σ). Faults with only behavioral evidence (h_V=1 selective-delivery, byz σ-refusal coordinated with mesh-flakiness, validity-divergence with byz passivity) are the *most damaging* per-slot grief vectors — they reliably miss the slot at L_0 when an adversarial byz exercises them — and they're the slowest to blacklist with confidence. The asymmetry follows from the structure of which fault classes leave on-wire cryptographic evidence vs which leave only behavioral patterns; it bounds how aggressively the deterrent (and the eventual blacklist extension) can be relied on per-slot under realistic adversarial conditions.

## Protocol

OBFTR runs in up to **R rounds**. Round 1 is the standard 2-RTT path (matching bare [OBFT](OBFT.md)); rounds 2..R are recovery rounds that fire on round-end timeout when the prior round's reconstruction failed at all layers. Each round has Phase 1 → Phase 2 → Phase 2.5 (L_C signaling) → Phase 3 (reconstruction). Round 1's Phase 1 is a fresh broadcast; subsequent rounds re-flood retained Phase-1 bundles plus per-round L_C claims.

### Phase 1 — Candidate broadcast

Phase 1 in round 1 has K per-layer windows (driven by the asymmetric fetch times): `[T_{K-1}, T_broadcast_max_1]` for the deepest backup, then progressively `[T_{K-2}, T_broadcast_max_1]`, ..., ending at `[T_0, T_broadcast_max_1]` for the primary. Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "OBFTR-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates OBFTR Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, other OBFTR message kinds, other consensus protocols sharing the same identity key). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

In rounds 2..R, fresh Phase-1 broadcasts happen with new leaders for the round's layer slots; gossipsub continues propagating bundles from prior rounds, which can be σ-emitted on in round r if first-observed at or before round r's `T_commit_r`.

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, and check the first-observation timestamp against the current round's `T_commit_r`. Accept the bundle for round `r` if first-observed in `[slot_start, T_commit_r]`; bundles arriving later in round r are not counted toward round-r σ-quorum but may be counted in round r+1 if first-observed at or before `T_commit_{r+1}`. Bundles first-observed past the final round's `T_commit_R` are rejected entirely. Bundles failing cryptographic-auth are silently dropped. (A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.)
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. A `not-valid` verdict turns the operator's commitment into NV (operationally identical to NR — see "Operator commitments — σ, NR, NV" below).

If a leader `L_k` fails to broadcast across all R rounds, that layer is unavailable; the cluster falls through to deeper layers. If all K leaders fail, the slot is missed.

**Bundle propagation.** Honest receivers re-flood every bundle via standard gossipsub on first observation. Across rounds, gossipsub continues propagating in the background — a bundle first-observed by some honest at `T_commit_r + ε` can reach the slow honest by `T_commit_{r+1}` if `Δ_round = T_commit_{r+1} − T_commit_r ≥ ε + 1 BTT`. The leader's σ_L^V partial alone is additionally re-broadcast each round in every operator's `KindCommit_r` witness section (see §Phase 2 / Wire format), providing redundancy against σ_L^V-drop but not against V-drop (V itself is not re-emitted at the application layer).

**Retention bounds.** Retention state for accepted Phase-1 bundles is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **two distinct `(V, σ_{L_k}^V, σ_{L_k}^{op})` tuples** — the first auth-valid bundle observed, plus, if a second auth-valid bundle with a *distinct* `value_root` is later observed, that one (sufficient as leader-equivocation evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Bundles whose cryptographic-auth checks fail are dropped without retention. Retention lifetime: until the operator's local end of round `R`'s Phase 3 (i.e., until reconstruction halts or the slot is declared missed); slot state is then cleared. Memory bound: `O(K · n · R)` bundles per slot in the worst case.

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with honest threshold partials produced in Phase 2 by operators who received the leader's bundle (or recovered it via gossipsub re-flooding across rounds), the cluster reaches `qV` real partials on `V_{L_k}` — closing the byzantine-leader selective-delivery grief under partial synchrony (see "Fault tolerance / Liveness").

**Equivocation handling — detect and slash.** If a participant observes two distinct `σ_V` partials from the same leader at the same slot/layer, that's leader equivocation. The pair of signed bundles is self-contained slashable evidence, gossipped for out-of-band slashing.

Local protocol response, by current state at the round's `T_commit_r`:

- **Retained 0 V**: the operator has no V to σ on at round r → NR (silent-leader rule). May σ in a later round if V arrives before that round's `T_commit`.
- **Retained exactly 1 V** (only one bundle reached this operator before `T_commit_r`): σ on that V if host validates; otherwise NV.
- **Retained ≥ 2 distinct V** (equivocation observed pre-T_commit_r): NR. The operator does not attempt to pick a winner; the slot may still succeed if other honest happened to retain only one V and σ on it (Pigeonhole 2 still ensures at most one V reaches qV cluster-wide).

The leader is required to sign `σ_V` exactly once per slot/layer (refreshes in the host's fetch loop are pre-signing only — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance); any second `σ_V` from the same leader is a protocol violation regardless of intent.

**Equivocation is permitted as a slashable byzantine fault.** OBFTR does not provide an in-protocol equivocation recovery mechanism. Some equivocation patterns naturally reach σ-quorum on one V (e.g., 2-of-3 honest σ-commit on the same V plus the leader's σ_L^V on that V = 3 = qV at f=1 n=4) — the slot succeeds in those cases as a side-effect, not via a specific protocol mechanism. Other patterns (1-1-1 split, asymmetric-retention) do not reach σ-quorum and the slot misses.

**Practically, an adversarial byzantine controls which pattern occurs.** A byzantine that times equivocation deliveries near the end of Phase 1 reliably engineers σ-locked split patterns (1-1-1, etc.) that don't reach qV. **In expectation, byzantine-leader equivocation slot-misses; the rational-byzantine deterrent (assumption 4) is the practical defense, not natural recovery.**

In all cases, the byzantine leader pays the stake-based slashing penalty — equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained on each pair of conflicting bundles.


**Operator commitments — σ, NR, NV (per round).** Each operator commits exactly once per (slot, layer, round) at `T_commit_r`, based on what they observed by then. Three states (no Defer):

- **σ (sign-on-V)**: the operator received the leader's bundle by `T_commit_r`, both protocol-level and application-level checks passed, and the operator did not retain ≥ 2 distinct V's at this layer (no equivocation observed). Materializes as a σ partial in the operator's `KindCommit_r` message at this layer.
- **NR (non-receipt)**: by `T_commit_r`, the operator did not receive an auth-valid Phase-1 bundle for this layer (silent-leader rule), or retained ≥ 2 distinct V's (equivocation rule).
- **NV (non-validity)**: host application returned `not valid` for `V_{L_k}`.

NR and NV are operationally interchangeable on the wire (both emit `σ_i^{IBE}(nr_tag_k)`).

**No cross-round σ-or-NR exclusivity.** Each round's commitments are independent. An operator who NR-emitted at L_0 in round 1 can σ-emit at L_0 in round 2 if the leader's bundle finally propagates by `T_commit_2` and host validates it. Each round has its own EKM signing-event log entry keyed on `(slot, layer, round, side, value_root)`. Cross-phase exclusivity *within a round* is preserved (an operator's Phase-1 σ_L^V in round r prevents NR/NV in the same round at that layer).

**Garbage-encryption deterrence.** A byzantine operator could broadcast a well-formed-but-undecryptable ciphertext at layer `k > 0` to fake encrypted-presence. Post-decryption verification (when prior layers' NR-quorums unlock decryption) surfaces garbage as self-contained slashable evidence (Rule 4); the rational-byzantine deterrent makes such DoS expensive. See "Slashing evidence" for the corresponding case.

### Phase 2 — Onion broadcast `[T_commit_r, T_commit_r + Δ_2]` (per round r)

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

**Phase 2 timing per round.** Each operator emits exactly one `KindCommit_r` message at `T_commit_r`, carrying their per-layer σ partials (for σ-state layers in this round) and NR partials (for NR-state layers in this round). The Phase 2 window `[T_commit_r, T_commit_r + Δ_2]` is sized for that message to propagate to all honest peers before round r's Phase 3.

**Δ_2 sizing per round.** `Δ_2 ≥ 1 BTT` minimum (`KindCommit_r` propagation budget). **Recommended for production: `Δ_2 = 1 BTT`** — tightened from the older `Δ_2 = 2·BTT` recommendation under the unified 1·BTT-per-emission convention (see [BFT-comparison.md §Sizing convention](BFT-comparison.md#sizing-convention)). Mesh-jitter / reflood absorption is structurally provided by **cross-round retention** — bundles or partials missing round r's σ-window remain usable in round r+1 — rather than by a per-round jitter cushion on Δ_2 itself. At Config A (P99=150ms, δ=50ms): `Δ_2 = 200ms`. Concrete tables and downstream timing throughout this document use the recommended sizing.

**Wire format.** Each operator emits exactly one `KindCommit_r` per (slot, operator, round) at `T_commit_r`, carrying:

- The K-layer onion of σ partials (plaintext at L_0, chained-encrypted at deeper layers) for layers where the operator is σ-state in round r.
- NR/NV partials `σ_i^{IBE}(nr_tag_k)` for layers where the operator is NR-state in round r.
- **Leader σ_L^V witness section.** For every Phase-1 bundle the operator has retained at this point (per §Phase 1's retention rules — typically one bundle per layer, two per layer in the equivocation-observed case; bundles accumulate across rounds via gossipsub re-flood), a plaintext copy `(layer k, value_root, σ_{L_k}^V(V_{L_k}))` extracted from the bundle. These are byte-for-byte copies of the leader's partial as `i` observed it — **not** new signings by `i` (no EKM event, no new signing obligation, no new cryptographic primitive). The section provides redundancy against Phase-1 bundle drop at peer receivers: a peer that didn't receive the leader's bundle directly can harvest σ_L^V from `i`'s witness section into the layer-`k` σ-pool (subject to the layer's chained-decryption gate when `k > 0`). Bandwidth is small (per witness ≈ 145 bytes = Layer + Leader + ValueRoot + σ partial + length-prefix overhead; cluster-wide at OBFTR's K=3 n=4 default ≈ 12 witnesses × 145 ≈ 1.74 KB per round; K=4 up-tier ≈ 2.3 KB). The mechanism does **not** address V-drop — σ_L^V verifies against `value_root(V)`, so a receiver lacking V cannot use a witnessed σ_L^V; receivers without V locally still rely on gossipsub Phase-1 re-flood (cross-round propagation) for V itself.

Auth-envelope binding: `(protocol_tag = "OBFTR-v1", message_kind = "commit", cluster_id, slot, round r, operator_id i, onion_payload, nr_partials, sigma_L_witnesses)` signed by `i`'s operator-identity key. Emitted at most once per operator per round.

Each operator includes per layer based on the three-state model from Phase 1:

- **σ-state**: include the (encrypted) σ partial in the onion at this layer.
- **NR-state** (silent-leader, equivocation-observed, or NV): include a partial `σ_i^{IBE}(nr_tag_k)` in the NR-partials section. These IBE partials are the witnesses that unlock the next layer's chained encryption.

**Per-operator commitment exclusivity is per-round, not cross-round.** Within a single round, cross-phase exclusivity holds: an operator who emitted `σ_i^V(V_{L_k})` (Phase-1 leader or Phase-2 onion) in round r cannot also emit NR/NV on `nr_tag_k` in round r. **Across rounds, commitments are independent**: an operator who NR-emitted at L_0 in round 1 may σ-emit at L_0 in round 2 if the leader's bundle finally propagates and host validates it; an operator who σ-emitted at L_0 in round 1 on V can σ-emit at L_0 in round 2 again on the same V (deduplicated per operator). Single-σ-V exclusivity *across rounds* still holds: an operator cannot σ on V in round 1 and σ on V' ≠ V at L_0 in round 2 (the same value-locking rule from Pigeonhole 2).

EKM enforces these exclusivities cryptographically (slashing-protection log keyed on `(slot, layer, round, commitment-side, value_root)`); see "Preconditions on the host application / Slashing-protection scope".

A byzantine operator that publishes both σ and NR on the same `(slot, layer, round)` is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For each layer's leader specifically, "cross-signing" includes any Phase-1 σ + same-round NR pair on the same slot.

### Phase 2.5 — L_C round-coordination signaling `[T_commit_r + Δ_2, T_commit_r + Δ_2 + Δ_2.5]` (per round r)

OBFTR's Phase 2.5 specifies **L_C round-coordination signaling** — the cluster-consensus mechanism for the frontier layer `L_C` and round transitions. Bare [OBFT](OBFT.md) has no Phase 2.5 (single-round, no rounds to coordinate between).

L_C round-coordination is OBFTR's structural addition to bare OBFT's R=1 baseline. It runs in parallel with the latter half of Phase 2 (overlapping window — each operator runs the L_C-consensus logic continuously as they observe peer broadcasts). One new wire message kind is emitted here.

#### L_C consensus — `KindLCClaim`

An operator emits a `KindLCClaim` at end of each round to inform peers of their local view of the cluster's frontier layer `L_C` — the deepest layer the operator has observed advancement past:

```
KindLCClaim {
  protocol_tag = "OBFTR-v1",
  message_kind = "lc-claim",
  cluster_id, slot, round r,
  observed_L_C: int,                         # operator's local L_C, in [0, K-1]
  pool_witness: {                            # evidence supporting the claim
    for each layer j in [0, observed_L_C):
      {kind: "nr-quorum", partials: [σ_a^{IBE}(nr_tag_j) for qEnc operators a]}
  },
  σ^op: operator-identity-key signature over the above
}
```

L_C is the smallest layer index `≥ 0` such that the operator has *not* observed NR-quorum at `L_C` yet (i.e., the cluster hasn't been able to advance past `L_C`). Initially `L_C = 0`; advances each time a layer's NR-quorum reaches.

When `qV` operators agree on `observed_L_C = X` (received cluster-wide via gossipsub), the cluster considers `L_C = X` **promoted**. The next round (round `r+1`) starts with `L_C = X` as its frontier — operators focus their re-flood and Phase-2 emissions on `L_X` and deeper, knowing layers `0..X-1` are dead (cannot reach σ-quorum, by Pigeonhole 1 — see "Fault tolerance / Safety").

Promotion accelerates round transitions: instead of waiting for the round-end timer (`RT`), an operator who observes `qV` `KindLCClaim` messages with the same `observed_L_C` can immediately fire round `r+1`'s start.

**Why promote?** L_C consensus does two things: (a) bandwidth savings — round `r+1` doesn't re-flood retained partials at layers `0..L_C-1` since those are dead; (b) faster round transitions — round `r+1` fires on observed cluster-consensus rather than timer expiry, saving up to one re-flood hop (~D) of latency. It is a coordination primitive — it does not unlock new recovery scope.

**Behavior under sub-partial-synchrony.** If honest operators observe different views (e.g., due to delayed / partial gossipsub delivery) and compute different `observed_L_C` values, no qV agreement reaches and the promotion doesn't fire. Round transitions then **fall back to the round-end timer** (`RT`). This is a liveness-only consideration — promotion is an acceleration mechanism, not a load-bearing recovery primitive — so the timer-based fallback preserves correctness and the round still transitions; only the latency-saving benefit is lost in that round. The next round's re-flood may resync views, restoring promotion in subsequent rounds.

**Narrow benefit window.** L_C consensus's value is concentrated in *mid-band* failure cases: round-failure where honest views align on the failure layer (e.g., aggressive-marginal recovery where all honest agree the round failed at L_0, just with some honest not having V yet). In **healthy cases** the round succeeds in σ-quorum and no transition is needed (no L_C benefit). In **strongly-divergent cases — where rounds are most needed for partition recovery — L_C views diverge** (different honest see different gossipsub state, propagation outliers, mesh visibility variance) and promotion falls back to the timer. So L_C consensus does *not* accelerate the worst-case scenarios where rounds matter most. It's an optimization for the shoulder cases between healthy and worst-case. **The protocol is correct without L_C consensus**; the spec includes it for the bandwidth-saving and round-transition-acceleration benefits in the mid-band. Implementations could omit L_C consensus and fall back to timer-based round transitions uniformly without affecting recovery scope — at the cost of longer round-transition latency in the mid-band cases.

### Phase 3 — Local decryption and reconstruction (per round r, from `T_commit_r + Δ_2 + Δ_2.5`)

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches on some V (output produced), or NR-quorum reaches (advance to next layer), or neither (stay at this layer for next round).

```
L_C = 0                                        # current frontier; advances per loop iteration
sigs[k] for k in [0, K)                        # σ pools, indexed by layer (initially empty)
nrs[k]  for k in [0, K)                        # NR pools, indexed by nr_tag_k (initially empty)

for k in [0, K):
    # Aggregate σ pool at layer k from all rounds' broadcasts so far.
    sigs[k] = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
            ∪ {σ_{L_k}^V(V_{L_k}) from peer KindCommit_r witness sections at layer k (any round so far), if valid}
            ∪ {σ_j^V(V) from received layer-k onion contents on any value V}
              (decrypted via accumulated NR-decryption keys at layers 0..k-1, if k > 0)
            # deduplicated per operator: leader's Phase-1 σ, witness-section
            # copies of σ_L^V (which collapse to identical bytes across peer
            # KindCommit_r messages from any round), and onion σ from the same
            # operator all collapse to one partial.
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
        # NR-quorum did not reach at L_k. Stay at L_C = k for next round.
        break    # exit the layer-walk; round r ends here

if L_C == K and no σ-quorum reached:
    # Walked all layers; no output. Round r ends.
    pass

# End of round r's reconstruction.
# If output produced, halt.
# If round r < R and no output: round r+1 fires (re-flood + late commit + retry).
# If round r == R and no output: slot misses.
```

**Round `r` boundary** is at `T_commit_r + Δ_2 + Δ_2.5 + ε_3`. By this time, the operator must have received all Phase-2 onions and NR partials they intend to count for round `r`; if reconstruction succeeded, output `(V, S)`; otherwise round `r+1` starts (or, if `r = R`, the slot misses for that operator). The deadline rule (caveat 3) bounds the gap between phases against propagation P99/P999 and clock skew. Late `KindCommit_r` arrivals can be incorporated by re-running the per-round reconstruction walk before round `r+1` starts (Pigeonhole semantics still hold; safe).

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at that layer: no σ partial, no NR partial. Standard threshold cryptography — only signed messages count. Re-flooding across rounds maximizes the chance that all honest broadcasts eventually reach all honest receivers within the partial-synchrony envelope.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within the slot's relay-submission deadline (see "Fault tolerance / Liveness"). If more than `f` operators are offline or byzantine combined, neither σ nor NR quorums reach their thresholds and the slot is missed. Within the f-bound, OBFTR recovers from network partitions (with bounded re-flood delay) given enough rounds within the slot's total budget. View-divergence cases — equivocation and host-validity divergence — are explicitly out of recovery scope (some splits succeed naturally as a side-effect of leader-σ-V counting; rest slot-miss). See [Assumptions and implications](#assumptions-and-implications).

### Round structure

OBFTR runs up to **R rounds** with timeout **RT** per round. Round `r ∈ {1, ..., R}` proceeds as follows:

1. **Round start**: at time `T_round_r_start`, the round begins. Round 1 starts at `slot_start + T_pre` (after host-application pre-fetch); round `r > 1` starts at `T_round_{r-1}_end` (round-end timer expiry) OR upon observing cluster-promoted L_C consensus (`KindLCClaim`-quorum) — whichever happens first.
2. **Phase 1 (round 1 only)**: K leaders broadcast their Phase-1 bundles per their per-layer fetch windows (`[T_{K-1}, T_broadcast_max_1]`, ..., `[T_0, T_broadcast_max_1]`). Round `r > 1` skips fresh Phase 1 — operators re-flood retained Phase-1 bundles instead.
3. **Phase 2** `[T_commit_r, T_commit_r + Δ_2]`: each operator emits a single `KindCommit_r` message at `T_commit_r` carrying their per-layer σ partials (for σ-state layers in this round) and NR partials (for NR-state layers in this round). Operators commit per round based on what they observed by `T_commit_r`; cross-round commitments are independent (an operator who NR-emitted at L_0 in round r-1 may σ-emit at L_0 in round r if V finally propagated). See "Phase 1 / Operator commitments".
4. **Phase 2.5** `[T_commit_r + Δ_2, T_commit_r + Δ_2 + Δ_2.5]`: operators emit `KindLCClaim` reporting their local `L_C` view at end of round `r`. The qV-quorum on a single `observed_L_C` value, if reached, accelerates round-`r+1` start.
5. **Phase 3** (from `T_commit_r + Δ_2 + Δ_2.5`): each operator runs the K-layer reconstruction walk. If σ-quorum reaches on some V at any layer, output the V; halt. If NR-quorum reached up to some layer `L_C < K`, advance L_C and continue the walk. If neither σ-quorum nor NR-quorum advance unlock at any layer, end of round `r` (round `r+1` starts, or slot misses if `r = R`).

**Round transitions**:
- If round `r < R` ends with no output, round `r+1` fires (with new leaders for round-r+1's layers).
- If round `r == R` ends with no output, slot misses.

**Round timing**: `RT = (T_commit_1 − BFT_start) + Δ_2 + Δ_2.5 + ε_3` for round 1 (Phase 1 occupies `[BFT_start, T_commit_1]`; Phase 2 → 2.5 → 3 follow); `RT = Δ_reflood + Δ_2 + Δ_2.5 + ε_3` for rounds 2..R, where `Δ_reflood ≈ 1 BTT` is the re-flood window. The slot's total budget is `R · RT` (approximately; round 1 is longer due to fresh Phase 1).

## Preconditions on the host application

OBFTR is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at the moment the operator considers committing σ to it. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be signed (the σ_V locks it). See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` before committing σ. A `not-valid` verdict turns the operator's commitment into NV (see "Phase 1 / Operator commitments — σ, NR, NV").

The protocol-level checks (cryptographic auth, envelope re-derivation, per-round timing cutoff `T_commit_r`) are protocol-internal and do not depend on the application.

For SSV's Ethereum proposer duty, application-level checks include slot match, proposer index match, fork/domain match, parent root match against the operator's view of the head, relay metadata validity, doppelganger / slashing-protection checks, and block encoding well-formedness. See the Application section for details. The protocol body does not enumerate or interpret these — they're host concerns.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot (potentially up to K, one per layer they validated). Constraints per (slot, layer):

- **Each layer's leader signs σ_V exactly once per (slot, layer)** — on the final V they commit to, after any pre-signing refreshes during the host's fetch loop. Refreshes update V via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this single-σ-V-per-(slot, layer) constraint cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational workflow.
- **Each operator commits σ-or-NR exclusively per (slot, layer, round).** Within a single round r, cross-phase exclusivity holds: an operator who included σ on any V at layer `k` in round r (Phase-1 leader's σ_V or Phase-2 σ-emit) may not subsequently broadcast NR/NV on `nr_tag_k` in round r, and may not σ on a different V' at the same layer in round r. Across rounds, commitments are independent: an operator may NR in round r and σ on V in round r+1 (if V finally propagated). Single-σ-V exclusivity *across rounds* still holds: cannot σ on V in round 1 and σ on V' ≠ V in round 2. EKM enforces these via slashing-protection log keyed on `(slot, layer, round, side, value_root)`.
- Every operator signs each layer's V_{L_k} they consider valid (host returns `valid`) in round r's `KindCommit_r`, provided they retained exactly one V at that layer (no equivocation observed pre-T_commit_r). Operators with no V retained or ≥ 2 V's retained NR.

EKM/slashing-protection must permit the operator's per-(layer, round) Phase-2 σ signings plus the leader's Phase-1 σ_V (exactly one per slot/layer the operator is leading), without flagging duplicates — the cluster's safety property collapses these to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating points are **candidate signing** (Phase-1 leader and Phase-2 σ-emit alike) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`).

**Cross-round σ partial dedup.** When an operator σ-emits in round 1 and the slot rolls to round 2, the operator's σ partial is re-flooded but not re-signed — the same partial is reused. Phase 3's reconstruction walk deduplicates per-operator: `σ_i^V(V_{L_k})` from any round counts as `1` partial in the σ-pool, regardless of how many rounds the partial appears in.

### EKM coordination model

The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log. Production EKMs (Web3Signer, Dirk, etc.) typically expose per-key signing checks; OBFTR requires a coordination layer above them.

**Log schema.** One row per signing event: `(slot, layer, side, value_root)` where `side ∈ {"σ", "NR"}`; `value_root` is set on σ-side entries, null on NR-side.

**Per-request checks.**

- **Sign σ on V at (slot, layer)** (V-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "σ", value_root(V))`. The single-σ-V rule means a second signing attempt with `V' ≠ V` at the same `(slot, layer)` is rejected even though the side matches — the existing row already contains `value_root(V)`.
- **Sign NR on `nr_tag_k` at (slot, layer)** (IBE-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "NR", null)`.

The same operator's σ partial is re-emittable across rounds without a new signing event — the log row already exists, the cached partial is re-broadcast. This satisfies cross-round exclusivity by construction.

**Implementation paths.** (a) A custom EKM that maintains the unified log natively across both keypair shares. (b) A coordinator component in the SSV operator process that wraps two standard EKMs (one per keypair share) and enforces the cross-keypair rules in the operator's slashing-protection database — a single transactional log, two keypair-specific signing-request handlers reading and writing it. The coordinator is novel work relative to standard per-key slashing-protection deployments (e.g., Web3Signer style): it requires (i) the unified log to be **atomic across both shares' signing operations** (sign-and-log must be all-or-nothing; if V-share is in remote signer and IBE-share is local, this is a two-phase-commit-flavored problem), (ii) the cached σ partial to be **persistent across operator restarts** (cross-round re-emission depends on the cache surviving), (iii) **deterministic re-signing as a fallback** if the cache is lost (BLS partial sigs are deterministic; the EKM must allow re-signing if the log row matches the same `(slot, layer, side, value_root)` rather than rejecting as duplicate). None of these are insurmountable, but the coordinator is OBFTR-specific engineering work — not a drop-in over an existing per-key slashing-protection database. Path (b) is the path SSV will most likely take.

**EKM correctness is defense-in-depth for safety, not the sole enforcement.** OBFTR's safety (Pigeonholes 1 and 2) holds when honest operators correctly implement the protocol rules — single-σ-V per (slot, layer) per operator, σ-XOR-NR per layer. The **protocol layer** (operator software implementing the OBFTR state machine) is the primary enforcement point: it determines when σ vs NR is requested from the EKM in the first place. The **EKM** is a catch-net: it rejects signing requests that violate the slashing-protection invariants, providing defense-in-depth even if the protocol layer is buggy.

For a single honest operator to produce safety-violating behavior (e.g., σ on V and σ on V' at the same `(slot, layer)`), **both layers must be buggy in compounding ways**: the protocol layer must request the second σ (violation of σ-eligibility logic) AND the EKM must fail to reject it (violation of slashing-protection lookup, atomicity, or persistence). A single-layer bug typically does not break safety:

- Protocol-layer bug only: the EKM rejects the bad request; no double-sign emitted on the wire.
- EKM-layer bug only: the protocol layer doesn't ask for double-signing, so the EKM bug is never exercised.

Cluster-wide safety violation (Pigeonhole 2 producing two qV-quorums on different V's) requires aggregating these single-operator violations to reach `2 · qV = 4f+2` partials across two V's. At `f = 1, n = 4`, one byzantine operator contributes ≤ 1 partial per V (≤ 2 total); three correct honest contribute exactly 3 partials total (single-σ-V each); sum 5 < 6 = 2 · qV. The minimum safety-violating configuration is therefore **one byzantine operator plus one honest operator with compounding protocol+EKM bugs** — together producing the missing partial. This is two misbehaving operators total, exceeding the `f = 1` trust budget. Single-layer bugs alone are tolerated; safety requires both layers to be correct on at least `n − f = 3` operators.

**Trust posture is the same as QBFT.** Both protocols rely on honest-majority correct implementation of the protocol logic *plus* correct slashing-protection — neither is "100% cryptographic" against operator-side software bugs (see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic)). The difference is in the slashing-protection layer's maturity: QBFT's per-key slashing-protection (Web3Signer, EIP-3076 interchange format) has decade-of-production hardening; the OBFTR coordinator is novel, so reaching comparable defense-in-depth robustness requires deliberate engineering investment in (a) test coverage on atomicity and crash-recovery paths, (b) fault-injection testing of the operator-restart scenario, (c) optionally operational margin via larger `n` (e.g., `n ≥ 5` keeps `f = 1` while expanding the bug-budget headroom). The investment is in raising the bar on the catch-net, not in patching a single point of safety failure.

**Summary of EKM failure modes.** A **maliciously compromised** EKM (signs requests outside protocol rules, or generates signatures the protocol layer didn't request) is byzantine-equivalent and directly consumes f-budget. A **passively buggy** EKM (fails to reject bad requests but doesn't generate signatures on its own) requires the protocol layer to also have a compounding bug for safety-violating behavior to actually occur — see the defense-in-depth analysis above. In both cases, the cluster's overall trust posture follows the standard "honest-majority cryptographic" framing — see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic).

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model. Operational rules that handle specific faults (cross-round retry, equivocation detect-and-slash, cross-signing detection, head-change refresh) are described inline in the relevant Phase sections; the analyses of *what those rules buy* and *under what conditions* live here.

### Trust model

- **Byzantine bound `f`** with cluster size `n = 3f+1` (the BFT-tight setting; see [§Assumed / Standard BFT trust bound at the tight setting](#assumed)): up to `f` operators may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, etc.). Exactly `2f+1` honest. Running example: `n = 4, f = 1` (3 honest guaranteed).
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `P99` (propagation P99/P999) and clock skew `δ`. Two distinct per-round cutoffs operationalize this: `T_broadcast_max_r = T_commit_r − 1 BTT` (leader broadcast deadline) and `T_accept_max_r = T_commit_r + Δ_2 − 1 BTT = T_commit_r` (per-round receiver acceptance horizon at tightened `Δ_2 = 1 BTT`). Round `r` boundary is at `T_commit_r + Δ_2 + Δ_2.5 + ε_3`; the slot's hard wall is the relay-submission deadline `T_relay_cutoff − T_submit`. Safety holds against arbitrary network adversaries; only liveness depends on synchrony.

  **OBFTR's R-round absorption window** = `T_accept_max_R − T_broadcast_max_1`:
  - Per-round absorption (single round, ignoring cross-round retention): `Δ_2 + 1 BTT`. Concrete: ~`2 BTT` ≈ 400ms at Config A with tightened `Δ_2 = 1 BTT`. Cross-round retention extends this to R·BTT cumulative.
  - Cross-round retention extends absorption by `(R − 1) · (Δ_2 + Δ_2.5 + ε_3 + Δ_reflood)` (each additional round adds its own per-round-window worth of inter-round time before its acceptance horizon fires).
  - At R=2 Config A with recommended Δ_2: total absorption ≈ `5 BTT + ε_3` ≈ 1050ms — roughly 2.5× a single round's window. (Decomposes as `Δ_2 + 1·BTT = 2·BTT` per-round plus one inter-round transition `Δ_2 + Δ_2.5 + ε_3 + Δ_reflood = 3·BTT + ε_3` ≈ 650ms.)

  Per-round Δ_2 is sized to `1 BTT` (KindCommit propagation budget); the multi-round absorption advantage of OBFTR vs single-round OBFT comes from per-round retry with new leaders, not from within-round Δ_2 widening. Each round commits at `T_commit_r` based on observed bundles; late bundles from round r naturally propagate via gossipsub before round r+1's `T_commit_{r+1}`.

  The "absorption window" framing is more precise than the simpler "R · P99" intuition: each round contributes both its own `Δ_2 + 1 BTT` of within-round absorption AND the inter-round time before the next round's acceptance horizon. R-round retry's recovery scope grows correspondingly faster than `R × P99`.

### Safety (cryptographic, honest-majority)

**Claim:** at most one full `V` signature is ever produced per OBFTR instance per slot — across any layer, on any value, across any combination of σ sources, across any round 1..R — cluster-wide, against an offline-aggregating byzantine within the f-bound, regardless of which honest aggregation rules are followed. (See [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic) for what this scope does and doesn't cover.)

The proof rests on three pigeonhole arguments — Pigeonholes 1 and 2 (single-layer) plus Pigeonhole 3 (chained encryption at `K > 2`). All three are properties of cluster-wide signed messages, enforced cryptographically by [EKM coordination model](#ekm-coordination-model) rules at the operator level — no honest-aggregation rule required.

The pool definitions used in the arguments below:

- **σ-pool on V at L_k**: `{σ_i^V(V) partials at L_k}`, deduplicated per operator. Includes the leader's Phase-1 σ_V when valid. Once a partial is emitted, it stays on the wire — no "revocation" semantics.
- **NR-pool at L_k**: `{σ_i^{IBE}(nr_tag_k) partials}`, deduplicated per operator.

**Pigeonhole 1 — σ-vs-NR at the same layer.** σ-quorum on `V` (any V) at `L_k` and NR-quorum on `nr_tag_k` cannot both reach.

- σ-quorum on V: `h_σ + byz_σ ≥ qV = 2f+1` (where h_σ counts honest with σ partials on V at L_k from any phase / round, deduplicated per operator).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Cross-phase + cross-round exclusivity (per "Slashing-protection scope"): `h_σ + h_NR ≤ n − f = 2f+1` (equality at `n = 3f+1`). Each honest commits σ-or-NR per layer at most once, EKM-enforced.
- **Leader-counting.** If the layer's leader is honest, their Phase-1 σ_V partial counts toward `h_σ` for the V they signed; cross-phase + cross-round exclusivity then forbids them from emitting NR/NV on `nr_tag_k`. If the leader is byzantine and equivocates, each per-V partial they publish counts toward `byz_σ_V` for that V (capped at 1 per byz per V by deduplication).
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

OBFTR's liveness is **partial-synchrony-conditional within the slot's relay-submission deadline** — the protocol's total slot budget. The R-round structure absorbs network-induced failures (re-flood completing across rounds). View-divergence (equivocation, validity-divergence) is NOT recovered by the protocol; some splits succeed naturally as a side-effect of leader-σ-V counting toward whichever V reaches qV first, others slot-miss. Equivocation cases are slashable (leader pays the stake-based penalty); validity-divergence is not attributable.

If propagation between honest operators stays bounded by `R · P99`, the protocol terminates with a V signature on some layer (the first layer where σ-quorum reaches under cluster-wide V receipt, or the deepest layer reachable via NR fall-through with a valid backup leader). If propagation exceeds `R · P99`, or more than `f` operators are byzantine/offline, the slot is missed. **Safety holds in either case.**

**Best case (round 1 healthy at L_0)**: all honest receive V_{L_0} within `1 BTT`; all 3 honest σ-emit + leader's Phase-1 σ = 4 ≥ qV. Slot succeeds in round 1 — Phase 1 + Phase 2 + Phase 2.5 budget ≈ 5 BTT ≈ 1000ms at Config A.

**Aggressive-marginal recovery (round 2 covers 2-of-3-honest missing in round 1)**: 1 honest received V before round-1's `T_commit_1`; 2 honest didn't. The 2 missing honest NR at round 1 (silent-leader rule). Round 1 ends with σ-pool = 1 + leader = 2 < qV, NR-pool = 2 < qEnc → no quorum reaches; round 2 fires. By round-2's `T_commit_2`, gossipsub has continued propagating the bundle and the 2 missing honest now have V. They σ-emit at L_0 in round 2 (per-round commitments are independent — round-1 NR doesn't lock them out of round-2 σ). σ-pool at round 2 = 3 + leader = 4 ≥ qV. Slot succeeds in ~3 RTTs. **OBFTR recovers what bare [OBFT](OBFT.md) (single-round) misses.**

**Equivocation handling (no protocol-level recovery; outcome depends on byzantine timing)**: byzantine `L_0` equivocates, broadcasting distinct V's selectively. Case analysis at `f = 1, n = 4` (3 honest A, B, C; byz D = leader):

Recovered via σ-quorum at L_0 (when honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool):

- **D delivers V to {A, B}, V' to {C}.** σ-pool on V = A + B + D's σ_L^V(V) = 3 = qV. σ-quorum reaches in round 1. Slot outputs V.
- **D delivers V to {A}, V' to {B, C}.** σ-pool on V' = B + C + D's σ_L^V(V') = 3 = qV. Reaches in round 1. Slot outputs V'.

Recovered via NR-quorum at L_0 → fall-through to L_1 (when all honest land in equivocation-NR; round-R force-NR produces NR-quorum):

- **All-equivocation-NR outcome (byz delivers V's early enough that re-flood spreads conflicts before Phase 2 σ-emit).** Each honest retains ≥ 2 distinct V's by Phase 2 emit time → all 3 in equivocation-NR. σ-pools at L_0 ≤ byz partials per V < qV. NR-pool: 3 honest NR + 0 byz (σ-locked from Phase 1) = 3 = qEnc → NR-quorum reaches → fall-through to L_1; if L_1 honest, slot succeeds at L_1. Asymmetric-retention patterns under 3+-V flood typically land here when byz delivery timing is "too early" for grief purposes.

Not recovered — slot misses at L_0 with no fall-through (σ-locked split patterns; slashable):

- **D delivers V to {A}, V' to {B}, both to C (or neither).** A σ-locked on V; B σ-locked on V'; C in equivocation-NR (received both) or no-V-yet (no V received). σ-pool on V = A + leader = 2 < qV. σ-pool on V' = B + leader = 2 < qV. C force-NR at round R; NR-pool = 1 < qEnc → no fall-through. Slot misses.
- **D delivers distinct V_1 to A, V_2 to B, V_3 to C (1-1-1 split).** Each honest σ-commits on their V_i on receipt before observing equivocation. All 3 honest σ-locked on different V's. σ-pool on each V_i = 1 honest + leader's σ_L^V = 2 < qV. NR-pool = 0 (all σ-locked, cross-phase exclusivity). Slot misses.
- **1-NR-NR (asymmetric retention under 3+-V flood, walkthrough).** D floods {V_1, V_2, V_3} with adversarially-ordered gossipsub delivery: A first-observes V_1 alone (before any other) → σ-locks on V_1; B first-observes {V_1, V_2} via gossipsub-ordered delivery → retains both → equivocation-NR; C first-observes {V_2, V_3} → retains both → equivocation-NR. D signs σ_L^V on V_1 only (locked at Phase 1 to a single V per single-σ-V exclusivity). End of round R Phase 2: σ-pool on V_1 = A + D's σ_L_0^V(V_1) = 2 < qV; B and C are equivocation-NR; NR-pool = 2 honest + 0 byz (σ-locked) = 2 < qEnc → no quorum on either side; no fall-through. Slot misses. The retention bound (2 distinct V's per `(slot, layer, leader_id)` per operator) means B and C have non-overlapping retention sets — even if they tried to coordinate on a "winner V," there isn't a common winner. This is the canonical asymmetric-retention shape an adversarial byz produces with selective ordering, and shows why the protocol cannot pick a winner cluster-wide from per-operator local retention state.

**Byzantine timing controls which class fires — and an *adversarial* byzantine reliably picks the slot-miss class.** Delivering V's early in Phase 1 leaves time for gossipsub re-flood to spread conflicts → all-honest-NR outcome → slot succeeds at L_1. Delivering near end-of-Phase-1 leaves insufficient re-flood time → σ-locked split outcome → slot misses at L_0. **The "naturally reaches qV" framing is misleading without this caveat**: it does not happen by random chance. Under non-adversarial byz behavior the natural-recovery cases fire occasionally (whenever byz's delivery timing happens to spread conflicts in time); under adversarial byz, byz controls the timing and picks the σ-locked split outcome on demand. **In expectation against an adversarial byz primary, all of these patterns slot-miss reliably.** The rational-byzantine deterrent (assumption 4) is what makes this tolerable across many slots — but the *evidence quality* for these patterns is the *behavioral* class (not the cryptographically-self-contained class), so single-observation slashing is not credible (see [§Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4)). Practical effect: byz can grief many slots before the pattern accumulates enough confidence for honest operators to act. (R-rounds do not help — these patterns are R-invariant.)

In all cases, Pigeonhole 2 ensures at most one V can reach qV cluster-wide regardless of how honest σ-emissions split — there is no two-output safety violation.

**Sub-partial-synchrony (real propagation > absorption window)**: if propagation between leader broadcast and any honest receiver's first-observation exceeds the cluster's R-round absorption window (per-round acceptance horizon + cross-round retention through round R — see [§Trust model](#trust-model) for the algebra; concrete value at Config A R=2 is ~`5 BTT + ε_3` ≈ 1050ms with recommended Δ_2 per round), late honest don't σ-emit by round R and slot misses. **No safety violation.** R is a tunable knob; larger R extends tolerance at the cost of more pessimistic timing.

**Multi-failure fall-through (K ≥ 3)**: with `K > 2` layers, the cluster can fall through past multiple silent or non-validating layers **within a single round** — Phase 3's reconstruction walk processes layers sequentially using local decryption (no per-layer RTT). E.g., at K=4 with L_0, L_1, L_2 all silent: NR-quorum reaches at L_0 in Phase 2 (silent-leader rule fires for all 3 honest cluster-wide); same at L_1 and L_2; σ-quorum at L_3 if L_3 leader honest. All happens in round 1's Phase 2 + Phase 3 windows. Recovery scope at K layers is achieved without round-change overhead — a structural difference from QBFT (which round-changes once per failed leader).

**Adversarial scheduling within the absorption window**: the network adversary delays each message by ≤ absorption window within the synchrony bound. The adversary's leverage scales with how many operators they can keep without V received through R rounds.

- *Safety unaffected.* Pigeonholes 1, 2, 3 are properties of the cluster-wide signed-message set, not of arrival times. The adversary can delay messages but cannot forge signatures or violate EKM rules. At most one V signature reconstructs cluster-wide regardless of timing.
- *Liveness — adversary delays V to ≤ 1 honest.* The other 2 honest σ-emit; σ-pool = 2 + leader = 3 = qV. **Quorum reaches in round 1 without the delayed operator.** At f=1, n=4 the adversary's leverage against ≤ 1 honest is wasted.
- *Liveness — adversary delays V to 2 honest.* 1 honest σ-emits in round 1; 2 NR (silent-leader rule). σ-pool = 1 + leader = 2 < qV. NR-pool = 2 < qEnc. No quorum reaches in round 1. Round 2 fires with re-flood from gossipsub peers (not just the leader). To keep V from those 2 honest in round 2, the adversary must delay messages from many sources — a stronger adversary than "delay any one message". If round-2 re-flood succeeds → all 3 honest σ-emit + leader = 4 ≥ qV; σ-quorum reaches in round 2 (~1.5s at Config A; per-round commitments are independent — round-1 NR doesn't lock the 2 missing honest out of round-2 σ). If adversary persists through round R → σ-pool stays ≤ 2 each round, NR-pool stays = 2 each round; neither quorum reaches; slot misses cleanly. (No safety violation — Pigeonholes hold.)
- *Liveness — adversary delays V to all 3 honest.* All 3 NR in round 1 (silent-leader rule). NR-pool = 3 = qEnc → NR-quorum reaches at L_0 in round 1 → Phase 3 walks to L_1 and tries σ-quorum on V_{L_1}. **If L_1 honest and L_1's broadcast reached cluster-wide** (e.g., the adversary only blocked L_0's V), σ-quorum at L_1 reaches in round 1's Phase 3; ✓ in ~1000ms. If L_1 also unreachable, no σ-quorum at L_1 in round 1 → round 2 fires; if round-2 re-flood delivers V_{L_0} to all 3, σ-quorum at L_0 reaches in round 2.

The R · P99 budget is the cumulative delivery-delay tolerance against any single operator. Within budget, the cluster either σ-recovers at L_0 or NR-falls-through to L_1 (recovers if L_1 honest). Outside budget, the slot misses cleanly.

**OBFTR recovers strictly more than bare [OBFT](OBFT.md) (single-round)** for partition cases (R-round retry with re-flood; per-round commitments independent so a round-r-NR honest can σ-emit in round r+1). It does **not match QBFT's full recovery scope** — view-divergence cases (host-validity divergence and equivocation patterns that don't naturally reach qV) are out of OBFTR's recovery scope. Equivocation is slashable (leader pays stake-based penalty); validity-divergence is not attributable but is bounded by host-side stabilization (assumption 3).

### Liveness comparison: OBFTR vs QBFT

The table below puts OBFTR and QBFT side-by-side per scenario at the standard SSV proposer-duty configuration (n=4, f=1, ~4s relay cutoff). All counts at uniform "1 BTT per emission" sizing — see [docs/BFT-comparison.md / Sizing convention](BFT-comparison.md#sizing-convention). For OBFTR, round timing assumes Configuration A (P99=150ms uniform, R=2, K=2 — see [Timing budget](#timing-budget--concrete-configurations)). For QBFT-SSV, RT = 2s = 10 BTT per round-change is SSV's current production tuning.

| Scenario | OBFTR outcome | QBFT-SSV outcome |
|---|---|---|
| Healthy (all honest receive V_{L_0}) | Round 1: σ-quorum reaches within Phase 1 + Phase 2 + Phase 2.5 budget (~600ms = 3 BTT). ✓ at L_0. | Round 1: PROPOSE→PREPARE→COMMIT + post-consensus (4 emissions × 1 BTT). ~0.8s (4 BTT). ✓ |
| Byzantine leader silent | Round 1: 0 σ-emits → all honest NR (silent-leader rule) → NR-quorum at L_0 reaches in Phase 2 → automatic fall-through to L_1 in same round's Phase 3 walk; σ-quorum at L_1 if leader honest. ✓ in ~1200ms. | Round 1: no PROPOSE arrives; round timeout (~2s). Round 2: new leader proposes; succeeds in ~1.6s. ✓ in ~3.6s. |
| Aggressive marginal (>1 of 3 honest miss V at round-1 cutoff) | Round 1: σ-pool = 1 + leader = 2 < qV; the 2 missing honest NR (silent-leader rule) (peer σ-emit observed). Round 2: re-flood delivers V; σ-quorum reaches. ✓ in ~2.4s. | Round 1: PREPARE-pool short; timeout (~2s). Round 2: new leader re-fetches + proposes; succeeds in ~1.6s. ✓ in ~3.6s. |
| Byzantine leader equivocates, 2-1 split where one V already has 2 honest + leader-σ_L^V | Round 1: σ-pool on the 2-honest-V = 2 + leader = 3 = qV; reaches naturally. ✓ in ~1200ms. | Round 1: PREPARE-pool split across V's; no quorum; timeout. Round 2: new leader proposes; succeeds. ✓ in ~3.6s. |
| Byzantine leader equivocates, σ-locked split pattern (1-1-1 split, 1-1-NR-C, 1-NR-NR; byz delivers near end-of-Phase-1) | σ-pools split below qV; NR-pool below qEnc (σ-locked operators can't NR). **✗ slot misses at L_0;** no fall-through. Equivocation slashable; rational-byzantine deterrent kicks byz out for future slots (see [Assumptions](#assumed)). | Round 1: PREPARE split; no quorum; timeout. Round 2: new leader proposes a fresh V (not one of the equivocated V_i); honest converge; succeeds. ✓ in ~3.6s. **QBFT recovers what OBFTR doesn't** via "new leader proposes fresh V" mode. |
| Byzantine leader equivocates, all-honest-NR outcome (byz delivers V's early; re-flood spreads conflicts before σ-emit) | All 3 honest in equivocation-NR. Round R force-NR → NR-quorum at L_0 → fall-through to L_1; if L_1 honest, ✓ at L_1 in ~RT × R. Equivocation slashable. | Same recovery as σ-locked split row: round-2 new leader proposes fresh V. ✓ in ~3.6s. |
| Multi-failure fall-through (multiple silent leaders) | At K=3 with L_0, L_1 silent: NR-quorum at L_0 and L_1 reaches in Phase 2; Phase 3's walk decrypts down to L_2; σ-quorum at L_2 if honest. **All in round 1's existing windows** (sequential local decryption, no per-layer RTT). ✓ in ~1200ms. | Round 1: leader 1 silent → timeout (~2s). Round 2: leader 2 silent → timeout (~2s). Round 3: leader 3 takes over; succeeds. ✓ in ~5.6s — past 4s relay cutoff. ✗ for proposer duty. **OBFTR's K-layer parallel fall-through beats QBFT's serial round-change for this case**, but only up to OBFTR's pre-fetched K depth. |
| Host-validity divergence (head-change mid-slot, strict host) | Honest-majority recovers (3-σ → L_0; 1-σ-3NV → L_1 via NR fall-through); the 2-2 boundary and validity-divergence-with-passive-byz cases miss (assumption 3 — the host stabilizes the verdict at Phase-1 acceptance to make divergence rare). | Round 1: validators with stale head don't PREPARE on the proposed V; PREPARE quorum may not reach; timeout. Round 2: new leader fetches at moved head; if quorum agrees, succeeds. ✓ in ~3.6s. **QBFT additionally recovers the boundary cases OBFTR misses** via fresh-V refetch on round-change. |
| Adversarial scheduling — adversary delays V to 2 honest persistently | Round 1: σ-pool = 1 + leader = 2 < qV; NR-pool = 2 < qEnc; no quorum. Round 2: re-flood from gossipsub (multiple sources). If delivery succeeds → all 3 honest σ-emit + leader = 4 = qV at L_0; ✓ in ~2.4s. If delay persists through R rounds → σ-pool ≤ 2, NR-pool = 2 each round; neither quorum reaches; slot misses cleanly. ✗ Safety holds. | Round 1: PREPARE delayed → timeout (~2s). Round 2: new leader; if adversary delays again → round 2 timeout (~2s). 4s consumed → relay cutoff missed. ✗ Within 4s, QBFT tolerates ≤ 1 round of adversarial delay. |
| Adversarial scheduling — adversary delays V to all 3 honest persistently | Round 1: all 3 honest NR (silent-leader rule) → NR-pool = 3 = qEnc → NR-quorum at L_0 → Phase 3 walks to L_1; if L_1 honest and reachable, σ-quorum at L_1 reaches in round 1 (~1200ms). ✓ at L_1. If L_1 also delayed, round 2 may σ-recover at L_0 if V arrives. | Round 1: timeout (no PREPARE quorum). Round 2: new leader; if adversary delays → round 2 timeout. ✗ |
| Sustained partition (real propagation > round budget) | OBFTR R · P99 budget exceeded; force-NR may not reach NR-quorum if too many honest are partitioned out; slot misses. ✗ Safety holds. | QBFT round-budget × RT exceeded; slot misses. ✗ Safety holds. |
| > f operators offline/byzantine | Standard 3f+1 violation; slot misses. ✗ Safety holds. | Same. ✗ Safety holds. |

**Summary of recovery-scope differences:**

- **OBFTR-strict-superset cases** (OBFTR recovers in fewer RTTs or more cleanly): healthy, byzantine-leader-silent (in-round NR fall-through vs. round-change), aggressive marginal, all-honest-NR equivocation (NR-fall-through to L_1 in round R when byz delivers V's early enough that re-flood spreads conflicts before σ-emit), multi-leader-failure (K-layer parallel fall-through within one round vs. K-1 serial round-changes), most adversarial-scheduling patterns within R · P99. These are OBFTR's wins because (a) NR fall-through is in-protocol within a single round, no round-change needed; (b) per-round overhead (~250ms at Config A) is much smaller than QBFT's RT (~2s).
- **QBFT-strict-superset cases**: 1-1-1 equivocation, host-validity divergence. QBFT's "round-change with new leader proposing fresh V" handles these structurally; OBFTR relies on assumption 3 (validity stabilization) and assumption 4 (rational-byzantine deterrent) respectively.
- **Both fail equivalently**: sustained partition beyond budget, > f byzantine.

The choice between OBFTR and QBFT for SSV proposer duty depends on observed re-org rate, the cluster's tolerance for the 1-1-1 equivocation case (handled by rational-byzantine deterrent in OBFTR, recovered in QBFT), and the relative weight of common-case latency vs. worst-case-coverage. Detailed cost-side trade-offs (latency, bandwidth, cryptographic primitive maturity) are in [Appendix A.3](#a3--comparison-with-qbft).

### Equivocation handling

See "Phase 1 / Equivocation handling — detect and slash" for the operational rule. Summary: when honest detects equivocation (two distinct σ_V partials from the same leader on different value_roots), they:

1. Stay σ-committed if already σ-emitted at this layer (cross-phase exclusivity binds them).
2. If σ-eligible but not yet σ-emitted, transition to equivocation-NR. The σ-emit precondition fails.
3. If already in no-V-yet, transition to equivocation-NR upon retaining ≥ 2 distinct V's. Recovery via re-flood-delivers-V is foreclosed.
4. All equivocation-NR operators force-NR at round R per the final-round rule. The protocol does not pick a winner cluster-wide.
5. Gossip the equivocation evidence (the pair of equivocating Phase-1 bundles) for out-of-band slashing.

The leader is required to sign `σ_V` *exactly once per (slot, layer)*; refreshes in the host's fetch loop are pre-signing only (see "Application: SSV Ethereum proposer duty / Head-change handling") and don't surface multiple `σ_V` partials on the wire. Any second `σ_V` from the same leader is a protocol violation.

OBFTR does not provide in-protocol equivocation recovery. Some equivocation patterns naturally reach qV on a single V (when honest happen to split such that 2-of-3 σ-emit on the same V; leader's σ_L^V on that V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. See "Liveness / Equivocation handling" for the full case analysis. Equivocation is treated as a slashable byzantine fault (Phase-1 bundles signed by leader's key are self-contained slashing evidence — see "Slashing evidence"); the rational-byzantine deterrent (assumption 4) is what makes the slot-miss tolerable across many slots.

**Cross-onion (operator-side) equivocation — counting and suppression.** If an operator `i` is observed with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different V, the dual partials are slashable evidence on the same logic as leader equivocation. For aggregation: by single-σ-V exclusivity (EKM-enforced — see "Slashing-protection scope"), an honest operator only ever emits σ on one V per layer, so any dual-V σ partials from the same operator are byzantine. The byzantine f-bound on per-V σ-pool contributions therefore holds without explicit suppression: byz contributes ≤ 1 partial per V regardless. Honest receivers MAY additionally elect to fully suppress `i`'s partials upon observing the equivocation evidence — this is not load-bearing for safety.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_k` is a slashable cross-signer. The pair is detected uniformly across phases and rounds:

- **σ from Phase 1 + NR/NV from Phase 2** — applies to a layer's leader specifically: a `σ_{L_k}^V(V_{L_k})` from the Phase-1 bundle paired with an `σ_{L_k}^{IBE}(nr_tag_k)` from the same operator at the same slot.
- **σ from Phase 2 onion + NR/NV from Phase 2** (any round) — any operator who included σ in their onion *and* broadcast a no-σ attestation, in any round.

Detection is straightforward — the dual partials are public. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it cryptographically — the EKM-enforced cross-phase exclusivity is what gives Pigeonhole 1 its load-bearing constraint, and a byzantine cross-signer simply consumes their f-bound contribution to one of σ-pool or NR-pool, not both effectively). The detection is purely for **attribution** and out-of-band punishment.

### Slashing evidence

Five rules surface byzantine fault evidence for *attribution and punishment*. Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to inform the surviving operators (and stakers) that a byzantine fault has occurred so the cluster can act on it — see [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the offline-equivalence model. The protocol-functional message flow (Phase-1 bundles, `KindCommit` partials) already carries the underlying signed messages on the wire; **honest operators MUST log observed evidence** per the rules below for later out-of-band aggregation. Log format and retention are implementation-defined; the manual-blacklist mechanism (planned protocol extension) is the canonical consumer. The surviving operators verify aggregated logs and decide whether to (a) blacklist the byzantine via cluster-wide config update (restoring `Byzantine ≡ Down`; planned protocol extension), (b) file a stake-slashing transaction via the SSV contract where the evidence is cryptographically self-contained, and/or (c) propagate the signal to stakers and to future cluster formation.

- **Self-contradiction (σ + NR/NV at the same layer).** If operator `i` emitted `σ_i^V(V)` at some layer AND `σ_i^{IBE}(nr_tag_k)` at the same layer, the dual partials are slashable evidence (cross-signing).
- **Leader equivocation.** Two distinct `σ_V` partials from the same leader at the same (slot, layer) are a self-contained slashable fault proof. Any observable double-signing is protocol-violating regardless of the leader's stated intent.
- **Cross-onion partial-sig equivocation.** Operator `i` emitting `σ_i^V(V)` and `σ_i^V(V')` for different `V` at the same layer is detectable from the partial sigs alone — single-σ-V exclusivity is EKM-enforced, so any dual-V observation is a slashable byzantine fault.
- **Fake encrypted-presence (post-decryption garbage at k > 0).** Operator `i` broadcasting an auth-signed `KindCommit` with an encrypted partial at layer `k > 0` that, after NR-quorum unlocks decryption, decrypts to garbage (not a valid σ partial on any leader-known V at L_k, or fails to decrypt entirely) is a slashable byzantine fault. The auth envelope binds `i` to the encrypted payload at signing time; post-decryption verification surfaces the garbage. Detection is **delayed and conditional on NR-quorum reaching at all prior layers** (so the chained encryption can be unlocked); when the slot misses cleanly without any NR-quorum reaching (e.g., σ-locked split at L_0, or NR-pool short of qEnc cluster-wide), the chained encryption stays sealed and the evidence is not surface-able through this rule. **Honest detection of fake encrypted-presence is therefore conditional on the slot progressing far enough for the relevant layer's encryption to unlock.** This is a real deterrent-strength reduction for adversarial byzantine that engineers slot-miss precisely to seal evidence; mitigated only by Rule 5 (when applicable at L_0) or by post-hoc decryption coordination outside the protocol's current scope.
- **Fake plaintext σ at L_0.** Operator `i` broadcasting an auth-signed `KindCommit` with a plaintext σ partial at L_0 that does not verify against any retained leader-broadcast V_{L_0} (where the receiver has retained at least one such V) is a slashable byzantine fault. The auth envelope binds `i` to the partial; partial-vs-V verification is a deterministic local check by any receiver with retained V. Detection is immediate (no decryption-unlocking dependency, unlike Rule 4) — the receiver can attribute the fault as soon as it observes both `i`'s auth-signed `KindCommit` and any leader-broadcast V_{L_0}. The fake partial does not contribute to σ-quorum (it doesn't verify against any retained V); it's purely a slashable accountability artifact.

Each piece of evidence is verifiable in isolation (signed by the offending operator's own keys) — any observer with the published partials and the (eventually) decrypted onion contents can independently confirm the byzantine action. **Acting on the evidence (slashing transaction, cluster removal) is a human-coordinated process**, not an automated protocol step; honest operators judge whether the evidence is compelling and decide whether to act.

**Evidence quality and surface-ability vary by rule:**

| Rule | Detection timing | Surface-ability | False-positive risk |
|---|---|---|---|
| 1. Self-contradiction (σ + NR/NV) | Immediate (dual partials on the wire) | Always — public partials | Very low |
| 2. Leader equivocation | Immediate (two σ_V from same leader) | Always — public bundles | Very low |
| 3. Cross-onion partial-sig equivocation | Immediate (two σ partials on different V) | Always — public partials | Very low |
| 4. Fake encrypted-presence (k > 0) | Delayed (post-decryption) | **Best-effort, conditional on slot progressing past prior layers' NR-quorum** — sealed if slot misses early. R-rounds do not help: if no NR-quorum reaches at any prior layer across all R rounds, the seal applies. | Very low when surfaced |
| 5. Fake plaintext σ at L_0 | Immediate (partial vs retained V check) | Conditional on receiver retaining (or auth-only-retaining) V — covered by Phase-1 bundle re-flood under partial synchrony | Very low |

**Rule 4 has a structural detection limit.** The chained-encryption construction makes Rule 4 evidence cryptographically inaccessible when prior layers' NR-quorum doesn't reach across all R rounds: decryption requires `qEnc` partials on `nr_tag_{0..k-1}`, those partials don't exist if honest σ'd at those layers, and honest cannot preemptively sign IBE partials on layers they intend to σ-side-commit to (that violates cross-phase exclusivity = cross-signing). This is a **fundamental limit of the IBE primitive as used here**, not a fixable spec gap.

**The seal applies in BOTH slot-success and slot-miss outcomes**, not just slot-misses:

- **Slot succeeds at L_0** (σ-quorum reaches at L_0): per Pigeonhole 1, NR-quorum at L_0 does NOT reach (σ and NR mutually exclusive at the same layer). Hence the chained encryption at L_1, L_2, ... stays sealed, and any fake encrypted-presence at deeper layers in this slot is invisible. **This is the common case for healthy slots** — a byzantine that fakes encrypted-presence at L_2 in every slot pays no per-slot cost on healthy slots where the cluster succeeds at L_0. The fake-presence is essentially "rehearsing the attack with no consequences" until a slot-miss-at-L_0 path happens to unlock the relevant encryption.
- **Slot misses at L_0** (no quorum reaches across all R rounds): σ-locked split equivocation, h_V=1 selective-delivery, validity-divergence deadlock — each leaves the chained encryption sealed. Compounded with byz fake-presence at deeper layers, byz gets two grief actions per detection.

Phase 2a/2b mitigates both cases by widening the set of paths that reach a quorum somewhere (so more layers' encryption unlocks more often), but does not fully close it — slots that miss for non-byz reasons (host-validity divergence, sustained partition) still leave Rule 4 evidence sealed.

**Practical implication for deployments.** Rule 4 functions as a *probabilistic* deterrent rather than an unconditional one: a byzantine that fakes encrypted-presence at L_k>0 expects detection only with the probability that NR-quorum reaches at all prior layers in subsequent slots where the deterrent's coordination process can still act. Deployments relying on assumption 4 (rational-byzantine deterrent) for L_k>0 fake-presence should weight the deterrent's effective strength accordingly — Rule 4 is *best-effort, not guaranteed surface-able*. Rule 5 (Fake plaintext σ at L_0) does not have this limitation since L_0's σ is plaintext.

The five classes are all *cryptographically self-contained* (high-confidence, low false-positive risk against honest operators) once surfaced. The asymmetry above — Rule 4's slot-progress-conditional surface-ability — is a real limitation that adversarial byzantine can exploit by engineering slot-miss precisely to seal Rule-4 evidence. Behavioral-pattern grief (selective-delivery, σ-refusal coordinated with honest flakiness) leaves no on-wire cryptographic evidence at all and is correspondingly harder for humans to act on with confidence — see [Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4).

### Failure modes

The slot misses (no V signature is produced) under any of the following. The cases split into two classes by relationship to OBFTR's operating assumptions:

- **Class A — assumption violations** (the listed condition violates one of OBFTR's assumptions; the protocol does not promise liveness when an assumption is violated). These are out-of-scope for OBFTR's recovery guarantees by construction.
- **Class B — permitted byzantine grief within the f-bound** (occurs *under* valid assumptions; one byzantine operator within the f-byzantine bound deliberately misbehaves to cause slot-miss). These are *permitted because they are eventually bounded* — every Class B grief leaves evidence on the wire (cryptographically self-contained for some classes, behavioral-pattern for others), and the rational-byzantine deterrent (assumption 4) bounds the byzantine's grief across slots via the eventual `Byzantine ≡ Down` collapse (manual blacklist by the surviving `n − f` operators; planned protocol extension) plus staker migration that collapses cluster-wide fee accrual. The boundedness is what makes Class B "permitted" rather than "fatal" — an attacker that griefs reliably ends up in the same fee position as if they had gone permanently offline (and worse, for cryptographically-self-contained faults: stake-slashable via the SSV contract). **However, the time-to-bound varies** — see [Evidence quality](#implications-of-the-rational-byzantine-deterrent-assumption-4) for which classes have cryptographic vs behavioral-pattern blacklist triggers.

The slot misses under any of:

- **[Class A]** **Sustained partition (real propagation > absorption window)** — violates assumption 2 (partial synchrony). Re-flood doesn't complete within the cluster's R-round absorption window (per-round acceptance + cross-round retention through round R; see [§Trust model](#trust-model) for the algebra). Honest who didn't receive V by any round's `T_commit_r` NR each round (silent-leader rule). If NR-pool stays below qEnc each round (e.g., partition splits the cluster such that some honest receive V and σ while others don't, leaving NR-pool short of qEnc and σ-pool short of qV), slot misses cleanly. **No safety violation.** The R parameter is tunable; larger R extends propagation tolerance at the cost of slot-budget consumption.
- **[Class A]** **More than `f` faults** — violates assumption 1 (BFT trust bound). More than `f` operators offline/byzantine combined. Standard `3f+1` trust bound. Slot misses regardless of round structure.
- **[Class A]** **Assumption 3 violated (host validity diverges mid-slot)** — violates assumption 3 (host validity unanimous at decision time). See [Implications of validity-divergence not being recovered](#implications-of-validity-divergence-not-being-recovered-assumption-3). The host's stabilization workflow (validate-once-and-lock at Phase-1 acceptance) is the design's path to keeping this from occurring; if it does occur beyond an honest majority (the 2-2 boundary, or validity-divergence with a passive byzantine), the slot misses cleanly (honest-majority splits recover). Not slashable (re-orgs are real-world events, not protocol violations); rational-byzantine deterrent does not apply.
- **[Class B — cryptographically slashable]** **Equivocation σ-locked split patterns** (1-1-1 split, 1-1-NR-C, 1-NR-NR at f=1 n=4 — patterns where some honest σ-commit on different V's before observing equivocation, leaving neither σ-quorum nor NR-quorum reachable at L_0 and no fall-through) — occurs under valid assumptions; one byzantine within f-bound equivocates to engineer the split. See [Implications of equivocation not being recovered](#implications-of-equivocation-not-being-recovered). **Equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained cryptographically slashable**; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots. The all-equivocation-NR case (e.g., asymmetric-retention patterns where every honest retains ≥ 2 V's by σ-emit time) actually recovers via L_1 fall-through and is not a failure mode at f=1 n=4 with L_1 honest.
- **[Mostly Class A]** **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates in non-recoverable patterns at every layer). At K ≥ f+1, byzantines alone (within f-bound) cannot cause this — pigeonhole guarantees ≥ 1 honest leader. So this typically requires either >f faults (Class A: violates assumption 1) or coincident non-byzantine independent failures (relay timeouts, host issues, etc. — also Class A in the sense that the cluster's operational assumptions about host availability are violated). Rare in practice. The K parameter controls the fall-through depth.
- **[Class A]** **Late deepest-layer leader broadcast at K=2.** A backup leader L_{K-1} (= L_1 at K=2) whose Phase-1 bundle arrives at no honest by any round's `T_commit_r` — e.g., the leader's fetch loop overruns substantially — is treated as silent at every round. NR-quorum reaches at L_1 in each round, but there is no L_2 to fall through to at K=2. **Slot misses**. At K ≥ 3 this falls through harmlessly to L_2.

  **Why Class A?** None of the six explicit assumptions is violated: BFT trust bound holds, partial synchrony bounds *gossipsub* propagation but not the leader's *application-internal fetch latency*, validity is orthogonal, EKM and DKGs are orthogonal. The failure relies on an *implicit* operational assumption — that honest leaders broadcast in time for at least one round's `T_commit_r`. A deployment that selects `K ≥ f+2` lifts this by guaranteeing ≥ 2 honest leaders so a single late-broadcasting leader doesn't foreclose the slot.

  **Mitigation paths:**
  - **Use K ≥ f+2** (a deployment choice — see §Setting). Adds at least 2 honest leaders so a single late-broadcasting honest leader doesn't foreclose the slot — fall-through to a deeper honest leader recovers. At f=1 n=4 this means K = 3 (~1KB extra per onion vs K=2; within practical bandwidth). At f=2 n=7, K = 4. **No protocol change**, just deployment configuration; this is the cleanest fix that actually recovers the slot. Deployments that select `K = f+1` accept exposure to this Class A failure mode as part of the deployment trade-off.
  - **Host-side hard deadline** (defense-in-depth; minor host-side discipline, no protocol change). The leader's fetch loop should abort and broadcast no bundle if it hasn't terminated by `T_broadcast_max_1 = T_commit_1 − 1 BTT`. Converts "late broadcast NR-locks the layer" into "silent leader" (handled cleanly via NR-quorum → fall-through). At K = f+1 this *cleans up the spec-tension* but doesn't recover the slot (silent + L_0-fail still falls through to non-existent layer). At K ≥ f+2 it complements the K choice by ensuring late broadcasts always fall into the silent-leader path rather than the NR-lock pathology.
- **[Class A]** **Validity-divergence deadlock at the 2-2 boundary (network-induced; no byzantine action required in the cleanest case; also validity-divergence with a passive byzantine).** Honest-majority splits recover (3-σ → L_0; 1-σ-3NV → L_1); the 2-2 split is the residual. OBFTR's R-round structure does not address it: verdicts are locked at acceptance per the host stabilization workflow, so re-flood across rounds doesn't reconcile divergence. The deadlock has three structural causes: per-operator independent validity verdict, leader's σ_V locked in Phase 1, and cross-phase exclusivity per operator.

  A beacon-chain re-org landing inside the gossipsub-acceptance window for the Phase-1 bundle can split honest operators between σ-eligibility (parent_root matched their pre-reorg head snapshot) and NV (parent_root didn't match their post-reorg head). The host's per-operator validate-once-and-lock workflow (§Head-change handling) preserves each operator's verdict but cannot reconcile cluster-wide divergence. At the boundary split (e.g., 2-σ vs 2-NV at f=1 n=4 with all 4 honest, where leader's Phase-1 σ_V counts as σ-side via cross-phase exclusivity): σ-pool = 2 < qV = 3; NR-pool = 2 < qEnc = 3. **Same algebraic deadlock as the byzantine selective-delivery case below**, but **no byzantine action required and no slashable evidence** in the cleanest form — re-orgs are real-world events, not protocol violations. The rational-byzantine deterrent does not apply (nobody to attribute fault to). Probability scales with the re-org rate × acceptance-window width / slot length; the host's stabilization workflow narrows the window (typical P99 ≈ 100–500ms vs slot length 12s) but doesn't eliminate it.

  **The deadlock zone widens when a byzantine within f-bound exercises f-budget passively** (silent or σ-on-V — neither is independently slashable; passive silence is the natural f-budget consumption pattern for an absent operator). At f=1 n=4 with leader honest, additional deadlock configurations include:

  - "1 non-leader σ + 1 non-leader NV + byz silent" (σ-pool = leader + 1 = 2 < qV; NR-pool = 1 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz silent" (σ-pool = leader = 1 < qV; NR-pool = 2 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz σ-on-V" (σ-pool = leader + byz = 2 < qV; NR-pool = 2 < qEnc).
  - Symmetric configs when leader's host-verdict is itself NV (Phase-1 σ_V locks leader σ-side regardless of leader's host's later opinion).

  The all-honest example captures the cleanest case; in real deployments with byz exercising f-budget, the practical slot-miss rate from validity-divergence is meaningfully higher than the all-honest example alone suggests. **The expected rate scales with `re-org rate × byz-passivity-rate × R-round-absorption-window-width`** — i.e., a deployment with re-orgs in 1% of slots, a byzantine adopting passive grief in some fraction of slots, and an R-round absorption window proportional to slot length compounds these probabilities into validity-divergence slot-misses. The R-round absorption window's contribution is non-trivial: at OBFTR(R=2) Config A recommended Δ_2, the validity-locking window is ~1050ms vs slot length 12000ms, contributing ~9% × `re-org rate × byz-passivity` to slot-miss rate. The host's stabilization workflow narrows the divergence window but does not eliminate it; byzantine passive f-budget consumption (silence or σ-on-V — neither cryptographically slashable individually) is essentially "free" within the f-bound, so byz can reliably contribute the passivity factor whenever exercising the deterrent's weak-attribution corner is favorable. See [Implications of validity-divergence not being recovered (assumption 3)](#implications-of-validity-divergence-not-being-recovered-assumption-3) for the full discussion.
- **[Class B — gossipsub-pattern slashable, weaker accountability]** **Byzantine selective Phase-1 delivery (h_V = 1 deadlock).** *A deliberate-byzantine grief vector — does not arise under all-honest behavior with no implementation bugs and normal network conditions.* (A structurally similar `h_V = 1` deadlock can arise under *sustained network partition* — `real propagation > R · P99` — where one honest is on the leader's side of the partition for the entire R-round window while two others are partitioned away; that case is captured by the "Sustained partition" failure mode above. The byzantine version is the worry because byz can *engineer* it deliberately rather than wait for unlucky network conditions.)

  Two variants, with different statuses after Defer removal:

  **Variant A — withhold-then-fake-σ (closed as a side-effect of Defer removal).** Earlier OBFT-family designs included a Defer state with a "no-V fallback" rule: receivers without V deferred their NR-decision based on observed peer σ-claims. That rule was the structural enabler of an attack where a byzantine L_0 withheld Phase-1 from *all* honest, emitted an auth-signed fake σ-claim in Phase 2, then selectively delivered Phase-1 late to one honest — engineering a deadlock via Defer→force-NR pacing. The current OBFTR removes Defer **primarily for spec/wire/EKM simplification** (3-state vs 4-state commitment lattice, single `KindCommit_r` emission per round vs multi-emission per round, no auth-only-retention pre-state, no transitional EKM events — same motivation as in [bare OBFT](OBFT.md#where-this-came-from)); closure of this attack is a structural side-effect of that removal, not its motivating reason. With Defer gone, receivers without V at `T_commit_r` immediately NR per the silent-leader rule each round, so a byzantine that withholds Phase-1 produces NR-pool = 3 = qEnc → NR-quorum at L_0 → clean fall-through to L_1 in round 1.

  **Variant B — selective Phase-1 delivery (still open).** A byzantine that broadcasts Phase-1 to exactly one honest (rather than withholding) σ-locks via their own Phase-1 σ_V, and the receiving honest σ-locks too. Cluster pools then sit at σ-pool = 2 (recipient + byz σ_V) and NR-pool = 2 (the two no-V honest; byz and recipient σ-locked, can't NR). Neither reaches qV/qEnc=3; the slot misses. **This is an algebraic limit at f=1, n=4** for the h_V = 1 case (h_V = number of honest operators with V at round-R cutoff): σ-quorum needs h_V ≥ 2 (since byz's σ_V contributes 1, total = h_V + 1); NR-quorum (with byz σ-locked) needs h_V = 0 (since 3 − h_V honest NR). The intermediate h_V = 1 fails both. Generalizes at higher f: the deadlock zone is `0 < h_V < 2f`.

  **R-invariant.** Rounds 2..R don't help — byz can re-target the selective delivery in any round (or keep the delivery in round 1 and the same h_V = 1 algebra holds in every subsequent round). Increasing R gives byzantine *more* timing flexibility without strengthening cluster recovery. **This is the key point about OBFTR's R-round structure: rounds defend against partition cases (re-flood completing across rounds) but provide no defense against Variant B.** Structural fixes are needed (Phase 2a/2b).

  **Why no in-protocol fix.** Raising the no-V-fallback threshold doesn't help: at f=1, no fixed threshold distinguishes byz selective-delivery from genuine partition (information-theoretically indistinguishable to receivers without V). f+1 leaves Variant B open; f+2 breaks aggressive-marginal recovery (1 honest with V emits 1 σ-claim, below threshold → NR-locked, can't σ when V arrives via re-flood). Closing Variant B in-protocol requires deferring σ-commitment past Phase-1 observation — a structural change to commitment timing, not a tunable threshold.

  **Current OBFTR does not defend against Variant B.** It is in the same class as 1-1-1 equivocation: byz can grief reliably, slot misses, no on-wire evidence beyond byz's selective-delivery pattern (which is hard to distinguish from network failures). The rational-byzantine deterrent (assumption 4) is the practical defense across many slots — repeated grief surfaces as a delivery-pattern signature at the gossipsub / observability layer that the surviving operators can use to trigger a manual blacklist (planned protocol extension), and stakers can use to migrate validators away.

  **Composability with sealed-evidence patterns.** When this attack succeeds and the slot misses cleanly at L_0 (no NR-quorum reaches at L_0 in any round), the chained encryption at L_1, L_2, ... stays sealed. Any byzantine fake-encrypted-presence at deeper layers (Rule 4 evidence) is *not surface-able* in this slot — see [§Slashing evidence](#slashing-evidence) "Evidence quality and surface-ability" table. So a byzantine that combines selective-delivery at L_0 with fake-presence at deeper layers gets two grief actions for the price of one detection: the L_0 grief succeeds (slot misses), and the L_k>0 fake-presence is sealed (no Rule 4 detection). This composition further weakens the deterrent's effective coverage in the worst-case attack chains — and **R-rounds do not help here either**, since the seal applies if no NR-quorum reaches across all R rounds at L_0.
- **[Class B — gossipsub-pattern slashable, weaker accountability]** **Transient mesh-flakiness (honest mesh-bounded observation), optionally amplified by a byzantine σ-refusal.** Per-round commitment in current OBFTR happens at `T_commit_r` based on what the operator observed by then via their own gossipsub mesh. An honest operator with poor mesh visibility (few peers, high mesh-hop latency, transient connectivity glitch, EL-CL desync, etc.) can fail to receive a leader's bundle by `T_commit_r` even when peers received it. They commit NR per the silent-leader rule. If σ-pool then falls short — a second mesh-flaky honest operator, or a byzantine within f-bound refusing to σ-emit — the round deadlocks: at f=1 n=4, leader honest, A poorly-meshed, B also poorly-meshed (or a byz refuses) → σ-pool = leader + 1 honest = 2 < qV; NR-pool < qEnc. **Deadlock at this round.** Pure honest flakiness is not slashable (no byz to attribute); only the byz-coordinated variant is, which is why it sits in Class B.

  At OBFTR(R≥2), round r+1 gives A's mesh a fresh chance to receive the bundle: per-round commitments are independent (no cross-round σ-or-NR exclusivity), so A can σ-emit in round r+1 if the leader's bundle finally propagates. This is OBFTR's structural fix vs single-round OBFT for this failure class. Round-2 σ-quorum reaches if A's mesh recovers within `Δ_round`.

  **Mitigations beyond R-round retry:**
  - **Mesh diversity at deployment level** — ensure each operator has diverse gossipsub peer connections to reduce the probability of a single operator hitting propagation outliers.
  - **Larger Δ_2** — extends the propagation budget per round; absorbs mesh-hop latency variance up to the budget.

  At OBFTR(R≥2), per-round retries reduce the deadlock rate; mesh diversity is still good defensive engineering.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

OBFTR uses **`K-1` IBE tags per slot** (the K-1 NR tags; the deepest layer has no NR tag). Each tag is a standard threshold-IBE tag using the cluster's IBE keypair. The chained encryption at each layer-transition is implemented as a single IBE ciphertext under `nr_tag_k`, nested across layers. At K=2 the chain has 1 level; at K=3, 2 levels; etc.

The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init. Long-lived, no per-slot rotation. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

**Chained encryption cost.** At layer K-1 (deepest), each σ partial is wrapped in `K-1` levels of IBE encryption. Per-onion size grows as `O(K)` ciphertext bytes (`K-1` levels × ~32-64 bytes ciphertext per level). At K=2 the chain has 1 level; at K=4 it has 3 levels. Concrete sizes: ~1 KB per onion at K=2, ~3 KB at K=4. Within practical SSV bandwidth budgets.

## Properties summary

| Property | OBFTR |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1` + EKM-enforced per-operator commitments, holds against offline-aggregating byzantine within the f-bound. Honest-majority cryptographic, not 100% cryptographic — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). Same trust posture as QBFT. |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition (assumption 3) |
| Termination (output guaranteed) | Conditional. **One-liner: consensus expected to complete by `slot_start + 2.40s` at Config A R=2 (BTT = 200ms, K = 3, tightened Δ_2 = 1 BTT per round), with submission slack to `slot_start + 4.00s` for relay submit; under conditions: (a) ≤ f operators byzantine/offline, (b) real propagation between leader broadcast and any honest first-observation ≤ R-round absorption window `~5 BTT + ε_3` ≈ 1050ms (cumulative across rounds via cross-round retention; see §Trust model), (c) host validity unanimous at decision time (assumption 3), (d) `K ≥ 3` (late-leader resilience).** Configurable R lets operators tune termination guarantee per duty's deadline budget. |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates form self-contained slashable evidence |
| Byzantine-leader-grief resistance | **Partial under non-adversarial byzantine; substantially weaker against adversarial byz that deliberately engineers grief patterns.** Closed under partial synchrony for incidental selective-delivery / late-delivery via leader-σ-V-in-Phase-1 + gossipsub re-flooding + R-round retry. Recovery via "natural" σ-quorum patterns (leader's σ_L^V completes a 2-of-3-honest pool) only fires when byz isn't actively timing deliveries. **Adversarial byzantine reliably engineers slot-miss when L_0**: σ-locked split equivocation (1-1-1, 1-1-NR-C, etc.) and h_V=1 selective-Phase-1-delivery deadlocks. At f=1 n=4 with uniform leader rotation, an adversarial byz primary can deterministically grief ~25% of slots (whenever they're L_0). **R-rounds do not help with these patterns** — they're R-invariant (more rounds give byz more timing flexibility without strengthening cluster recovery; see §Failure modes / h_V = 1). The rational-byzantine deterrent (assumption 4) is the only protocol-level defense, and it works *across slots in expectation*, not per-slot. |
| Mesh-flakiness tolerance (honest operator with poor gossipsub mesh visibility) | **Partial (R-round-bounded).** A mesh-flaky honest operator who fails to observe peer σ-emits within a round's NR-decision window NR-emits per the silent-leader rule, becoming a byzantine-equivalent f-budget consumer *for that round* — within a single round this can deadlock (σ-pool short: a second mesh-flaky operator, or a byzantine within f refusing to σ-emit). **R-rounds recover it**: per-round commitments are independent (no cross-round σ-or-NR exclusivity), so a round-`r`-NR honest σ-emits in round `r+1` once its mesh delivers V; cross-round retention is the structural mesh-jitter / reflood absorber (the tightened `Δ_2 = 1 BTT` covers P99 propagation only). The residual is sustained mesh-flakiness exceeding the R-round absorption window, which misses cleanly. QBFT's round-reset recovers the same class per-round; 2abOBFT recovers it in a single round via the `KindNoValue` no-lock. |
| Validity-divergence under strict host | **Partial** — honest-majority recovers (3-σ → L_0; 1-σ-3NV → L_1); the 2-2 boundary and validity-divergence-with-passive-byz cases miss (assumption 3 — the host stabilizes the verdict at Phase-1 acceptance to make divergence rare). See [Assumptions](#implications-of-validity-divergence-not-being-recovered-assumption-3). |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (K-layer fall-through, K configurable) |
| Round-change recovery | Yes — R rounds with re-flood + per-round independent commitments. ~Δ_round per round (~250ms typical at low P99), vs QBFT's ~2s round-change. |
| Recovery scope vs QBFT | Strictly more than bare [OBFT](OBFT.md) (single-round) for partition recovery (R-round retry with re-flood; per-round commitments independent so a round-r-NR honest can σ-emit in round r+1 if V arrives). View-divergence (validity-divergence and equivocation patterns that don't naturally reach qV) is out of scope — handled by assumptions 3 (host validity stabilization) and 4 (rational-byzantine deterrent), see [Assumptions](#assumptions-and-implications). Strictly better latency profile per recovery round (~10× faster). |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block, a typical OBFTR configuration is **`K = 3, R = 2`** — `K = 3` at f=1 sits one layer above the BFT-liveness minimum (`K ≥ f+1 = 2`), with R=2 contributing one round of cross-round retention. **OBFTR's K-default diverges from bare OBFT and 2abOBFT** (which default to `K = f+1 = 2` at n=4 — see [OBFT.md §Application](OBFT.md#application-ssv-ethereum-proposer-duty)): OBFTR's R-round retry substitutes for one layer of K-fall-through (a round-1-NR honest can σ-emit at round 2 — per-round commitments are independent), so OBFTR's effective fall-through depth at K=3 R=2 is comparable to bare OBFT's K=2 plus one round of cross-round retention. The `K = f+2 = 3` choice at OBFTR is structural, not a different recommendation philosophy. One recovery round absorbs partition cases where ≥1 honest first-observes V_{L_0} between round 1's `T_commit_1` and round 2's `T_commit_2`, within the 4s relay cutoff. `K = 4 (= n)` is also viable for additional fall-through depth at slightly higher onion bandwidth. For like-for-like family comparisons in this doc set, K=4 is used across protocols (see Appendix A and [BFT-comparison.md](BFT-comparison.md)). `K = 2` (BFT-liveness minimum at f=1) is also supported — the smallest envelope, with the most reliance on R-round retry for fall-through; it exposes the Class A late-deepest-layer-broadcast failure mode (see §Failure modes) since L_2 doesn't exist to fall through to, even with R-round retention. The K choice is per-cluster and deployment-dependent (see [§Setting](#setting) for the full K-bounds discussion).

Out-of-scope cases (host-validity divergence, 1-1-1 equivocation splits) are addressed by [Assumptions and implications](#assumptions-and-implications) — host stabilizes the validity verdict at Phase-1 acceptance; equivocation 1-1-1 falls back on the rational-byzantine deterrent.

| OBFTR concept | SSV mapping |
|---|---|
| `n` participants | 4 |
| `f` byzantine bound | 1 |
| `K` layers | 3 (recommended; `= f+2`) or 4 (`= n`, max fall-through depth) |
| `R` rounds | 1 (no recovery) or 2 (one recovery round) |
| `RT` round timeout | depends on D (propagation budget) — see [Timing budget — concrete configurations](#timing-budget--concrete-configurations) |
| Slot | Ethereum slot for which the cluster is proposer |
| V-signing keypair | the validator's split BLS key (already exists in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| `L_0` (primary leader) | designated MEV proposer for the slot |
| `V_{L_0}` | MEV-optimized block fetched late from the relay |
| `L_1, ..., L_{K-1}` (backup leaders) | separately designated operators, distinct from `L_0` and from each other |
| `V_{L_k}` for k ≥ 1 | safe early-fetched blocks from vanilla beacon-node payloads, refreshed on head changes (per the leader's pre-signing fetch loop) |
| `T_commit_1` | round 1 commit / view-fix deadline — anchor: `slot_start + 1.5s` across all configurations below |
| `T_broadcast_max_1` | round 1 leader broadcast deadline — `T_commit_1 − 1 BTT` (at tightened per-emission sizing); per-layer fetch windows are `[T_k_1, T_broadcast_max_1]` with `T_{K-1} < ... < T_0 ≤ T_broadcast_max_1`. Bundles broadcast at this deadline propagate over `1 BTT` to all-honest first-observation by `T_commit_1`; jitter slack lives in cross-round retention (round 2 absorbs round 1's tail). |
| `T_commit_r` | round `r` receiver acceptance cutoff — bundles first-observed past `T_commit_r` at any honest receiver are not counted by that receiver toward σ-quorum at round `r`, but may still be picked up in round `r+1` if they propagate by `T_commit_{r+1}` |
| `T_relay_cutoff` | slot's hard relay-submission deadline (`slot_start + 4.0s` for SSV proposer); reconstruction must complete with `T_submit ≈ 100ms` of slack to land (matches OBFT.md's `header_submit_headroom`) |

Cryptographic safety (`qEnc = qV` + chained encryption + EKM-enforced single-σ-V exclusivity per (slot, layer, round)) ensures only one block can ever get a valid validator signature, regardless of K, R, or round structure. R-round retry only enables more recovery scenarios; it cannot produce two outputs.

### Timing budget — concrete configurations

For fair comparison across configurations, all setups target completion within the 4s relay cutoff. Submission headroom varies per setup based on consensus length. Each setup uses the same total consensus budget allocated differently across rounds based on R.

For the QBFT comparison at uniform "1 BTT per emission" sizing (see [docs/BFT-comparison.md / Sizing convention](BFT-comparison.md#sizing-convention)), QBFT R1 healthy = 4 BTT = 0.8s. Under SSV's production RT = 2s, R1+R2 = 2s + 0.8s = 2.8s — fits at BFT_start ≤ 0.8s; tight at the BFT_start = 1.2s anchor used in this section (overshoots by 100ms). QBFT-no-reflood (per-round timers: R1 timeout 3·BTT, R≥2 4·BTT) gives R1+R2 = 1.6s (8·BTT from BFT_start), which fits comfortably at this anchor (1.1s margin). **QBFT-no-reflood R=2 fits at this anchor** under tightened per-emission sizing; QBFT-SSV R=2 tight by 100ms.

Common parameters: **P99 = 150ms (uniform across rounds), δ = 50ms, n = 4, f = 1**, **`BTT = 200ms`**. Per-round window minimums: `Δ_2.5 ≥ 1 BTT = 200ms`, `ε_3 ≈ 50ms` (propagation-independent — Δ_2.5 absorbs end-of-Phase-2 NR-partial propagation, so ε_3 is purely local reconstruction processing), `Δ_reflood ≥ 1 BTT = 200ms`. `Δ_2 = 1 BTT = 200ms` at tightened per-emission sizing. **Leader broadcast deadline** for round 1 is `T_broadcast_max_1 = T_commit_1 − 1 BTT`; the 1 BTT slack between leader broadcast and `T_commit_1` is for P99 propagation to all honest before round 1's σ-emit. **Receiver acceptance horizon** per round is `T_accept_max_r = T_commit_r + Δ_2 − 1 BTT = T_commit_r` at tightened `Δ_2 = 1 BTT` — no within-round late re-flood absorption; jitter slack lives in cross-round retention.

**MEV-fetch-budget note.** The `T_broadcast_max_1 = T_commit_1 − 1 BTT` deadline is **200ms tighter** than the naive "Phase 1 fetch occupies 0–T_commit" reading. The 200ms gap is P99 propagation slack between leader broadcast and all-honest first-observation; it is not extra fetch budget. Deployments comparing OBFTR to other protocols' "fetch ends at T_commit" framing should account for this. The cost is unavoidable: it is the propagation budget that makes the leader's broadcast reliably observable by all honest before `T_commit_1`.

**Deadline-alignment principle.** The "phantom R2 deadline" framing (used implicitly above): R=1 setups have only an R1 deadline at `TIME_FINAL`; R=2 setups have an R1 deadline (when round 1 must terminate to leave room for round 2) and an R2 deadline at `TIME_FINAL`. By aligning the *outermost* deadline across setups, the comparison shows what each can do given identical total time. R=1 gets one big round; R=2 splits its budget across two rounds with re-flood between them.

#### OBFTR(n=4, K=3, R=1) and OBFTR(n=4, K=4, R=1)

R=1 uses the full 2.0s budget for one extended round. With per-round single-emission of `KindCommit_r`, Δ_2 only needs to cover the propagation budget for that one message; the remaining slot budget goes to Phase 1 fetch.

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch | 1200ms | slot_start + 1.20s | All K leaders' fetch windows fit within 0–1.2s; `T_broadcast_max_1 = 1.20s` |
| Phase-1 broadcast slack | 200ms | slot_start + 1.40s | `T_commit_1 = T_broadcast_max_1 + 1 BTT = 1.40s` (tightened per-emission sizing) |
| Round 1 Phase 2 | 200ms | slot_start + 1.60s | Δ_2 = 1 BTT; KindCommit propagation |
| Round 1 Phase 2.5 | 200ms | slot_start + 1.80s | L_C signaling |
| Round 1 Phase 3 | 50ms | slot_start + 1.85s | propagation-independent |
| Submission | 2150ms | slot_start + 4.00s | generous submission headroom (widens by 400ms vs older 2·BTT/emission framing) |

**Recovery scope.** K-layer fall-through within the single round (silent leaders absorbed via NR-quorum chain in Phase 3 reconstruction walk). At R=1, no within-round partition recovery — bundles arriving past `T_commit_1` at any honest receiver are not counted; the cluster relies on K-layer fall-through to a deeper backup whose bundle did propagate in time. For partition tolerance use R≥2, which retries with new leaders and lets late bundles be picked up in round 2.

K=3: 3 fall-through layers (L_0 → L_1 → L_2). K=4: 4 layers (+L_3). Same timing — Phase 2/2.5/3 don't depend on K — but K=4 has more fall-through depth and ~3KB extra bandwidth.

**Bandwidth (healthy):** ~25 KB at K=3, ~28 KB at K=4 (includes σ_L^V witness section ≈ +2.3 KB at K=4). No round-2 overhead (single round).

#### OBFTR(n=4, K=3, R=2) and OBFTR(n=4, K=4, R=2)

R=2 splits the 2.0s budget into two rounds with new leaders broadcasting in round 2 (or re-broadcasting retained bundles via gossipsub). Each round is a self-contained 3-phase OBFT instance; per-round commitments are independent.

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch (round 1) | 1100ms | slot_start + 1.10s | Round 1 leaders fetch; `T_broadcast_max_1 = 1.10s` |
| Round 1 broadcast slack | 200ms | slot_start + 1.30s | `T_commit_1 = T_broadcast_max_1 + 1 BTT = 1.30s` (tightened per-emission sizing) |
| Round 1 Phase 2 | 200ms | slot_start + 1.50s | Δ_2 = 1 BTT; KindCommit_1 propagation |
| Round 1 Phase 2.5 | 200ms | slot_start + 1.70s | L_C signaling |
| Round 1 Phase 3 | 50ms | slot_start + 1.75s | reconstruction |
| Round 2 re-flood | 200ms | slot_start + 1.95s | `T_commit_2 = 1.95s`; re-flood retained R1 bundles via gossipsub (1 BTT — no fresh fetch in R2) |
| Round 2 Phase 2 | 200ms | slot_start + 2.15s | Δ_2 = 1 BTT; KindCommit_2 propagation |
| Round 2 Phase 2.5 | 200ms | slot_start + 2.35s | L_C signaling |
| Round 2 Phase 3 | 50ms | slot_start + 2.40s | end of round R; consensus expected complete |
| Submission | 1600ms | slot_start + 4.00s | comfortable submission headroom (widens by 600ms vs older 2·BTT/emission framing) |

**Recovery scope.** K-layer fall-through within each round + per-round retry. Bundles late at round-1's `T_commit_1` may be picked up in round 2 if they propagate by `T_commit_2`. Per-round commitments are independent (no cross-round σ-or-NR exclusivity), so an operator who NR-emitted in round 1 may σ-emit in round 2 if the leader's bundle finally propagates.

**Bandwidth (healthy):** ~25 KB at K=3, ~28 KB at K=4 (includes σ_L^V witness section ≈ +2.3 KB at K=4). **+~25-28 KB for round-2 KindCommit_2** if round 1 fails (~52 KB total in failure case).

#### QBFT(n=4, R=2) at recommended sizing

QBFT round structure: PROPOSE → PREPARE → COMMIT → post-consensus partial-sig (4 emission cycles × 1 BTT at tightened sizing ≈ 800ms = 4 BTT healthy at BTT = 200ms — see [docs/BFT-comparison.md / Sizing convention](BFT-comparison.md#sizing-convention)).

| Path | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch | 1200ms | slot_start + 1.20s | leader fetch |
| **Healthy path (R1 succeeds)** | | | |
| Round 1 (3-phase consensus + post-consensus) | ~800ms | slot_start + 2.00s | 3 BTT consensus + 1 BTT post-consensus |
| **Failure path (R1 fails → R2)** | | | |
| Round 1 timeout (round-change timer fires) | RT | varies | RT ≥ ROUND_CHANGE preamble + R1 healthy = ~4·BTT minimum at tightened sizing |
| Round 2 (3-phase consensus + post-consensus) | ~800ms | end of R1 timeout + 0.8s | new leader proposes fresh V |

**R2 fit at tightened sizing.** R1+R2 budget = R1 timeout + R2 (incl ROUND_CHANGE hop + post-consensus). For BFT_start = 1.2s, slot budget = 2.7s. **QBFT-no-reflood R2 fits comfortably at this anchor**: R1+R2 = 8·BTT = 1.6s ≤ 2.7s (1.1s margin). **QBFT-SSV R2 tight at this anchor**: RT=2.0s + R2=0.8s = 2.8s > 2.7s by 100ms; fits at BFT_start ≤ 800ms.

**Comparison conclusion.** Under tightened per-emission sizing OBFTR(R=2) takes ~2.4s of consensus work (Phase 1 fetch + R1 + R2 cycles per the table above); at BFT_start = 1.2s anchor it ends at slot_start + 3.6s with 0.4s submission headroom. QBFT-no-reflood(R=2) takes 1.6s (R1+R2 from BFT_start) and fits with 1.1s margin to the 2.7s budget at the same anchor. QBFT-SSV(R=2) takes 2.8s and overshoots by 100ms (fits at BFT_start ≤ 800ms). Round-2 retry is meaningfully available across all three protocols post-tighten — a significant change from the older 2·BTT/emission framing where QBFT(R=2) barely fit.

**Recovery scope when R2 fits.** Round-change with new leader proposing fresh V — handles partition, validity-divergence, and equivocation cases (the new leader fetches at the moved head; honest converge on the new V). Covers exactly the view-divergence cases OBFTR can't recover from.

**Bandwidth (healthy):** ~14 KB. **+~14 KB for round 2** in failure case (~28 KB total).

#### Comparison summary (BTT = 200ms)

| Setup | Final round ends (from BFT_start = 0) | Submission headroom | V-delivery absorption | Recovery scope | Bandwidth (healthy / failure) |
|---|---|---|---|---|---|
| OBFTR(K=3, R=1) | 1.85s | 2.15s | ~2.0s (continuous mesh) | 3-layer fall-through; soft partition absorption via long Phase 2 | ~25 KB / n/a |
| OBFTR(K=4, R=1) | 1.85s | 2.15s | ~2.0s (continuous mesh) | 4-layer fall-through; soft partition absorption | ~28 KB / n/a |
| OBFTR(K=3, R=2) ★ | 2.40s | 1.60s | ~2.0s (across rounds + explicit re-flood) | 3-layer fall-through + explicit re-flood retry; robust against mesh failures | ~25 KB / ~47 KB |
| OBFTR(K=4, R=2) | 2.40s | 1.60s | ~2.0s (across rounds + explicit re-flood) | 4-layer fall-through + explicit re-flood retry | ~28 KB / ~52 KB |
| QBFT-no-reflood(R=2) (per-round RT) | 1.6s (R1+R2 from BFT_start) | 1.1s at BFT_start = 1.2s | n/a (PROPOSE-driven) | Round-change with new V — covers view-divergence (validity, equivocation) | ~14 KB / ~28 KB |
| QBFT-SSV(R=2) (RT = 2s) | 2.8s (R1+R2 from BFT_start) | tight at BFT_start ≤ 800ms only | n/a (PROPOSE-driven) | Round-change with new V | ~14 KB / ~28 KB |

★ = the appendix's running deployment default for like-for-like comparisons.

**Key observations:**

- **All OBFTR setups fit within the 4s relay cutoff** at BTT=200ms with comfortable submit headroom. Under tightened per-emission sizing, R=1 setups have ~2.15s headroom and R=2 setups have ~1.6s. QBFT-no-reflood(R=2) fits at BFT_start = 1.2s with ~1.1s submit slack; QBFT-SSV(R=2) needs BFT_start ≤ 800ms.
- **R=1 vs R=2 OBFTR have similar V-delivery absorption** but via different mechanisms. R=1's long Phase 2 absorbs delays via continuous gossipsub propagation (relies on mesh staying functional). R=2's two rounds with explicit re-flood absorb delays via deliberate retransmission (re-flood from operators that retained bundles re-establishes propagation if the mesh path failed mid-round). **R=2 is more robust against gossipsub mesh failures**; R=1 is simpler with no round-transition machinery.
- **K=4 vs K=3 vs K=2 trades bandwidth for fall-through depth.** Same timing fit at this BTT (Phase 2/2.5/3 don't depend on K). K=4 = n provides max fall-through; K=3 = f+2 lifts the late-leader pathology; K=2 = f+1 is the BFT-liveness minimum (smallest envelope, relies on R-round retry for fall-through). All three are spec-supported; the choice is deployment-dependent.
- **QBFT(R=2) fits under both the no-reflood per-round timers and (more tightly) SSV's RT=2s production setting** at BFT_start = 1.2s under tightened per-emission sizing. Recovery scope structurally covers view-divergence cases that OBFTR doesn't (validity-divergence, equivocation), via the round-change-fetches-fresh-V mechanism.
- **OBFTR(K=3, R=2) ★** — the deployment chosen as the appendix's running default — combines K = f+2 (late-leader-resilient layer count) with explicit round-2 retry, fits within the 4s budget.

**At higher BTT (e.g., 600ms):** the comparison shifts. R=2 OBFTR's per-round windows grow proportionally; at BTT=600ms R=2 takes 6·BTT = 3.6s, barely fitting any BFT_start. R=1 OBFTR's extended Phase 2 still works (just absorbs proportionally less variance). QBFT's R1 healthy at 4 BTT = 2.4s vs RT=2s = 4·BTT × 5; round-change triggers comfortably. **High-BTT networks favor R=1 OBFTR** for absorbing variance within a single extended Phase 2. See §Failure modes / Sustained partition for the high-BTT liveness limit.

The deadline-tuning rule: each round's duration `Δ_2 + Δ_2.5 + ε_3` is bounded below by `Δ_2 ≥ D_r + δ`, `Δ_2.5 ≥ D_r + δ`, `Δ_reflood ≥ D_r + δ` where `D_r` is the propagation budget for round `r`. Concrete numbers should come from production telemetry (P99 / P999 tails of gossip propagation, EKM signing latency, beacon submit latency, relay submission latency).

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_{L_k}` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_{L_k}`. The protocol consumes the verdict; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_{L_k})` *exactly once per slot/layer*, on the final `V_{L_k}` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_{L_k}` is locked on the originally-signed value. The layer's leader cannot subsequently emit NR/NV on `nr_tag_k` — the Phase-1 σ is their σ-side commitment per cross-phase + cross-round exclusivity.

**Receiver-side validity stabilization — host best-effort toward assumption 3.** OBFTR requires host validity to be unanimous at decision time (see [Assumptions](#assumed)). For SSV's proposer duty, the host narrows the divergence window via per-operator validity stabilization: each operator runs the validity check (including `parent_root`-vs-head) **once at Phase-1 acceptance** against a stable head snapshot, then locks the verdict for the remainder of the slot. Subsequent head movements within that operator do not flip the verdict.

**The validity-locking window spans the full R-round absorption window, not just `1 BTT`.** Operators accept Phase-1 bundles anywhere across the R-round acceptance horizons (within-round at `[slot_start, T_accept_max_r]` + cross-round retention extending to `T_accept_max_R`). Each operator locks their verdict at first-observation, so the cluster-wide spread of lock-times equals the total absorption window. At OBFTR(R=2) Config A with tightened per-round `Δ_2 = 1 BTT`, this is ~1050ms — substantially wider than what a naive "narrow gossipsub-acceptance window ≈ 1 BTT" reading would suggest. A re-org landing anywhere inside this window can split honest verdicts across the boundary; the wider window means a proportionally higher rate of validity-divergence slot-misses for the same re-org distribution.

**Δ_2 (per-round) and R sizing have competing pressures.** Wider per-round `Δ_2` and/or larger R:
- ✓ Wider absorption window → more partition recovery in-protocol.
- ✓ More processing-delay margin per round.
- ✗ Wider validity-locking window → proportionally higher validity-divergence rate for the same re-org distribution.
- ✗ Less submission headroom and (for Δ_2) less leader fetch budget.

Deployments choosing `Δ_2` and R should weight these per their re-org rate and partition-tail observations. The recommended `Δ_2 = 1 BTT` per round at R = 2 is the tightened default (mesh-jitter absorbed by cross-round retention, not per-round Δ_2); deployments under wider partition tails may go higher; deployments with high re-org rate may prefer larger R rather than wider Δ_2.

**This per-operator locking does NOT guarantee cluster-wide convergence.** Operators accept Phase-1 bundles at different `t` within the gossipsub propagation window (operator-local first-observation timestamps, plus clock skew δ). If a re-org happens during this acceptance window, operator A may snapshot pre-reorg (verdict = valid) while operator B snapshots post-reorg (verdict = invalid). Both locks hold; cluster views diverge. The locking ensures *per-operator* stability across the slot, narrowing the divergence window to "re-org during gossipsub-acceptance window" — typical P99 ≈ 100–500ms — but does not eliminate it.

When the residual window fires (re-org lands inside the acceptance window AND operators split across the boundary), assumption 3 is violated and the slot may miss in-protocol — neither σ-quorum (capped at honest-σ-validators + leader's σ_L^V) nor NR-quorum (capped at honest-NV-count) reaches threshold under adversarial byz that withholds NR. This deadlock is the cost of strict re-validation. The looser alternative (validate once at acceptance, never re-check) avoids the in-protocol deadlock but commits on a V whose parent may become orphaned (relay/beacon submission rejection at submit time, also a slot miss). Hosts pick between the two failure modes based on observed re-org rates.

The "permit and slot-miss" framing parallels OBFTR's equivocation handling: the residual validity-divergence split (the 2-2 boundary, or with a passive byzantine) is a view-divergence pattern the protocol does not recover from — an honest majority does recover. Unlike equivocation, it's not attributable to anyone (re-orgs are real-world events, not protocol violations), so no slashing applies; it relies on assumption 3 being approximately true.

**Backup-leader re-org resistance.** Fetching `V_{L_1}` from a deeper-confirmed parent (the `T_1 < T_0` asymmetric schedule already accommodates this) reduces the likelihood that L_1's parent becomes orphaned. Backup is structurally re-org-resistant by construction.

Implementation notes:

- Each operator validates `parent_root` against a stable head snapshot taken at Phase-1 acceptance time; the verdict is locked into the operator's local state for the remainder of the slot.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" and "single V per operator per layer" — a second signing attempt with a different V at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing AND operator-side double-signing detectable cluster-wide.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same.

2. **Per-round deadline coordination.** Clock skew across operators must be bounded by `δ` and known. Per-round deadlines:

   - **`T_broadcast_max_r = T_commit_r − 1 BTT`**: round-r leader broadcast deadline (tightened per-emission sizing). Each layer's leader (in round r's leader set) must broadcast by this time so all honest first-observe by `T_commit_r` under P99 propagation; jitter slack lives in cross-round retention.
   - **`T_commit_r`**: round-r receiver acceptance cutoff. Bundles first-observed past `T_commit_r` at any honest receiver are not counted by that receiver in round r, but may still be picked up by gossipsub propagation in time for round r+1.

   Per-round window minimums:

   - **`Δ_2 ≥ 1 BTT`** for each round's Phase-2 window: every honest's `KindCommit_r` emitted at `T_commit_r` must propagate to every other honest by `T_commit_r + Δ_2` (start of Phase 2.5).
   - **`Δ_2.5 ≥ 1 BTT`** for each round's Phase-2.5 window: L_C claims propagate cluster-wide.
   - **`Δ_reflood ≥ 1 BTT`** between rounds: round-r+1 leaders broadcast (or retained bundles re-broadcast) and propagate before `T_commit_{r+1}`.

   All are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

3. **Choosing K (layer count).** K is per-duty:

   - `K = 2`: minimum (one primary + one backup). Fits SSV proposer duty's 4s budget with margin. Recovery scope: handles 1 byzantine leader at L_0, with L_1 as backup.
   - `K = 3..n`: larger K provides additional fall-through layers for non-byzantine multi-failure (rare events: relay timeouts, network jitter, validity divergence at multiple layers). At `n = 4`, max useful K is 4 (one layer per operator). Each extra layer adds a level of chained encryption (~1 KB per onion at K=3, ~2 KB at K=4 — within practical bandwidth).

   **K bound (see §Setting for the full discussion):**
   - **`K ≥ f+1`** — BFT-liveness minimum (≥ 1 honest leader exists by pigeonhole over the f-byz bound). This is the only K-floor the protocol mandates. `K ≥ f+2` additionally provides late-leader-resilience (≥ 2 honest leaders); whether to use it is a deployment choice.

4. **Choosing R (round count) and RT (round timeout).** R is per-duty, governed by the duty's deadline budget:

   - **Proposer duty** (4s relay cutoff): R = 2 fits cleanly. Round 1 = ~3.5s (full Phase 1 → 3); round 2 = ~250ms (re-flood + Phase 2'+2.5'+3'). Submission window: ~250ms.
   - **Attestation duty** (12s slot, 16s aggregation cutoff): R = 3..5 fits comfortably. More rounds extend partial-synchrony tolerance.
   - **DKG / non-time-critical duties**: R can be very large (e.g., R = 10) since deadline budget is generous.

   The `R · P99` propagation tolerance is the protocol's effective resilience knob. Increasing R extends recovery scope at the cost of slot-budget consumption.

5. **Tag construction and replay.** Each NR tag must uniquely bind `(slot, cluster, layer)` to prevent replay across slots/layers/clusters. Structure: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`.

6. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one OBFTR instance and assumes:
   - Single OBFTR instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between OBFTR and any other path that signs against the V-signing share.
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction across all rounds) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k` across all rounds), not just submission.

7. **Equivocation is permitted, not recovered.** OBFTR does not provide an in-protocol equivocation recovery mechanism. Some patterns reach qV naturally (when 2-of-3 honest happen to σ-commit on the same V at f=1 n=4; leader's σ_L^V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. Equivocation is treated as a slashable byzantine fault; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots.

## Where this came from

OBFTR is the multi-round generalization of [OBFT](OBFT.md): R rounds where each round runs a self-contained 3-phase OBFT instance (Phase 1 → Phase 2 → Phase 3), per-round commitments are independent (a round-r-NR honest can σ-emit at round r+1), and L_C cluster-consensus coordinates round transitions. Two design choices distinguish OBFTR from a QBFT-style multi-round protocol:

1. **Per-round retry with re-flood, no cross-round commitment lock-in.** An honest operator who NR'd at round r (e.g., V didn't arrive in time) is not locked out of σ-emitting at round r+1 if V finally propagates. EKM keys round-r commitments under `(slot, layer, round, side, value_root)`, so within-round σ XOR NR exclusivity is preserved per round without forcing cross-round exclusivity. This recovers aggressive-marginal failures (>1 of n−f honest missing re-flood at round-1 cutoff).
2. **L_C cluster-consensus** (`KindLCClaim`): operators broadcast their local view of the cluster's frontier layer; qV agreement promotes L_C cluster-wide, accelerating round transitions and saving bandwidth on dead layers.

The R-round retry covers network-partition tails up to `~R · P99` propagation tolerance — the case bare OBFT (single-round) cannot recover. The cost is +per-round EKM signing-event tags, +per-round Phase-1 retention through round R, plus the L_C consensus signaling overhead.

The result is a protocol that **strictly improves on bare [OBFT](OBFT.md) (single-round)** for partition cases (R-round retry with re-flood; per-round commitments independent) at the same 2-RTT healthy-path latency. It does **not match QBFT's full recovery scope** — Class A failure modes (assumption violations) and Class B byzantine grief patterns are out of scope, handled by assumptions 3 (host validity stabilization) and 4 (rational-byzantine deterrent) respectively. The R (round count) parameter is the partial-synchrony tolerance knob; K (layer count) is the multi-failure-fall-through depth knob.

## Appendix A — Protocol comparisons

This appendix gives high-level structural comparisons against the protocols OBFTR relates to: [OBFT](OBFT.md) (the single-round simplification), [2abOBFT](2abOBFT.md) (the Phase-2-split successor that recovers equivocation and validity-divergence in-protocol), and QBFT (SSV's existing consensus protocol).

### A.1 — Comparison with [OBFT](OBFT.md)

OBFT is OBFTR with `R = 1` and the round-retry machinery stripped. They share Phase 1 / Phase 2 / Phase 3 structure, K-layer fall-through, chained encryption, the three commitment states (σ, NR, NV), and the five slashing-evidence rules.

| Aspect | OBFTR (R = 2) | [OBFT](OBFT.md) |
|---|---|---|
| Round structure | Up to R rounds with re-flood retry; round transitions on timer or L_C-quorum promotion | Single round, no retry |
| L_C cluster-consensus signaling | `KindLCClaim` in Phase 2.5 | **Removed** (no rounds to transition between) |
| Phase 2.5 | L_C round-coordination signaling (`KindLCClaim`) | **Removed** entirely (single-round, no rounds to coordinate between) |
| Per-round acceptance widening | `T_candidate_accept_r` widens across rounds; auth-only-retention for next round | **Removed** — single receiver acceptance window aligned with σ-emit-propagation feasibility |
| Cross-round σ-or-NR exclusivity | EKM enforces across rounds + cross-phase | **Cross-phase only** — no rounds to span |
| EKM cross-share atomicity | Required across rounds for re-emission semantics | Per single signing event only |
| EKM persistent partial-sig cache | Required (cached σ partial must survive operator restart for cross-round re-emission) | **Not required** |
| EKM deterministic re-signing fallback | Required | **Not required** |
| Partial-synchrony envelope | `R · P99` (e.g., `2·P99` at R=2) | `P99` (single round) |
| Slot budget at K=4, BTT=200ms | Ends at slot_start + 2.40s (with tightened `Δ_2 = 1 BTT` per round at R=2) | Ends at slot_start + 2.00s at earliest T_commit; OBFT typically anchors T_commit later (3.40s at Config A) to redirect the saved budget into MEV-fetch — consensus then ends at slot_start + 3.85s. See [OBFT.md §Timing budget](OBFT.md#timing-budget). Saves ~0.5s of consensus runway either way. |
| Submission headroom (4s relay cutoff) | 0.90s | 2.00s |
| Bandwidth (healthy, n=4, at each protocol's default K) | ~25 KB across 2 emissions per round at OBFTR's K=3 default (includes σ_L^V witness section ≈ +1.74 KB per round); K=4 up-tier: ~28 KB | ~6–8 KB across 1 emission at OBFT's K=2 default (includes σ_L^V witness section ≈ +1.2 KB at K=2 n=4); K=4 up-tier: ~28 KB |
| Bandwidth (worst case at R=2 with round-1 failure) | ~52 KB | n/a (no round 2) |
| High-P99 fit (P99 = 500ms) | Does not fit 4s relay cutoff | Fits with ~1.3s submission headroom |

**When to choose which:**

- Use **OBFTR** when the propagation tail is meaningfully wider than P99/P999 budget and within-envelope coverage of `(P99, R · P99]` partitions is needed.
- Use **[OBFT](OBFT.md)** when the partition envelope at single-round absorption (`P99`) suffices; or when high-P99 fit / submission headroom / spec simplicity outweigh the wider absorption.

### A.2 — Comparison with [2abOBFT](2abOBFT.md)

[2abOBFT](2abOBFT.md) is the single-round Phase-2a/2b successor. Both protocols carry the Phase-1 leader σ-witness; 2abOBFT replaces OBFTR's R-round retry with a `KindNoValue` no-lock + upgrade, recovering **σ-locked-split (f-f) equivocation** and **transient mesh-flakiness** at L_0 and deciding a **late L_0-leader at L_0** — in a single round, at the cost of OBFTR's cross-round partition-tail retry. The residual misses (1-1-1 equivocation, the 2-2 validity boundary) are shared.

| Aspect | OBFTR | [2abOBFT](2abOBFT.md) |
|---|---|---|
| Phase-1 leader σ-witness | Yes — leader signs σ_V at Phase 1 (cryptographic head-start) | **Yes** — leader signs the per-layer σ-witness `LWitness` at Phase 1 (same head-start) |
| Phase-2 structure | Single Phase 2 + Phase 2.5 (L_C signaling), per round | Phase-2 split: 2a (`KindValue` / `KindNoValue` / `KindCommit-NRDirect`) + dynamic 2b (`KindCommit-NR`, fired once the no-value cohort reaches `qEnc`) |
| σ-locked-split equivocation (f-f) | Slot misses (slashable; R-invariant) | **Recovered at L_0** — witness head-start + peer-reflood-V harvest of the value, then upgrade |
| 1-1-1 equivocation | Slot misses (slashable) | Slot misses (σ-locked, no NR pivot) — shared residual |
| h_V=1 selective-delivery | Slot misses — OBFTR's witness section carries σ_L^V but not V, so V-drop receivers can't recover (no peer-reflood-V) | **Recovered at L_0** under healthy mesh (peer-reflood-V: `KindValue` carries V + the forwarded witness); degraded-mesh tail misses |
| Validity-divergence (re-org during acceptance) | Honest-majority recovers (3-σ → L_0; 1-σ-3NV → L_1); 2-2 boundary + passive-byz miss | **Same** — honest-majority recovers; 2-2 boundary + passive-byz miss |
| Transient mesh-flakiness | Recovered across R-rounds (cross-round retention) | **Recovered in a single round** (`KindNoValue` no-lock) |
| Round structure | R rounds — cross-round retry catches partitions up to the R·BTT absorption window | Single round — no cross-round retry; partitions beyond one round's window miss |
| Healthy-path latency | 2 RTTs per round | 2 RTTs (Phase 2b adds a round only on NR fall-through) |

**2abOBFT trades OBFTR's R-round retry for single-round recovery of σ-locked-split equivocation and transient mesh-flakiness** (via the `KindNoValue` no-lock + the Phase-1 witness head-start). For deployments where those grief patterns matter more than partition-tail retry (high-MEV proposer slots, adversarial-byz clusters, mesh-flakiness conditions), 2abOBFT closes them in-protocol rather than relying on assumption 4 alone; the shared residual (1-1-1 equivocation, the 2-2 validity boundary) remains.

### A.3 — Comparison with QBFT

QBFT is SSV's existing consensus protocol. The key structural difference: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus partial-sig collection)**; OBFTR (and the rest of the OBFT family) fuses the two by embedding partial signatures inside Phase-2 onions. Most observable trade-offs trace back to this structural difference.

For per-scenario liveness behavior (recovery scope, mechanism, outcome) see [Liveness comparison: OBFTR vs QBFT](#liveness-comparison-obft-vs-qbft). This appendix covers the structural / cost dimensions: protocol shape, latency, bandwidth, safety posture, primitive complexity, and deployment maturity.

| Aspect | QBFT | OBFTR (R=2, K=2 for proposer) |
|---|---|---|
| Protocol shape | Multi-round (PROPOSE → PREPARE → COMMIT) with per-round leader rotation; round-change on timeout | R-round, K-layer onion fall-through; round transitions via timer or L_C consensus |
| Consensus and signing | Decoupled: consensus reaches a decided value, then a separate post-consensus phase collects 2f+1 partial sigs | Fused: Phase-2 onions carry σ partials directly, Phase 3 reconstructs |
| RTTs to signed output (min, healthy) | 3 (consensus) + 1 (post-consensus) ≈ 4 | 2 (Phase 1 + Phase 2) |
| Termination guarantee | Eventually-terminating across rounds (under partial synchrony) | Conditional on `R · P99 ≥ real_propagation`; tunable via R |
| Safety posture | Honest-majority (`2f+1` honest) + correct quorum-certificate verification | Honest-majority cryptographic via `qEnc = qV = 2f+1` + chained IBE + EKM-enforced per-operator commitments. Same trust posture as QBFT — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). |
| Bandwidth (1 round, healthy n=4) | ~14 KB | ~23 KB (includes σ_L^V witness section ≈ +2.3 KB) |
| Bandwidth (1 round failure n=4) | +12 KB per round + a full additional round on top | +~22 KB for round 2 re-flood (only if round 1 failed; includes round-2 σ_L^V witness re-emit) |
| Latency (healthy, n=4) | ~750 ms | ~250 ms (Config A) — see [Timing budget](#timing-budget--concrete-configurations) for higher-D configurations |
| Latency (1 round failure, n=4) | ~3.0 s (round-1 timeout 2s + round-2 success ~750 ms) | ~500 ms (Config A: round 1 fail at ~250 ms + round 2 succeed at ~250 ms) |
| Latency (2 round failures, n=4) | Misses 4s relay cutoff | ~750 ms (Config A; round 1, 2 fail, round 3 succeed if R ≥ 3) |
| Slashing-protection scope | Single block sig per slot, gated at submission time | Multiple per-share sigs per slot at candidate-signing (Phase-1 leader, Phase-2 onion across rounds) gated by EKM cross-keypair coordination — see [EKM coordination model](#ekm-coordination-model) |
| Cryptographic primitive | BLS threshold signatures only | BLS threshold signatures + threshold IBE / SWE (drand/tlock-style) |
| Application contract | Decides on any value the application proposes | K-layer with primary + backup-leader fetches |
| Production maturity | What SSV runs today; corner cases hit and fixed | New codebase; deployment confidence to be derived |

**Cost-side trade-off summary:**

- **Latency.** OBFTR wins on every dimension at Config A: ~3× faster healthy, ~6× faster on round-1 failure. At Config B (P99 = 500ms), OBFTR's advantage shrinks because per-round windows must scale with D, but R = 2 still wins on round-1-failure recovery if it fits the budget.
- **Bandwidth.** Comparable healthy-path; OBFTR slightly higher due to onion encryption. Round-failure bandwidth: OBFTR lower per recovery round (~21 KB vs QBFT's ~12 KB per round + full additional consensus round).
- **Cryptography.** QBFT only needs BLS threshold signatures. OBFTR additionally needs threshold IBE / SWE (drand/tlock-style; audited, deployed since 2023). The IBE primitive is more novel; for risk-averse deployments, this is a real consideration.
- **Spec surface.** OBFTR is meaningfully larger spec than bare [OBFT](OBFT.md) (R-round structure, L_C consensus, cross-round atomicity). Comparable to QBFT once you account for QBFT's view-change protocol, prepared-certificate verification, etc.
- **Maturity.** QBFT is production. OBFTR is a new codebase — deployment confidence has to be derived.

**Where QBFT genuinely wins for proposer duty:**

- **Validity-divergence recovery.** The concrete scenario: head re-org mid-slot invalidates parent_root for L_0 candidate; honest verdicts genuinely diverge. QBFT round-changes through with new leader fetching at new head. OBFTR requires the host to stabilize the verdict at Phase-1 acceptance (assumption 3) — sufficient when re-orgs are rare relative to the slot budget, but can lead to submission rejection if the locked-on parent later becomes orphaned.
- **1-1-1 equivocation recovery.** QBFT's round-change with new leader proposing a fresh V breaks the deadlock. OBFTR relies on the rational-byzantine deterrent (assumption 4) — the byzantine pays in future slots, but the affected slot misses.
- **Cryptographic primitive simplicity.** BLS-only, no IBE.
- **Production maturity.** QBFT is what SSV runs today.

**Where OBFTR wins:**

- **Healthy-path and recovery latency.** Round-overhead at Config A is ~400ms; QBFT round-change is ~2s. OBFTR can fit ~7-9 recovery rounds in the 4s relay cutoff that QBFT fits ~2.
- **Network-partition recovery and adversarial scheduling.** OBFTR's R configurable; QBFT's round-budget capped by RT × R ≤ deadline.
- **Multi-leader-failure recovery.** OBFTR's K-layer parallel fall-through resolves K-1 silent layers within a single round; QBFT round-changes K-1 times serially. For K=3 with 2 silent leaders, OBFTR recovers in ~1000ms; QBFT in ~5s.
- **All-equivocation-NR recovery** (byz delivers V's early enough for re-flood to spread conflicts before σ-emit; all honest land in equivocation-NR). NR-pool reaches qEnc at L_0 in round 1 → fall-through to L_1 in same round (~1000ms at Config A); QBFT round-change (~2s). σ-locked split patterns where byz delivers near end-of-Phase-1 are not recovered by either protocol uniformly — see [Liveness comparison: OBFTR vs QBFT](#liveness-comparison-obft-vs-qbft).
- **Configurable per duty.** OBFTR's R and K knobs let operators tune per-duty (proposer = `R=2, K=2`; attestation = `R=4, K=3`; DKG = `R=10, K=n`); QBFT has a single round-timeout knob.

The operational bottom line: OBFTR decisively wins on common-case latency and partition-class recovery; QBFT wins on validity-divergence and 1-1-1-equivocation recovery (rarer modes that OBFTR addresses via assumption 3 and assumption 4 respectively). For SSV proposer duty under a 4s relay cutoff, the choice depends on observed re-org rate, cluster's tolerance for the 1-1-1 equivocation case via the rational-byzantine deterrent, and the relative weight of common-case latency vs. worst-case coverage.
