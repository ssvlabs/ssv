# OBFTR — Onion BFT with R-Rounds

A multi-round agreement protocol for SSV clusters that produces one collective threshold-signed value per "slot" against a hard deadline. OBFTR achieves agreement *cryptographically* (cluster-wide unique output via threshold cryptography over EKM-enforced per-operator commitments) over a configurable K-layer onion structure, with up to R recovery rounds providing graceful degradation under network partitions and byzantine-leader equivocation.

OBFTR generalizes the [TBFT](TBFT.md) shape: K layers (configurable, `K ≤ n`), R rounds (configurable, `R ≥ 1`). At `R = 1` and `K = 2`, OBFTR reduces to baseline TBFT. The added machinery — Defer state for deferred commitment, R-round retry with re-flood, L_C cluster-consensus for round-transition coordination — extends recovery to network-partition cases (≤ R·D propagation tolerance) which baseline TBFT misses.

OBFTR's recovery scope is intentionally bounded. The protocol recovers from **network-partition** patterns (gossipsub propagation > round-1 cutoff but ≤ R·D) via Defer + re-flood across rounds. The protocol does NOT recover from **view-divergence** patterns: equivocation (byzantine leader sending different V's to different honest) and validity-divergence (e.g., re-org during gossipsub-acceptance window causing some honest to validate V and others to NV). View-divergence cases either succeed naturally (when the split happens to leave 2-of-3 honest σ-committed on the same V at f=1 n=4, allowing leader's σ_L^V to push the pool to qV) or slot-miss. Equivocation in non-naturally-recovered patterns is **slashable** (the byzantine leader pays the stake-based penalty); validity-divergence is not attributable to anyone and just slot-misses. See [Assumptions and implications](#assumptions-and-implications) for the explicit list. The TBFTR-style **Phase 2a/2b split** closes the equivocation gap at +1 RTT cost; documented as a future improvement, not in current OBFTR.

The protocol description below targets `n = 4` (`f = 1`) as the running example, with K and R tunable per duty. SSV's Ethereum proposer duty is used as the running application example. Generalizations to `n ≥ 7` are noted inline where the algebra changes.

## When to use it

**Suited for:** any SSV duty (proposer, attestation, sync committee, DKG) where TBFT's 2-RTT healthy-path latency is desired plus tolerance for network partitions. The configurable R and RT let operators tune recovery aggression per duty's deadline budget. Particularly suited for proposer duty (4s relay cutoff) where the round-2 overhead is ~250ms vs QBFT's ~2s round-change.

**Adversarial-byz operating conditions warrant Phase 2a/2b alongside OBFTR.** Bare OBFTR (at any R) does not defend against an adversarial byzantine that deliberately engineers σ-locked split equivocation or h_V=1 selective-delivery deadlocks — these patterns are R-invariant and reliably slot-miss when byz is L_0, regardless of how many rounds are configured. The rational-byzantine deterrent (assumption 4) handles them across many slots, but per-slot they cause clean slot-miss with weakly-slashable behavioral evidence. The TBFTR-style **Phase 2a/2b split** ([§Where this came from](#where-this-came-from)) is the structural fix that closes these patterns in-protocol at +1 RTT per round; **for deployments operating under realistic adversarial conditions (small clusters, transient operators, weak governance, high-stake-to-grief-value ratios), Phase 2a/2b should be considered near-term**, not future. OBFTR(R≥2) standalone is the spec-richest *multi-round point*; OBFTR + Phase 2a/2b is the more robust *production* point for adversarial deployments.

**Not suited for:** scenarios requiring host-validity-divergence recovery within a slot — OBFTR assumes host validity is unanimous at decision time (see [Assumptions](#assumptions-and-implications)). QBFT is the appropriate choice when validity is unstable across the consensus window. Not suited for adversarial-byz operating conditions without Phase 2a/2b adoption — see above.

**Also not suited for:** general-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. OBFTR (like TBFT) gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n = 3f+1` (the BFT-tight setting; matches SSV's deployment configurations `n ∈ {4, 7, 10, 13}`). The running example is `n = 4, f = 1`. The threshold formula `qV = qEnc = 2f+1` below equals `n − f` exactly at this setting; this equality is what makes the Pigeonhole arguments in [§Safety](#safety-cryptographic-honest-majority) hold.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) as the threshold-signing scheme for no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety". At `n = 4`, `qEnc = 3`.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts and protocol-level claims, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- **K layers** (`max(2, f+1) ≤ K ≤ n`, configurable; **K ≥ f+2 strongly recommended** — see below) with deterministically-derived leaders: layer 0 with **primary leader** `L_0`, layer 1 with **backup leader** `L_1`, ..., layer `K-1` with `L_{K-1}`. Leaders must be distinct (`L_i ≠ L_j` for `i ≠ j`). For `K = n`, every cluster member is a leader at exactly one layer per slot.

  **Two distinct K bounds:**
  - **`K ≥ f+1` is the BFT-liveness minimum** — ensures at least one layer has an honest leader (by pigeonhole over the f-byz bound). At K < f+1, all leaders could be byzantine and no σ-quorum reaches at any layer.
  - **`K ≥ f+2` is the late-leader-resilience minimum** — ensures at least *two* honest leaders exist, so a single late-broadcasting honest leader doesn't foreclose the slot via the cross-round NR-lock pathology (see §Failure modes / Late deepest-layer leader broadcast). At K = f+1 with the single honest leader running late, the slot misses; at K ≥ f+2 a second honest leader provides fall-through redundancy.

  Concrete minimums by f: at `f = 1`, BFT-min `K = 2` (matches baseline TBFT) but **late-leader-resilient `K = 3` recommended**; at `f = 2, n = 7`, BFT-min `K = 3` but resilient `K = 4` recommended; at `f = 3, n = 10`, resilient `K = 5`. The K choice within the recommended range is per-duty: `K = f+2` for proposer duty (smallest K with both bounds satisfied, fits 4s relay cutoff with margin); `K ≥ f+3` for duties with longer budgets (attestation, sync committee) or where additional non-byzantine multi-failure tolerance is desired.
- **R rounds** (`R ≥ 1`, configurable) with **round timeout** `RT`. Round 1 runs the standard Phase 1 → Phase 2 → Phase 2.5 → Phase 3 sequence (2-RTT healthy path). Rounds 2..R fire on timeout when prior round's reconstruction failed at all layers — they re-flood retained Phase-1 bundles and allow late σ-emit by Defer-state operators who newly received V via re-flood. The final round (`R`) forces NR-emit on any operator still in the Defer state. The R choice is per-duty: `R = 2` for proposer duty (one recovery round fits 4s budget); `R ≥ 3` for non-proposer duties.
- Per-layer leader-fetch deadlines `T_{K-1} < T_{K-2} < ... < T_1 < T_0`, plus a per-round cluster deadline `T_commit_r` for round `r`. (`T_commit_r` is a *view-fix point* for round `r`: each operator commits its stance based on what it observed by `T_commit_r`. Reconstruction and submission happen after.) The asymmetric fetch times let primary leaders fetch high-MEV values late (`T_0` close to `T_commit_1`) while deeper layers' leaders fetch safe early values from deeper-confirmed parents (`T_{K-1}` well before `T_0`).
- Two distinct per-round deadlines (do not conflate):
  - **Per-round leader broadcast deadline** `T_broadcast_max_r = T_commit_r − 2(D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. For round 1, each layer's leader must finish broadcasting by `T_broadcast_max_1` so that under worst-case propagation, all honest first-observe by `T_commit_1 − (D + δ)` (within round-1 receiver acceptance window with comfortable slack). Per-layer fetch windows fit within this deadline: `T_k + Δ_1 ≤ T_broadcast_max_1`. For rounds r > 1, no fresh broadcast happens — re-flood at round-r start completes within `Δ_reflood = D + δ`.
  - **Per-round receiver acceptance horizon** `T_accept_max_r = T_commit_r + Δ_2 − (D + δ)`. Receivers accept Phase-1 bundles whose first-observation time is at or before `T_accept_max_r` for round r. A bundle first-observed past `T_accept_max_r` cannot trigger a downstream σ-emit observable by peers in round r (the σ-emit propagation horizon is exactly `T_accept_max_r`), so it is auth-only-retained for round r+1's acceptance window if first-observed at or before `T_accept_max_{r+1}` (see "Phase 1 / Retention bounds"). Bundles first-observed past the final round's `T_accept_max_R` are rejected entirely.

  **Why two deadlines.** The leader broadcast deadline ensures honest leaders' bundles propagate to all honest within partial-synchrony in time for round-1 σ-emit. The receiver acceptance horizon (per round) absorbs late gossipsub re-flood up through the σ-emit-propagation feasibility horizon for that round. The window extends *past* `T_commit_r` (into Phase 2) — that is what enables within-round σ-emit on late-arriving bundles, which combined with cross-round retention gives OBFTR's `R · D` partial-synchrony envelope.

  **Per-round acceptance widening (cross-round retention).** Bundles first-observed in `(T_accept_max_r, T_accept_max_{r+1}]` are auth-only-retained — they remain on the wire but cannot be σ-signed in round r — and become accepted at round r+1 start (when their first-observation time is at or before `T_accept_max_{r+1}`). This is OBFTR's structural mechanism for partition recovery beyond a single round's σ-emit-propagation window: bundles late by one round get a fresh chance in the next.

  This is OBFTR's key timing structure vs TBFT: late delivery is not silently dropped — it's absorbed into the current round's acceptance window if within the σ-emit-propagation horizon, or deferred to the next round if past it.
- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

## Assumptions and implications

OBFTR's claims hold conditional on a small set of explicit assumptions about the deployment environment. They are consolidated here; the rest of the spec assumes them and refers back to this section rather than re-deriving the trade-offs each time.

### Assumed

1. **Standard BFT trust bound at the tight setting.** `n = 3f + 1`, up to `f` operators may be byzantine. At `n = 4`, `f = 1`. SSV deploys at the BFT-tight bound (`n ∈ {4, 7, 10, 13}` for `f ∈ {1, 2, 3, 4}`); the threshold formula `qV = qEnc = 2f+1` below requires this tightness to give `2f+1 = n − f`, which is what the Pigeonhole arguments depend on. Honest operators run protocol-conformant software (correct EKM rule enforcement, correct host application, correct gossipsub behavior). Byzantine operators may deviate arbitrarily within their f-bound.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `D` (cluster gossipsub P99/P999) and clock skew `δ`. Safety is unconditional on timing; only liveness depends on this.

3. **Host validity is unanimous at decision time** (best-effort assumption). OBFTR assumes the host application's `valid` / `not-valid` verdict on `V_{L_k}` is the same across all honest operators by the time they emit Phase 2. Operators may transiently diverge (e.g., a head change observed by some but not others). The host's job is to make divergence rare via per-operator stabilization — typically by validating against a stable head snapshot taken at Phase-1 acceptance time, then locking the verdict for the remainder of the slot. **This per-operator locking does not give cluster-wide convergence; it narrows the divergence window to events that land inside the receiver acceptance window** = R-round absorption window (per-round acceptance horizons + cross-round retention through round R; ≈ 1150ms at Config A R=2 with recommended Δ_2 = 2(D+δ) per round). When divergence does occur — a re-org during the acceptance window with operators accepting on either side of it — the assumption is violated and the slot may miss; the protocol does not recover. **Note**: the per-round receiver acceptance horizon (which extends into each round's Phase 2 to absorb late re-flood) plus cross-round retention widens the validity-locking time spread proportionally with the absorption window. At OBFTR(R=2) Config A, this is substantially wider than the older "T_candidate_accept = T_commit_r − (D+δ)" framing's per-round window. The wider absorption window trades partition recovery against validity-divergence exposure. See [Application: SSV Ethereum proposer duty / Head-change handling](#head-change-handling) for the SSV-specific stabilization workflow and the residual divergence window.

   The validity check exists to prevent the cluster from agreeing on a garbage / invalid V — it is not a divergence-recovery mechanism. NV is operationally identical to NR for protocol counting; it does not trigger any in-protocol divergence-handling path.

4. **Persistent operator set with rational-byzantine deterrent.** OBFTR operates within a stable SSV cluster running protocol instances over many slots. The deterrent is the same one that already disciplines an offline operator under SSV's network-wide threat model: per-validator operator fees flow continuously to all cluster operators regardless of per-slot contribution (the remaining `n − f` honest carry the work at zero ops cost to the silent/byzantine), and stakers — who can observe per-cluster slot-miss rates — are expected to migrate validators away from underperforming clusters, collapsing the silent/byzantine operator's fee accrual to zero. SSV is already designed for the operator-down case ("the cluster and stakers deal with it"); the rational-byzantine claim is that a byzantine operator gains nothing an offline operator wouldn't already get, and has reputation (persistent across slots) to lose.

   **Asymmetry — Byzantine vs Down — and what restores equivalence.** With QBFT, `Byzantine ≡ Down` automatically: round-change rotates past silent or malformed PROPOSE/PREPARE/COMMIT, so the worst a byzantine can do per-slot is silently going offline. With OBFT-family, byzantine is *significantly worse on latency than Down* — equivocation σ-locked splits, h_V=1 selective-delivery, fake-encrypted-presence, and behavioral σ-refusal can engineer per-slot grief above what equivalent offline behavior would produce. The expected mitigation is **manual blacklisting**: the cluster's surviving `n − f` operators agree out-of-band on the misbehaving operator's identity, push a config-file update to their nodes treating that operator's messages as silent for subsequent slots, and the byzantine's effective contribution becomes identical to offline — restoring the `Byzantine ≡ Down` guarantee. **The blacklist mechanism is a planned OBFT-family protocol extension** — current OBFT/OBFTR/2abOBFT do not specify it; until added, the byzantine's per-slot grief surface above offline behavior is bounded only by stakers eventually migrating validators away from the cluster.

   The on-wire byzantine-fault evidence ([§Slashing evidence](#slashing-evidence)) informs both (a) staker migration decisions and (b), once the extension lands, the cluster operators' blacklist trigger. Filing a stake-slashing transaction via the SSV contract is a complementary punitive action available where the evidence is cryptographically self-contained, but is not the primary deterrent — the primary deterrent is the economic equivalence to going offline plus the eventual `Byzantine ≡ Down` collapse via blacklisting. See [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the evidence-quality discussion and how it interacts with the blacklist's detection latency.

5. **Coordinated EKM across both keypair shares.** The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log keyed on `(slot, layer, side, value_root)`. See [EKM coordination model](#ekm-coordination-model).

6. **Independent V-keypair and IBE-keypair DKGs over the same operator set at the same threshold `qV = qEnc = 2f+1`.** Both DKGs are run once at cluster init with all `n` operators participating; the shared operator set and threshold is what makes Pigeonhole 1's algebra work (`h_σ + h_NR ≤ 2f+1` across the cross-keypair signing log).

### Implications of safety being honest-majority cryptographic, not 100% cryptographic

OBFTR's safety holds against an offline-aggregating byzantine *given the protocol rules are correctly enforced by honest operators*. Enforcement happens at two independent layers per operator — the **protocol layer** (operator software implementing the OBFTR state machine, deciding when to request σ vs NR vs Defer) and the **EKM** (slashing-protection log that rejects bad signing requests as defense-in-depth). Together they ensure single-σ-V per (slot, layer) per operator and σ-XOR-NR per layer; these rules reduce the safety proof to pigeonhole arguments on the cluster-wide signed-message set.

An operator whose software actively misbehaves (maliciously compromised, or with compounding protocol+EKM bugs that produce byzantine-equivalent signing behavior) consumes f-budget directly. With `f = 1` byz at `n = 4`, one such operator is tolerated; two violate the trust bound and Pigeonhole arguments can fail. Single-layer bugs (protocol-only or EKM-only) do **not** produce byzantine behavior on their own — the other layer catches them. See [EKM coordination model](#ekm-coordination-model) for the full defense-in-depth analysis.

**This is the same trust posture as QBFT.** QBFT's safety also holds under f-byz with honest-majority correct code paths. A bug in `2f+1` honest operators (e.g., the post-consensus signing path signs both candidates from a split decision, or the prepared-certificate verification accepts conflicting commit certificates) would equally violate QBFT's safety guarantees. Neither protocol is "100% cryptographic" against operator-side software bugs; both rely on operator software correctness for honest operators.

Accordingly, "cryptographic safety" in this spec means: against a partially-synchronous byzantine network adversary plus up to `f` byzantine operators, honest operators following the protocol cannot produce two contradictory outputs. It does not mean: safety against arbitrary bugs in honest operator software.

### Implications of validity-divergence not being recovered (assumption 3)

**This is a TBFT-family limitation, not specific to OBFTR.** [TBFT.md](TBFT.md) "Application-validity-divergence — known liveness limit" documents the same algebraic deadlock with the same root cause: per-operator independent validity verdict + leader's Phase-1 σ_V locked + cross-phase exclusivity. OBFTR inherits all three from TBFT and adds nothing that recovers from it — R-round retry doesn't help (verdicts are locked at acceptance), Defer state doesn't help (Defer is for partition recovery, not verdict reconciliation), L_C consensus doesn't help (it coordinates the frontier layer, not the validity verdict).

If assumption 3 is violated mid-slot — honest verdicts genuinely diverge after Phase 2 emit — OBFTR cannot recover within the slot. There is no fresh-V refetch mechanism. The byzantine leader's Phase-1 σ_V is locked; honest who NV cannot switch to σ; cluster deadlocks at L_k or falls through to L_{k+1} (where the same divergence pattern may repeat).

For SSV proposer duty, the host's stabilization workflow (validate parent_root once at acceptance, lock the verdict) is the design's path to satisfying assumption 3 — same approach as baseline TBFT. If the host cannot guarantee unanimous validity (e.g., re-orgs are common enough that locking-at-acceptance leads to too many submission rejections), the appropriate fixes are at the protocol-family level, not OBFTR-specific:

- **Phase 2a/2b** (TBFTR-style; see [Path forward — Phase 2a/2b](#future-improvement--phase-2a2b-for-full-equivocation-recovery)). Defers σ-commitment until after a Phase-2a observation phase that lets the cluster converge on a stabilized validity verdict before any operator binds. This is the structural fix at the TBFT family level. Costs +1 RTT per round.
- **Use a deterministic / finalized parent.** Validity criterion that doesn't depend on each operator's chain view at evaluation time — e.g., parent must be a finalized block (2 epochs old, all operators agree). Eliminates divergence by construction but loses late-MEV (you can only build on finalized parents).
- **QBFT.** Round-changes through with a new leader fetching at the moved head — covers validity-divergence as a side-effect of round-change recovery. Comes with QBFT's own ~2s round-change latency.

These three are the structural options. Smaller mitigations (relax cross-phase exclusivity, allow NV → σ flip via aggregator filtering, etc.) all break safety against an offline-aggregating byzantine — see the Phase 2a/2b discussion at [Path forward](#future-improvement--phase-2a2b-for-full-equivocation-recovery) for why.

### Implications of equivocation not being recovered

OBFTR does not provide an in-protocol equivocation recovery mechanism. Outcomes split into three classes:

- **σ-quorum reaches at L_0 naturally** (slot succeeds): honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool to qV.
- **NR-quorum reaches at L_0 → fall-through to L_1** (slot succeeds at L_1 if L_1 honest): all 3 honest land in Defer-due-to-equivocation (typical when byz delivers V's early enough for gossipsub re-flood to spread conflicts before Phase 2 σ-emit). Round-R force-NR produces qEnc-quorum, decryption unlocks L_1, σ-quorum at L_1 reaches.
- **σ-locked split patterns** (slot misses): honest σ-states split into mixed σ-locked + Defer (1-1-1 split, 1-1-Defer-C, 1-Defer-Defer at f=1 n=4). σ-pools split below qV; NR-pool capped by σ-locked operators below qEnc; no fall-through.

A byzantine controlling delivery timing picks the class. Delivering near end-of-Phase-1 (insufficient re-flood time) reliably engineers the σ-locked split slot-miss outcome. Equivocation evidence is slashable in all cases; the rational-byzantine deterrent (assumption 4) is what makes the slot-miss tolerable across many slots.

The rational-byzantine deterrent (assumption 4) is what makes this a tolerable failure mode in expectation: a byzantine that equivocation-griefs in slot N pays for it from slot N+1 onward via the eventual `Byzantine ≡ Down` collapse (manual blacklist by surviving operators; planned protocol extension) plus staker migration collapsing cluster-wide fee accrual; equivocation-evidence bundles additionally enable stake slashing via the SSV contract. Phase 2a/2b (future improvement) closes the recovery gap without relying on the deterrent.

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

**The bottom line: the attacks that matter most for adversarial-byz liveness are precisely the ones the deterrent can least credibly punish on a per-slot basis.** This is not a bug in the deterrent — it follows from the structure of which fault classes leave on-wire cryptographic evidence vs which leave only behavioral patterns. It IS, however, the load-bearing reason why bare OBFTR (at any R, since these failures are R-invariant) is exposed to adversarial byz beyond what assumption 4's expected-value framing captures cleanly until both the blacklist extension lands AND coordination latency is short enough to materially bound the grief window. **Phase 2a/2b is the structural fix** that closes the high-grief-severity / low-evidence-quality faults at the protocol level, restoring the `Byzantine ≡ Down` equivalence in-protocol (no blacklist needed for these faults), leaving the deterrent to handle only the high-evidence-quality (already-recoverable, fast-to-blacklist) faults.

## Protocol

OBFTR runs in up to **R rounds**. Round 1 is the standard 2-RTT TBFT-equivalent path; rounds 2..R are recovery rounds that fire on round-end timeout when the prior round's reconstruction failed at all layers. Each round has Phase 1 → Phase 2 → Phase 2.5 (L_C signaling) → Phase 3 (reconstruction). Round 1's Phase 1 is a fresh broadcast; subsequent rounds re-flood retained Phase-1 bundles plus per-round L_C claims.

### Phase 1 — Candidate broadcast

Phase 1 in round 1 has K per-layer windows (driven by the asymmetric fetch times): `[T_{K-1}, T_{K-1} + Δ_1]` for the deepest backup, then progressively `[T_{K-2}, T_{K-2} + Δ_1]`, ..., ending at `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "OBFTR-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates OBFTR Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, baseline TBFT, other OBFTR message kinds, etc.). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

In rounds 2..R, no fresh Phase-1 broadcast happens — operators **re-flood** all retained Phase-1 bundles (from any earlier round) at round start. A bundle that was auth-only-retained in round `r` (because first-observed past `T_accept_max_r`) may be **first-accepted** in round `r+1` if its first-observation time is at or before `T_accept_max_{r+1}` (the per-round acceptance horizons widen forward; see "Retention bounds" below).

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, and check the first-observation timestamp against the current round's `T_accept_max_r = T_commit_r + Δ_2 − (D + δ)`. Accept the bundle for round `r` if first-observed in `[slot_start, T_accept_max_r]`; auth-only-retain for round `r+1` if later but at or before `T_accept_max_{r+1}`; reject entirely if past `T_accept_max_R` (final round). Bundles failing cryptographic-auth are silently dropped. (A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.)
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. A `not-valid` verdict turns the operator's commitment into NV (operationally identical to NR — see "Operator commitments — σ, NR, NV, Defer" below).

If a leader `L_k` fails to broadcast across all R rounds, that layer is unavailable; the cluster falls through to deeper layers. If all K leaders fail, the slot is missed.

**Within-round late-bundle behavior.** A bundle whose first-observation time is in `(T_commit_r − (D+δ), T_accept_max_r]` — i.e., received during round r's Phase 2 (rather than before `T_commit_r`) — is still accepted *for round r*. The receiver runs validation as above; if it passes and the operator was in Defer state for that layer, the operator transitions to σ-state and emits σ within round r's Phase 2 (see §Phase 2 sub-phasing). This is what makes the "operators who become σ-eligible during the window" path work — within-round late re-flood is absorbed by the receiver acceptance window extending into Phase 2.

**Bundle propagation.** Honest receivers re-flood every bundle via standard gossipsub on first observation. Subsequent rounds re-flood again at round start to maximize cluster-wide reception under sub-partial-synchrony.

**Retention bounds.** Retention state for accepted Phase-1 bundles is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **two distinct `(V, σ_{L_k}^V, σ_{L_k}^{op})` tuples** — the first auth-valid bundle observed, plus, if a second auth-valid bundle with a *distinct* `value_root` is later observed, that one (sufficient for both Phase-2 σ-signing on the chosen V *and* leader-equivocation evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Bundles whose cryptographic-auth checks fail are dropped without retention. Bundles first-observed past the **final round's acceptance horizon** `T_accept_max_R` are rejected entirely. Retention lifetime: until the operator's local end of round `R`'s Phase 3 (i.e., until reconstruction halts or the slot is declared missed); slot state is then cleared. This caps memory at `O(K · n)` bundles per slot in the worst case (every leader equivocates).

**Per-round acceptance widening.** OBFTR's per-round acceptance horizons are nested: `T_accept_max_1 ≤ T_accept_max_2 ≤ ... ≤ T_accept_max_R`. A bundle first-observed at time `t` is accepted in the smallest round `r` such that `t ≤ T_accept_max_r`. Bundles auth-valid but received past round `r`'s horizon are retained in **auth-only** state until they pass round `r' > r`'s horizon; once accepted in round `r'`, they may be σ-signed via Phase 2 in round `r'`. This is OBFTR's structural mechanism for partition recovery: bundles late by one round get a fresh chance in the next.

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with honest threshold partials produced in Phase 2 by operators who received the leader's bundle (or recovered it via gossipsub re-flooding across rounds), the cluster reaches `qV` real partials on `V_{L_k}` — closing the byzantine-leader selective-delivery grief under partial synchrony (see "Fault tolerance / Liveness").

**Equivocation handling — detect and slash.** If a participant observes two distinct `σ_V` partials from the same leader at the same slot/layer, that's leader equivocation. The pair of signed bundles is self-contained slashable evidence, gossipped for out-of-band slashing.

Local protocol response, by current commitment state at observation time:

- **Already σ-emitted** (Phase-1 leader's σ_V on the wire, or Phase-2 σ already gossiped): stay σ-locked. Cross-phase + cross-round exclusivity prevents retroactive withdrawal.
- **σ-eligible but not yet σ-emitted** (acceptance succeeded earlier in this round, Phase 2 onion not yet constructed): transition to Defer-due-to-equivocation. The σ-emit precondition ("no equivocation observed") now fails.
- **In Defer-due-to-partition** (no V retained yet): transition to Defer-due-to-equivocation upon retaining ≥ 2 distinct V's (e.g., re-flood delivers multiple V's at once). Recovery via "re-flood eventually delivers V, σ-emit on V" is foreclosed.

In all Defer-due-to-equivocation cases: unrecoverable within the slot; force-NR at round R per the final-round rule. The protocol does not attempt to "pick a winner" cluster-wide — under f=1 byzantine and OBFTR's retention bound (2 distinct V's per `(slot, layer, leader_id)`), no rule based on per-operator local state can guarantee cluster-wide convergence on a single V (different operators may retain different V-pairs under adversarial gossipsub-ordered delivery; see [Implications of equivocation not being recovered](#implications-of-equivocation-not-being-recovered)).

The leader is required to sign `σ_V` exactly once per slot/layer (refreshes in the host's fetch loop are pre-signing only — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance); any second `σ_V` from the same leader is a protocol violation regardless of intent.

**Equivocation is permitted as a slashable byzantine fault.** OBFTR does not provide an in-protocol equivocation recovery mechanism. Some equivocation patterns naturally reach σ-quorum on one V (e.g., 2-of-3 honest σ-commit on the same V plus the leader's σ_L^V on that V = 3 = qV at f=1 n=4) — the slot succeeds in those cases as a side-effect, not via a specific protocol mechanism. Other patterns (e.g., 1-1-1 split where each honest σ-commits on a different V before observing equivocation, or asymmetric-retention patterns where Defer-state honest see different V-pairs under byz-controlled delivery order) do not reach σ-quorum and the slot misses.

**Practically, an adversarial byzantine controls which pattern occurs.** A byzantine that times equivocation deliveries near the end of Phase 1 (leaving insufficient time for cross-honest gossipsub re-flood to spread the conflict before Phase 2 σ-emit) reliably engineers the σ-locked split patterns (1-1-1, 1-1-Defer, 1-Defer-Defer) that don't reach qV. The natural-recovery cases (2-1 split where 2 honest happen to σ-commit on the same V; all-Defer fall-through to L_1) only fire when the byzantine fumbles the timing — delivers V's early enough that re-flood converges honest views before σ-emit. **In expectation, byzantine-leader equivocation slot-misses; the rational-byzantine deterrent (assumption 4) is the practical defense, not natural recovery.** A byzantine indifferent to the deterrent (e.g., last-slot-before-exit) can grief reliably; a byzantine that values future participation pays once and stops.

In all cases, the byzantine leader pays the stake-based slashing penalty — equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained on each pair of conflicting bundles.

The TBFTR-style **Phase 2a/2b split** (broadcast-only Phase-2a, then σ-emit on a deterministically-chosen V in Phase-2b after Phase-2a observation completes) is the structurally correct fix for full equivocation recovery and preserves cryptographic safety AND f-tolerant liveness; documented as a future improvement, not in current OBFTR.

**Operator commitments — σ, NR, NV, Defer.** OBFTR extends TBFT's three-state commitment model with a fourth state, **Defer**, which is what enables multi-round recovery without breaking cross-phase exclusivity. For each layer, an operator's commitment falls into one of four buckets:

- **σ (sign-on-V)**: the operator received the leader's bundle on time, both protocol-level and application-level checks passed, and the operator has not observed equivocation evidence at this layer through σ-emit time (acceptance-time eligibility is checked, then re-checked at Phase 2 onion construction; if equivocation is observed in between, the operator transitions to Defer-due-to-equivocation rather than σ-emitting). Materializes as a σ partial in the Phase-2 onion (or as the leader's Phase-1 σ for the layer's own leader). Once σ-emitted, the operator is **σ-locked** at this layer for the entire slot (cross-phase + cross-round exclusivity).
- **NR (non-receipt, evidence-driven)**: the operator has positive evidence that this layer cannot validate locally:
  - **NR-silent**: cutoff for the current round passed AND no peer σ-emit on this layer is observed cluster-wide (the leader is presumed silent). Emittable in any round once the silent-leader condition is met.
  - **NV (non-validity)**: host application returned `not valid` for V_{L_k}.
- **Defer (uncommitted)**: cutoff for the current round passed, peer σ-emit on this layer is observed cluster-wide (so the leader is *not* silent — some V exists somewhere in the cluster), but the operator is not σ-eligible. Two sub-cases:
  - **Defer-due-to-partition**: the operator does not yet have an auth-valid Phase-1 bundle that the host validates. Recoverable within the slot if re-flood delivers such a bundle before the next round's cutoff (operator transitions to σ-state).
  - **Defer-due-to-equivocation**: the operator has retained ≥ 2 distinct auth-valid Phase-1 bundles at this layer (leader equivocation evidence). Unrecoverable within the slot — re-flood only delivers more bundles, not fewer, so the σ-eligibility precondition ("no equivocation observed") cannot be re-established. The operator force-NRs at round R per the final-round rule. See "Equivocation handling — detect and slash".

  Defer is **not visible on the wire** (no message is broadcast for Defer state — it's pure local state). At round R (final round), all Defer operators force-transition: σ if Defer-due-to-partition resolved (V received via re-flood, host validates, no equivocation observed), else NR (silent-leader rule, applies to both unresolved Defer-due-to-partition and all Defer-due-to-equivocation operators).

**NR and NV are operationally interchangeable for the protocol.** Both materialize on the wire as a single message kind: a partial `σ_i^{IBE}(nr_tag_k)` from the IBE keypair on the layer's NR tag. The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to throughout as "NR-quorum" or "no-σ quorum" for short). The distinction between NR-silent and NV is **local-only diagnostic** (an operator may log *why* it didn't sign σ — for telemetry — but the cluster-wide message and counting logic are identical). All references to "NR" in the rest of this document encompass NR-silent + NV unless stated otherwise.

**Defer is the key OBFTR addition.** It lets the cluster distinguish "the leader is silent (commit NR fast, fall through)" from "I just haven't received V yet (wait, V might still arrive)". The discriminator is **observed peer σ-emit cluster-wide** (visible at layer 0 as plaintext σ partials, visible at deeper layers via the same encrypted-partial broadcast presence — see "Peer σ-emit observability" below). If any honest peer's σ-emit is observed on this layer, the cluster knows V exists, so an operator without V locally should defer rather than NR-emit. This rule preserves baseline TBFT's fast L_0-silent fall-through (no peer σ-emit ⇒ NR-emit immediately) while enabling partition recovery (peer σ-emit observed ⇒ Defer until re-flood completes in next round). At L_0 specifically, the σ-emit observability check is conditional on whether the receiver has a retained Phase-1 bundle — see "Validity-gate on observed σ-emit (L_0 specifically)" below.

**Validity-gate on observed σ-emit (L_0 specifically).** At L_0, σ partials are plaintext. The Defer rule at L_0 applies the validity check **conditionally on whether the receiver has a retained Phase-1 bundle** — that is the only state that lets the receiver actually verify the partial against a known V.

- **Receiver has ≥ 1 retained V at L_0.** Verify the σ partial against each retained V. Only partials that verify count as "peer σ-emit observed". An auth-valid `KindOnion` carrying a plaintext σ partial that does not verify against any retained V is treated as if no σ-emit happened — it does NOT push the operator into Defer. This closes the byzantine-garbage-σ grief: byz cannot force honest with retained V into Defer by emitting auth-signed-but-unverifiable partials.
- **Receiver has 0 retained V at L_0** (the partition case — the Phase-1 bundle hasn't propagated to this receiver yet). The receiver has no V to verify against and cannot distinguish byzantine garbage from a legitimate σ on a V they haven't received. Fall back to the deeper-layer rule: an auth-signed `KindOnion` claiming σ at L_0 counts as σ-emit observed, regardless of partial validity. This preserves Defer-due-to-partition recovery: a receiver who hasn't yet received V but observes peer σ-claims defers rather than NR-emitting, hoping re-flood delivers V before the next round's cutoff. Byzantine garbage in this case costs one round of unnecessary Defer (the operator force-NRs at round R if V never arrives) but does not foreclose recovery.

The asymmetry reflects what's distinguishable. With a retained V, the receiver can tell garbage from legitimate σ-emit; without one, they can't, and the encrypted-presence-equivalent fallback preserves the partition-recovery scenarios described in the Liveness section (aggressive-marginal recovery, adversarial scheduling against ≤ 2 honest) at the cost of a bounded grief surface in the no-V case.

**The no-V fallback is the structural enabler of the h_V=1 selective-delivery deadlock** (see [§Failure modes / Byzantine selective-delivery grief at the final round](#failure-modes)). The fallback rule says: a no-V receiver that observes any auth-signed `KindOnion` claiming σ at L_0 → Defer (preserving partition recovery). A byzantine L_0 leader can exploit this by emitting an auth-signed `KindOnion` with a fake plaintext σ at L_0 *without ever broadcasting Phase-1*. All no-V honest receivers (which is everyone, since byz withholds Phase-1) see byz's σ-claim, fall into Defer through rounds 1..R-1, and force-NR at round R only after byz selectively delivers V to exactly one honest. Combined with byz's selective late delivery, this engineers the h_V=1 split. **Without the no-V fallback, no-V receivers would silent-leader-NR fast → NR-quorum at L_0 → fall-through to L_1**; the deadlock requires the fallback. Rule 5 (Fake plaintext σ at L_0) is the post-hoc attribution mechanism for byz's fake σ in this scenario, but it doesn't prevent the within-slot grief — see [§Slashing evidence / Rule 5](#slashing-evidence). The structural fix that closes this without the fallback's grief surface is Phase 2a/2b (defers σ-commitment until after a cluster-wide σ-eligibility observation phase, so byz cannot manipulate h_V to 1 by pre-broadcasting fake σ).

At deeper layers (k > 0), the σ partial is encrypted and validity isn't checkable until decryption — encrypted-presence alone is sufficient for Defer (with the fake-encrypted-presence slashing rule as the post-decryption attribution backstop; see "Slashing evidence").

**Peer σ-emit observability at deeper layers.** At layer `k > 0`, σ partials are chained-encrypted (see Phase 2). Receivers can observe *that* a peer broadcast a layer-`k` onion entry (the encrypted ciphertext is on the wire, in an auth-valid `KindOnion` from the peer's operator-identity key — see Phase 2) without decrypting it — that's sufficient for the Defer rule. The Defer rule asks "does any peer auth-claim to have σ at this layer?", which the auth-signed encrypted-presence check answers. It doesn't require knowing *which* V the peer σ'd on (that knowledge would require decryption, which requires NR-quorum at prior layers). At decryption time (when prior layers' NR-quorums have unlocked the chained encryption), all encrypted partials become plaintext and Pigeonhole 2 applies normally.

**Garbage-encryption deterrence.** A byzantine operator could broadcast a well-formed-but-undecryptable ciphertext at layer `k` (encrypted under wrong tag, or arbitrary garbage bytes wrapped in a structurally-valid IBE ciphertext envelope) to fake encrypted-presence and DoS honest into Defer with no real σ-emit ever materializing. To deter this: at decryption time (when prior layers' NR-quorums unlock decryption), if a peer's auth-signed `KindOnion` decrypts at layer `k` to garbage (not a valid σ partial on any leader-known V at L_k, or fails to decrypt entirely), the auth envelope is self-contained slashable evidence of a "fake encrypted-presence" byzantine fault — `i` signed the envelope binding their identity to the encrypted payload; post-decryption verification surfaces the garbage. Detection is delayed (requires NR-quorum at layers `0..k-1` to unlock decryption) but attribution is unambiguous. The rational-byzantine deterrent (see [Assumptions](#assumed)) makes the DoS attack expensive — repeated grief gets the byzantine blacklisted from the cluster (planned protocol extension restoring `Byzantine ≡ Down`) and triggers staker migration that collapses cluster-wide fee accrual. See "Slashing evidence" for the corresponding case.

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

**Phase 2 sub-phasing.** Within the Phase 2 window `[T_commit_r, T_commit_r + Δ_2]`, σ-emit and NR/Defer commitment happen at different times — not simultaneously at T_commit_r:

- **σ-emit (continuous during window)**: operators in σ-state at T_commit_r emit their σ partials (Phase-2 onion) immediately at T_commit_r. Operators who become σ-eligible *during* the window (because late gossipsub re-flood delivers V within the receiver acceptance window — see §Phase 1 / Within-round late-bundle behavior) emit σ as soon as eligibility is determined.
- **NR/Defer commitment (end of window)**: operators in NR/Defer-eligible state delay their commitment until the end of the window, at `T_commit_r + Δ_2`, and base the decision on σ-emits observed throughout the window. The `Δ_2 ≥ D + δ` budget is what ensures σ-emits at the start of Phase 2 propagate to all honest before the NR/Defer decision fires.

Without this sub-phasing the Defer rule cannot operate correctly — at T_commit_r start, no Phase-2 σ-emits have been observed yet, so receivers without V would always NR-emit (per silent-leader rule), foreclosing Defer-due-to-partition recovery. The implicit timing was load-bearing in earlier drafts; this sub-phasing makes it explicit.

**Late σ-emit limitation and the receiver acceptance horizon (per round).** Two related propagation horizons inside round r's Phase 2:

- **σ-emit propagation horizon** (peer-NR-decision feasibility): a σ-emit at time `T_commit_r + t` propagates to other honest by `T_commit_r + t + (D+δ)`. For other honest to observe before their NR/Defer decision at `T_commit_r + Δ_2`: `t ≤ Δ_2 − (D+δ)`. At `Δ_2 = D+δ` minimum this requires `t = 0` (no late-σ-emit window); at `Δ_2 = 2(D+δ)` recommended this allows late σ-emit up to `t = D+δ`.
- **Receiver acceptance horizon** (bundle-acceptance feasibility): a Phase-1 bundle first-observed at time `t_obs` lets the receiving operator σ-emit at `t_obs` (or shortly after, post-validation). The σ-emit is observable by peers iff `t_obs + (D+δ) ≤ T_commit_r + Δ_2`, i.e., `t_obs ≤ T_accept_max_r = T_commit_r + Δ_2 − (D+δ)`. This is exactly the receiver acceptance horizon for round r from §Phase 1: the cutoff is what makes within-round acceptance worth doing.

The two horizons coincide by construction. **A bundle accepted within round r's window (first-observed in `[slot_start, T_accept_max_r]`) produces a σ-emit observable by peers' round-r NR-decision; a bundle past `T_accept_max_r` cannot, hence is auth-only-retained for round r+1.**

Setting `Δ_2 ≥ 2(D + δ)` widens both horizons (late-σ-emit window = receiver acceptance window past `T_commit_r` = `D+δ`), giving each round meaningful within-round partition recovery on top of cross-round retention. The recommendation costs `D+δ` of additional Phase-2 latency per round at proportional latency cost.

**Optional: defensive widening to `Δ_2 ≥ 2(D + δ) + ε_proc`** if the cluster's per-operator processing variance (BLS partial-sig generation + EKM coordination + host validation, on the σ-emitter side) is non-negligible relative to `D + δ`. The boundary case (concrete example below) shows that at exactly `Δ_2 = 2(D + δ)`, an operator first-observing V at `T_accept_max_r` and σ-emitting after `ε_proc` ms of processing has its σ partial arrive at peers `ε_proc` past their NR-decision — invisible for peer Defer purposes (still counted in Phase-3 reconstruction or in round r+1 if auth-only-retained). Heavy-processing deployments (remote EKM, slow BLS implementation, host validation hitting a beacon-node call) should widen `Δ_2` by their P99 `ε_proc`. This is *additive* to the validity-divergence trade-off discussed at §Head-change handling: wider `Δ_2` gives more processing margin AND more partition absorption AND more validity-divergence exposure.

**Symmetric concern on Δ_3 sizing.** NR partials are emitted at `T_commit_r + Δ_2` (end of Phase 2) and propagate to peers within `D + δ` (covered by Δ_2.5 in OBFTR). The emitter's own NR-side processing (`ε_proc` for IBE partial signing + EKM coordination) happens before the message hits the wire, so it's not separately budgeted in `Δ_2.5 ≥ D + δ` and `Δ_3 ≈ 100ms` — these account for receiver-side propagation and reconstruction processing only. If emitter `ε_proc` is significant, NR partials arrive correspondingly later; deployments should confirm that `Δ_2.5 + Δ_3 ≥ (D + δ) + ε_proc_emitter + ε_3` covers both sides. At Config A with negligible `ε_proc`, the existing `Δ_2.5 = 150ms` + `Δ_3 = 100ms` budget covers normal cases.

**Concrete example.** At Config A (D = 100ms, δ = 50ms, Δ_2 = 300ms): both the σ-emit propagation horizon and the receiver acceptance horizon coincide at `T_accept_max_r = T_commit_r + 150ms`. Suppose operator A first-observes V at exactly `T_commit_r + 150ms` (the boundary), σ-emits with negligible processing delay. A's σ-emit propagates to peers by `T_commit_r + 150 + (D+δ) = T_commit_r + 300ms = T_commit_r + Δ_2 = end of Phase 2 = NR-decision time`. Peers observe A's σ-emit *exactly at* their NR-decision — borderline observable, depending on local clock skew. If A's processing delay (validation, EKM signing) adds `ε_proc` ms, A's σ-emit happens at `T_commit_r + 150 + ε_proc`, propagates to peers by `T_commit_r + 300 + ε_proc` — `ε_proc` past NR-decision, invisible for peer Defer purposes (though still counted in Phase-3 σ-pool reconstruction; bundle is also auth-only-retained for round r+1's window). In practice this means: setting `Δ_2 = 2(D+δ)` exactly leaves zero processing-delay margin for the boundary case; deployments may want `Δ_2 = 2(D+δ) + ε_proc` to absorb realistic per-operator processing variance.

Each operator emits their commitment per layer based on the four-state model from Phase 1:

- **σ-state**: include the (encrypted) σ partial in the onion at this layer.
- **NR-state** (silent-leader, equivocation-as-NR, or NV): emit a partial `σ_i^{IBE}(nr_tag_k)` separately from the onion. These IBE partials are the witnesses that unlock the next layer.
- **Defer-state**: omit the layer from the onion AND do not emit NR. (No wire artifact for Defer state — it's purely local.) Defer is permitted in rounds 1..R-1; at round R, all Defer operators must transition to either σ (if Defer-due-to-partition resolved: V received via re-flood, host validates, no equivocation observed) or NR (final-round timeout, applies to unresolved Defer-due-to-partition and all Defer-due-to-equivocation).

**Wire format: two distinct auth-wrapped message kinds.** The Phase 2 sub-phasing — σ-emit during the window, NR/Defer commitment at end — requires the σ-side and NR-side commitments to be on the wire at different times. Bundling both into a single message would either (a) defeat the σ-emit propagation timing (if held until end of Phase 2), or (b) require multiple emissions of the same message kind with different content (messy). OBFTR splits into two message kinds per round:

- **`KindOnion` (σ-side, emitted during round r's Phase 2 as σ-eligibility is determined per layer).** Carries `i`'s K-layer onion: plaintext σ partial at L_0, chained-encrypted σ partials at deeper layers (per the encryption construction above). Auth-envelope binding: `(protocol_tag = "OBFTR-v1", message_kind = "phase2-onion", cluster_id, slot, round r, operator_id i, onion_payload)` signed by `i`'s operator-identity key. May be emitted multiple times per operator within round r if σ-eligibility transitions late (e.g., late re-flood or auth-only-retained bundle becoming accepted at round r start delivers V to a previously-Defer-state operator at layer k → operator emits a fresh `KindOnion` reflecting the new σ-eligibility). Receivers track per-(operator, layer) σ-presence cumulatively across all auth-valid `KindOnion` emissions from the same operator within a round; the encrypted-presence check (used by the Defer rule at deeper layers) treats any auth-valid `KindOnion` containing a layer-k entry as σ-emit observed at layer k.
- **`KindNR` (NR-side, emitted at end of round r's Phase 2 = `T_commit_r + Δ_2`).** Carries `i`'s NR/NV partials for layers committed to NR at end-of-Phase-2 commitment. Auth-envelope binding: `(protocol_tag = "OBFTR-v1", message_kind = "phase2-nr", cluster_id, slot, round r, operator_id i, nr_partials)` signed by `i`'s operator-identity key. Emitted at most once per operator per round.

Both messages auth-wrapped with the operator-identity key. The auth signature attributes every per-layer commitment to `i`'s identity. Receivers reject any `KindOnion` or `KindNR` whose envelope auth fails verification. This is what makes the encrypted-presence check at deeper layers attributable: a peer cannot anonymously broadcast garbage ciphertext to fake encrypted-presence.

**Why this split is necessary.** Under the sub-phasing semantics, σ-emits must propagate to peers *during* Phase 2 (so peers without V can observe σ-emit cluster-wide and Defer rather than NR-emit). NR partials, by construction, are determined at the *end* of Phase 2. Holding σ partials until end of Phase 2 to bundle with NR partials would break the Defer rule's observability requirement. Splitting into `KindOnion` (σ-emittable as eligibility is determined) + `KindNR` (NR-emittable at end) keeps both timings honest while preserving the auth-attribution that Pigeonholes and the encrypted-presence check rely on. The split applies independently per round at OBFTR(R≥2): each round has its own `KindOnion` and `KindNR` emissions, with cross-round σ-or-NR exclusivity enforced by EKM as before.

**Per-operator commitment is exclusive across phases AND rounds.** OBFTR extends TBFT's cross-phase exclusivity to also span across all R rounds. The commitment is *one decision per operator per layer, spanning Phase 1, Phase 2, and rounds 1..R*:

- An operator who emitted `σ_i^V(V_{L_k})` at layer `k` on any value `V` (in any round) has σ-side committed at this layer; they may **not** subsequently broadcast an NR/NV partial on `nr_tag_k`. They may also **not** σ on a different `V'` at the same `(slot, layer)` — see Pigeonhole 2 in "Fault tolerance / Safety".
- An operator who emitted an NR/NV partial on `nr_tag_k` (in any round) has NR-side committed at this layer; they may **not** subsequently emit σ on any V at L_k.
- The layer-`k` **leader**'s Phase-1 σ_V counts as their σ-side commitment at layer `k`. They may not subsequently emit NR/NV on `nr_tag_k`.
- Across layers, commitments are **independent**: an operator's σ-or-NR commitment at layer `k` does not constrain their commitment at layer `j ≠ k`. Hedging across layers is preserved (an operator may σ at multiple layers if they validated multiple V's).

EKM enforces these exclusivities cryptographically (slashing-protection log keyed on `(slot, layer, commitment-side)`); see "Preconditions on the host application / Slashing-protection scope".

A byzantine operator that publishes both σ and NR on the same `(slot, layer)` is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For each layer's leader specifically, "cross-signing" includes any Phase-1 σ + later NR pair on the same slot; the rule applies uniformly across phases and rounds.

### Phase 2.5 — L_C consensus `[T_commit_r + Δ_2, T_commit_r + Δ_2 + Δ_2.5]` (per round r)

Phase 2.5 is OBFTR's structural addition to TBFT. It runs in parallel with the latter half of Phase 2 (overlapping window — each operator runs Phase 2.5 logic continuously as they observe peer broadcasts). One new wire message kind is emitted here.

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

### Phase 3 — Local decryption and reconstruction `[T_commit_r + Δ_2 + Δ_2.5, T_round_r_end]` (per round r)

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches on some V (output produced), or NR-quorum reaches (advance to next layer), or neither (stay at this layer for next round).

```
L_C = 0                                        # current frontier; advances per loop iteration
sigs[k] for k in [0, K)                        # σ pools, indexed by layer (initially empty)
nrs[k]  for k in [0, K)                        # NR pools, indexed by nr_tag_k (initially empty)

for k in [0, K):
    # Aggregate σ pool at layer k from all rounds' broadcasts so far.
    sigs[k] = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
            ∪ {σ_j^V(V) from received layer-k onion contents on any value V}
              (decrypted via accumulated NR-decryption keys at layers 0..k-1, if k > 0)
            # deduplicated per operator: leader's Phase-1 σ and onion σ from
            # the same operator collapse to one partial.
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

**`T_round_r_end`** for the deadline rule is the cutoff by which the operator must have received all Phase-2 onions and NR partials they intend to count for round `r`. Practically, `T_round_r_end = T_commit_r + Δ_2 + Δ_2.5 + Δ_3` where Δ_3 is the reconstruction window. The deadline rule (caveat 3) bounds the gap between phases against propagation P99/P999 and clock skew.

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at that layer: no σ partial, no NR partial. Standard threshold cryptography — only signed messages count. Re-flooding across rounds maximizes the chance that all honest broadcasts eventually reach all honest receivers within the partial-synchrony envelope.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within `T_round_R_end` (see "Fault tolerance / Liveness"). If more than `f` operators are offline or byzantine combined, neither σ nor NR quorums reach their thresholds and the slot is missed. Within the f-bound, OBFTR recovers from network partitions (with bounded re-flood delay) given enough rounds within the slot's total budget. View-divergence cases — equivocation and host-validity divergence — are explicitly out of recovery scope (some splits succeed naturally as a side-effect of leader-σ-V counting; rest slot-miss). See [Assumptions and implications](#assumptions-and-implications).

### Round structure

OBFTR runs up to **R rounds** with timeout **RT** per round. Round `r ∈ {1, ..., R}` proceeds as follows:

1. **Round start**: at time `T_round_r_start`, the round begins. Round 1 starts at `slot_start + T_pre` (after host-application pre-fetch); round `r > 1` starts at `T_round_{r-1}_end` (round-end timer expiry) OR upon observing cluster-promoted L_C consensus (`KindLCClaim`-quorum) — whichever happens first.
2. **Phase 1 (round 1 only)**: K leaders broadcast their Phase-1 bundles per their per-layer fetch windows (`[T_{K-1}, T_{K-1} + Δ_1]`, ..., `[T_0, T_0 + Δ_1]`). Round `r > 1` skips fresh Phase 1 — operators re-flood retained Phase-1 bundles instead.
3. **Phase 2** `[T_commit_r, T_commit_r + Δ_2]`: each operator emits their K-layer onion and any NR partials based on their current per-layer commitment state. Operators in **σ-state** emit σ; operators in **NR-state** emit NR; operators in **Defer-state** emit nothing for that layer. Operators who newly received V via re-flood since last round σ-emit on V (transitioning from Defer to σ), provided they have not observed equivocation evidence at this layer. Operators who observe equivocation evidence at a layer where they're still in Defer stay in Defer (no protocol-level winner-completion); they may force-NR at round R per the final-round rule. See "Phase 1 / Equivocation handling — detect and slash".
4. **Phase 2.5** `[T_commit_r + Δ_2, T_commit_r + Δ_2 + Δ_2.5]`: operators emit `KindLCClaim` reporting their local `L_C` view at end of round `r`. The qV-quorum on a single `observed_L_C` value, if reached, accelerates round-`r+1` start.
5. **Phase 3** `[T_commit_r + Δ_2 + Δ_2.5, T_round_r_end]`: each operator runs the K-layer reconstruction walk. If σ-quorum reaches on some V at any layer, output the V; halt. If NR-quorum reached up to some layer `L_C < K`, advance L_C and continue the walk. If neither σ-quorum nor NR-quorum advance unlock at any layer, end of round `r`.

**Round transitions**:
- If round `r < R` ends with no output, round `r+1` fires.
- If round `r == R` ends with no output, slot misses.

**Final-round force-commit (round R)**:
- Operators in Defer state at round `R`'s Phase 2 are forced to commit: σ-emit if Defer-due-to-partition has resolved (V received locally via re-flood, host validates, no equivocation observed); else NR-emit (per the silent-leader rule, applies to unresolved Defer-due-to-partition and all Defer-due-to-equivocation operators, since round R's cutoff is the slot's hard deadline). This guarantees all honest at every layer have transitioned out of Defer by round R's Phase 2 — making NR-quorum reachable cluster-wide if the leader was genuinely silent or equivocated.
- Final-round NR-emit by an operator may flip the cluster's outcome at that layer from σ-side to NR-side fall-through. This is acceptable: by round R, the protocol is converging on a final answer, and the trade-off is "fall-through to a deeper layer in round R" vs "miss the slot entirely".

**Round timing**: `RT = Δ_1 + Δ_2 + Δ_2.5 + Δ_3` for round 1 (full Phase 1 → 2 → 2.5 → 3); `RT = Δ_reflood + Δ_2 + Δ_2.5 + Δ_3` for rounds 2..R, where `Δ_reflood ≈ D + δ` is the re-flood window. The slot's total budget is `R · RT` (approximately; round 1 is longer due to fresh Phase 1).

## Preconditions on the host application

OBFTR is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at the moment the operator considers committing σ to it. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be signed (the σ_V locks it). See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` before committing σ. A `not-valid` verdict turns the operator's commitment into NV (see "Phase 1 / Operator commitments — σ, NR, NV, Defer").

The protocol-level checks (cryptographic auth, envelope re-derivation, per-round timing cutoff `T_accept_max_r`) are protocol-internal and do not depend on the application.

For SSV's Ethereum proposer duty, application-level checks include slot match, proposer index match, fork/domain match, parent root match against the operator's view of the head, relay metadata validity, doppelganger / slashing-protection checks, and block encoding well-formedness. See the Application section for details. The protocol body does not enumerate or interpret these — they're host concerns.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot (potentially up to K, one per layer they validated). Constraints per (slot, layer):

- **Each layer's leader signs σ_V exactly once per (slot, layer)** — on the final V they commit to, after any pre-signing refreshes during the host's fetch loop. Refreshes update V via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this single-σ-V-per-(slot, layer) constraint cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational workflow.
- **Each operator commits σ-or-NR exclusively per (slot, layer), across phases AND across rounds.** Honest who include σ on any V at layer `k` (in any round) may not subsequently broadcast NR/NV on `nr_tag_k` AND may not σ on a different V' at the same layer (single-σ-V per operator per layer); honest who broadcast NR/NV may not subsequently include σ at L_k. Each layer's leader's Phase-1 σ counts as their σ-side commitment — they cannot emit NR/NV for that layer. EKM enforces this cross-phase + cross-round + single-V exclusivity by coordinating across the operator's V-signing and IBE-signing shares (distinct keys, but slashing-protection log keys on (slot, layer)): an NR/NV partial on `nr_tag_k` is rejected if the same operator has previously signed σ at L_k, and vice versa; a σ partial on V' at L_k is rejected if the same operator has previously signed σ on V ≠ V' at L_k. Pigeonhole 1 and 2 below rely on these rules.
- Every operator signs each layer's V_{L_k} they consider valid (host returns `valid`) in their Phase-2 onion, provided no equivocation evidence is observed at that layer. The Defer state (no σ, no NR) means the operator hasn't decided yet — Defer-due-to-partition operators may still σ-emit in a later round if V is received via re-flood and the host validates it (no equivocation observed); Defer-due-to-equivocation operators stay in Defer through round R and force-NR. See "Phase 1 / Equivocation handling — detect and slash".

EKM/slashing-protection must permit the operator's per-layer per-round Phase-2 σ signings (one σ per layer per slot, but possibly across multiple rounds — round 1's σ partial is the same partial re-emittable in later rounds) plus the leader's Phase-1 σ_V (exactly one per slot/layer the operator is leading), without flagging duplicates — the cluster's safety property collapses these to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating points are **candidate signing** (Phase-1 leader and Phase-2 onion alike) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`).

**Cross-round σ partial dedup.** When an operator σ-emits in round 1 and the slot rolls to round 2, the operator's σ partial is re-flooded but not re-signed — the same partial is reused. Phase 3's reconstruction walk deduplicates per-operator: `σ_i^V(V_{L_k})` from any round counts as `1` partial in the σ-pool, regardless of how many rounds the partial appears in.

### EKM coordination model

The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log. Production EKMs (Web3Signer, Dirk, etc.) typically expose per-key signing checks; OBFTR requires a coordination layer above them.

**Log schema.** One row per signing event: `(slot, layer, side, value_root)` where `side ∈ {"σ", "NR"}`; `value_root` is set on σ-side entries, null on NR-side.

**Per-request checks.**

- **Sign σ on V at (slot, layer)** (V-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "σ", value_root(V))`. The single-σ-V rule means a second signing attempt with `V' ≠ V` at the same `(slot, layer)` is rejected even though the side matches — the existing row already contains `value_root(V)`.
- **Sign NR on `nr_tag_k` at (slot, layer)** (IBE-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "NR", null)`.

The same operator's σ partial is re-emittable across rounds without a new signing event — the log row already exists, the cached partial is re-broadcast. This satisfies cross-round exclusivity by construction.

**Implementation paths.** (a) A custom EKM that maintains the unified log natively across both keypair shares. (b) A coordinator component in the SSV operator process that wraps two standard EKMs (one per keypair share) and enforces the cross-keypair rules in the operator's slashing-protection database — a single transactional log, two keypair-specific signing-request handlers reading and writing it. The coordinator is novel work relative to standard per-key slashing-protection deployments (e.g., Web3Signer style): it requires (i) the unified log to be **atomic across both shares' signing operations** (sign-and-log must be all-or-nothing; if V-share is in remote signer and IBE-share is local, this is a 2PC-flavored problem), (ii) the cached σ partial to be **persistent across operator restarts** (cross-round re-emission depends on the cache surviving), (iii) **deterministic re-signing as a fallback** if the cache is lost (BLS partial sigs are deterministic; the EKM must allow re-signing if the log row matches the same `(slot, layer, side, value_root)` rather than rejecting as duplicate). None of these are insurmountable, but the coordinator is OBFTR-specific engineering work — not a drop-in over an existing per-key slashing-protection database. Path (b) is the path SSV will most likely take.

**EKM correctness is defense-in-depth for safety, not the sole enforcement.** OBFTR's safety (Pigeonholes 1 and 2) holds when honest operators correctly implement the protocol rules — single-σ-V per (slot, layer) per operator, σ-XOR-NR per layer. The **protocol layer** (operator software implementing the OBFTR state machine) is the primary enforcement point: it determines when σ vs NR vs Defer is requested from the EKM in the first place. The **EKM** is a catch-net: it rejects signing requests that violate the slashing-protection invariants, providing defense-in-depth even if the protocol layer is buggy.

For a single honest operator to produce safety-violating behavior (e.g., σ on V and σ on V' at the same `(slot, layer)`), **both layers must be buggy in compounding ways**: the protocol layer must request the second σ (violation of σ-eligibility logic) AND the EKM must fail to reject it (violation of slashing-protection lookup, atomicity, or persistence). A single-layer bug typically does not break safety:

- Protocol-layer bug only: the EKM rejects the bad request; no double-sign emitted on the wire.
- EKM-layer bug only: the protocol layer doesn't ask for double-signing, so the EKM bug is never exercised.

Cluster-wide safety violation (Pigeonhole 2 producing two qV-quorums on different V's) requires aggregating these single-operator violations to reach `2 · qV = 4f+2` partials across two V's. At `f = 1, n = 4`, one byzantine operator contributes ≤ 1 partial per V (≤ 2 total); three correct honest contribute exactly 3 partials total (single-σ-V each); sum 5 < 6 = 2 · qV. The minimum safety-violating configuration is therefore **one byzantine operator plus one honest operator with compounding protocol+EKM bugs** — together producing the missing partial. This is two misbehaving operators total, exceeding the `f = 1` trust budget. Single-layer bugs alone are tolerated; safety requires both layers to be correct on at least `n − f = 3` operators.

**Trust posture is the same as QBFT.** Both protocols rely on honest-majority correct implementation of the protocol logic *plus* correct slashing-protection — neither is "100% cryptographic" against operator-side software bugs (see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic)). The difference is in the slashing-protection layer's maturity: QBFT's per-key slashing-protection (Web3Signer, EIP-3076 interchange format) has decade-of-production hardening; the OBFTR coordinator is novel, so reaching comparable defense-in-depth robustness requires deliberate engineering investment in (a) test coverage on atomicity and crash-recovery paths, (b) fault-injection testing of the operator-restart scenario, (c) optionally operational margin via larger `n` (e.g., `n ≥ 5` keeps `f = 1` while expanding the bug-budget headroom). The investment is in raising the bar on the catch-net, not in patching a single point of safety failure.

**Summary of EKM failure modes.** A **maliciously compromised** EKM (signs requests outside protocol rules, or generates signatures the protocol layer didn't request) is byzantine-equivalent and directly consumes f-budget. A **passively buggy** EKM (fails to reject bad requests but doesn't generate signatures on its own) requires the protocol layer to also have a compounding bug for safety-violating behavior to actually occur — see the defense-in-depth analysis above. In both cases, the cluster's overall trust posture follows the standard "honest-majority cryptographic" framing — see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic).

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model. Operational rules that handle specific faults (Defer-state deferral, equivocation detect-and-slash, cross-signing detection, head-change refresh) are described inline in the relevant Phase sections; the analyses of *what those rules buy* and *under what conditions* live here.

### Trust model

- **Byzantine bound `f`** with cluster size `n = 3f+1` (the BFT-tight setting; see [§Assumed / Standard BFT trust bound at the tight setting](#assumed)): up to `f` operators may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, etc.). Exactly `2f+1` honest. Running example: `n = 4, f = 1` (3 honest guaranteed).
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `D` (propagation P99/P999) and clock skew `δ`. Two distinct per-round cutoffs operationalize this: `T_broadcast_max_r = T_commit_r − 2(D + δ)` (leader broadcast deadline, round 1 only) and `T_accept_max_r = T_commit_r + Δ_2 − (D + δ)` (per-round receiver acceptance horizon). Phase 3's reconstruction deadline is `T_round_r_end = T_commit_r + Δ_2 + Δ_2.5 + Δ_3`. Safety holds against arbitrary network adversaries; only liveness depends on synchrony.

  **OBFTR's R-round absorption window** = `T_accept_max_R − T_broadcast_max_1`:
  - Per-round absorption (single round, ignoring cross-round retention): `Δ_2 + (D + δ)`. Concrete: ~`3(D + δ)` ≈ 450ms at Config A with recommended `Δ_2 = 2(D + δ)`.
  - Cross-round retention extends absorption by `(R − 1) · (Δ_2 + Δ_2.5 + Δ_3 + Δ_reflood)` (each additional round adds its own per-round-window worth of inter-round time before its acceptance horizon fires).
  - At R=2 Config A with recommended Δ_2: total absorption ≈ `7(D + δ) + Δ_3` ≈ 1150ms — roughly 2.5× a single round's window.

  **The R=2 figure assumes matched per-round Δ_2 sizing.** Deployments could choose narrower per-round Δ_2 at R=2 (e.g., `Δ_2 = D + δ` minimum per round, since round 2's auth-only-retention provides late-delivery recovery without needing within-round late-σ-emit slack). At minimum per-round Δ_2: each round absorbs `2(D + δ)`, total `~5(D + δ) + Δ_3` ≈ 850ms vs OBFT's `2(D + δ)` ≈ 300ms — ratio shrinks. The 2.5× figure is the upper bound under matched-Δ_2 sizing; the 1.7× figure (`5(D+δ) / 3(D+δ)`) approximates the lower bound when both protocols size Δ_2 differently per their priorities. Per-round Δ_2 sizing is itself a deployment trade-off (narrower = tighter latency + less validity-divergence exposure; wider = more partition absorption + more processing margin).

  The "absorption window" framing is more precise than the simpler "R · D" intuition: each round contributes both its own `Δ_2 + (D + δ)` of within-round absorption AND the inter-round time before the next round's acceptance horizon. R-round retry's recovery scope grows correspondingly faster than `R × D`.

### Safety (cryptographic, honest-majority)

**Claim:** at most one full `V` signature is ever produced per OBFTR instance per slot — across any layer, on any value, across any combination of σ sources, across any round 1..R — cluster-wide, against an offline-aggregating byzantine within the f-bound, regardless of which honest aggregation rules are followed. (See [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic) for what this scope does and doesn't cover.)

The proof rests on three pigeonhole arguments — the same as baseline TBFT (Pigeonholes 1, 2) extended to chained encryption at `K > 2` (Pigeonhole 3, per [TBFT.md](TBFT.md) Appendix C). All three are properties of cluster-wide signed messages, enforced cryptographically by [EKM coordination model](#ekm-coordination-model) rules at the operator level — no honest-aggregation rule required.

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

**Cryptographic primitive — chained IBE.** Layer-`k` σ partials are encrypted under `nr_tag_0 ∧ nr_tag_1 ∧ ... ∧ nr_tag_{k-1}`. Decryption requires NR-quorum at each prior layer. Implementation: each level of the chain is a standard threshold-IBE ciphertext (drand/tlock) using `nr_tag_j` as the tag. At K=2 the chain has only one level (single tag `nr_tag_0`); at K=3 there are two levels nested; etc. Same construction as [TBFT.md](TBFT.md) Appendix C.

The arguments above apply symmetrically to all K layers. **None of the proofs depends on honest operators excluding cross-signers from their aggregation** — they're properties of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule. Cross-phase exclusivity (σ XOR NR per layer) and single-σ-V (one V per operator per layer) are enforced cryptographically by EKM at signing time, not by aggregator-side filtering.

### Liveness (synchrony-conditional)

OBFTR's liveness is **partial-synchrony-conditional within `T_round_R_end`** — the protocol's total slot budget. The R-round structure absorbs network-induced failures (re-flood completing across rounds). View-divergence (equivocation, validity-divergence) is NOT recovered by the protocol; some splits succeed naturally as a side-effect of leader-σ-V counting toward whichever V reaches qV first, others slot-miss. Equivocation cases are slashable (leader pays the stake-based penalty); validity-divergence is not attributable.

If propagation between honest operators stays bounded by `R · D`, the protocol terminates with a V signature on some layer (the first layer where σ-quorum reaches under cluster-wide V receipt, or the deepest layer reachable via NR fall-through with a valid backup leader). If propagation exceeds `R · D`, or more than `f` operators are byzantine/offline, the slot is missed. **Safety holds in either case.**

**Best case (round 1 healthy at L_0)**: all honest receive V_{L_0} within `D + δ`; all 3 honest σ-emit + leader's Phase-1 σ = 4 ≥ qV. Slot succeeds in 2 RTTs (Phase 1 + Phase 2). Same as baseline TBFT.

**Aggressive-marginal recovery (round 2 covers 2-of-3-honest missing re-flood)**: 1 honest received V before round-1's commit; 2 honest received it past round 1's acceptance horizon `T_accept_max_1` (re-flood incomplete in round 1's window). Per Defer rule (peer σ-emit observed by the 2 missing honest), they don't NR-emit in round 1 — they Defer. The bundle is auth-only-retained for round 2. Round 1 ends with σ-pool = 1 + leader = 2 < qV, NR-pool = 0. Round 2 fires; the auth-only-retained bundle becomes accepted at round-2 start; the 2 honest σ-emit in round 2. σ-pool = 3 + leader = 4 ≥ qV. Slot succeeds in ~3 RTTs. **OBFTR recovers what baseline TBFT misses.**

**Equivocation handling (no protocol-level recovery; outcome depends on byzantine timing)**: byzantine `L_0` equivocates, broadcasting distinct V's selectively. Case analysis at `f = 1, n = 4` (3 honest A, B, C; byz D = leader):

Recovered via σ-quorum at L_0 (when honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool):

- **D delivers V to {A, B}, V' to {C}.** σ-pool on V = A + B + D's σ_L^V(V) = 3 = qV. σ-quorum reaches in round 1. Slot outputs V.
- **D delivers V to {A}, V' to {B, C}.** σ-pool on V' = B + C + D's σ_L^V(V') = 3 = qV. Reaches in round 1. Slot outputs V'.

Recovered via NR-quorum at L_0 → fall-through to L_1 (when all honest land in Defer-due-to-equivocation; round-R force-NR produces NR-quorum):

- **All-Defer outcome (byz delivers V's early enough that re-flood spreads conflicts before Phase 2 σ-emit).** Each honest retains ≥ 2 distinct V's by Phase 2 emit time → all 3 in Defer-due-to-equivocation. σ-pools at L_0 ≤ byz partials per V < qV. Round R force-NR: 3 honest force-NR + 0 byz (σ-locked from Phase 1) = 3 = qEnc → NR-quorum reaches → fall-through to L_1; if L_1 honest, slot succeeds at L_1. Asymmetric-retention patterns under 3+-V flood typically land here when byz delivery timing is "too early" for grief purposes.

Not recovered — slot misses at L_0 with no fall-through (σ-locked split patterns; slashable):

- **D delivers V to {A}, V' to {B}, both to C (or neither).** A σ-locked on V; B σ-locked on V'; C in Defer-due-to-equivocation (received both) or Defer-due-to-partition (no V received). σ-pool on V = A + leader = 2 < qV. σ-pool on V' = B + leader = 2 < qV. C force-NR at round R; NR-pool = 1 < qEnc → no fall-through. Slot misses.
- **D delivers distinct V_1 to A, V_2 to B, V_3 to C (1-1-1 split).** Each honest σ-commits on their V_i on receipt before observing equivocation. All 3 honest σ-locked on different V's. σ-pool on each V_i = 1 honest + leader's σ_L^V = 2 < qV. NR-pool = 0 (all σ-locked, cross-phase exclusivity). Slot misses.
- **1-Defer-Defer (asymmetric retention under 3+-V flood, walkthrough).** D floods {V_1, V_2, V_3} with adversarially-ordered gossipsub delivery: A first-observes V_1 alone (before any other) → σ-locks on V_1; B first-observes {V_1, V_2} via gossipsub-ordered delivery → retains both → Defer-due-to-equivocation; C first-observes {V_2, V_3} → retains both → Defer-due-to-equivocation. D signs σ_L^V on V_1 only (locked at Phase 1 to a single V per single-σ-V exclusivity). End of round R Phase 2: σ-pool on V_1 = A + D's σ_L_0^V(V_1) = 2 < qV; B and C force-NR; NR-pool = 2 honest + 0 byz (σ-locked) = 2 < qEnc → no quorum on either side; no fall-through. Slot misses. The retention bound (2 distinct V's per `(slot, layer, leader_id)` per operator) means B and C have non-overlapping retention sets — even if they tried to coordinate on a "winner V," there isn't a common winner. This is the canonical asymmetric-retention shape an adversarial byz produces with selective ordering, and shows why the protocol cannot pick a winner cluster-wide from per-operator local retention state.

**Byzantine timing controls which class fires — and an *adversarial* byzantine reliably picks the slot-miss class.** Delivering V's early in Phase 1 leaves time for gossipsub re-flood to spread conflicts → all-Defer outcome → slot succeeds at L_1. Delivering near end-of-Phase-1 leaves insufficient re-flood time → σ-locked split outcome → slot misses at L_0. **The "naturally reaches qV" framing is misleading without this caveat**: it does not happen by random chance. Under non-adversarial byz behavior the natural-recovery cases fire occasionally (whenever byz's delivery timing happens to spread conflicts in time); under adversarial byz, byz controls the timing and picks the σ-locked split outcome on demand. **In expectation against an adversarial byz primary, all of these patterns slot-miss reliably.** The rational-byzantine deterrent (assumption 4) is what makes this tolerable across many slots — but the *evidence quality* for these patterns is the *behavioral* class (not the cryptographically-self-contained class), so single-observation slashing is not credible (see [§Implications of the rational-byzantine deterrent / Evidence quality by fault class](#implications-of-the-rational-byzantine-deterrent-assumption-4)). Practical effect: byz can grief many slots before the pattern accumulates enough confidence for honest operators to act. (R-rounds do not help — these patterns are R-invariant.)

In all cases, Pigeonhole 2 ensures at most one V can reach qV cluster-wide regardless of how honest σ-emissions split — there is no two-output safety violation. The TBFTR-style **Phase 2a/2b split** (broadcast-only Phase-2a where operators re-flood retained Phase-1 bundles without σ-emitting, then Phase-2b where σ-emits happen on the cluster's stabilized winner V) recovers all equivocation patterns at +1 RTT cost; documented as future improvement, not in current OBFTR.

**Sub-partial-synchrony (real propagation > absorption window)**: if propagation between leader broadcast and any honest receiver's first-observation exceeds the cluster's R-round absorption window (per-round acceptance horizon + cross-round retention through round R — see [§Trust model](#trust-model) for the algebra; concrete value at Config A R=2 is ~`7(D+δ)` ≈ 1150ms with recommended Δ_2 per round), late honest don't σ-emit by round R and slot misses. **No safety violation.** R is a tunable knob; larger R extends tolerance at the cost of more pessimistic timing.

**Multi-failure fall-through (K ≥ 3)**: with `K > 2` layers, the cluster can fall through past multiple silent or non-validating layers **within a single round** — Phase 3's reconstruction walk processes layers sequentially using local decryption (no per-layer RTT). E.g., at K=4 with L_0, L_1, L_2 all silent: NR-quorum reaches at L_0 in Phase 2 (silent-leader rule fires for all 3 honest cluster-wide); same at L_1 and L_2; σ-quorum at L_3 if L_3 leader honest. All happens in round 1's Phase 2 + Phase 3 windows. Recovery scope at K layers is achieved without round-change overhead — a structural difference from QBFT (which round-changes once per failed leader).

**Adversarial scheduling within the absorption window**: the network adversary delays each message by ≤ absorption window within the synchrony bound. The adversary's leverage scales with how many operators they can keep in Defer state through R rounds.

- *Safety unaffected.* Pigeonholes 1, 2, 3 are properties of the cluster-wide signed-message set, not of arrival times. The adversary can delay messages but cannot forge signatures or violate EKM rules. At most one V signature reconstructs cluster-wide regardless of timing.
- *Liveness — adversary delays V to ≤ 1 honest.* The other 2 honest σ-emit; σ-pool = 2 + leader = 3 = qV. **Quorum reaches in round 1 without the delayed operator.** At f=1, n=4 the adversary's leverage against ≤ 1 honest is wasted.
- *Liveness — adversary delays V to 2 honest.* 1 honest σ-emits in round 1; 2 stay Defer. σ-pool = 1 + leader = 2 < qV. Round 2 fires with re-flood from gossipsub peers (not just the leader). To keep V from those 2 honest in round 2, the adversary must delay messages from many sources — a stronger adversary than "delay any one message". If round-2 re-flood succeeds → σ-quorum reaches in round 2 (~1.15s at Config A). If adversary persists through round R → those 2 force-NR at round R; NR-pool reaches qEnc → automatic fall-through to L_1.
- *Liveness — adversary delays V to all 3 honest.* All in Defer in round 1. Round 2 re-flood: if delivery succeeds, all σ-emit → σ-pool = 4 ≥ qV. If adversary persists through R rounds → all force-NR → NR-quorum → fall-through to L_1.

The R · D budget is the cumulative delivery-delay tolerance against any single operator. Within budget, the cluster either σ-recovers at L_0 or NR-falls-through to L_1 (recovers if L_1 honest). Outside budget, the slot misses cleanly.

**OBFTR recovers strictly more than baseline TBFT** for partition cases (Defer state + R-round retry). It does **not match QBFT's full recovery scope** — view-divergence cases (host-validity divergence and equivocation patterns that don't naturally reach qV) are out of OBFTR's recovery scope. Equivocation is slashable (leader pays stake-based penalty); validity-divergence is not attributable but is bounded by host-side stabilization (assumption 3). The TBFTR-style **Phase 2a/2b split** (future improvement) closes the equivocation gap at +1 RTT cost.

### Liveness comparison: OBFTR vs QBFT

The table below puts OBFTR and QBFT side-by-side per scenario at the standard SSV proposer-duty configuration (n=4, f=1, ~4s relay cutoff). For OBFTR, round timing assumes Configuration A (D=100ms uniform, R=2, K=2 — see [Timing budget](#timing-budget--concrete-configurations)). For QBFT, RT≈2s per round-change is SSV's current production tuning.

| Scenario | OBFTR outcome | QBFT outcome |
|---|---|---|
| Healthy (all honest receive V_{L_0}) | Round 1: σ-quorum reaches in 2 RTTs (~500ms consensus). ✓ at L_0. | Round 1: PROPOSE→PREPARE→COMMIT (3 RTTs) + post-consensus (1 RTT). ~750ms. ✓ |
| Byzantine leader silent | Round 1: 0 σ-emits → all honest NR (silent-leader rule) → NR-quorum at L_0 reaches in Phase 2 → automatic fall-through to L_1 in same round's Phase 3 walk; σ-quorum at L_1 if leader honest. ✓ in ~500ms. | Round 1: no PROPOSE arrives; round timeout (~2s). Round 2: new leader proposes; succeeds in ~750ms. ✓ in ~2.75s. |
| Aggressive marginal (>1 of 3 honest miss V at round-1 cutoff) | Round 1: σ-pool = 1 + leader = 2 < qV; the 2 missing honest stay Defer (peer σ-emit observed). Round 2: re-flood delivers V; σ-quorum reaches. ✓ in ~1.15s. | Round 1: PREPARE-pool short; timeout (~2s). Round 2: new leader re-fetches + proposes; succeeds in ~750ms. ✓ in ~2.75s. |
| Byzantine leader equivocates, 2-1 split where one V already has 2 honest + leader-σ_L^V | Round 1: σ-pool on the 2-honest-V = 2 + leader = 3 = qV; reaches naturally. ✓ in ~500ms. | Round 1: PREPARE-pool split across V's; no quorum; timeout. Round 2: new leader proposes; succeeds. ✓ in ~2.75s. |
| Byzantine leader equivocates, σ-locked split pattern (1-1-1 split, 1-1-Defer-C, 1-Defer-Defer; byz delivers near end-of-Phase-1) | σ-pools split below qV; NR-pool below qEnc (σ-locked operators can't NR). **✗ slot misses at L_0;** no fall-through. Equivocation slashable; rational-byzantine deterrent kicks byz out for future slots (see [Assumptions](#assumed)). | Round 1: PREPARE split; no quorum; timeout. Round 2: new leader proposes a fresh V (not one of the equivocated V_i); honest converge; succeeds. ✓ in ~2.75s. **QBFT recovers what OBFTR doesn't** via "new leader proposes fresh V" mode. |
| Byzantine leader equivocates, all-Defer outcome (byz delivers V's early; re-flood spreads conflicts before σ-emit) | All 3 honest in Defer-due-to-equivocation. Round R force-NR → NR-quorum at L_0 → fall-through to L_1; if L_1 honest, ✓ at L_1 in ~RT × R. Equivocation slashable. | Same recovery as σ-locked split row: round-2 new leader proposes fresh V. ✓ in ~2.75s. |
| Multi-failure fall-through (multiple silent leaders) | At K=3 with L_0, L_1 silent: NR-quorum at L_0 and L_1 reaches in Phase 2; Phase 3's walk decrypts down to L_2; σ-quorum at L_2 if honest. **All in round 1's existing windows** (sequential local decryption, no per-layer RTT). ✓ in ~500ms. | Round 1: leader 1 silent → timeout (~2s). Round 2: leader 2 silent → timeout (~2s). Round 3: leader 3 takes over; succeeds. ✓ in ~5s — past 4s relay cutoff. ✗ for proposer duty. **OBFTR's K-layer parallel fall-through beats QBFT's serial round-change for this case**, but only up to OBFTR's pre-fetched K depth. |
| Host-validity divergence (head-change mid-slot, strict host) | Out of scope (assumption 3 — host stabilizes verdict at Phase-1 acceptance). If assumption holds, no divergence. If violated, slot misses (no in-protocol recovery). | Round 1: validators with stale head don't PREPARE on the proposed V; PREPARE quorum may not reach; timeout. Round 2: new leader fetches at moved head; if quorum agrees, succeeds. ✓ in ~2.75s. **QBFT recovers what OBFTR doesn't** via fresh-V refetch on round-change. |
| Adversarial scheduling — adversary delays V to 2 honest persistently | Round 1: σ-pool = 1 + leader = 2 < qV; 2 honest in Defer. Round 2: re-flood from gossipsub (multiple sources). If delivery succeeds → σ-quorum reaches at ~1.15s. ✓ If delay persists through R rounds → 2 honest force-NR at round R → NR-quorum → fall-through to L_1. ✓ if L_1 honest. | Round 1: PREPARE delayed → timeout (~2s). Round 2: new leader; if adversary delays again → round 2 timeout (~2s). 4s consumed → relay cutoff missed. ✗ Within 4s, QBFT tolerates ≤ 1 round of adversarial delay. |
| Adversarial scheduling — adversary delays V to all 3 honest persistently | Round 1: all in Defer. Round 2: re-flood. If delivery succeeds → all σ-emit → σ-pool = 4 ≥ qV. ✓ in ~1.15s. If persists → all force-NR at round R → NR-quorum → fall-through to L_1. ✓ if L_1 honest. | Round 1: timeout (no PREPARE quorum). Round 2: new leader; if adversary delays → round 2 timeout. Same as above. ✗ |
| Sustained partition (real propagation > round budget) | OBFTR R · D budget exceeded; force-NR may not reach NR-quorum if too many honest are partitioned out; slot misses. ✗ Safety holds. | QBFT round-budget × RT exceeded; slot misses. ✗ Safety holds. |
| > f operators offline/byzantine | Standard 3f+1 violation; slot misses. ✗ Safety holds. | Same. ✗ Safety holds. |

**Summary of recovery-scope differences:**

- **OBFTR-strict-superset cases** (OBFTR recovers in fewer RTTs or more cleanly): healthy, byzantine-leader-silent (in-round NR fall-through vs. round-change), aggressive marginal, all-Defer equivocation (NR-fall-through to L_1 in round R when byz delivers V's early enough that re-flood spreads conflicts before σ-emit), multi-leader-failure (K-layer parallel fall-through within one round vs. K-1 serial round-changes), most adversarial-scheduling patterns within R · D. These are OBFTR's wins because (a) NR fall-through is in-protocol within a single round, no round-change needed; (b) per-round overhead (~250ms at Config A) is much smaller than QBFT's RT (~2s).
- **QBFT-strict-superset cases**: 1-1-1 equivocation, host-validity divergence. QBFT's "round-change with new leader proposing fresh V" handles these structurally; OBFTR relies on assumption 3 (validity stabilization) and assumption 4 (rational-byzantine deterrent) respectively.
- **Both fail equivalently**: sustained partition beyond budget, > f byzantine.

The choice between OBFTR and QBFT for SSV proposer duty depends on observed re-org rate, the cluster's tolerance for the 1-1-1 equivocation case (handled by rational-byzantine deterrent in OBFTR, recovered in QBFT), and the relative weight of common-case latency vs. worst-case-coverage. Detailed cost-side trade-offs (latency, bandwidth, cryptographic primitive maturity) are in [Appendix A.3](#a3--comparison-with-qbft).

### Equivocation handling

See "Phase 1 / Equivocation handling — detect and slash" for the operational rule. Summary: when honest detects equivocation (two distinct σ_V partials from the same leader on different value_roots), they:

1. Stay σ-committed if already σ-emitted at this layer (cross-phase exclusivity binds them).
2. If σ-eligible but not yet σ-emitted, transition to Defer-due-to-equivocation. The σ-emit precondition fails.
3. If already in Defer-due-to-partition, transition to Defer-due-to-equivocation upon retaining ≥ 2 distinct V's. Recovery via re-flood-delivers-V is foreclosed.
4. All Defer-due-to-equivocation operators force-NR at round R per the final-round rule. The protocol does not pick a winner cluster-wide.
5. Gossip the equivocation evidence (the pair of equivocating Phase-1 bundles) for out-of-band slashing.

The leader is required to sign `σ_V` *exactly once per (slot, layer)*; refreshes in the host's fetch loop are pre-signing only (see "Application: SSV Ethereum proposer duty / Head-change handling") and don't surface multiple `σ_V` partials on the wire. Any second `σ_V` from the same leader is a protocol violation.

OBFTR does not provide in-protocol equivocation recovery. Some equivocation patterns naturally reach qV on a single V (when honest happen to split such that 2-of-3 σ-emit on the same V; leader's σ_L^V on that V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. See "Liveness / Equivocation handling" for the full case analysis. Equivocation is treated as a slashable byzantine fault (Phase-1 bundles signed by leader's key are self-contained slashing evidence — see "Slashing evidence"); the rational-byzantine deterrent (assumption 4) is what makes the slot-miss tolerable across many slots. The TBFTR-style **Phase 2a/2b split** is the structurally correct full fix — recovers all equivocation patterns at +1 RTT cost — documented as future improvement.

**Cross-onion (operator-side) equivocation — counting and suppression.** If an operator `i` is observed with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different V, the dual partials are slashable evidence on the same logic as leader equivocation. For aggregation: by single-σ-V exclusivity (EKM-enforced — see "Slashing-protection scope"), an honest operator only ever emits σ on one V per layer, so any dual-V σ partials from the same operator are byzantine. The byzantine f-bound on per-V σ-pool contributions therefore holds without explicit suppression: byz contributes ≤ 1 partial per V regardless. Honest receivers MAY additionally elect to fully suppress `i`'s partials upon observing the equivocation evidence — this is not load-bearing for safety.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_k` is a slashable cross-signer. The pair is detected uniformly across phases and rounds:

- **σ from Phase 1 + NR/NV from Phase 2** — applies to a layer's leader specifically: a `σ_{L_k}^V(V_{L_k})` from the Phase-1 bundle paired with an `σ_{L_k}^{IBE}(nr_tag_k)` from the same operator at the same slot.
- **σ from Phase 2 onion + NR/NV from Phase 2** (any round) — any operator who included σ in their onion *and* broadcast a no-σ attestation, in any round.

Detection is straightforward — the dual partials are public. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it cryptographically — the EKM-enforced cross-phase exclusivity is what gives Pigeonhole 1 its load-bearing constraint, and a byzantine cross-signer simply consumes their f-bound contribution to one of σ-pool or NR-pool, not both effectively). The detection is purely for **attribution** and out-of-band punishment.

### Slashing evidence

Five rules surface byzantine fault evidence for *attribution and punishment*. Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to inform the surviving operators (and stakers) that a byzantine fault has occurred so the cluster can act on it — see [Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4) for the offline-equivalence model. The protocol surfaces the evidence; the surviving operators verify it and decide whether to (a) blacklist the byzantine via cluster-wide config update (restoring `Byzantine ≡ Down`; planned protocol extension), (b) file a stake-slashing transaction via the SSV contract where the evidence is cryptographically self-contained, and/or (c) propagate the signal to stakers and to future cluster formation.

- **Self-contradiction (σ + NR/NV at the same layer).** If operator `i` emitted `σ_i^V(V)` at some layer AND `σ_i^{IBE}(nr_tag_k)` at the same layer, the dual partials are slashable evidence (cross-signing).
- **Leader equivocation.** Two distinct `σ_V` partials from the same leader at the same (slot, layer) are a self-contained slashable fault proof. Any observable double-signing is protocol-violating regardless of the leader's stated intent.
- **Cross-onion partial-sig equivocation.** Operator `i` emitting `σ_i^V(V)` and `σ_i^V(V')` for different `V` at the same layer is detectable from the partial sigs alone — single-σ-V exclusivity is EKM-enforced, so any dual-V observation is a slashable byzantine fault.
- **Fake encrypted-presence (post-decryption garbage at k > 0).** Operator `i` broadcasting an auth-signed `KindOnion` with an encrypted partial at layer `k > 0` that, after NR-quorum unlocks decryption, decrypts to garbage (not a valid σ partial on any leader-known V at L_k, or fails to decrypt entirely) is a slashable byzantine fault. The auth envelope binds `i` to the encrypted payload at signing time; post-decryption verification surfaces the garbage. Detection is **delayed and conditional on NR-quorum reaching at all prior layers** (so the chained encryption can be unlocked); when the slot misses cleanly without any NR-quorum reaching (e.g., σ-locked split at L_0, or NR-pool short of qEnc cluster-wide), the chained encryption stays sealed and the evidence is not surface-able through this rule. **Honest detection of fake encrypted-presence is therefore conditional on the slot progressing far enough for the relevant layer's encryption to unlock.** This is a real deterrent-strength reduction for adversarial byzantine that engineers slot-miss precisely to seal evidence; mitigated only by Rule 5 (when applicable at L_0) or by post-hoc decryption coordination outside the protocol's current scope.
- **Fake plaintext σ at L_0 (immediate detection by retained-V receivers).** Operator `i` broadcasting an auth-signed `KindOnion` with a plaintext σ partial at L_0 that does not verify against any retained leader-broadcast V_{L_0} (where the receiver has retained at least one such V) is a slashable byzantine fault. The auth envelope binds `i` to the partial; partial-vs-V verification is a deterministic local check by any receiver with retained V. **Detection is immediate** (no decryption-unlocking dependency, unlike Rule 4) — the receiver can attribute the fault as soon as it observes both `i`'s auth-signed onion and any leader-broadcast V_{L_0}. **Receivers MUST gossip the evidence** (the auth-signed `KindOnion` envelope plus a retained Phase-1 bundle for V the partial fails to verify against) so that receivers without retained V (the targets of the byzantine-σ-into-Defer attack) can eventually receive the attribution evidence and act on it — closing the asymmetric-detection gap created by the L_0 validity-gate fallback for no-retained-V receivers (see §Phase 1 / Validity-gate on observed σ-emit (L_0 specifically)).

  **Rate-limit (anti-amplification rule).** A byzantine operator can broadcast multiple distinct fake-σ envelopes at L_0 (the auth envelope binds `(slot, operator_id, onion_payload)` — different `onion_payload` produces different envelopes; EKM does not constrain byz here). Without bounds, each fake envelope would trigger MUST-gossip from every retained-V receiver, creating an amplification surface of `h_retained × M × evidence_size` per slot for `M` distinct fake envelopes. **Each receiver MUST gossip evidence at most once per `(slot, layer, operator_id)` tuple** — additional auth-signed fake-σ envelopes from the same operator at the same `(slot, layer)` are observed locally (and may be retained for cumulative slashing) but do not trigger additional gossip. Gossipsub-layer message-id deduplication may also apply (implementation-level). This caps amplification to `h_retained × evidence_size` cluster-wide per byzantine fake-σ event.

  **Cumulative load across slots is not bounded by this rule.** A byzantine sustaining fake-σ griefing across N slots produces ~`N × h_retained × evidence_size` of evidence-gossip traffic over time (per byzantine operator). For high-frequency adversarial griefing, this is bounded per-slot but cumulative across the slot stream. Mitigation is at the gossipsub mesh layer (rate-limiting / scoring of high-volume evidence broadcasters at the network layer), not the protocol layer. The evidence each receiver retains for cumulative slashing is what gives the deterrent its multi-slot accumulation; the rate-limit only caps amplification per single byzantine event.

Each piece of evidence is verifiable in isolation (signed by the offending operator's own keys) — any observer with the published partials and the (eventually) decrypted onion contents can independently confirm the byzantine action. **Acting on the evidence (slashing transaction, cluster removal) is a human-coordinated process**, not an automated protocol step; honest operators judge whether the evidence is compelling and decide whether to act.

**Evidence quality and surface-ability vary by rule:**

| Rule | Detection timing | Surface-ability | False-positive risk |
|---|---|---|---|
| 1. Self-contradiction (σ + NR/NV) | Immediate (dual partials on the wire) | Always — public partials | Very low |
| 2. Leader equivocation | Immediate (two σ_V from same leader) | Always — public bundles | Very low |
| 3. Cross-onion partial-sig equivocation | Immediate (two σ partials on different V) | Always — public partials | Very low |
| 4. Fake encrypted-presence (k > 0) | Delayed (post-decryption) | **Best-effort, conditional on slot progressing past prior layers' NR-quorum** — sealed if slot misses early. R-rounds do not help: if no NR-quorum reaches at any prior layer across all R rounds, the seal applies. | Very low when surfaced |
| 5. Fake plaintext σ at L_0 | Immediate (partial vs retained V check) | Always when retained-V receivers gossip evidence (MUST-gossip rule, rate-limited per `(slot, layer, operator_id)`) | Very low |

**Rule 4 has a structural detection limit.** The chained-encryption construction makes Rule 4 evidence cryptographically inaccessible when prior layers' NR-quorum doesn't reach across all R rounds: decryption requires `qEnc` partials on `nr_tag_{0..k-1}`, those partials don't exist if honest σ'd or Defer'd at those layers, and honest cannot preemptively sign IBE partials on layers they intend to σ-side-commit to (that violates cross-phase exclusivity = cross-signing). This is a **fundamental limit of the IBE primitive as used here**, not a fixable spec gap.

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

- **[Class A]** **Sustained partition (real propagation > absorption window)** — violates assumption 2 (partial synchrony). Re-flood doesn't complete within the cluster's R-round absorption window (per-round acceptance + cross-round retention through round R; see [§Trust model](#trust-model) for the algebra). Honest who didn't receive V at any round's acceptance horizon stay in Defer; final-round force-NR transitions them to NR. If even forced NR-pool is short of qEnc, slot misses cleanly. **No safety violation.** The R parameter is tunable; larger R extends propagation tolerance at the cost of slot-budget consumption.
- **[Class A]** **More than `f` faults** — violates assumption 1 (BFT trust bound). More than `f` operators offline/byzantine combined. Standard `3f+1` trust bound. Slot misses regardless of round structure.
- **[Class A]** **Assumption 3 violated (host validity diverges mid-slot)** — violates assumption 3 (host validity unanimous at decision time). See [Implications of validity-divergence not being recovered](#implications-of-validity-divergence-not-being-recovered-assumption-3). The host's stabilization workflow (validate-once-and-lock at Phase-1 acceptance) is the design's path to keeping this from occurring; if it does occur, the slot misses cleanly. Not slashable (re-orgs are real-world events, not protocol violations); rational-byzantine deterrent does not apply.
- **[Class B — cryptographically slashable]** **Equivocation σ-locked split patterns** (1-1-1 split, 1-1-Defer-C, 1-Defer-Defer at f=1 n=4 — patterns where some honest σ-commit on different V's before observing equivocation, leaving neither σ-quorum nor NR-quorum reachable at L_0 and no fall-through) — occurs under valid assumptions; one byzantine within f-bound equivocates to engineer the split. See [Implications of equivocation not being recovered](#implications-of-equivocation-not-being-recovered). **Equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained cryptographically slashable**; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots. The all-Defer-due-to-equivocation case (e.g., asymmetric-retention patterns where every honest retains ≥ 2 V's by σ-emit time) actually recovers via L_1 fall-through and is not a failure mode at f=1 n=4 with L_1 honest.
- **[Mostly Class A]** **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates in non-recoverable patterns at every layer). At K ≥ f+1, byzantines alone (within f-bound) cannot cause this — pigeonhole guarantees ≥ 1 honest leader. So this typically requires either >f faults (Class A: violates assumption 1) or coincident non-byzantine independent failures (relay timeouts, host issues, etc. — also Class A in the sense that the cluster's operational assumptions about host availability are violated). Rare in practice. The K parameter controls the fall-through depth.
- **[Class A]** **Late deepest-layer leader broadcast at K=2 (NR-lock at backup layer).** A backup leader L_{K-1} (= L_1 at K=2) whose Phase-1 bundle's first cluster-observation arrives after round R's final acceptance horizon `T_accept_max_R` — e.g., the leader's fetch loop overruns Δ_1 by enough to miss every round's acceptance horizon — gets rejected entirely. *Even within the per-round widening*, a sufficiently late broadcast can produce a deadlock: by round-r NR-decision time at L_1, if no honest has V_{L_1} accepted (the bundle is auth-only-retained, not yet first-acceptable for round r), no honest is σ-eligible at L_1 → no peer σ-emit observable cluster-wide → silent-leader rule fires → honest NR-emit at L_1 → cross-round exclusivity locks them. The bundle eventually becomes accepted in round r' > r, but no honest can σ-emit (NR-locked from round r). NR-pool at L_1 reaches qEnc → fall-through to L_2, which doesn't exist at K=2. **Slot misses** despite the bundle eventually being accepted. (At K ≥ 3 this falls through harmlessly to L_2 — the NR-lock at L_1 advances the L_C frontier and L_2's σ-quorum can still reach. K=2-specific failure.)

  **Why Class A?** None of the six explicit assumptions is violated: BFT trust bound holds, partial synchrony bounds *gossipsub* propagation but not the leader's *application-internal fetch latency*, validity is orthogonal, EKM and DKGs are orthogonal. The failure relies on an *implicit* operational assumption — that honest leaders broadcast within their Phase-1 fetch window plus the per-round widening slack. When this implicit assumption fails (legitimate operational delay, no byzantine action), the protocol's per-round acceptance widening collides with cross-round σ-or-NR exclusivity (which locks NR from any earlier round where the bundle wasn't yet accepted).

  **Concrete budget at Configuration A** (T_commit_1 = 2s, D + δ = 150ms, recommended `Δ_2 = 2(D + δ) = 300ms`): for at least one honest to σ-emit observably at L_1 before round-1 NR-decision, L_1's bundle must be first-observed within round 1's acceptance window — i.e., by `T_accept_max_1 = T_commit_1 + Δ_2 − (D + δ) = 2.15s`. With L_1's nominal schedule at `T_1 + Δ_1 ≈ 1.1s`, that's ~1.05s of leader-fetch slack within round 1 alone. The R-round retention extends this further across rounds, but at K=2 once any honest NR-locks the bundle is unrecoverable.

  **Mitigation paths (in order of recommendation):**
  - **Use K ≥ f+2** (the recommended config; see §Setting). Adds at least 2 honest leaders so a single late-broadcasting honest leader doesn't foreclose the slot — fall-through to a deeper honest leader recovers. At f=1 n=4 this means K = 3 (~1KB extra per onion vs K=2; within practical bandwidth). At f=2 n=7, K = 4. Etc. **No protocol change**, just deployment configuration; this is the cleanest fix that actually recovers the slot.
  - **Host-side hard deadline** (defense-in-depth on top of K ≥ f+2; minor host-side discipline, no protocol change). The leader's fetch loop should abort and broadcast no bundle if it hasn't terminated by `T_broadcast_max_1 = T_commit_1 − 2(D + δ)`. Converts "late broadcast NR-locks the layer" into "silent leader" (handled cleanly via NR-quorum → fall-through). At K = f+1 (BFT-min) this *cleans up the spec-tension* but doesn't recover the slot (silent + L_0-fail still falls through to non-existent layer). At K ≥ f+2 it complements the K choice by ensuring late broadcasts always fall into the silent-leader path rather than the NR-lock pathology.
  - **Phase 2a/2b** ([Path forward](#future-improvement--phase-2a2b-for-full-equivocation-recovery)). Structural fix that handles K = f+1 too. No Phase-1 σ_V from the leader → no early commitment by anyone → late bundle observed in Phase-2a is σ-emittable in Phase-2b. Costs +1 RTT/round (same as the other Phase 2a/2b benefits). Worth adopting if the deployment also wants the validity-divergence and Class B byzantine grief recovery that Phase 2a/2b brings.
- **[Class A]** **Validity-divergence deadlock (network-induced; no byzantine action required in the cleanest case).** **This is a TBFT-family issue inherited by OBFTR — not introduced or worsened by OBFTR's R-round / Defer-state machinery.** The same algebraic deadlock is documented in [TBFT.md "Application-validity-divergence — known liveness limit"](TBFT.md). Both protocols share the three structural causes: per-operator independent validity verdict, leader's σ_V locked in Phase 1, and cross-phase exclusivity per operator. OBFTR's R-round structure does not address it (verdicts are locked at acceptance per the host stabilization workflow, so re-flood across rounds doesn't reconcile divergence).

  A beacon-chain re-org landing inside the gossipsub-acceptance window for the Phase-1 bundle can split honest operators between σ-eligibility (parent_root matched their pre-reorg head snapshot) and NV (parent_root didn't match their post-reorg head). The host's per-operator validate-once-and-lock workflow (§Head-change handling) preserves each operator's verdict but cannot reconcile cluster-wide divergence. At the boundary split (e.g., 2-σ vs 2-NV at f=1 n=4 with all 4 honest, where leader's Phase-1 σ_V counts as σ-side via cross-phase exclusivity): σ-pool = 2 < qV = 3; NR-pool = 2 < qEnc = 3. **Same algebraic deadlock as the byzantine selective-delivery case below**, but **no byzantine action required and no slashable evidence** in the cleanest form — re-orgs are real-world events, not protocol violations. The rational-byzantine deterrent does not apply (nobody to attribute fault to). Probability scales with the re-org rate × acceptance-window width / slot length; the host's stabilization workflow narrows the window (typical D ≈ 100–500ms vs slot length 12s) but doesn't eliminate it.

  **The deadlock zone widens when a byzantine within f-bound exercises f-budget passively** (silent or σ-on-V — neither is independently slashable; passive silence is the natural f-budget consumption pattern for an absent operator). At f=1 n=4 with leader honest, additional deadlock configurations include:

  - "1 non-leader σ + 1 non-leader NV + byz silent" (σ-pool = leader + 1 = 2 < qV; NR-pool = 1 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz silent" (σ-pool = leader = 1 < qV; NR-pool = 2 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz σ-on-V" (σ-pool = leader + byz = 2 < qV; NR-pool = 2 < qEnc).
  - Symmetric configs when leader's host-verdict is itself NV (Phase-1 σ_V locks leader σ-side regardless of leader's host's later opinion).

  The all-honest example captures the cleanest case; in real deployments with byz exercising f-budget, the practical slot-miss rate from validity-divergence is meaningfully higher than the all-honest example alone suggests. **The expected rate scales with `re-org rate × byz-passivity-rate × R-round-absorption-window-width`** — i.e., a deployment with re-orgs in 1% of slots, a byzantine adopting passive grief in some fraction of slots, and an R-round absorption window proportional to slot length compounds these probabilities into validity-divergence slot-misses. The R-round absorption window's contribution is non-trivial: at OBFTR(R=2) Config A recommended Δ_2, the validity-locking window is ~1150ms vs slot length 12000ms, contributing ~9.6% × `re-org rate × byz-passivity` to slot-miss rate. The host's stabilization workflow narrows the divergence window but does not eliminate it; byzantine passive f-budget consumption (silence or σ-on-V — neither cryptographically slashable individually) is essentially "free" within the f-bound, so byz can reliably contribute the passivity factor whenever exercising the deterrent's weak-attribution corner is favorable. See [Implications of validity-divergence not being recovered (assumption 3)](#implications-of-validity-divergence-not-being-recovered-assumption-3) for the full discussion. Phase 2a/2b (see [Path forward — Phase 2a/2b](#future-improvement--phase-2a2b-for-full-equivocation-recovery)) eliminates this deadlock structurally via late σ-emit on cluster-stabilized verdict.
- **[Class B — gossipsub-pattern slashable, weaker accountability]** **Byzantine selective-delivery grief at the final round (h_V = 1 deadlock).** *A deliberate-byzantine grief vector — does not arise under all-honest behavior with no implementation bugs and normal network conditions.* The attack requires the L_0 leader to actively (1) **withhold the Phase-1 bundle from all honest peers** in rounds 1..R-1 — an honest leader broadcasts via gossipsub immediately on producing V, so withholding is a deliberate deviation; (2) **emit an auth-signed σ-claim in their Phase-2 onion despite not having broadcast Phase 1** — an honest leader's Phase-2 σ is a follow-on to their Phase-1 broadcast, not standalone; (3) **selectively deliver the Phase-1 bundle to exactly one honest operator at round R**, timed precisely so re-flood cannot spread V to other honest before the round-R Phase-2 cutoff — honest leaders don't time deliveries to specific operators. Each step is a deliberate deviation from honest protocol behavior; none occurs incidentally. (A structurally similar `h_V = 1` deadlock can arise under *sustained network partition* — `real propagation > R · D` — where one honest is on the leader's side of the partition for the entire R-round window while two others are partitioned away; that case is captured by the "Sustained partition" failure mode above. The byzantine version is the worry because byz can *engineer* it deliberately rather than wait for unlucky network conditions.)

  With these byzantine actions in place: per the L_0 conditional gate fallback for receivers without retained V, all honest defer through rounds 1..R-1. At round R, byz selectively delivers the Phase-1 bundle to *exactly one* honest operator (A). A σ-emits at round R; the other 2 honest force-NR (no V received). Final pools: σ-pool = A + byz σ_V = 2 < qV = 3; NR-pool = 2 honest + 0 byz (σ-locked) = 2 < qEnc = 3. **Neither quorum reaches at L_0; no fall-through.** Slot misses without leaving equivocation evidence (no double-signing — the attack uses only selective delivery of a single σ_V).

  **The deadlock is fundamental at f = 1, n = 4** for the h_V = 1 case (h_V = number of honest operators with V at round R cutoff): σ-quorum needs h_V ≥ 2 (since byz's σ_V contributes 1, total = h_V + 1); NR-quorum (with byz σ-locked) needs h_V = 0 (since 3 − h_V honest force-NR). The intermediate h_V = 1 fails both. Generalizes at higher f: the deadlock zone is `0 < h_V < 2f`.

  **The R parameter does not provide additional defense against this attack class.** The deadlock is invariant under R: byzantine engineers h_V = 1 by withholding the bundle from all honest until the final round, then delivering to exactly one honest. Rounds 1..R-1 don't change the outcome — they're rounds during which byzantine maintains the Defer-keeping σ-claim (cheap for byz) and honest stay in Defer (no commitment). Increasing R gives byzantine *more* timing flexibility (any round can be the delivery round) without strengthening cluster recovery. **This is the key point about OBFTR's R-round structure: rounds defend against partition cases (re-flood completing across rounds) but provide no defense against this attack class.** Structural fixes are needed (Phase 2a/2b).

  **Defenses considered, all with trade-offs:**
  - *f+1 witness threshold for the Defer fallback* (suggested in adversarial review): require ≥ f+1 distinct σ-claims observed before the no-V-retained receiver defers; otherwise NR-emit. **Defeats the attack** (byz's lone σ-claim falls below threshold → honest NR → NR-quorum reaches in round 1 → fall-through to L_1). **Breaks aggressive-marginal recovery at f = 1** (1 honest with V emits 1 σ-claim, also below threshold → NR-locked, can't σ when V arrives via re-flood). At f=1, no fixed threshold distinguishes byz selective-delivery from genuine partition (information-theoretically indistinguishable to receivers without V). Higher-f deployments may have more design room.
  - *Phase 2a/2b split* (TBFTR-style, [future improvement](#future-improvement--phase-2a2b-for-full-equivocation-recovery)): broadcast-only Phase-2a (re-flood retained Phase-1 bundles + σ-emit observation, no σ partials), then Phase-2b emits σ partials based on Phase-2a observations. This eliminates the deadlock by deferring σ-commitment until cluster-wide σ-side observability is established. Costs +1 RTT per round.

  **Current OBFTR does not defend against this attack.** It is in the same class as 1-1-1 equivocation: byz can grief reliably, slot misses, no on-wire evidence beyond byz's withholding pattern (which is hard to distinguish from network failures). The rational-byzantine deterrent (assumption 4) is the practical defense across many slots — repeated grief surfaces as a withholding pattern at the gossipsub / observability layer that the surviving operators can use to trigger a manual blacklist (planned protocol extension), and stakers can use to migrate validators away. The TBFTR-style Phase 2a/2b split is the structurally correct fix.

  **Composability with sealed-evidence patterns.** When this attack succeeds and the slot misses cleanly at L_0 (no NR-quorum reaches at L_0 in any round), the chained encryption at L_1, L_2, ... stays sealed. Any byzantine fake-encrypted-presence at deeper layers (Rule 4 evidence) is *not surface-able* in this slot — see [§Slashing evidence](#slashing-evidence) "Evidence quality and surface-ability" table. So a byzantine that combines selective-delivery at L_0 with fake-presence at deeper layers gets two grief actions for the price of one detection: the L_0 grief succeeds (slot misses), and the L_k>0 fake-presence is sealed (no Rule 4 detection). This composition further weakens the deterrent's effective coverage in the worst-case attack chains — and **R-rounds do not help here either**, since the seal applies if no NR-quorum reaches across all R rounds at L_0.
- **[Class B — gossipsub-pattern slashable, weaker accountability]** **Byzantine σ-refusal coordinated with honest mesh-bounded observation / transient flakiness (cross-round NR-lock).** The Defer rule's spec wording says "peer σ-emit observed cluster-wide", but operationally each operator only observes via their own gossipsub mesh. An honest operator with poor mesh visibility (few peers, high mesh-hop latency, transient connectivity glitch, EL-CL desync, etc.) can fail to observe peer σ-emits within their NR-decision window even when peers have σ-emitted — the Defer rule's intent (cluster-wide observation) is not what the implementation can deliver (mesh-bounded observation). This is a real design fragility, not a purely adversarial edge case: under realistic gossipsub conditions, individual operators occasionally hit propagation outliers.

  Failure mode: poorly-meshed honest A applies silent-leader rule (no peer σ-emit observed in their mesh window) → A NR-emits → A NR-locked across rounds. Round 2 onwards: A's mesh recovers (peers visible, σ-emits arrive), but A is locked. Combined with byzantine within f-bound refusing to σ-emit (within-f-budget passive consumption — silence is indistinguishable from honest-but-flaky, weakly slashable), σ-pool falls short of qV: at f=1 n=4, leader honest, A poorly-meshed, byz refuses → σ-pool = leader + 1 honest = 2 < 3 = qV; NR-pool = A + 0 byz = 1 < qEnc. **Deadlock.**

  **At f = 1, n = 4 this requires only ONE poorly-meshed honest plus ONE byz σ-refusal.** Mesh outliers happen in production gossipsub — operators occasionally hit propagation tails wider than the recommended `Δ_2 = 2(D+δ)` budget (peer churn, gossipsub mesh restructuring, geographic outliers, EL-CL desync, brief connectivity glitches). A byzantine within f-bound that adopts a "silent" strategy (never σ-emits, never NRs except when natural; no slashable cryptographic evidence — silence is indistinguishable from honest-but-flaky) makes this a *recurring* deadlock pattern under realistic conditions, **not just an adversarial edge case**. Its expected rate scales with `P(any honest mesh outlier in slot) × P(byz active in cluster)` — both non-negligible quantities for production deployments.

  The doc's Defer rule wording "observed cluster-wide" should be read as "observed via the operator's gossipsub mesh, which approximates cluster-wide under partial synchrony with bounded D + δ but degrades under mesh-visibility outliers". When mesh visibility is poor for some honest, the Defer rule's protective effect — keeping operators uncommitted while V propagates — fails for them.

  **Mitigations:**
  - **Phase 2a/2b** ([Path forward](#future-improvement--phase-2a2b-for-full-equivocation-recovery)) — defers commitment past mesh-visibility outliers; if honest's mesh visibility recovers before Phase-2b, they σ instead of NR-locking. **This is the structural fix — the only mitigation that closes the deadlock rather than narrowing its rate.**
  - **Mesh diversity at deployment level** — ensure each operator has diverse gossipsub peer connections (mesh degree, geographic diversity) to reduce the probability of a single operator hitting propagation outliers. *Constant-factor mitigation only.*
  - **Larger Δ_2** — extends the σ-emit observation window to absorb mesh-hop latency variance. At minimum `Δ_2 = D + δ`, only direct-mesh paths propagate before NR-decision; `Δ_2 ≥ 2(D + δ)` allows multi-hop paths to complete (see §Timing budget for the late-σ-emit propagation analysis). *Constant-factor mitigation only — outliers wider than `2(D+δ)` are not absorbed.*

  Mesh diversity and Δ_2 widening reduce the deadlock rate but do not eliminate it. For deployments where this rate is significant, Phase 2a/2b is the only structural fix.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

OBFTR uses **`K-1` IBE tags per slot** (the K-1 NR tags; the deepest layer has no NR tag). Each tag is a standard threshold-IBE tag using the cluster's IBE keypair. The chained encryption at each layer-transition is implemented as a single IBE ciphertext under `nr_tag_k`, nested across layers per [TBFT.md](TBFT.md) Appendix C. At K=2 the chain has 1 level; at K=3, 2 levels; etc.

The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init. Long-lived, no per-slot rotation. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

**Chained encryption cost.** At layer K-1 (deepest), each σ partial is wrapped in `K-1` levels of IBE encryption. Per-onion size grows as `O(K)` ciphertext bytes (`K-1` levels × ~32-64 bytes ciphertext per level). At K=2 the chain has 1 level; at K=4 it has 3 levels. Concrete sizes: ~1 KB per onion at K=2, ~3 KB at K=4. Within practical SSV bandwidth budgets — same scaling as baseline TBFT extended to K > 2.

## Properties summary

| Property | OBFTR |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1` + EKM-enforced per-operator commitments, holds against offline-aggregating byzantine within the f-bound. Honest-majority cryptographic, not 100% cryptographic — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). Same trust posture as QBFT. |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition (assumption 3) |
| Termination (output guaranteed) | Conditional. **One-liner: terminates within `T_round_R_end ≈ slot_start + 3.50s` at Config A R=2 (D = 100ms, δ = 50ms, K = 3, recommended Δ_2 = 2(D+δ) per round) under conditions: (a) ≤ f operators byzantine/offline, (b) real propagation between leader broadcast and any honest first-observation ≤ R-round absorption window `~7(D + δ) + Δ_3` ≈ 1150ms (lower at narrower per-round Δ_2; see §Trust model), (c) host validity unanimous at decision time (assumption 3), (d) `K ≥ 3` (late-leader resilience).** Configurable R lets operators tune termination guarantee per duty's deadline budget. |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates form self-contained slashable evidence |
| Byzantine-leader-grief resistance | **Partial under non-adversarial byzantine; substantially weaker against adversarial byz that deliberately engineers grief patterns.** Closed under partial synchrony for incidental selective-delivery / late-delivery via leader-σ-V-in-Phase-1 + gossipsub re-flooding + R-round retry (extends baseline TBFT's "1 of 3 honest missing re-flood" coverage to "any honest who eventually receive V within R · D propagation budget" via Defer state). Recovery via "natural" σ-quorum patterns (leader's σ_L^V completes a 2-of-3-honest pool) only fires when byz isn't actively timing deliveries. **Adversarial byzantine reliably engineers slot-miss when L_0**: σ-locked split equivocation (1-1-1, 1-1-Defer-C, etc.) and h_V=1 selective-delivery deadlocks. At f=1 n=4 with uniform leader rotation, an adversarial byz primary can deterministically grief ~25% of slots (whenever they're L_0). **R-rounds do not help with these patterns** — they're R-invariant (more rounds give byz more timing flexibility without strengthening cluster recovery; see §Failure modes / h_V = 1). The rational-byzantine deterrent (assumption 4) is the only protocol-level defense, and it works *across slots in expectation*, not per-slot. Effective deterrent strength is deployment-specific (stake-to-grief-value ratio, governance responsiveness, slashability evidence quality — see [§Implications of the rational-byzantine deterrent](#implications-of-the-rational-byzantine-deterrent-assumption-4)). For deployments under realistic adversarial conditions, **Phase 2a/2b is the structural fix and should be considered near-term, not future** — it converts all R-invariant byz grief patterns into clean fall-through at +1 RTT cost per round. |
| Mesh-flakiness tolerance (honest operator with poor gossipsub mesh visibility) | **Limited.** A mesh-flaky honest operator who fails to observe peer σ-emits within the NR-decision window can NR-emit incorrectly, becoming a byzantine-equivalent f-budget consumer for that slot. Combined with byz σ-refusal, this creates a deadlock that the protocol cannot recover from within the slot. **R-rounds do not help** — cross-round NR-lock binds the early NR-emit across all R rounds. The recommended `Δ_2 ≥ 2(D + δ)` absorbs typical mesh-jitter (up to one full `D + δ` of additional slack on top of P99 propagation) but doesn't cover wider mesh outliers. QBFT's round-reset semantics handle this case better (a flaky operator's bad PREPARE doesn't lock them across rounds); OBFTR inherits cross-phase exclusivity from TBFT and locks across rounds via cross-round exclusivity. Phase 2a/2b mitigates by deferring commitment until mesh visibility has had a full additional propagation cycle to stabilize. |
| Validity-divergence under strict host | **Out of scope** — see [Assumptions](#implications-of-validity-divergence-not-being-recovered-assumption-3); host stabilizes the verdict at Phase-1 acceptance |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (K-layer fall-through, K configurable) |
| Round-change recovery | Yes — R rounds with re-flood + Defer-state. ~Δ_round per round (~250ms typical at low D), vs QBFT's ~2s round-change. |
| Recovery scope vs QBFT | Strictly more than baseline TBFT for partition recovery (Defer state + R-round retry). View-divergence (validity-divergence and equivocation patterns that don't naturally reach qV) is out of scope — handled by assumptions 3 (host validity stabilization) and 4 (rational-byzantine deterrent), see [Assumptions](#assumptions-and-implications). Strictly better latency profile per recovery round (~10× faster). Phase 2a/2b (future improvement) would close the equivocation gap at +1 RTT. |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block, the recommended OBFTR configuration is **`K = 3, R = 2`** — `K = 3 = f+2` at f=1 satisfies both the BFT-liveness minimum (`K ≥ f+1`) and the late-leader-resilience recommendation (`K ≥ f+2`; see §Setting and §Failure modes / Late deepest-layer leader broadcast). One recovery round absorbs partition cases and equivocation patterns where ≥1 honest is in Defer at evidence-detection time, within the 4s relay cutoff. K=4 (= n) is also viable for additional fall-through depth at slightly higher onion bandwidth. **`K = 2` is the BFT-minimum *for liveness against byzantine* (one byzantine + one honest backup), but OBFTR requires `K ≥ 3` for late-leader resilience** — at `K = 2`, the deepest-layer leader's `T_broadcast_max_1` overrun lands in the Class A late-deepest-layer-broadcast failure mode (see §Failure modes) with no fall-through, even with R-round retention; `K ≥ 3` provides at least two honest leaders so a single late-broadcasting honest leader doesn't foreclose the slot. **For OBFTR deployments, `K = 2` is not a viable minimum despite satisfying the BFT-byz liveness bound; use `K ≥ 3`.**

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
| `T_broadcast_max_1` | round 1 leader broadcast deadline — `T_commit_1 − 2(D + δ)` (= 1.20s at D=100ms, δ=50ms); per-layer fetch windows fit within `[0, T_broadcast_max_1]` with `T_{K-1} < ... < T_0 ≤ T_broadcast_max_1 − Δ_1`. Bundles broadcast at this deadline have `(D + δ)` of propagation slack to all-honest first-observation by `T_commit_1 − (D + δ)`. |
| `T_accept_max_r` | round `r` receiver acceptance horizon — `T_commit_r + Δ_2 − (D + δ)`; bundles first-observed past it are auth-only-retained for round `r+1` (or rejected past the final round's horizon) |
| `T_round_r_end` | round `r` reconstruction deadline — concrete value depends on D and config; see configurations below |

Cryptographic safety (`qEnc = qV` + chained encryption + EKM-enforced cross-phase/cross-round/single-σ-V exclusivity) ensures only one block can ever get a valid validator signature, regardless of K, R, or round structure. R-round retry only enables more recovery scenarios; it cannot produce two outputs.

### Timing budget — concrete configurations

For fair comparison across configurations, all setups share a common consensus deadline `TIME_FINAL = slot_start + 3.5s` — the last round of every setup must complete by this time. Submission headroom = `4s − 3.5s = 0.5s` (2× the 250ms minimum reserved for relay submission). Each setup uses the same total consensus budget of **2.0s** (= TIME_FINAL − 1.5s start), allocated differently across rounds based on R.

For the QBFT comparison, we use **`RT_QBFT = 1s`** rather than SSV's production default of 2s. With the 2s default, QBFT(R=2)'s R2 ends at slot_start + 4.25s — past the 4s relay cutoff entirely. The 1s value lets R1's healthy 750ms complete before the timeout fires while still leaving room for R2 within the budget; deployments using SSV-default 2s timeout cannot run QBFT(R=2) at this start time. The 1s timeout is more aggressive than production: legitimate slow rounds (relay slowness, beacon hiccups) above 1s would trigger false-positive round-changes.

Common parameters: **D = 100ms (uniform across rounds), δ = 50ms, n = 4, f = 1**. Per-round window minimums: `Δ_2.5 ≥ D + δ = 150ms`, `Δ_3 ≈ 100ms` (propagation-independent — Δ_2.5 absorbs end-of-Phase-2 NR-partial propagation, so Δ_3 is purely local reconstruction processing), `Δ_reflood ≥ D + δ = 150ms`. `Δ_2` is sized per-config to absorb each setup's full budget. **Leader broadcast deadline** for round 1 is `T_broadcast_max_1 = T_commit_1 − 2(D + δ) = 1.20s` (300ms inside the nominal 0–1.5s Phase-1 fetch window); the 300ms slack between leader broadcast and `T_commit_1` is for propagation to all honest before round 1's σ-emit. **Receiver acceptance horizon** per round is `T_accept_max_r = T_commit_r + Δ_2 − (D + δ)` — accepts within-round late re-flood up through Phase 2's σ-emit-propagation feasibility limit.

**MEV-fetch-budget note.** The `T_broadcast_max_1 = T_commit_1 − 2(D + δ) = 1.20s` deadline is **300ms tighter** than the naive "Phase 1 fetch occupies 0–T_commit = 0–1.5s" reading. The 300ms gap (1.20s → 1.50s) is propagation slack between leader broadcast and all-honest first-observation under worst-case partial-synchrony; it is not extra fetch budget. Deployments comparing OBFTR to other protocols' "1.5s fetch" framing should account for this — OBFTR's primary leader has 1.20s of effective MEV-relay-fetch time at Config A, not 1.50s. The cost is unavoidable: it is the propagation budget that makes the leader's broadcast reliably observable by all honest before `T_commit_1`.

**Deadline-alignment principle.** The "phantom R2 deadline" framing (used implicitly above): R=1 setups have only an R1 deadline at `TIME_FINAL`; R=2 setups have an R1 deadline (when round 1 must terminate to leave room for round 2) and an R2 deadline at `TIME_FINAL`. By aligning the *outermost* deadline across setups, the comparison shows what each can do given identical total time. R=1 gets one big round; R=2 splits its budget across two rounds with re-flood between them.

#### OBFTR(n=4, K=3, R=1) and OBFTR(n=4, K=4, R=1)

R=1 uses the full 2.0s budget for one extended round. Phase 2 inflated to absorb late-σ-emit propagation (continuous gossipsub propagation absorbs late-arriving σ-emits up to `Δ_2 − (D+δ)` before NR-decision; the rest of the consensus phases use minimum windows).

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch | 1500ms | slot_start + 1.50s | All K leaders' fetch windows fit within 0–1.5s |
| Round 1 Phase 2 | 1750ms | slot_start + 3.25s | Δ_2 = 1.75s; **late-σ-emit budget = 1.6s** |
| Round 1 Phase 2.5 | 150ms | slot_start + 3.40s | minimum; L_C signaling |
| Round 1 Phase 3 | 100ms | slot_start + 3.50s | propagation-independent |
| Submission | 500ms | slot_start + 4.00s | 2× the 250ms minimum |

**Recovery scope.** K-layer fall-through within the single round (silent leaders absorbed via NR-quorum chain in Phase 3 reconstruction walk). The 1.6s late-σ-emit budget effectively absorbs gossipsub-mesh propagation delays up to that bound — a *soft* partition recovery without explicit re-flood. Operators receiving V late within Phase 2 σ-emit and have others observe before NR-decision. Relies on **continuous gossipsub mesh propagation** throughout the long Phase 2 window; if the mesh fails completely mid-window (e.g., key peers go offline), no automatic recovery — there's no explicit re-flood broadcast to redistribute V.

K=3: 3 fall-through layers (L_0 → L_1 → L_2). K=4: 4 layers (+L_3). Same timing — Phase 2/2.5/3 don't depend on K — but K=4 has more fall-through depth and ~3KB extra bandwidth.

**Bandwidth (healthy):** ~24 KB at K=3, ~27 KB at K=4. No round-2 overhead (single round).

#### OBFTR(n=4, K=3, R=2) and OBFTR(n=4, K=4, R=2)

R=2 splits the 2.0s budget into two rounds with a re-flood window between them. Smaller per-round Phase 2 (675ms each) but **explicit re-flood** between rounds redistributes V to operators that didn't have it.

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch | 1500ms | slot_start + 1.50s | |
| Round 1 Phase 2 | 675ms | slot_start + 2.175s | Δ_2 = 675ms; late-σ-emit budget per round = 525ms |
| Round 1 Phase 2.5 | 150ms | slot_start + 2.325s | |
| Round 1 Phase 3 | 100ms | slot_start + 2.425s | |
| Round 2 re-flood | 150ms | slot_start + 2.575s | re-flood retained Phase-1 bundles |
| Round 2 Phase 2 | 675ms | slot_start + 3.250s | |
| Round 2 Phase 2.5 | 150ms | slot_start + 3.400s | |
| Round 2 Phase 3 | 100ms | slot_start + 3.500s | |
| Submission | 500ms | slot_start + 4.00s | |

**Recovery scope.** K-layer fall-through within each round + **explicit round-2 re-flood retry**. Aggressive-marginal scenarios (some honest had V at round-1 cutoff, some didn't) recover via round-2 re-flood completing delivery, then σ-emit in round 2 with the just-arrived V. Total V-delivery absorption budget across both rounds: ~1.6s (R1's 525ms + R2's 525ms + 150ms re-flood window — operators that receive V anywhere up to ~3.1s observe it in time for R2 NR-decision). **More robust against gossipsub mesh failures** than R=1: the re-flood is a deliberate broadcast from operators that retained the bundle, re-establishing propagation if the original mesh path failed.

**Bandwidth (healthy):** ~24 KB at K=3, ~27 KB at K=4. **+~21 KB for round-2 re-flood + Phase 2'/2.5'/3'** if round 1 fails (~45–50 KB total in failure case).

#### QBFT(n=4, R=2) with `RT_QBFT = 1s`

QBFT round structure: PROPOSE → PREPARE → COMMIT → post-consensus partial-sig (4 RTTs ≈ 750ms healthy at D = 100ms). Round-change timeout reduced to 1s to fit R2 within `TIME_FINAL`.

| Path | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch | 1500ms | slot_start + 1.50s | leader fetch |
| **Healthy path (R1 succeeds)** | | | |
| Round 1 (PROPOSE+PREPARE+COMMIT+post-cons.) | ~750ms | slot_start + 2.25s | submit immediately |
| **Failure path (R1 fails → R2)** | | | |
| Round 1 timeout (round-change timer fires) | 1000ms | slot_start + 2.50s | RT_QBFT = 1s |
| Round 2 (PROPOSE+PREPARE+COMMIT+post-cons.) | ~750ms | slot_start + 3.25s | new leader proposes fresh V |
| Submission (worst case) | 750ms | slot_start + 4.00s | 0.25s natural slack before TIME_FINAL |

**R2 ends at slot_start + 3.25s** — within `TIME_FINAL = 3.5s` deadline by 0.25s. ✓

**Recovery scope.** Round-change with new leader proposing fresh V — handles partition, validity-divergence, and equivocation cases (the new leader fetches at the moved head; honest converge on the new V). Covers exactly the view-divergence cases OBFTR can't recover from.

**RT_QBFT = 1s trade-off.** SSV's production default is 2s; the 1s value used here is the smallest that keeps R1 healthy (750ms) under the timer while letting R2 fit within budget. With production 2s, R2 ends at 4.25s — past the 4s slot cutoff. With < 750ms timeout, R1 healthy itself wouldn't complete before timeout fires (false-positive round-changes on every healthy slot). The 1s value is a hypothetical for fair comparison; deployments using production 2s timeout cannot run QBFT(R=2) at start = 1.5s.

**Bandwidth (healthy):** ~14 KB. **+~14 KB for round 2** in failure case (~28 KB total).

#### Comparison summary (TIME_FINAL = 3.5s)

| Setup | Final round ends | Submission headroom | V-delivery absorption | Recovery scope | Bandwidth (healthy / failure) |
|---|---|---|---|---|---|
| OBFTR(K=3, R=1) | 3.50s | 0.50s | 1.6s (continuous mesh) | 3-layer fall-through; soft partition absorption via long Phase 2 | ~24 KB / n/a |
| OBFTR(K=4, R=1) | 3.50s | 0.50s | 1.6s (continuous mesh) | 4-layer fall-through; soft partition absorption | ~27 KB / n/a |
| OBFTR(K=3, R=2) ★ | 3.50s | 0.50s | ~1.6s (across rounds + explicit re-flood) | 3-layer fall-through + explicit re-flood retry; robust against mesh failures | ~24 KB / ~45 KB |
| OBFTR(K=4, R=2) | 3.50s | 0.50s | ~1.6s (across rounds + explicit re-flood) | 4-layer fall-through + explicit re-flood retry | ~27 KB / ~50 KB |
| QBFT(R=2, RT=1s) | 3.25s | 0.50s nominal / 0.75s actual | n/a (PROPOSE-driven) | Round-change with new V — covers view-divergence (validity, equivocation) | ~14 KB / ~28 KB |

★ = recommended default.

**Key observations:**

- **All five setups end within `TIME_FINAL = 3.5s`** with the apples-to-apples alignment. QBFT(R=2) at RT=1s has 0.25s of natural slack before the deadline; the OBFTR setups use the full budget.
- **R=1 vs R=2 OBFTR have similar V-delivery absorption** (~1.6s) but via different mechanisms. R=1's long Phase 2 absorbs delays via continuous gossipsub propagation (relies on mesh staying functional). R=2's two rounds with explicit re-flood absorb delays via deliberate retransmission (re-flood from operators that retained bundles re-establishes propagation if the mesh path failed mid-round). **R=2 is more robust against gossipsub mesh failures**; R=1 is simpler with no round-transition machinery.
- **K=4 vs K=3 trades bandwidth for fall-through depth.** Same timing fit at this D (Phase 2/2.5/3 don't depend on K). K=4 = n provides max fall-through; K=3 = f+2 is the BFT-recommended minimum.
- **QBFT(R=2) only fits with aggressive `RT_QBFT = 1s`** (vs SSV's 2s default). Recovery scope structurally covers view-divergence cases that OBFTR doesn't (validity-divergence, equivocation), via the round-change-fetches-fresh-V mechanism. Trade-off: 1s timeout is borderline-safe (legitimate slow rounds may exceed 1s, triggering false-positive round-changes).
- **OBFTR(K=3, R=2) ★ recommended** — combines BFT-recommended K with explicit round-2 retry, fits at the apples-to-apples deadline.

**At higher D (e.g., 500ms):** the comparison shifts. R=2 OBFTR's per-round windows must each grow to absorb D+δ = 550ms minimums; the 2.0s budget barely fits 2 rounds. R=1 OBFTR's extended Phase 2 still works (just absorbs proportionally less variance). QBFT's R1 healthy at 4×D≈400ms vs RT=1s still fits, but R2 timing extends past `TIME_FINAL`. **High-D networks favor R=1 OBFTR** for absorbing variance within a single extended Phase 2. See §Failure modes / Sustained partition for the high-D liveness limit.

The deadline-tuning rule: each round's `T_round_r_end − T_commit_r ≥ Δ_2 + Δ_2.5 + Δ_3` for that round, and `Δ_2 ≥ D_r + δ`, `Δ_2.5 ≥ D_r + δ`, `Δ_reflood ≥ D_r + δ` where `D_r` is the propagation budget for round `r`. Concrete numbers should come from production telemetry (P99 / P999 tails of gossip propagation, EKM signing latency, beacon submit latency, relay submission latency).

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_{L_k}` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_{L_k}`. The protocol consumes the verdict; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_{L_k})` *exactly once per slot/layer*, on the final `V_{L_k}` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_{L_k}` is locked on the originally-signed value. The layer's leader cannot subsequently emit NR/NV on `nr_tag_k` — the Phase-1 σ is their σ-side commitment per cross-phase + cross-round exclusivity.

**Receiver-side validity stabilization — host best-effort toward assumption 3.** OBFTR requires host validity to be unanimous at decision time (see [Assumptions](#assumed)). For SSV's proposer duty, the host narrows the divergence window via per-operator validity stabilization: each operator runs the validity check (including `parent_root`-vs-head) **once at Phase-1 acceptance** against a stable head snapshot, then locks the verdict for the remainder of the slot. Subsequent head movements within that operator do not flip the verdict.

**The validity-locking window spans the full R-round absorption window, not just `D + δ`.** Operators accept Phase-1 bundles anywhere across the R-round acceptance horizons (within-round at `[slot_start, T_accept_max_r]` + cross-round retention extending to `T_accept_max_R`). Each operator locks their verdict at first-observation, so the cluster-wide spread of lock-times equals the total absorption window. At OBFTR(R=2) Config A with recommended per-round `Δ_2 = 2(D + δ)`, this is ~1150ms — substantially wider than what a naive "narrow gossipsub-acceptance window ≈ D+δ" reading would suggest. A re-org landing anywhere inside this window can split honest verdicts across the boundary; the wider window means a proportionally higher rate of validity-divergence slot-misses for the same re-org distribution.

**Δ_2 (per-round) and R sizing have competing pressures.** Wider per-round `Δ_2` and/or larger R:
- ✓ Wider absorption window → more partition recovery in-protocol.
- ✓ More processing-delay margin per round.
- ✗ Wider validity-locking window → proportionally higher validity-divergence rate for the same re-org distribution.
- ✗ Less submission headroom and (for Δ_2) less leader fetch budget.

Deployments choosing `Δ_2` and R should weight these per their re-org rate and partition-tail observations. The recommended `Δ_2 = 2(D + δ)` per round at R = 2 is a balanced default; deployments under low re-org rate but wider partition tails may go higher; deployments with high re-org rate but tight partition tails may go lower.

**This per-operator locking does NOT guarantee cluster-wide convergence.** Operators accept Phase-1 bundles at different `t` within the gossipsub propagation window (operator-local first-observation timestamps, plus clock skew δ). If a re-org happens during this acceptance window, operator A may snapshot pre-reorg (verdict = valid) while operator B snapshots post-reorg (verdict = invalid). Both locks hold; cluster views diverge. The locking ensures *per-operator* stability across the slot, narrowing the divergence window to "re-org during gossipsub-acceptance window" — typical D ≈ 100–500ms — but does not eliminate it.

When the residual window fires (re-org lands inside the acceptance window AND operators split across the boundary), assumption 3 is violated and the slot may miss in-protocol — neither σ-quorum (capped at honest-σ-validators + leader's σ_L^V) nor NR-quorum (capped at honest-NV-count) reaches threshold under adversarial byz that withholds NR. This deadlock is the cost of strict re-validation. The looser alternative (validate once at acceptance, never re-check) avoids the in-protocol deadlock but commits on a V whose parent may become orphaned (relay/beacon submission rejection at submit time, also a slot miss). Hosts pick between the two failure modes based on observed re-org rates.

The "permit and slot-miss" framing parallels OBFTR's equivocation handling: validity-divergence is a view-divergence pattern that the protocol does not recover from. Unlike equivocation, it's not attributable to anyone (re-orgs are real-world events, not protocol violations), so no slashing applies; it relies on assumption 3 being approximately true.

**Backup-leader re-org resistance.** Fetching `V_{L_1}` from a deeper-confirmed parent (the `T_1 < T_0` asymmetric schedule already accommodates this) reduces the likelihood that L_1's parent becomes orphaned. Backup is structurally re-org-resistant by construction.

Implementation notes:

- Each operator validates `parent_root` against a stable head snapshot taken at Phase-1 acceptance time; the verdict is locked into the operator's local state for the remainder of the slot.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" and "single V per operator per layer" — a second signing attempt with a different V at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing AND operator-side double-signing detectable cluster-wide.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same. **Same DKG cost as baseline TBFT**.

2. **Per-round deadline coordination.** Clock skew across operators must be bounded by `δ` and known. Two distinct per-round deadlines (do not conflate):

   - **`T_broadcast_max_1 = T_commit_1 − 2(D + δ)`**: round-1 leader broadcast deadline. Each layer's leader must broadcast by this time so all honest first-observe by `T_commit_1 − (D + δ)` under worst-case propagation. Per-layer fetch windows fit within `[0, T_broadcast_max_1]`.
   - **`T_accept_max_r = T_commit_r + Δ_2 − (D + δ)`**: per-round receiver acceptance horizon. Receivers accept bundles first-observed in `[slot_start, T_accept_max_r]` for round r. Bundles past `T_accept_max_r` are auth-only-retained for round r+1 (or rejected past the final round's `T_accept_max_R`).

   Per-round window minimums:

   - **`Δ_2 ≥ D + δ`** (BFT-minimum) for each round's Phase-2 window: every honest's Phase-2 σ-emit at the start of the window must arrive at every other honest by `T_commit_r + Δ_2` (the NR-decision time / start of Phase 2.5). **`Δ_2 ≥ 2(D + δ)` is recommended** to widen the per-round receiver acceptance horizon and the late-σ-emit propagation window by `D + δ`, giving meaningful within-round partition recovery.
   - **`Δ_2.5 ≥ D + δ`** for each round's Phase-2.5 window: L_C claims propagate cluster-wide AND end-of-Phase-2 NR-partials propagate to all honest before Phase 3 reconstruction starts.
   - **`Δ_reflood ≥ D + δ`** between rounds: re-flood at round `r+1` start must complete before round `r+1`'s cutoff.

   All are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

3. **Choosing K (layer count).** K is per-duty:

   - `K = 2`: minimum (one primary + one backup). Matches baseline TBFT's onion structure. Fits SSV proposer duty's 4s budget with margin. Recovery scope: handles 1 byzantine leader at L_0, with L_1 as backup.
   - `K = 3..n`: larger K provides additional fall-through layers for non-byzantine multi-failure (rare events: relay timeouts, network jitter, validity divergence at multiple layers). At `n = 4`, max useful K is 4 (one layer per operator). Each extra layer adds a level of chained encryption (~1 KB per onion at K=3, ~2 KB at K=4 — within practical bandwidth).

   **Two K bounds (see §Setting for the full discussion):**
   - **`K ≥ f+1`** — BFT-liveness minimum (≥ 1 honest leader exists by pigeonhole over the f-byz bound).
   - **`K ≥ f+2`** — late-leader-resilience recommendation (≥ 2 honest leaders, so a single late-broadcasting honest leader doesn't foreclose the slot via the cross-round NR-lock pathology — see §Failure modes / Late deepest-layer leader broadcast).

   Recommended: **`K = f+2` for proposer duty** (`K=3` at f=1; `K=4` at f=2; `K=5` at f=3); `K ≥ f+3` for non-proposer duties (attestation, sync committee, DKG) where additional multi-failure tolerance is desired or budgets are looser.

4. **Choosing R (round count) and RT (round timeout).** R is per-duty, governed by the duty's deadline budget:

   - **Proposer duty** (4s relay cutoff): R = 2 fits cleanly. Round 1 = ~3.5s (full Phase 1 → 3); round 2 = ~250ms (re-flood + Phase 2'+2.5'+3'). Submission window: ~250ms.
   - **Attestation duty** (12s slot, 16s aggregation cutoff): R = 3..5 fits comfortably. More rounds extend partial-synchrony tolerance.
   - **DKG / non-time-critical duties**: R can be very large (e.g., R = 10) since deadline budget is generous.

   The `R · D` propagation tolerance is the protocol's effective resilience knob. Increasing R extends recovery scope at the cost of slot-budget consumption.

5. **Tag construction and replay.** Each NR tag must uniquely bind `(slot, cluster, layer)` to prevent replay across slots/layers/clusters. Structure: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`.

6. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one OBFTR instance and assumes:
   - Single OBFTR instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between OBFTR and any other path that signs against the V-signing share.
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction across all rounds) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k` across all rounds), not just submission.

7. **Equivocation is permitted, not recovered.** OBFTR does not provide an in-protocol equivocation recovery mechanism. Some patterns reach qV naturally (when 2-of-3 honest happen to σ-commit on the same V at f=1 n=4; leader's σ_L^V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. Equivocation is treated as a slashable byzantine fault; the rational-byzantine deterrent (assumption 4) makes this tolerable across many slots. The TBFTR-style **Phase 2a/2b split** (broadcast-only Phase-2a, then σ-emit on a deterministically-chosen V in Phase-2b after Phase-2a observation completes) is the structurally correct full fix; +1 RTT per round (total ~R·(D+δ) per slot), preserves cryptographic safety AND f-tolerant liveness. Documented as future improvement, not in current OBFTR.

## Where this came from

OBFTR is the result of a design exploration starting from baseline [TBFT](TBFT.md), prompted by the question: can TBFT be extended with rounds to recover from network partitions — *without* QBFT-style leader rotation (which is heavy: ~2s round-change overhead) and *without* Phase 2.5-style cross-signing (which requires EKM relaxation and cross-signer-exclusion logic)?

The answer was two additions to baseline TBFT, both preserving cryptographic safety (no honest-aggregation-rule dependency):

1. **Deferred-NR commitment** (Defer state): operators don't NR-emit on cutoff if they've observed peer σ-emit cluster-wide — they wait, hoping re-flood completes in a later round. This recovers aggressive-marginal failures (>1 of n−f honest missing re-flood at round-1 cutoff) without breaking cross-phase exclusivity.
2. **L_C cluster-consensus** (`KindLCClaim`): operators broadcast their local view of the cluster's frontier layer; qV agreement promotes L_C cluster-wide, accelerating round transitions and saving bandwidth on dead layers.

An earlier draft of OBFTR included a **winner-completion rule** for equivocation recovery: Defer-state honest who observed equivocation σ-emitted on the V with the lowest `value_root` hash among retained Phase-1 bundles. The rule was removed after analysis showed: (a) under the retention bound (2 distinct V's per `(slot, layer, leader)`), byzantine flooding of 3+ V's with adversarial gossipsub-ordered delivery can produce asymmetric retention sets where no V is the argmin in 2+ honest's local sets — divergent winners, no σ-quorum, slot misses; (b) the recovery scope was already partial (only Defer-state honest, not σ-locked honest) — patterns like 1-1-1 split (each honest σ-commits on receipt of a different V) were never covered. Rather than retain a partial mechanism with caveats, OBFTR now treats equivocation as a permitted-but-slashable pattern: some splits succeed naturally (when 2-of-3 honest happen to σ-commit on the same V), others slot-miss. The rational-byzantine deterrent (assumption 4) makes this tolerable across many slots.

An even earlier draft included a **skip mechanism** (separate `skip_tag_k` IBE tags, chained-OR encryption, persistent-divergence trigger) intended to also recover validity-divergence under strict host policy. That mechanism was removed after audit found it relied on an "effective σ-pool" honest-aggregation rule rather than a cryptographic property — a byzantine offline aggregator could ignore the rule and produce a second V signature from raw σ partials. Validity-divergence recovery within a slot is no longer an OBFTR goal — the design assumes the host stabilizes the verdict at Phase-1 acceptance time as a best-effort host-side mechanism (assumption 3 in [Assumptions](#assumed)).

**Path forward — Phase 2a/2b is the family-level structural fix.** Phase 2a/2b is not OBFTR-specific — it comes from [TBFTR](TBFTR.md) and addresses a TBFT-family limitation (validity-divergence + adversarial byzantine deadlock) that current TBFT and OBFTR both inherit. The TBFTR-style Phase-2 split (broadcast-only Phase-2a where operators re-flood retained Phase-1 bundles without σ-emitting, then Phase-2b where σ-emits happen on a deterministically-chosen V after Phase-2a observation completes) is the structural fix for the limitations that current TBFT and OBFTR document in their respective failure-mode sections. **Smaller variants (relax cross-phase exclusivity, allow NV → σ flip via aggregator filtering, separate cryptographic tags for tentative-vs-final commitment, etc.) all either break safety against an offline-aggregating byzantine — the failure mode of the earlier "skip mechanism" draft — or are isomorphic to Phase 2a/2b under a different name.** Phase 2a/2b's structural shape is essentially forced: synchronous consensus on validity (or σ-commitment more generally) requires explicit cluster-wide coordination, which means an observation phase before commitment.

The mechanism — deferring σ-commitment until after cluster-wide Phase-2a observation — has two main effects on the failure-mode taxonomy:

1. **Recovers the Class A validity-divergence deadlock** (assumption 3 violated by re-org during acceptance window). Phase-2a's observation step lets operators see cluster-wide σ-eligibility state and converge on a stabilized validity verdict; Phase-2b σ-emit happens on that verdict rather than each operator's local at-acceptance snapshot. This brings validity-divergence into recovery scope rather than leaving it as an out-of-scope assumption violation. (The "leader σ_L^V locking on stale V" concern collapses into this same fix — without a Phase-1 σ_L^V, the leader doesn't pre-lock; Phase-2b's σ-emit is on the post-observation stabilized V.)

   **Same fix also resolves the Class A late-deepest-layer-leader-broadcast NR-lock at K=2** (see §Failure modes). Without Phase-1 σ_V from the leader, no early operator-side σ/NR commitment; a late-arriving Phase-1 bundle observed in Phase-2a is σ-emittable in Phase-2b without any pre-locked NR commitments to override.

2. **Removes the Class B byzantine grief surface** for byzantine actions that exploit early σ-commitment:
   - *Equivocation σ-locked split patterns* (1-1-1, 1-1-Defer, all-pattern). By deferring σ-emit, no honest carries an "initial" σ partial on a non-winner V; single-σ-V exclusivity stays intact; Pigeonhole 2 holds; at most one V reaches qV cluster-wide *regardless of equivocation pattern*.
   - *Byzantine selective-delivery grief* (h_V = 1 deadlock). Phase-2a's observation establishes cluster-wide σ-side eligibility before any σ-commitment; byz cannot manipulate h_V to 1 because honest don't σ-commit until Phase-2b sees the post-observation pool.
   - *Byzantine refusal coordinated with honest transient flakiness* (the cross-round σ-or-NR transient-error lock-in pattern). Honest operator who would NR in round r due to transient flakiness can defer their commitment to Phase-2b; if the condition resolves before Phase-2b, they σ instead of NR-locking themselves out for the slot. Without that early NR-commitment, byz's σ-refusal cannot complete the deadlock.

   These Class B grief patterns are currently "permitted" because they're eventually accountable via rational-byzantine deterrent, but with weak (gossipsub-pattern / operator-unreliability) slashability. Phase 2a/2b removes the protocol-level grief surface so the deterrent doesn't need to do this work.

3. **Bonus: improves per-operator participation efficiency** under transient errors — operators with brief flakiness during Phase 2 don't lose their slot contribution. At f=1 n=4 this is mostly an across-many-slots efficiency gain (the per-slot σ-quorum often reaches without the flaky operator's contribution); at higher f it matters more per-slot.

Costs **+1 RTT per round** — each round's Phase 2 grows by ~D+δ for the Phase-2a observation window, so total per-slot cost is ~R·(D+δ) (e.g., +2(D+δ) at R=2 for proposer duty). Preserves cryptographic safety AND f-tolerant liveness — strictly better than the qV-bump alternative which trades f-tolerance for safety.

**Status: near-term for any deployment under realistic adversarial conditions.** Current OBFTR (without Phase 2a/2b) trades the recovery properties above for spec simplicity and the +1-RTT-per-round savings. The trade-off is acceptable when **all three** hold:

1. The deployment's re-org rate is low enough that assumption 3 (host validity unanimous at decision time) holds in practice (re-orgs during gossipsub-acceptance window are sufficiently rare).
2. Byzantine operators value future participation enough that assumption 4 (rational-byzantine deterrent) is *quantitatively* effective, not just *qualitatively* present — stake-to-grief-value ratio is high, governance is responsive, slashability evidence quality is strong (the cryptographic-self-contained slashing rules cover most byzantine fault classes the deployment is exposed to).
3. The cluster's coordination SLA is short enough that across-slot accountability bounds grief faster than byz can re-enter.

For deployments where any of these is weak — small clusters, transient operators, weak governance, high-stake-to-grief-value MEV proposer slots, low-evidence-quality fault classes (selective-delivery, mesh-flakiness-correlated NR-refusal) — **Phase 2a/2b should be considered near-term, not future**. The +1-RTT-per-round cost (e.g., +2(D+δ) at R=2 for proposer duty, ~300ms at D=100ms) is small relative to the slot budget and substantially smaller than the cost of running adversarial-byz exposure under the bare protocol.

**This is a more aggressive framing than "future improvement."** Bare OBFTR(R≥2) is the spec-richest *multi-round point*; OBFTR + Phase 2a/2b is the more robust *production* point for adversarial deployments and arguably should be the recommended configuration for SSV proposer duty unless deployment conditions explicitly support assumption 4 strength.

The result is a protocol that **strictly improves on baseline TBFT's recovery scope** for partition cases (Defer state + R-round retry) at TBFT's healthy-path latency (2 RTTs). It does **not match QBFT's full recovery scope** — Class A failure modes (assumption violations) and Class B byzantine grief patterns are out of scope, handled by assumptions 3 (host validity stabilization) and 4 (rational-byzantine deterrent) respectively. The R (round count) parameter is the partial-synchrony tolerance knob; K (layer count) is the multi-failure-fall-through depth knob; **Phase 2a/2b is the recovery-scope expander — it converts the Class A validity-divergence failure mode into a recovered case and removes the Class B grief surface, at +1 RTT per round.**

OBFTR generalizes baseline TBFT (`R=1, K=2`) and incorporates structural ideas from TBFTR's chained encryption (used unchanged at K > 2) and bid-routing variants (which OBFTR can compose with as host-supplied leader-determination).

## Appendix A — Protocol comparisons

This appendix gives high-level structural comparisons against the protocols OBFTR relates to: baseline [TBFT](TBFT.md) (the special case `R=1, K=2`), [TBFTR](TBFTR.md) (the K-generic generalization with Phase-2-split machinery), and QBFT (SSV's existing consensus protocol).

### A.1 — Comparison with baseline TBFT

OBFTR is a **strict superset** of baseline TBFT: at `R=1, K=2`, OBFTR reduces exactly to TBFT. The added machinery (Defer state, R-round retry, L_C consensus) extends partition-recovery scope without affecting baseline's properties.

| Aspect | Baseline TBFT | OBFTR (R=2, K=2) |
|---|---|---|
| Phase 1 bundle | `(V, σ^V_L, σ^op_L(envelope))` | Same |
| Phase 2 onion at layer k | `E_{nr_tag_0}(σ_i^V(V_{L_k}))` (single-tag IBE at K=2) | Same |
| Operator commitment states | σ, NR, NV (3 states) | σ, NR-silent, NV, **Defer** (4 states; Defer is OBFTR's addition) |
| Tag count per slot | 1 (`nr_tag_0`) | Same |
| Phase 2.5 | n/a | `KindLCClaim` message kind for round-transition coordination |
| Round structure | Single-shot (R = 1) | R rounds with re-flood retry |
| Equivocation handling | Honest who detect equivocation emit NR; single-V receivers can deadlock | Detect-and-slash; honest stay in current commitment state (no winner-pick). Some patterns reach qV naturally (when leader-σ_L^V completes a 2-of-3-honest pool); others slot-miss. Equivocation slashable; rational-byzantine deterrent (assumption 4) makes this tolerable. Phase 2a/2b (future improvement) closes the gap. |
| Aggressive marginal (>1 of 3 honest miss re-flood) | Slot misses | Recovered via Defer state in round 2 |
| Validity-divergence under strict host | Slot misses | Out of scope — assumed unanimous (see [Assumptions](#assumed)) |
| Healthy-path latency | 2 RTTs | 2 RTTs (unchanged) |
| Failure-recovery latency | n/a (slot misses) | ~Δ_round per round (~250 ms typical) |
| Bandwidth (healthy, n=4) | ~21 KB | ~22 KB (+~1 KB for L_C signaling) |
| Bandwidth (round 2, n=4) | n/a | +~21 KB for round 2 re-flood + Phase 2'+2.5'+3' |
| DKG ceremony | 2 keypairs (V-share, IBE-share) | Same |

**Migration path**: a cluster running baseline TBFT can adopt OBFTR incrementally by enabling the optional features: (1) Defer-state rule (decision-time only; no wire change at K=2 since layer-0 σ is plaintext); (2) round-2 retry; (3) `KindLCClaim` message kind for round-transition acceleration. Each is independently useful. For full equivocation coverage, **Phase 2a/2b** (TBFTR-style Phase-2 split) is needed — costs +1 RTT per round (total ~R·(D+δ) per slot) but preserves cryptographic safety AND f-tolerant liveness. The Phase-1 protocol-tag (`OBFTR-v1` vs `TBFT-v1`) provides envelope domain separation.

### A.2 — Comparison with TBFTR

[TBFTR](TBFTR.md) is the K-generic spec with V-plaintext + Phase-2-split machinery, designed for `n ≥ 7`. OBFTR and TBFTR share the K-layer onion structure and chained encryption, but diverge on recovery mechanism:

| Aspect | TBFTR | OBFTR |
|---|---|---|
| K | K-generic (typically `K = ⌈n/2⌉`) | K-generic, configurable per-duty (recommended K=2 for proposer, K≥3 for non-proposer) |
| Phase 2 split | 2a (onion only) + 2b (late σ + NR) | Single Phase 2 + Phase 2.5 (L_C signaling) |
| V-plaintext at deeper layers | Yes — onion carries `V ‖ C_k(σ_partial)` | No — only σ partial encrypted (V_{L_k} learned via Phase-1 broadcast retention; not in onion) |
| Recovery mechanism | Phase 2b late σ-emit (operators who recovered V via peer onions) | Defer state + R-round retry; equivocation not recovered in-protocol |
| Equivocation single-V | Phase-2 split recovers all patterns | Not recovered in-protocol; some patterns reach qV naturally (2-of-3 honest happen to σ-commit on same V); rest slot-miss (equivocation slashable) |
| Round structure | Single-shot per slot (no rounds) | R rounds (configurable) |
| Bandwidth | Larger (V-plaintext per layer × n operators) | Smaller per-onion; +R-round retry overhead on demand |

**OBFTR replaces TBFTR's Phase-2-split with rounds**, achieving similar recovery scope for partition cases with cleaner spec structure. The R-round structure subsumes Phase-2b's "late σ-emit" via the same mechanism (re-flood + Defer transition to σ across rounds). For full equivocation recovery, OBFTR can adopt the TBFTR-style Phase-2 split as a future improvement.

For new SSV deployments, OBFTR supersedes TBFT and TBFTR. TBFTR remains a useful reference for the K-generic onion structure analysis.

### A.3 — Comparison with QBFT

QBFT is SSV's existing consensus protocol. The key structural difference: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus partial-sig collection)**; OBFTR (and TBFT) fuses the two by embedding partial signatures inside Phase-2 onions. Most observable trade-offs trace back to this structural difference.

For per-scenario liveness behavior (recovery scope, mechanism, outcome) see [Liveness comparison: OBFTR vs QBFT](#liveness-comparison-obft-vs-qbft). This appendix covers the structural / cost dimensions: protocol shape, latency, bandwidth, safety posture, primitive complexity, and deployment maturity.

| Aspect | QBFT | OBFTR (R=2, K=2 for proposer) |
|---|---|---|
| Protocol shape | Multi-round (PROPOSE → PREPARE → COMMIT) with per-round leader rotation; round-change on timeout | R-round, K-layer onion fall-through; round transitions via timer or L_C consensus |
| Consensus and signing | Decoupled: consensus reaches a decided value, then a separate post-consensus phase collects 2f+1 partial sigs | Fused: Phase-2 onions carry σ partials directly, Phase 3 reconstructs |
| RTTs to signed output (min, healthy) | 3 (consensus) + 1 (post-consensus) ≈ 4 | 2 (Phase 1 + Phase 2) |
| Termination guarantee | Eventually-terminating across rounds (under partial synchrony) | Conditional on `R · D ≥ real_propagation`; tunable via R |
| Safety posture | Honest-majority (`2f+1` honest) + correct quorum-certificate verification | Honest-majority cryptographic via `qEnc = qV = 2f+1` + chained IBE + EKM-enforced per-operator commitments. Same trust posture as QBFT — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). |
| Bandwidth (1 round, healthy n=4) | ~14 KB | ~22 KB |
| Bandwidth (1 round failure n=4) | +12 KB per round + a full additional round on top | +~21 KB for round 2 re-flood (only if round 1 failed) |
| Latency (healthy, n=4) | ~750 ms | ~250 ms (Config A) — see [Timing budget](#timing-budget--concrete-configurations) for higher-D configurations |
| Latency (1 round failure, n=4) | ~3.0 s (round-1 timeout 2s + round-2 success ~750 ms) | ~500 ms (Config A: round 1 fail at ~250 ms + round 2 succeed at ~250 ms) |
| Latency (2 round failures, n=4) | Misses 4s relay cutoff | ~750 ms (Config A; round 1, 2 fail, round 3 succeed if R ≥ 3) |
| Slashing-protection scope | Single block sig per slot, gated at submission time | Multiple per-share sigs per slot at candidate-signing (Phase-1 leader, Phase-2 onion across rounds) gated by EKM cross-keypair coordination — see [EKM coordination model](#ekm-coordination-model) |
| Cryptographic primitive | BLS threshold signatures only | BLS threshold signatures + threshold IBE / SWE (drand/tlock-style) |
| Application contract | Decides on any value the application proposes | K-layer with primary + backup-leader fetches |
| Production maturity | What SSV runs today; corner cases hit and fixed | New codebase; deployment confidence to be derived |

**Cost-side trade-off summary:**

- **Latency.** OBFTR wins on every dimension at Config A: ~3× faster healthy, ~6× faster on round-1 failure. At Config B (D = 500ms), OBFTR's advantage shrinks because per-round windows must scale with D, but R = 2 still wins on round-1-failure recovery if it fits the budget.
- **Bandwidth.** Comparable healthy-path; OBFTR slightly higher due to onion encryption. Round-failure bandwidth: OBFTR lower per recovery round (~21 KB vs QBFT's ~12 KB per round + full additional consensus round).
- **Cryptography.** QBFT only needs BLS threshold signatures. OBFTR additionally needs threshold IBE / SWE (drand/tlock-style; audited, deployed since 2023). The IBE primitive is more novel; for risk-averse deployments, this is a real consideration.
- **Spec surface.** OBFTR is meaningfully larger spec than baseline TBFT (new states, new message kinds, R-round structure). Comparable to QBFT once you account for QBFT's view-change protocol, prepared-certificate verification, etc.
- **Maturity.** QBFT is production. OBFTR is a new codebase — deployment confidence has to be derived.

**Where QBFT genuinely wins for proposer duty:**

- **Validity-divergence recovery.** The concrete scenario: head re-org mid-slot invalidates parent_root for L_0 candidate; honest verdicts genuinely diverge. QBFT round-changes through with new leader fetching at new head. OBFTR requires the host to stabilize the verdict at Phase-1 acceptance (assumption 3) — sufficient when re-orgs are rare relative to the slot budget, but can lead to submission rejection if the locked-on parent later becomes orphaned.
- **1-1-1 equivocation recovery.** QBFT's round-change with new leader proposing a fresh V breaks the deadlock. OBFTR relies on the rational-byzantine deterrent (assumption 4) — the byzantine pays in future slots, but the affected slot misses.
- **Cryptographic primitive simplicity.** BLS-only, no IBE.
- **Production maturity.** QBFT is what SSV runs today.

**Where OBFTR wins:**

- **Healthy-path and recovery latency.** Round-overhead at Config A is ~250ms; QBFT round-change is ~2s. OBFTR can fit ~12 recovery rounds in the 4s relay cutoff that QBFT fits ~2.
- **Network-partition recovery and adversarial scheduling.** OBFTR's R configurable; QBFT's round-budget capped by RT × R ≤ deadline.
- **Multi-leader-failure recovery.** OBFTR's K-layer parallel fall-through resolves K-1 silent layers within a single round; QBFT round-changes K-1 times serially. For K=3 with 2 silent leaders, OBFTR recovers in ~500ms; QBFT in ~5s.
- **All-Defer equivocation recovery** (byz delivers V's early enough for re-flood to spread conflicts before σ-emit; all honest land in Defer-due-to-equivocation). Round-R force-NR produces NR-quorum at L_0 → fall-through to L_1 (~RT × R, ~500ms at Config A); QBFT round-change (~2s). σ-locked split patterns where byz delivers near end-of-Phase-1 are not recovered by either protocol uniformly — see [Liveness comparison: OBFTR vs QBFT](#liveness-comparison-obft-vs-qbft).
- **Configurable per duty.** OBFTR's R and K knobs let operators tune per-duty (proposer = `R=2, K=2`; attestation = `R=4, K=3`; DKG = `R=10, K=n`); QBFT has a single round-timeout knob.

The operational bottom line: OBFTR decisively wins on common-case latency and partition-class recovery; QBFT wins on validity-divergence and 1-1-1-equivocation recovery (rarer modes that OBFTR addresses via assumption 3 and assumption 4 respectively). For SSV proposer duty under a 4s relay cutoff, the choice depends on observed re-org rate, cluster's tolerance for the 1-1-1 equivocation case via the rational-byzantine deterrent, and the relative weight of common-case latency vs. worst-case coverage.

## Appendix B — L_Bid mini-consensus extension

This appendix specifies an opportunistic bid-routing extension to OBFTR. **L_Bid** is a bid-determined top layer prepended to OBFTR's rotation-determined K layers (yielding a `K' = K + 1` configuration). The extension adds a **slot-level mini-consensus phase** between Phase 1 and the first round's Phase 2 that resolves L_Bid's identity cluster-wide before σ-commitment. The mini-consensus is QBFT-style in spirit (1 round of verdict broadcast + convergence on a quorum-determined value) but scoped narrowly to L_Bid identity selection — it does not bind threshold partials and has no round-change machinery; failure of the mini-consensus cleanly falls through to L_0.

The mini-consensus closes the C1 (selective bid-withholding), C2 (bidder equivocation), and C3 (validity-divergence majority) deadlock surfaces that bare-TBFT-style B.3 sketches in [TBFT.md](TBFT.md) leave open. It introduces two narrower residual surfaces at L_Bid (2-1-byz-defect, verdict-equivocation) inherited from the convergence-rule structure.

OBFTR's R-round retry semantics apply at L_Bid + the rotation layers in the standard way: L_Bid σ-or-NR commit happens at each round per OBFTR's per-round Phase 2, with cross-round σ-or-NR exclusivity. The mini-consensus itself runs **once per slot** (at slot start, between Phase 1 and round-1 Phase 2) — V_LBid is fixed for the slot and reused across all R rounds.

### When to use it

**Suited for**: deployments where MEV bid-routing upside is significant relative to (a) the +1 RTT slot-budget cost of the mini-consensus phase and (b) the new adversarial-byz residual surfaces at L_Bid. For SSV proposer duty under Config A: high-MEV slots where bid-routed block value-capture exceeds slot-loss-rate cost from new failure modes.

**Not suited for**: deployments prioritizing minimum slot latency (the +1 RTT consumes round budget that would otherwise be available for retry rounds) or where adversarial-byz at L_Bid is a hard constraint (the new residuals are slashable but slot-miss without fall-through; see [§Liveness](#liveness)).

### Setting

Adds to OBFTR's setting:

- **K' = K + 1 layers**: L_Bid (top, bid-determined) + OBFTR's rotation-determined L_0, ..., L_{K-1}.
- **Bid envelopes**: every operator broadcasts a bid envelope at slot start.
- **Mini-consensus window** `Δ_minicon`: new phase between Phase 1 and round-1 Phase 2.
- **L_Bid σ-eligibility**: determined cluster-wide by mini-consensus, fixed for the slot (used across all R rounds).
- **R rounds**: OBFTR's standard multi-round structure applies. Each round has Phase 1 + Phase 2 + Phase 3-and-L_C-signaling per OBFTR base. The mini-consensus phase is **slot-level, not per-round** — V_LBid is determined once at slot start.

`qV = qEnc = 2f+1` and the BLS+IBE keypair structure are unchanged from OBFTR base.

### Wire kinds

In addition to OBFTR's wire kinds (`Phase1Bundle`, `KindOnion_r`, `KindNR_r`, `KindLCClaim`, `KindCertificate`):

- **`KindBid`** (new): operator `i`'s bid envelope. Payload `(protocol_tag = "OBFTR-LBid-v1", message_kind = "bid-envelope", cluster_id, slot, operator_id i, bid_value, V_i, relay_attestation)`, signed by `i`'s operator-identity key.
- **`KindBidVerdict`** (new): operator `i`'s mini-consensus verdict. Payload `(protocol_tag = "OBFTR-LBid-v1", message_kind = "minicon-verdict", cluster_id, slot, operator_id i, predicted_LBid_value_root_or_null)`, signed by `i`'s operator-identity key.

Per-round `KindOnion_r` and `KindNR_r` carry σ-or-NR commitments at all K' layers per round.

### Per-layer windows and deadlines

Slot timeline:

| Phase | Window | Activity |
|---|---|---|
| Phase 1 fetch | `[slot_start, T_broadcast_max]` | Operators fetch V_i; rotation leaders prepare Phase-1 bundles; all operators prepare bid envelopes |
| Phase 1 broadcast | `[T_broadcast_max, T_commit_1]` | Rotation leaders broadcast Phase-1 bundles; all operators broadcast `KindBid`. Propagation slack `D + δ` lets bundles reach all honest by `T_commit_1`. |
| **Mini-consensus** | `[T_commit_1, T_commit_1 + Δ_minicon]` | Bid-envelope re-flood + `KindBidVerdict` broadcast. V_LBid resolved (or null) by end of window. **Slot-level — does not repeat per round.** |
| Round 1 Phase 2 | `[T_commit_1 + Δ_minicon, T_commit_1 + Δ_minicon + Δ_2]` | σ-or-NR commit at all K' layers for round 1 |
| Round 1 Phase 3 + L_C | `[T_commit_1 + Δ_minicon + Δ_2, T_commit_2]` | Round-1 reconstruction walk + L_C signaling for round transition |
| Round 2..R Phase 2 | per-round windows | σ-or-NR commit per round at all K' layers; cross-round σ retention applies |
| Round 2..R Phase 3 + L_C | per-round windows | Reconstruction walk + L_C signaling |
| Round R end | `T_round_end` | Final reconstruction; slot succeeds or misses |

Sizing:
- `Δ_minicon ≥ D + δ` (verdicts must propagate within window).
- **Recommended** `Δ_minicon = 2(D + δ)` for jitter absorption.
- Per-round `Δ_2`, `Δ_3`: same as OBFTR base.

At Config A (D=100ms, δ=50ms, R=2, recommended sizing): mini-consensus = 300ms (slot-level, once). Each round ≈ 500-650ms. Total slot budget post-`T_commit_1` ≈ Δ_minicon + R × per-round-budget ≈ 300 + 1100 = 1400ms at R=2. Within SSV's 4s relay cutoff.

### Protocol

#### Phase 1 — Bid + rotation leader broadcast

Each operator `i` (regardless of whether they're a rotation leader at any round):
1. Fetches V_i (relay or vanilla) with bid_value.
2. Constructs `KindBid` envelope; signs with operator-identity key; gossips.

In parallel, each rotation leader L_k for k ∈ {0, ..., K-1} broadcasts their Phase-1 bundle as in bare OBFTR. **Coherence rule**: rotation leader's Phase-1 bundle V matches their bid envelope V_i (single-V-per-operator-per-slot).

Receivers retain bid envelopes per `(slot, operator_id)`; second distinct `KindBid` from same operator is bid-equivocation (Rule 7), slashable.

#### Mini-consensus — verdict broadcast and L_Bid resolution (slot-level)

Same as OBFT's mini-consensus phase (see [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)):

1. Each operator computes `bid_set_i` from received-and-validated bid envelopes by `T_commit_1 + Δ_minicon − (D + δ)`.
2. If `|bid_set_i| ≥ n − f` AND optional parent-root filter passes: `predicted_LBid_i = argmax over bid_value` (op_id tiebreak).
3. Else: `predicted_LBid_i = null`.
4. Broadcast `KindBidVerdict` envelope at the latest-safe time within the window.
5. Verdict equivocation (a second distinct verdict from same operator) is slashable Rule 8.

**Convergence rule**: at mini-consensus end, compute `verdict_pool[V] = | { distinct ops broadcasting first-observed verdict on hash(V) } |`. If `∃V : verdict_pool[V] ≥ qV`, that V is V_LBid for the slot. Else: no L_Bid; cluster falls through to L_0.

V_LBid is **fixed for the slot** — it does not change across rounds. All R rounds use the same V_LBid (or "no L_Bid" if mini-consensus failed).

#### Phase 2 (per round) — σ-or-NR commit at K' layers

Per OBFTR's standard per-round Phase 2, with K' layers:

- **L_Bid**:
  - V_LBid resolved AND operator retains V_LBid locally AND host re-validates valid: emit plaintext σ partial `σ_i^V(V_LBid)` at L_Bid in this round's `KindOnion_r`.
  - Else: emit NR partial `σ_i^{IBE}(nr_tag_LBid)` in this round's `KindNR_r`.
  - **Cross-round σ-or-NR exclusivity at L_Bid**: an operator who σ'd on V_LBid in round r is σ-locked at L_Bid for the entire slot; cannot NR in round r+1. EKM enforces.
- **L_0, ..., L_{K-1}** (rotation layers): same as bare OBFTR's per-round Phase 2 logic, with chained encryption updated to include `nr_tag_LBid` at the outermost wrap (analogous to OBFT's adaptation).

#### Phase 3 (per round) — Reconstruction walk + L_C signaling

Per OBFTR base. K'-layer walk starting from L_Bid. If σ-quorum reaches at any layer, reconstruct and halt. If NR-quorums chain through all layers without σ-quorum, proceed to round r+1. L_C cluster-consensus signaling per OBFTR base.

If round R completes without σ-quorum at any layer, slot misses.

### Safety

Identical to bare OBFTR. Verdicts don't bind threshold partials. EKM enforces single-σ-V per (slot, layer) per operator, with cross-round atomicity per OBFTR's EKM coordination model. Pigeonholes 1, 2, 3 hold unchanged with K' layers.

### Slashing-evidence rules

OBFTR's existing rules unchanged. Two new L_Bid-specific rules:

- **Rule 7 — Bid equivocation** (same as OBFT + L_Bid): two distinct `KindBid` envelopes from same operator at same slot. Self-contained slashable.
- **Rule 8 — Verdict equivocation / verdict-vs-action equivocation**: two distinct `KindBidVerdict` from same operator, OR `KindBidVerdict(σV(V_X))` paired with Phase-2 NR partial on `nr_tag_LBid` (or vice versa) at any round. Self-contained slashable; verdict-vs-action form is gossipsub-pattern-quality at boundary cases.

### Liveness

#### Recovery scope at L_Bid

Mini-consensus closes C1/C2/C3 at L_Bid (same as OBFT + L_Bid):

- **C1 — Selective bid-withholding**: byz withholds → some honest verdict NULL → verdict-pool fragments → no V reaches qV → all NR_LBid → fall-through to L_0. **Closed.**
- **C2 — Bidder equivocation**: byz sends conflicting bids → diverging predicted L_Bids → verdict-pool fragments → no V reaches qV → all NR_LBid → fall-through. **Closed.**
- **C3 — Validity-divergence majority on V_LBid (3-of-4 at f=1 n=4)**: 3 honest verdict σV(V_X), 1 NV. `verdict_pool[V_X] = 3 = qV` → cluster σ-binds. σ-pool reaches qV. **Closed for 3-of-4 majority.** 2-2 split remains hard algebraic limit.

#### Recovery scope at rotation layers L_0, ..., L_{K-1}

Identical to bare OBFTR. R-round retry, Defer state, L_C signaling apply uniformly.

#### New residual failure modes at L_Bid

Same algebraic shape as OBFT + L_Bid:

- **2-1-byz-defect at L_Bid** (R-invariant): byz bid-equivocates + verdict-claims σV on majority-V + defects to NR partial. σ-pool < qV; NR-pool < qEnc; deadlock at L_Bid in round 1. **R-rounds do not help** — the σ-locked operators stay locked across rounds (cross-round σ-or-NR-V exclusivity); round 2 doesn't reset the deadlock because the same V_LBid is reused (mini-consensus is slot-level, not per-round). Slashable Rule 8.
- **Verdict-equivocation at L_Bid** (R-invariant): byz issues different verdicts to different peers; per-peer convergence diverges; deadlock at L_Bid in round 1. R-rounds do not help (same reason as above). Slashable Rule 8.

**The R-invariance of these residuals matches OBFTR base's adversarial-byz exposure profile** ([§Failure modes](#failure-modes)) — multi-round retry covers partition cases (re-flood completing across rounds) but not adversarial patterns where byz engineers cross-round σ-locks.

**Trigger frequency**: byz is always a bidder, so they can attempt bid-equivocation any slot — vs OBFTR's L_0 surfaces which trigger only when byz is rotation L_0 (1/n slots).

### Best/worst time-to-completion

Measured from `T_commit_1` (mini-consensus start). At Config A (D=100ms, δ=50ms, R=2, recommended sizing):

| Scenario | Time | Mechanism |
|---|---|---|
| Healthy fast path: round-1 L_Bid σ-quorum reaches early | ~Δ_minicon + (D+δ) ≈ 450ms | Mini-consensus + 1 RTT in round-1 Phase 2; reconstruction at L_Bid plaintext |
| Healthy completion at L_Bid (canonical, round 1) | ~Δ_minicon + Δ_2 + Δ_3 ≈ 850ms | Round-1 full Phase 2 + Phase 3 |
| Mini-consensus fails → fall-through to L_0 in round 1 | ~Δ_minicon + Δ_2 + Δ_3 ≈ 850ms | NR-quorum at L_Bid; round-1 walk to L_0 |
| Round-1 fails (e.g., partition at L_Bid + L_0..L_{K-1}); round-2 succeeds | ~Δ_minicon + 2 × per-round-budget ≈ 1400-1500ms | OBFTR's standard R-round retry; mini-consensus result reused |
| Multi-leader silent within budget (K-1 = 3) at K=4 | ~Δ_minicon + per-round Phase 3 ≈ 850-1000ms | K'-layer walk in round-1 Phase 3 (in-round; sequential local decryption) |
| L_Bid 2-1-byz-defect or verdict-equivocation | slot misses (R-invariant) | Deadlock at L_Bid blocks fall-through; cross-round σ-locks prevent recovery |

Best (success) ≈ 450ms (round-1 fast); worst (success within R=2) ≈ 1500ms; ~3× spread. Healthy-path is +Δ_minicon vs bare OBFTR.

### Comparison with bare OBFTR

| Aspect | Bare OBFTR | OBFTR + L_Bid mini-consensus |
|---|---|---|
| Slot structure | Phase 1 → R rounds × (Phase 2 + Phase 3 + L_C) | Phase 1 → Mini-consensus → R rounds × (Phase 2 + Phase 3 + L_C) |
| Layers | K (rotation-determined) | K' = K + 1 (L_Bid + K rotation-determined) |
| Mini-consensus scope | n/a | Slot-level (once); V_LBid fixed across all R rounds |
| Wire kinds | Phase1Bundle, KindOnion_r, KindNR_r, KindLCClaim, KindCertificate | + KindBid, KindBidVerdict |
| Slashing-evidence rules | OBFTR base | + Rule 7 bid equivocation, + Rule 8 verdict equivocation |
| Healthy-path latency (round-1, post-T_commit_1) | ~500-650ms | ~750-850ms (+Δ_minicon) |
| Best-case latency | ~150-300ms | ~450ms |
| Worst-case latency at R=2 (success) | ~1.15s | ~1.5s |
| Time-to-completion spread | ~4-5× best/worst | ~3× best/worst |
| Bandwidth (n=4, K=4 healthy) | ~30-40 KB | ~35-45 KB (+n bid envelopes, +n verdicts, +1 chained encryption layer) |
| Submission headroom (4s cutoff, R=2) | ~2.85s | ~2.5s |
| EKM coordination | OBFTR's cross-round atomic coordinator | Same (mini-consensus verdicts don't consume EKM) |
| Cryptographic primitives | BLS threshold + threshold IBE/SWE | Same |
| **Safety** | Cryptographic via Pigeonholes 1, 2, 3 | **Same** |
| Rotation-layer (L_0/.../L_{K-1}) liveness | OBFTR base recovery scope (R-round retry, partition tolerance up to R·D) | **Same** |
| L_Bid liveness — C1 selective bid-withholding | n/a | **Closed** (verdict-quorum-short → fall-through) |
| L_Bid liveness — C2 bidder equivocation | n/a | **Closed** (verdict-quorum-short → fall-through) |
| L_Bid liveness — C3 validity-majority (3-of-4) | n/a | **Closed** (verdict-quorum reaches on majority) |
| L_Bid liveness — 2-1-byz-defect | n/a | **Open**: R-invariant deadlock; slot-miss-without-fall-through; slashable Rule 8 |
| L_Bid liveness — verdict-equivocation | n/a | **Open**: R-invariant deadlock; slashable Rule 8 |
| L_Bid liveness — 2-2 validity split | n/a | **Open**: hard algebraic limit |
| Bid-routing value capture | n/a | Highest-bid block on healthy path |
| Adversarial-byz trigger frequency at the bid layer | n/a | Higher than rotation-layer surfaces — byz is always a bidder |
| Multi-round retry effectiveness at L_Bid | n/a | Same as OBFTR base for partition cases; **R-invariant for adversarial-byz patterns** (cross-round σ-locks block recovery) |

**Net trade vs bare OBFTR**: pays Δ_minicon (~300ms) plus the same L_Bid-specific adversarial-byz residuals as OBFT + L_Bid (2-1-byz-defect, verdict-equivocation; R-invariant; slashable but slot-miss-without-fall-through), in exchange for bid-routing value capture on the healthy path. The L_0/.../L_{K-1} layers' R-round recovery scope is unchanged (partition-tail absorption up to `R · D` is preserved). Whether favorable depends on byzantine-frequency assumption, MEV-value-capture upside, and partition-tail observed in production.

**Comparison with bare-TBFT-style B.3 sketch**: closes C1, C2, and C3 deadlocks via the cluster-wide convergence rule. The R-invariance of the residual L_Bid surfaces (2-1-byz-defect, verdict-equivocation) matches OBFTR's existing R-invariant adversarial exposures — round-machinery doesn't help with these patterns at any R, so the L_Bid residuals don't degrade OBFTR's recovery profile beyond what its own L_0 surfaces already expose.

