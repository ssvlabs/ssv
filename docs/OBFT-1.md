# OBFT-1 — Single-Round Onion BFT

A single-round agreement protocol for SSV clusters that produces one collective threshold-signed value per "slot" against a hard deadline. OBFT-1 is the simpler-spec cousin of [OBFT](OBFT.md) — it preserves OBFT's cryptographic safety (cluster-wide unique output via threshold cryptography over EKM-enforced per-operator commitments) and OBFT's K-layer onion structure for parallel leader fall-through, but drops the R-round retry machinery entirely. Single-round only: agreement runs once per slot against a single hard deadline; there is no round-change, no cross-round re-flood, no L_C cluster-consensus signaling.

OBFT-1 generalizes the [TBFT](TBFT.md) shape with K layers (configurable, `max(2, f+1) ≤ K ≤ n`) and a single agreement round. At `K = 2`, OBFT-1 reduces to baseline TBFT. The Defer state and the K-layer reconstruction walk in Phase 3 are kept (load-bearing for parallel leader fall-through and for late-σ-emit-within-Phase-2 sub-phasing). The R-round retry, cross-round re-flood, cross-round σ-or-NR exclusivity, L_C cluster-consensus signaling (`KindLCClaim`), per-round acceptance widening, auth-only-retention state, and cross-round σ-partial dedup are all removed.

OBFT-1's recovery scope is intentionally bounded. Within the partial-synchrony envelope `D` (single round = single D worth of propagation tolerance), OBFT-1 recovers all the same cases [OBFT](OBFT.md) does at any R: healthy path, silent leader, multi-leader fall-through within Phase 3's reconstruction walk (no per-layer RTT), all-Defer-due-to-equivocation fall-through to next layer via end-of-Phase-2 force-NR. The protocol does NOT recover from **view-divergence** patterns: equivocation (byzantine leader sending different V's to different honest) and validity-divergence (e.g., re-org during gossipsub-acceptance window causing some honest to validate V and others to NV). View-divergence cases either succeed naturally (when the split happens to leave 2-of-3 honest σ-committed on the same V at f=1 n=4, allowing leader's σ_L^V to push the pool to qV) or slot-miss. Equivocation in non-naturally-recovered patterns is **slashable** (the byzantine leader pays the stake-based penalty); validity-divergence is not attributable to anyone and just slot-misses. See [Assumptions and implications](#assumptions-and-implications) for the explicit list. The TBFTR-style **Phase 2a/2b split** closes the equivocation gap at +1 RTT cost; documented as a future improvement, not in current OBFT-1.

What OBFT-1 explicitly does NOT cover that [OBFT(R≥2)](OBFT.md) does: real propagation in `(D, R · D]` ("aggressive-marginal" partition tails). Under the partial-synchrony assumption these are out of envelope at OBFT-1 by definition (`real propagation > D` is a Class A "sustained partition" failure under R=1's framing) — the same way `real propagation > 2D` is out of envelope at OBFT(R=2). The line moves with R; OBFT-1 picks the smallest R consistent with single-round operation and accepts the corresponding envelope.

The protocol description below targets `n = 4` (`f = 1`) as the running example, with `K = 4` (i.e., K = n) as the recommended default for SSV proposer duty (every cluster member is a leader at exactly one layer; pigeonhole guarantees ≥3 honest leaders at f=1). K is tunable per duty within `max(2, f+1) ≤ K ≤ n`. SSV's Ethereum proposer duty is used as the running application example. Generalizations to `n ≥ 7` are noted inline where the algebra changes.

## When to use it

**Suited for:** SSV proposer duty under healthy-network partial synchrony (`D` ≈ 100ms cluster gossipsub P99/P999), where TBFT's 2-RTT healthy-path latency plus K-layer parallel leader fall-through is sufficient and round-change machinery is not desired. Also well-suited for high-D networks (`D` ≈ 300–500ms) where [OBFT(R=2)](OBFT.md) does not fit the 4s relay cutoff but a single round still does. Generally: deployments that prioritize spec/EKM simplicity, more submission headroom, and high-D fit over the larger partial-synchrony envelope of multi-round OBFT.

**Not suited for:** deployments where the gossipsub P99→P9999 propagation tail is meaningfully wider than `D` and within-envelope coverage of those tails matters — use [OBFT(R=2)](OBFT.md) instead, which extends the envelope to `2D`. Also not suited for: scenarios requiring host-validity-divergence recovery within a slot (OBFT-1 assumes host validity is unanimous at decision time, see [Assumptions](#assumptions-and-implications); QBFT is the appropriate choice when validity is unstable across the consensus window).

**Also not suited for:** general-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. OBFT-1 (like TBFT and OBFT) gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n ≥ 3f+1` (standard BFT). The running example is `n = 4, f = 1`; algebra generalizes.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) as the threshold-signing scheme for no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety". At `n = 4`, `qEnc = 3`.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts and protocol-level claims, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- **K layers** (`max(2, f+1) ≤ K ≤ n`, configurable; **K ≥ f+2 strongly recommended** — see below) with deterministically-derived leaders: layer 0 with **primary leader** `L_0`, layer 1 with **backup leader** `L_1`, ..., layer `K-1` with `L_{K-1}`. Leaders must be distinct (`L_i ≠ L_j` for `i ≠ j`). For `K = n`, every cluster member is a leader at exactly one layer per slot.

  **Two distinct K bounds:**
  - **`K ≥ f+1` is the BFT-liveness minimum** — ensures at least one layer has an honest leader (by pigeonhole over the f-byz bound). At K < f+1, all leaders could be byzantine and no σ-quorum reaches at any layer.
  - **`K ≥ f+2` is the late-leader-resilience minimum** — ensures at least *two* honest leaders exist, so a single late-broadcasting honest leader doesn't foreclose the slot via the deepest-layer NR-lock pathology (see §Failure modes / Late deepest-layer leader broadcast). At K = f+1 with the single honest leader running late, the slot misses; at K ≥ f+2 a second honest leader provides fall-through redundancy.

  Concrete minimums by f: at `f = 1`, BFT-min `K = 2` (matches baseline TBFT) but **late-leader-resilient `K = 3` recommended**, with **`K = n = 4` as the OBFT-1 default** for SSV proposer duty (every cluster member leads exactly one layer; maximum honest-leader probability via pigeonhole); at `f = 2, n = 7`, BFT-min `K = 3` but resilient `K = 4` recommended; at `f = 3, n = 10`, resilient `K = 5`.
- **Single agreement round.** OBFT-1 fixes `R = 1`: one Phase 1 → Phase 2 → Phase 3 sequence per slot, no retry, no re-flood across rounds, no L_C cluster-consensus signaling. The slot's reconstruction deadline is the only deadline. Defer-state operators who do not receive V before end of Phase 2 force-NR at end of Phase 2 (see §Phase 2). For deployments needing the larger `R · D` partial-synchrony envelope of multi-round retry, see [OBFT(R≥2)](OBFT.md).
- Per-layer leader-fetch deadlines `T_{K-1} < T_{K-2} < ... < T_1 < T_0`, plus a single cluster deadline `T_commit`. (`T_commit` is the *view-fix point*: each operator commits its stance based on what it observed by `T_commit`. Reconstruction and submission happen after.) The asymmetric fetch times let primary leaders fetch high-MEV values late (`T_0` close to `T_commit`) while deeper layers' leaders fetch safe early values from deeper-confirmed parents (`T_{K-1}` well before `T_0`).
- A **candidate acceptance cutoff** `T_candidate_accept = T_commit − (D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. Receivers accept candidate bundles whose first-observation time is at or before `T_candidate_accept`. Bundles first-observed after the cutoff are rejected entirely (no retention, no later-acceptance path — single round, single cutoff). This is the chief simplification vs OBFT(R≥2), which widens the cutoff per-round to defer late-arriving bundles to subsequent rounds.
- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

## Assumptions and implications

OBFT-1's claims hold conditional on a small set of explicit assumptions about the deployment environment. They are consolidated here; the rest of the spec assumes them and refers back to this section rather than re-deriving the trade-offs each time.

### Assumed

1. **Standard BFT trust bound.** `n ≥ 3f + 1`, up to `f` operators may be byzantine. At `n = 4`, `f = 1`. Honest operators run protocol-conformant software (correct EKM rule enforcement, correct host application, correct gossipsub behavior). Byzantine operators may deviate arbitrarily within their f-bound.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `D` (cluster gossipsub P99/P999) and clock skew `δ`. Safety is unconditional on timing; only liveness depends on this. **OBFT-1's effective propagation envelope is exactly `D`**, since R = 1: real propagation > D for any honest receiver violates assumption 2 at OBFT-1's framing and the slot may miss cleanly (Class A "sustained partition", see §Failure modes). Multi-round OBFT (R ≥ 2) extends the envelope to `R · D`; OBFT-1 trades that envelope width for spec/EKM simplicity (see [Where this came from](#where-this-came-from)).

3. **Host validity is unanimous at decision time** (best-effort assumption). OBFT assumes the host application's `valid` / `not-valid` verdict on `V_{L_k}` is the same across all honest operators by the time they emit Phase 2. Operators may transiently diverge (e.g., a head change observed by some but not others). The host's job is to make divergence rare via per-operator stabilization — typically by validating against a stable head snapshot taken at Phase-1 acceptance time, then locking the verdict for the remainder of the slot. **This per-operator locking does not give cluster-wide convergence; it narrows the divergence window to events that land inside the gossipsub-acceptance window** (typical D ≈ 100–500ms vs slot length 12s). When divergence does occur — a re-org during the acceptance window with operators accepting on either side of it — the assumption is violated and the slot may miss; the protocol does not recover. See [Application: SSV Ethereum proposer duty / Head-change handling](#head-change-handling) for the SSV-specific stabilization workflow and the residual divergence window.

   The validity check exists to prevent the cluster from agreeing on a garbage / invalid V — it is not a divergence-recovery mechanism. NV is operationally identical to NR for protocol counting; it does not trigger any in-protocol divergence-handling path.

4. **Persistent operator set with reputation deterrent.** OBFT operates within a stable SSV cluster running protocol instances over many slots. Byzantine actions (equivocation, fake encrypted-presence, cross-signing, false NR) leave self-contained slashable evidence on the wire — verifiable by any observer with the published partials and (where applicable) post-decryption onion contents. **The protocol surfaces this evidence; the cluster's honest operators decide whether to act on it.** Acting means coordinating offline ("in the meat-space") to:

   - File a stake-slashing transaction against the byzantine operator via the SSV contract.
   - Remove the operator from future slot participation in this cluster (kick-out).
   - Propagate the reputation signal to future cluster formation (the operator becomes less attractive for new clusters).

   None of this is automated by the protocol — it's a human-supervised coordination process triggered by honest operators when they judge the on-wire evidence to be compelling (see [Implications of the reputation deterrent](#implications-of-the-reputation-deterrent-assumption-4) for the full discussion of evidence quality and how honest operators decide).

   This persistence is what makes the "permitted as slashable byzantine fault" framing meaningful: byzantine grief that succeeds in one slot pays for it in future participation, *if* honest operators coordinate to act on the evidence. Detection may be post-hoc within a slot (e.g., garbage-encryption at L_k > 0 is detected only when prior layers' NR-quorums unlock decryption; 1-1-1 equivocation evidence converges via re-flood across rounds), but the evidence record is durable — any honest operator that eventually observes the evidence can verify it independently and bring it to the cluster's coordination process.

5. **Coordinated EKM across both keypair shares.** The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log keyed on `(slot, layer, side, value_root)`. OBFT-1 simplifies the EKM coordination model relative to OBFT(R≥2): no cross-round atomicity, no persistent partial-sig cache for cross-round re-emission, no deterministic re-signing fallback — see [EKM coordination model](#ekm-coordination-model).

6. **Independent V-keypair and IBE-keypair DKGs over the same operator set at the same threshold `qV = qEnc = 2f+1`.** Both DKGs are run once at cluster init with all `n` operators participating; the shared operator set and threshold is what makes Pigeonhole 1's algebra work (`h_σ + h_NR ≤ 2f+1` across the cross-keypair signing log).

### Implications of safety being honest-majority cryptographic, not 100% cryptographic

OBFT-1's safety holds against an offline-aggregating byzantine *given the protocol rules are correctly enforced by honest operators*. Enforcement happens at two independent layers per operator — the **protocol layer** (operator software implementing the OBFT-1 state machine, deciding when to request σ vs NR vs Defer) and the **EKM** (slashing-protection log that rejects bad signing requests as defense-in-depth). Together they ensure single-σ-V per (slot, layer) per operator and σ-XOR-NR per layer; these rules reduce the safety proof to pigeonhole arguments on the cluster-wide signed-message set.

An operator whose software actively misbehaves (maliciously compromised, or with compounding protocol+EKM bugs that produce byzantine-equivalent signing behavior) consumes f-budget directly. With `f = 1` byz at `n = 4`, one such operator is tolerated; two violate the trust bound and Pigeonhole arguments can fail. Single-layer bugs (protocol-only or EKM-only) do **not** produce byzantine behavior on their own — the other layer catches them. See [EKM coordination model](#ekm-coordination-model) for the full defense-in-depth analysis.

**This is the same trust posture as QBFT.** QBFT's safety also holds under f-byz with honest-majority correct code paths. A bug in `2f+1` honest operators (e.g., the post-consensus signing path signs both candidates from a split decision, or the prepared-certificate verification accepts conflicting commit certificates) would equally violate QBFT's safety guarantees. Neither protocol is "100% cryptographic" against operator-side software bugs; both rely on operator software correctness for honest operators.

Accordingly, "cryptographic safety" in this spec means: against a partially-synchronous byzantine network adversary plus up to `f` byzantine operators, honest operators following the protocol cannot produce two contradictory outputs. It does not mean: safety against arbitrary bugs in honest operator software.

### Implications of validity-divergence not being recovered (assumption 3)

**This is a TBFT-family limitation, not specific to OBFT-1.** [TBFT.md](TBFT.md) "Application-validity-divergence — known liveness limit" documents the same algebraic deadlock with the same root cause: per-operator independent validity verdict + leader's Phase-1 σ_V locked + cross-phase exclusivity. OBFT-1 inherits all three from TBFT and adds nothing that recovers from it — Defer state doesn't help (Defer is for partition recovery within Phase 2's late-σ-emit window, not verdict reconciliation). [OBFT(R≥2)](OBFT.md) doesn't help either (verdicts are locked at acceptance, so re-flood across rounds doesn't reconcile divergence) — this is purely a TBFT-family inherited limitation, not an OBFT-1-specific gap.

If assumption 3 is violated mid-slot — honest verdicts genuinely diverge after Phase 2 emit — OBFT-1 cannot recover within the slot. There is no fresh-V refetch mechanism. The byzantine leader's Phase-1 σ_V is locked; honest who NV cannot switch to σ; cluster deadlocks at L_k or falls through to L_{k+1} (where the same divergence pattern may repeat).

For SSV proposer duty, the host's stabilization workflow (validate parent_root once at acceptance, lock the verdict) is the design's path to satisfying assumption 3 — same approach as baseline TBFT. If the host cannot guarantee unanimous validity (e.g., re-orgs are common enough that locking-at-acceptance leads to too many submission rejections), the appropriate fixes are at the protocol-family level, not OBFT-1-specific:

- **Phase 2a/2b** (TBFTR-style; see [Path forward — Phase 2a/2b](#where-this-came-from)). Defers σ-commitment until after a Phase-2a observation phase that lets the cluster converge on a stabilized validity verdict before any operator binds. This is the structural fix at the TBFT family level. Costs +1 RTT per slot.
- **Use a deterministic / finalized parent.** Validity criterion that doesn't depend on each operator's chain view at evaluation time — e.g., parent must be a finalized block (2 epochs old, all operators agree). Eliminates divergence by construction but loses late-MEV (you can only build on finalized parents).
- **QBFT.** Round-changes through with a new leader fetching at the moved head — covers validity-divergence as a side-effect of round-change recovery. Comes with QBFT's own ~2s round-change latency.

These three are the structural options. Smaller mitigations (relax cross-phase exclusivity, allow NV → σ flip via aggregator filtering, etc.) all break safety against an offline-aggregating byzantine — see the Phase 2a/2b discussion at [Path forward](#where-this-came-from) for why.

### Implications of equivocation not being recovered

OBFT-1 does not provide an in-protocol equivocation recovery mechanism. Outcomes split into three classes:

- **σ-quorum reaches at L_0 naturally** (slot succeeds): honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool to qV.
- **NR-quorum reaches at L_0 → fall-through to L_1** (slot succeeds at L_1 if L_1 honest): all 3 honest land in Defer-due-to-equivocation (typical when byz delivers V's early enough for gossipsub re-flood to spread conflicts before Phase 2 σ-emit time). End-of-Phase-2 force-NR produces qEnc-quorum at L_0, decryption unlocks L_1, σ-quorum at L_1 reaches in the same Phase 3 reconstruction walk.
- **σ-locked split patterns** (slot misses): honest σ-states split into mixed σ-locked + Defer (1-1-1 split, 1-1-Defer-C, 1-Defer-Defer at f=1 n=4). σ-pools split below qV; NR-pool capped by σ-locked operators below qEnc; no fall-through.

A byzantine controlling delivery timing picks the class. Delivering near end-of-Phase-1 (insufficient re-flood time) reliably engineers the σ-locked split slot-miss outcome. Equivocation evidence is slashable in all cases; the persistent reputation deterrent (assumption 4) is what makes the slot-miss tolerable across many slots.

The reputation deterrent (assumption 4) is what makes this a tolerable failure mode in expectation: a byzantine that equivocation-griefs in slot N pays for it in slot N+1 onward via stake slashing and cluster kick-out. Phase 2a/2b (future improvement) closes the recovery gap without relying on the deterrent. The exposure to equivocation σ-locked splits at OBFT-1 is identical to OBFT(R≥2) — these are R-invariant patterns; the round-retry machinery doesn't help with them at any R, so dropping rounds doesn't make them worse.

### Implications of the reputation deterrent (assumption 4)

The reputation deterrent affects *liveness only*, not safety. Pigeonholes 1, 2, 3 hold cryptographically against any byzantine within the f-bound regardless of whether the byzantine cares about reputation — a byzantine ignoring future-slot consequences (e.g., last-slot-before-exit) cannot violate safety, only grief liveness.

Specifically:

- **Safety unaffected:** No matter how aggressively byzantine operators misbehave (1-1-1 equivocation, fake encrypted-presence, cross-signing), at most one V signature reconstructs cluster-wide per slot. This is a property of the cluster-wide signed-message set under EKM enforcement (assumptions 1, 5).
- **Liveness affected:** Byzantine that ignores the deterrent may grief more slots. Each affected slot misses cleanly (no safety violation). Across many slots, a reputation-respecting byzantine griefs once and pays; a reputation-ignoring byzantine griefs repeatedly until governance kicks them out.

The deterrent therefore matters for *expected liveness across many slots*, not for per-slot correctness. Per-slot safety and per-slot liveness scope are unconditional on assumption 4.

**Slashing is human-supervised best-effort coordination, not an automated cryptographic action.** OBFT-1 does not include an in-protocol slashing mechanism. The "reputation deterrent" referenced throughout this spec is a coordination process among honest cluster operators (and the cluster's broader governance, where applicable): the protocol surfaces byzantine actions as on-wire evidence; honest operators coordinate offline — *in the meat-space* — to verify the evidence, judge whether it is compelling, and act on it (stake slashing via the SSV contract, removal from future cluster participation, reputation propagation to future cluster formation).

Specifically:

- **Protocol's role**: every byzantine fault class produces on-wire evidence signed by the offender's own keys, verifiable in isolation by any observer (no cluster-wide coordination needed for verification — see [§Slashing evidence](#slashing-evidence) for the four evidence rules). The protocol surfaces the evidence; it does not act on it.
- **Honest operators' role**: collect evidence, judge compellingness, coordinate offline to act. Whether to act, and how aggressively, is a human decision — there is no protocol-level automated trigger.

The deterrent's effective strength therefore depends on three deployment-level factors, none of which the protocol controls:

1. **Evidence quality.** Some fault classes leave self-contained cryptographic proofs (a single signed message-pair conclusively demonstrates the byzantine action — humans can act with high confidence). Others leave only behavioral patterns (no single message proves it; humans must aggregate observations across slots, with risk of false-positive against an honest-but-flaky operator).
2. **Coordination responsiveness.** How quickly honest operators converge on "yes, this byzantine misbehaved; we should act." Active operators with clear governance act faster than passive ones; small clusters with one or two honest operators take longer to build consensus than larger clusters.
3. **Byzantine's stake.** A byzantine that values continued cluster membership avoids actions that would get them removed. A byzantine already exiting (no stake to lose) is not deterred per-slot; their grief is bounded to a few slots before removal but is essentially "free" within those slots.

**Evidence quality by fault class:**

| Fault class | Evidence type | False-positive risk |
|---|---|---|
| Equivocation, cross-signing, cross-onion partial-sig equivocation, fake-encrypted-presence | **Cryptographic, self-contained** — a single signed message-pair conclusively demonstrates the action | Very low — proof is unambiguous; humans can act on a single observation |
| Selective-delivery / withholding grief, byzantine σ-refusal coordinated with honest transient flakiness | **Behavioral pattern** — no single signed message proves it; requires aggregating observations across operators / slots | Higher — hard to distinguish byzantine intent from honest-but-flaky operator behavior; humans should wait for the pattern to accumulate before acting |

For high-evidence-quality faults, honest operators can act decisively on a single observed fault. For low-evidence-quality faults, operators need patience — wait for the pattern to accumulate enough confidence before acting, or risk punishing an honest-but-flaky operator. There is no automated trigger that resolves this trade-off; it's a human judgment per fault class.

**The deterrent works in expectation across many slots, not per-slot.** Per-slot, a byzantine within the f-bound can grief slot success in various ways (Class B failure modes; see [§Failure modes](#failure-modes)). Across many slots, a byzantine that consistently misbehaves accumulates evidence; honest operators eventually act and remove them. The "permitted byzantine grief" framing in §Failure modes is therefore conditional on (a) honest operators actively monitoring and willing to coordinate, (b) byzantines having stakes that meaningfully exceed short-term grief value, (c) the cluster's coordination layer functioning. Where those conditions don't hold (passive small clusters, byzantines on their way out, dysfunctional governance), the deterrent's effective strength is correspondingly weaker.

## Protocol

OBFT-1 runs **a single agreement round** per slot: Phase 1 → Phase 2 → Phase 3. Phase 1 is a fresh broadcast (no re-flood across rounds, since there is only one round). The slot's hard deadline (`T_round_end`) is the cluster's reconstruction cutoff; a slot that does not reach σ-quorum at any layer by `T_round_end` is missed.

### Phase 1 — Candidate broadcast

Phase 1 has K per-layer windows (driven by the asymmetric fetch times): `[T_{K-1}, T_{K-1} + Δ_1]` for the deepest backup, then progressively `[T_{K-2}, T_{K-2} + Δ_1]`, ..., ending at `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "OBFT-1-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates OBFT-1 Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, baseline TBFT, OBFT, other OBFT-1 message kinds, etc.). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, and check the first-observation timestamp against `T_candidate_accept = T_commit − (D + δ)`. Bundles first-observed after `T_candidate_accept` are rejected entirely (no retention, no later-acceptance path). Bundles failing cryptographic-auth are silently dropped. (A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.)
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. A `not-valid` verdict turns the operator's commitment into NV (operationally identical to NR — see "Operator commitments — σ, NR, NV, Defer" below).

If a leader `L_k` fails to broadcast (or broadcasts after `T_candidate_accept`), that layer is unavailable; the cluster falls through to deeper layers via NR-quorum. If all K leaders fail, the slot is missed.

**Bundle propagation.** Honest receivers re-flood every bundle via standard gossipsub on first observation. There is no second re-flood event in OBFT-1 (no rounds): cluster-wide reception relies on gossipsub's organic propagation completing within the partial-synchrony envelope `D` before `T_candidate_accept`.

**Retention bounds.** Retention state for accepted Phase-1 bundles is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **two distinct `(V, σ_{L_k}^V, σ_{L_k}^{op})` tuples** — the first auth-valid bundle observed, plus, if a second auth-valid bundle with a *distinct* `value_root` is later observed, that one (sufficient for both Phase-2 σ-signing on the chosen V *and* leader-equivocation evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Bundles whose cryptographic-auth checks fail are dropped without retention. Bundles first-observed past `T_candidate_accept` are rejected entirely. Retention lifetime: until the operator's local end of Phase 3 (i.e., until reconstruction halts or the slot is declared missed); slot state is then cleared. This caps memory at `O(K · n)` bundles per slot in the worst case (every leader equivocates) — same bound as OBFT, simpler bookkeeping (no auth-only-retention state, no per-round widening).

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with honest threshold partials produced in Phase 2 by operators who received the leader's bundle, the cluster reaches `qV` real partials on `V_{L_k}` — closing the byzantine-leader selective-delivery grief under partial synchrony (see "Fault tolerance / Liveness").

**Equivocation handling — detect and slash.** If a participant observes two distinct `σ_V` partials from the same leader at the same slot/layer, that's leader equivocation. The pair of signed bundles is self-contained slashable evidence, gossipped for out-of-band slashing.

Local protocol response, by current commitment state at observation time:

- **Already σ-emitted** (Phase-1 leader's σ_V on the wire, or Phase-2 σ already gossiped): stay σ-locked. Cross-phase exclusivity prevents retroactive withdrawal.
- **σ-eligible but not yet σ-emitted** (acceptance succeeded earlier, Phase 2 onion not yet constructed): transition to Defer-due-to-equivocation. The σ-emit precondition ("no equivocation observed") now fails.
- **In Defer-due-to-partition** (no V retained yet): transition to Defer-due-to-equivocation upon retaining ≥ 2 distinct V's (e.g., gossipsub delivers multiple V's at once). Recovery via "late re-flood within Phase 2 delivers V, σ-emit on V" is foreclosed.

In all Defer-due-to-equivocation cases: unrecoverable within the slot; force-NR at end of Phase 2 per the end-of-Phase-2 force-commit rule. The protocol does not attempt to "pick a winner" cluster-wide — under f=1 byzantine and OBFT-1's retention bound (2 distinct V's per `(slot, layer, leader_id)`), no rule based on per-operator local state can guarantee cluster-wide convergence on a single V (different operators may retain different V-pairs under adversarial gossipsub-ordered delivery; see [Implications of equivocation not being recovered](#implications-of-equivocation-not-being-recovered)).

The leader is required to sign `σ_V` exactly once per slot/layer (refreshes in the host's fetch loop are pre-signing only — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance); any second `σ_V` from the same leader is a protocol violation regardless of intent.

**Equivocation is permitted as a slashable byzantine fault.** OBFT-1 does not provide an in-protocol equivocation recovery mechanism. Some equivocation patterns naturally reach σ-quorum on one V (e.g., 2-of-3 honest σ-commit on the same V plus the leader's σ_L^V on that V = 3 = qV at f=1 n=4) — the slot succeeds in those cases as a side-effect, not via a specific protocol mechanism. Other patterns (e.g., 1-1-1 split where each honest σ-commits on a different V before observing equivocation, or asymmetric-retention patterns where Defer-state honest see different V-pairs under byz-controlled delivery order) do not reach σ-quorum and the slot misses.

**Practically, an adversarial byzantine controls which pattern occurs.** A byzantine that times equivocation deliveries near the end of Phase 1 (leaving insufficient time for cross-honest gossipsub re-flood to spread the conflict before Phase 2 σ-emit) reliably engineers the σ-locked split patterns (1-1-1, 1-1-Defer, 1-Defer-Defer) that don't reach qV. The natural-recovery cases (2-1 split where 2 honest happen to σ-commit on the same V; all-Defer fall-through to L_1) only fire when the byzantine fumbles the timing — delivers V's early enough that re-flood converges honest views before σ-emit. **In expectation, byzantine-leader equivocation slot-misses; the persistent reputation deterrent (assumption 4) is the practical defense, not natural recovery.** A byzantine indifferent to the deterrent (e.g., last-slot-before-exit) can grief reliably; a byzantine that values future participation pays once and stops. (The exposure here is identical to OBFT(R≥2) — these are R-invariant patterns.)

In all cases, the byzantine leader pays the stake-based slashing penalty — equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained on each pair of conflicting bundles.

The TBFTR-style **Phase 2a/2b split** (broadcast-only Phase-2a, then σ-emit on a deterministically-chosen V in Phase-2b after Phase-2a observation completes) is the structurally correct fix for full equivocation recovery and preserves cryptographic safety AND f-tolerant liveness; documented as a future improvement, not in current OBFT-1.

**Operator commitments — σ, NR, NV, Defer.** OBFT-1 extends TBFT's three-state commitment model with a fourth state, **Defer**, which is what enables late-σ-emit-within-Phase-2 recovery without breaking cross-phase exclusivity. For each layer, an operator's commitment falls into one of four buckets:

- **σ (sign-on-V)**: the operator received the leader's bundle on time, both protocol-level and application-level checks passed, and the operator has not observed equivocation evidence at this layer through σ-emit time (acceptance-time eligibility is checked, then re-checked at Phase 2 onion construction; if equivocation is observed in between, the operator transitions to Defer-due-to-equivocation rather than σ-emitting). Materializes as a σ partial in the Phase-2 onion (or as the leader's Phase-1 σ for the layer's own leader). Once σ-emitted, the operator is **σ-locked** at this layer for the entire slot (cross-phase exclusivity).
- **NR (non-receipt, evidence-driven)**: the operator has positive evidence that this layer cannot validate locally:
  - **NR-silent**: `T_candidate_accept` cutoff has passed AND no peer σ-emit on this layer is observed cluster-wide (the leader is presumed silent).
  - **NV (non-validity)**: host application returned `not valid` for V_{L_k}.
- **Defer (uncommitted)**: cutoff has passed, peer σ-emit on this layer is observed cluster-wide (so the leader is *not* silent — some V exists somewhere in the cluster), but the operator is not σ-eligible. Two sub-cases:
  - **Defer-due-to-partition**: the operator does not yet have an auth-valid Phase-1 bundle that the host validates. Recoverable within the slot **only if late gossipsub re-flood delivers such a bundle before end of Phase 2's late-σ-emit window** (operator transitions to σ-state). The window is `Δ_2 − (D + δ)` (see Phase 2 sub-phasing); narrow but non-empty when `Δ_2 ≥ 2(D + δ)` is configured.
  - **Defer-due-to-equivocation**: the operator has retained ≥ 2 distinct auth-valid Phase-1 bundles at this layer (leader equivocation evidence). Unrecoverable within the slot — re-flood only delivers more bundles, not fewer, so the σ-eligibility precondition ("no equivocation observed") cannot be re-established. The operator force-NRs at end of Phase 2. See "Equivocation handling — detect and slash".

  Defer is **not visible on the wire** (no message is broadcast for Defer state — it's pure local state). At end of Phase 2, all Defer operators force-transition: σ if Defer-due-to-partition resolved (V received via late re-flood, host validates, no equivocation observed), else NR (silent-leader rule, applies to both unresolved Defer-due-to-partition and all Defer-due-to-equivocation operators). This is the **end-of-Phase-2 force-commit rule** — OBFT-1's analog of OBFT's "round-R force-commit", but operating at end of Phase 2 within the single round.

**NR and NV are operationally interchangeable for the protocol.** Both materialize on the wire as a single message kind: a partial `σ_i^{IBE}(nr_tag_k)` from the IBE keypair on the layer's NR tag. The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to throughout as "NR-quorum" or "no-σ quorum" for short). The distinction between NR-silent and NV is **local-only diagnostic** (an operator may log *why* it didn't sign σ — for telemetry — but the cluster-wide message and counting logic are identical). All references to "NR" in the rest of this document encompass NR-silent + NV unless stated otherwise.

**Defer is what enables late-σ-emit within Phase 2 without breaking cross-phase exclusivity.** It lets the cluster distinguish "the leader is silent (commit NR fast, fall through)" from "I just haven't received V yet (wait, V might still arrive within Phase 2's late-σ-emit window)". The discriminator is **observed peer σ-emit cluster-wide** (visible at layer 0 as plaintext σ partials, visible at deeper layers via the same encrypted-partial broadcast presence — see "Peer σ-emit observability" below). If any honest peer's σ-emit is observed on this layer, the cluster knows V exists, so an operator without V locally should defer rather than NR-emit. This rule preserves baseline TBFT's fast L_0-silent fall-through (no peer σ-emit ⇒ NR-emit immediately) while enabling within-Phase-2 partition recovery (peer σ-emit observed ⇒ Defer until late re-flood completes or end-of-Phase-2 force-commit). At L_0 specifically, the σ-emit observability check is conditional on whether the receiver has a retained Phase-1 bundle — see "Validity-gate on observed σ-emit (L_0 specifically)" below.

**Validity-gate on observed σ-emit (L_0 specifically).** At L_0, σ partials are plaintext. The Defer rule at L_0 applies the validity check **conditionally on whether the receiver has a retained Phase-1 bundle** — that is the only state that lets the receiver actually verify the partial against a known V.

- **Receiver has ≥ 1 retained V at L_0.** Verify the σ partial against each retained V. Only partials that verify count as "peer σ-emit observed". An auth-valid `KindOnion` carrying a plaintext σ partial that does not verify against any retained V is treated as if no σ-emit happened — it does NOT push the operator into Defer. This closes the byzantine-garbage-σ grief: byz cannot force honest with retained V into Defer by emitting auth-signed-but-unverifiable partials.
- **Receiver has 0 retained V at L_0** (the partition case — the Phase-1 bundle hasn't propagated to this receiver yet). The receiver has no V to verify against and cannot distinguish byzantine garbage from a legitimate σ on a V they haven't received. Fall back to the deeper-layer rule: an auth-signed `KindOnion` claiming σ at L_0 counts as σ-emit observed, regardless of partial validity. This preserves Defer-due-to-partition recovery: a receiver who hasn't yet received V but observes peer σ-claims defers rather than NR-emitting, hoping late gossipsub re-flood delivers V before end of Phase 2. Byzantine garbage in this case costs the operator a force-NR at end of Phase 2 (instead of immediate NR), but does not foreclose recovery.

The asymmetry reflects what's distinguishable. With a retained V, the receiver can tell garbage from legitimate σ-emit; without one, they can't, and the encrypted-presence-equivalent fallback preserves the within-Phase-2 partition-recovery scenarios at the cost of a bounded grief surface in the no-V case.

At deeper layers (k > 0), the σ partial is encrypted and validity isn't checkable until decryption — encrypted-presence alone is sufficient for Defer (with the fake-encrypted-presence slashing rule as the post-decryption attribution backstop; see "Slashing evidence").

**Peer σ-emit observability at deeper layers.** At layer `k > 0`, σ partials are chained-encrypted (see Phase 2). Receivers can observe *that* a peer broadcast a layer-`k` onion entry (the encrypted ciphertext is on the wire, in an auth-valid `KindOnion` from the peer's operator-identity key — see Phase 2) without decrypting it — that's sufficient for the Defer rule. The Defer rule asks "does any peer auth-claim to have σ at this layer?", which the auth-signed encrypted-presence check answers. It doesn't require knowing *which* V the peer σ'd on (that knowledge would require decryption, which requires NR-quorum at prior layers). At decryption time (when prior layers' NR-quorums have unlocked the chained encryption), all encrypted partials become plaintext and Pigeonhole 2 applies normally.

**Garbage-encryption deterrence.** A byzantine operator could broadcast a well-formed-but-undecryptable ciphertext at layer `k` (encrypted under wrong tag, or arbitrary garbage bytes wrapped in a structurally-valid IBE ciphertext envelope) to fake encrypted-presence and DoS honest into Defer with no real σ-emit ever materializing. To deter this: at decryption time (when prior layers' NR-quorums unlock decryption), if a peer's auth-signed `KindOnion` decrypts at layer `k` to garbage (not a valid σ partial on any leader-known V at L_k, or fails to decrypt entirely), the auth envelope is self-contained slashable evidence of a "fake encrypted-presence" byzantine fault — `i` signed the envelope binding their identity to the encrypted payload; post-decryption verification surfaces the garbage. Detection is delayed (requires NR-quorum at layers `0..k-1` to unlock decryption) but attribution is unambiguous. The persistent reputation deterrent (see [Assumptions](#assumed)) makes the DoS attack expensive — repeated grief gets the byzantine kicked out of the cluster. See "Slashing evidence" for the corresponding case.

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

**Phase 2 sub-phasing.** Within the Phase 2 window `[T_commit, T_commit + Δ_2]`, σ-emit and NR/Defer commitment happen at different times — not simultaneously at `T_commit`:

- **σ-emit (start of window)**: operators in σ-state at `T_commit` emit their σ partials (Phase-2 onion) immediately at `T_commit`. Operators who become σ-eligible *during* the window (e.g., late gossipsub re-flood delivers V) emit σ as soon as eligibility is determined.
- **NR/Defer commitment (end of window)**: operators in NR/Defer-eligible state delay their commitment until the end of the window, at `T_commit + Δ_2`, and base the decision on σ-emits observed throughout the window. The `Δ_2 ≥ D + δ` budget is what ensures σ-emits at the start of Phase 2 propagate to all honest before the NR/Defer decision fires.

Without this sub-phasing the Defer rule cannot operate correctly — at `T_commit` start, no Phase-2 σ-emits have been observed yet, so receivers without V would always NR-emit (per silent-leader rule), foreclosing Defer-due-to-partition recovery within Phase 2. The implicit timing was load-bearing in earlier drafts of OBFT; this sub-phasing makes it explicit, and is **load-bearing for OBFT-1 specifically** since it is the only within-slot recovery mechanism for late re-flood (no round-2 fallback).

**Late σ-emit limitation.** A σ-emit at time `T_commit + t` (where `t` is during Phase 2) propagates to other honest by `T_commit + t + D + δ`. For other honest to observe it before their NR/Defer decision at `T_commit + Δ_2`, we need `t + D + δ ≤ Δ_2`, i.e., `t ≤ Δ_2 − (D + δ)`. With `Δ_2 = D + δ` (the minimum), this requires `t = 0` — only σ-emits at the very start of Phase 2 are observable in time. Late σ-emits (operators receiving V near end of Phase 2) won't propagate before NR-decision and are effectively invisible for the slot — they cannot keep other honest in Defer state past their decision point.

**Recommendation: set `Δ_2 ≥ 2(D + δ)` for OBFT-1.** This widens the late-σ-emit window to `D + δ`, allowing operators receiving V via late re-flood to σ-emit and have their σ-claim propagate before peers' NR-decisions. Since OBFT-1 has no round-2 fallback for late deliveries, the late-σ-emit window inside Phase 2 is the protocol's only within-slot partition-recovery mechanism. The recommendation costs `D + δ` of additional Phase-2 latency (e.g., 150ms at D = 100ms, δ = 50ms) — small relative to the slot budget at typical D, and substantially larger headroom is available because Phase 2.5 is dropped (see §Application: SSV Ethereum proposer duty / Timing budget).

Each operator emits their commitment per layer based on the four-state model from Phase 1:

- **σ-state**: include the (encrypted) σ partial in the onion at this layer.
- **NR-state** (silent-leader, equivocation-as-NR, or NV): emit a partial `σ_i^{IBE}(nr_tag_k)` separately from the onion. These IBE partials are the witnesses that unlock the next layer.
- **Defer-state**: omit the layer from the onion AND do not emit NR. (No wire artifact for Defer state — it's purely local.) At end of Phase 2, all Defer operators must transition to either σ (if Defer-due-to-partition resolved: V received via late re-flood, host validates, no equivocation observed) or NR (silent-leader rule applies to unresolved Defer-due-to-partition and all Defer-due-to-equivocation).

`i` gossips its onion together with all NR partials, **wrapped in an auth envelope**: a structured `KindOnion` message signed with `i`'s operator-identity key, binding `(protocol_tag = "OBFT-1-v1", message_kind = "phase2-onion", cluster_id, slot, operator_id i, onion_payload, nr_partials)`. The auth signature attributes every per-layer commitment in the onion (plaintext σ at L_0, encrypted σ at deeper layers, NR partials) to `i`'s identity. Receivers reject any `KindOnion` whose envelope auth fails verification. This is what makes the encrypted-presence check at deeper layers (used by the Defer rule — see "Operator commitments — σ, NR, NV, Defer / Peer σ-emit observability at deeper layers") attributable: a peer cannot anonymously broadcast garbage ciphertext to fake encrypted-presence.

**Per-operator commitment is exclusive across phases.** OBFT-1 inherits TBFT's cross-phase exclusivity. The commitment is *one decision per operator per layer, spanning Phase 1 and Phase 2*:

- An operator who emitted `σ_i^V(V_{L_k})` at layer `k` on any value `V` has σ-side committed at this layer; they may **not** subsequently broadcast an NR/NV partial on `nr_tag_k`. They may also **not** σ on a different `V'` at the same `(slot, layer)` — see Pigeonhole 2 in "Fault tolerance / Safety".
- An operator who emitted an NR/NV partial on `nr_tag_k` has NR-side committed at this layer; they may **not** subsequently emit σ on any V at L_k.
- The layer-`k` **leader**'s Phase-1 σ_V counts as their σ-side commitment at layer `k`. They may not subsequently emit NR/NV on `nr_tag_k`.
- Across layers, commitments are **independent**: an operator's σ-or-NR commitment at layer `k` does not constrain their commitment at layer `j ≠ k`. Hedging across layers is preserved (an operator may σ at multiple layers if they validated multiple V's).

EKM enforces these exclusivities cryptographically (slashing-protection log keyed on `(slot, layer, commitment-side)`); see "Preconditions on the host application / Slashing-protection scope". OBFT-1's EKM does **not** need cross-round atomicity (no rounds), persistent partial-sig caching for cross-round re-emission, or deterministic re-signing fallback — see [EKM coordination model](#ekm-coordination-model) for the simplified rules.

A byzantine operator that publishes both σ and NR on the same `(slot, layer)` is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For each layer's leader specifically, "cross-signing" includes any Phase-1 σ + later NR pair on the same slot.

### Phase 3 — Local decryption and reconstruction `[T_commit + Δ_2, T_round_end]`

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches on some V (output produced), or NR-quorum reaches (advance to next layer), or neither (stay at this layer for next round).

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches on some V (output produced), or NR-quorum reaches (advance to next layer), or neither (slot misses).

```
L_C = 0                                        # current frontier; advances per loop iteration
sigs[k] for k in [0, K)                        # σ pools, indexed by layer (initially empty)
nrs[k]  for k in [0, K)                        # NR pools, indexed by nr_tag_k (initially empty)

for k in [0, K):
    # Aggregate σ pool at layer k.
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
        # NR-quorum did not reach at L_k. No path forward; slot misses.
        break    # exit the layer-walk

if L_C == K and no σ-quorum reached:
    # Walked all layers; no output. Slot misses.
    pass

# End of reconstruction. If output produced, halt; else slot misses.
```

**`T_round_end`** for the deadline rule is the cutoff by which the operator must have received all Phase-2 onions and NR partials they intend to count. Practically, `T_round_end = T_commit + Δ_2 + Δ_3` where `Δ_3` is the reconstruction window. The deadline rule (caveat 3) bounds the gap between phases against propagation P99/P999 and clock skew.

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at that layer: no σ partial, no NR partial. Standard threshold cryptography — only signed messages count. Within the partial-synchrony envelope `D`, gossipsub propagation is expected to deliver all honest broadcasts to all honest receivers before `T_round_end`.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within `T_round_end` (see "Fault tolerance / Liveness"). If more than `f` operators are offline or byzantine combined, neither σ nor NR quorums reach their thresholds and the slot is missed. Within the f-bound, OBFT-1 covers all in-envelope cases (real propagation ≤ D): healthy path, silent-leader fall-through, multi-leader fall-through (sequential within Phase 3 reconstruction), all-Defer-due-to-equivocation fall-through to next layer via end-of-Phase-2 force-NR. View-divergence cases — equivocation σ-locked splits and host-validity divergence — are explicitly out of recovery scope (some splits succeed naturally as a side-effect of leader-σ-V counting; rest slot-miss). Real propagation > D is Class A "sustained partition" under R=1's framing — out of scope by definition. See [Assumptions and implications](#assumptions-and-implications).

### Slot structure

OBFT-1 runs a single agreement round per slot. The slot proceeds as follows:

1. **Phase 1**: K leaders broadcast their Phase-1 bundles per their per-layer fetch windows (`[T_{K-1}, T_{K-1} + Δ_1]`, ..., `[T_0, T_0 + Δ_1]`), with `T_0 + Δ_1 ≤ T_candidate_accept = T_commit − (D + δ)`.
2. **Phase 2** `[T_commit, T_commit + Δ_2]`: each operator emits their K-layer onion and any NR partials based on their current per-layer commitment state. Operators in **σ-state** emit σ at start of Phase 2; operators who become σ-eligible during the window (via late re-flood) emit σ as soon as eligibility is determined; operators in **NR/Defer-eligible** state delay commitment until end of window (`T_commit + Δ_2`) and base the decision on σ-emits observed throughout the window. At end of Phase 2, all Defer-state operators force-commit per the end-of-Phase-2 force-commit rule (see [§Phase 2](#phase-2--onion-broadcast-t_commit-t_commit--Δ_2) → "Operator commitments — σ, NR, NV, Defer"): σ if Defer-due-to-partition resolved (V received via late re-flood, host validates, no equivocation observed), else NR.
3. **Phase 3** `[T_commit + Δ_2, T_round_end]`: each operator runs the K-layer reconstruction walk. If σ-quorum reaches on some V at any layer, output the V; halt. If NR-quorum reaches up to some layer `L_C < K`, advance L_C and continue the walk. If neither σ-quorum nor NR-quorum advance unlock at any layer, the slot misses.

**Slot timing**: `T_round_end = T_commit + Δ_2 + Δ_3`. Phase 1 fetch occupies `[slot_start, T_commit]`. The slot's total consensus budget (Phase 2 + Phase 3) is `Δ_2 + Δ_3 ≈ 2(D + δ) + 100ms` with the recommended Phase 2 widening (see §Phase 2 — Late σ-emit limitation).

## Preconditions on the host application

OBFT-1 is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at the moment the operator considers committing σ to it. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be signed (the σ_V locks it). See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` before committing σ. A `not-valid` verdict turns the operator's commitment into NV (see "Phase 1 / Operator commitments — σ, NR, NV, Defer").

The protocol-level checks (cryptographic auth, envelope re-derivation, timing cutoff `T_candidate_accept`) are protocol-internal and do not depend on the application.

For SSV's Ethereum proposer duty, application-level checks include slot match, proposer index match, fork/domain match, parent root match against the operator's view of the head, relay metadata validity, doppelganger / slashing-protection checks, and block encoding well-formedness. See the Application section for details. The protocol body does not enumerate or interpret these — they're host concerns.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot (potentially up to K, one per layer they validated). Constraints per (slot, layer):

- **Each layer's leader signs σ_V exactly once per (slot, layer)** — on the final V they commit to, after any pre-signing refreshes during the host's fetch loop. Refreshes update V via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this single-σ-V-per-(slot, layer) constraint cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational workflow.
- **Each operator commits σ-or-NR exclusively per (slot, layer), across phases.** Honest who include σ on any V at layer `k` may not subsequently broadcast NR/NV on `nr_tag_k` AND may not σ on a different V' at the same layer (single-σ-V per operator per layer); honest who broadcast NR/NV may not subsequently include σ at L_k. Each layer's leader's Phase-1 σ counts as their σ-side commitment — they cannot emit NR/NV for that layer. EKM enforces this cross-phase + single-V exclusivity by coordinating across the operator's V-signing and IBE-signing shares (distinct keys, but slashing-protection log keys on (slot, layer)): an NR/NV partial on `nr_tag_k` is rejected if the same operator has previously signed σ at L_k, and vice versa; a σ partial on V' at L_k is rejected if the same operator has previously signed σ on V ≠ V' at L_k. Pigeonhole 1 and 2 below rely on these rules.
- Every operator signs each layer's V_{L_k} they consider valid (host returns `valid`) in their Phase-2 onion, provided no equivocation evidence is observed at that layer. The Defer state (no σ, no NR) means the operator hasn't decided yet — Defer-due-to-partition operators may still σ-emit later within Phase 2 if V arrives via late re-flood and the host validates it (no equivocation observed); Defer-due-to-equivocation operators stay in Defer until end of Phase 2 and then force-NR. See "Phase 1 / Equivocation handling — detect and slash".

EKM/slashing-protection must permit the operator's per-layer Phase-2 σ signings (one σ per layer per slot) plus the leader's Phase-1 σ_V (exactly one per slot/layer the operator is leading), without flagging duplicates — the cluster's safety property collapses these to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating points are **candidate signing** (Phase-1 leader and Phase-2 onion alike) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`).

### EKM coordination model

The "EKM" referenced throughout this spec is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log. Production EKMs (Web3Signer, Dirk, etc.) typically expose per-key signing checks; OBFT-1 requires a coordination layer above them.

**Log schema.** One row per signing event: `(slot, layer, side, value_root)` where `side ∈ {"σ", "NR"}`; `value_root` is set on σ-side entries, null on NR-side. No round dimension (single-round protocol).

**Per-request checks.**

- **Sign σ on V at (slot, layer)** (V-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "σ", value_root(V))`. The single-σ-V rule means a second signing attempt with `V' ≠ V` at the same `(slot, layer)` is rejected even though the side matches — the existing row already contains `value_root(V)`.
- **Sign NR on `nr_tag_k` at (slot, layer)** (IBE-keypair share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "NR", null)`.

**Simplifications relative to OBFT(R≥2).** OBFT-1 drops three EKM concerns that OBFT requires for cross-round operation:

- **No cross-round atomicity.** OBFT(R≥2) requires sign-and-log to be atomic across both shares (V-share + IBE-share) so that a partial signed in round 1 is correctly recognized in round 2. OBFT-1 has no rounds, so atomicity collapses to standard per-request transactional behavior.
- **No persistent partial-sig cache.** OBFT(R≥2) caches σ partials so they can be re-emitted in rounds 2..R without re-signing; the cache must survive operator restarts. OBFT-1 has no re-emission, no cache requirement.
- **No deterministic re-signing fallback.** OBFT(R≥2) needs a fallback if the cached partial is lost mid-slot (allow re-signing if the log row matches the same `(slot, layer, side, value_root)`). OBFT-1 has no re-signing path; the slot's σ-partial is signed exactly once per (slot, layer) per operator and never reused across the protocol.

**Implementation paths.** (a) A custom EKM that maintains the unified log natively across both keypair shares. (b) A coordinator component in the SSV operator process that wraps two standard EKMs (one per keypair share) and enforces the cross-keypair rules in the operator's slashing-protection database — a single transactional log, two keypair-specific signing-request handlers reading and writing it. The coordinator is **simpler** than the OBFT-equivalent: it requires only the unified log to be transactionally consistent across both shares for the **single signing event per (slot, layer)**; no atomicity-spanning-rounds, no persistence-of-cached-partials, no deterministic-re-signing-fallback. Path (b) is the path SSV will most likely take.

**EKM correctness is defense-in-depth for safety, not the sole enforcement.** OBFT-1's safety (Pigeonholes 1 and 2) holds when honest operators correctly implement the protocol rules — single-σ-V per (slot, layer) per operator, σ-XOR-NR per layer. The **protocol layer** (operator software implementing the OBFT-1 state machine) is the primary enforcement point: it determines when σ vs NR vs Defer is requested from the EKM in the first place. The **EKM** is a catch-net: it rejects signing requests that violate the slashing-protection invariants, providing defense-in-depth even if the protocol layer is buggy.

For a single honest operator to produce safety-violating behavior (e.g., σ on V and σ on V' at the same `(slot, layer)`), **both layers must be buggy in compounding ways**: the protocol layer must request the second σ (violation of σ-eligibility logic) AND the EKM must fail to reject it (violation of slashing-protection lookup or atomicity). A single-layer bug typically does not break safety:

- Protocol-layer bug only: the EKM rejects the bad request; no double-sign emitted on the wire.
- EKM-layer bug only: the protocol layer doesn't ask for double-signing, so the EKM bug is never exercised.

Cluster-wide safety violation (Pigeonhole 2 producing two qV-quorums on different V's) requires aggregating these single-operator violations to reach `2 · qV = 4f+2` partials across two V's. At `f = 1, n = 4`, one byzantine operator contributes ≤ 1 partial per V (≤ 2 total); three correct honest contribute exactly 3 partials total (single-σ-V each); sum 5 < 6 = 2 · qV. The minimum safety-violating configuration is therefore **one byzantine operator plus one honest operator with compounding protocol+EKM bugs** — together producing the missing partial. This is two misbehaving operators total, exceeding the `f = 1` trust budget. Single-layer bugs alone are tolerated; safety requires both layers to be correct on at least `n − f = 3` operators.

**Trust posture is the same as QBFT.** Both protocols rely on honest-majority correct implementation of the protocol logic *plus* correct slashing-protection — neither is "100% cryptographic" against operator-side software bugs (see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic)). The difference is in the slashing-protection layer's maturity: QBFT's per-key slashing-protection (Web3Signer, EIP-3076 interchange format) has decade-of-production hardening; the OBFT-1 coordinator is novel, so reaching comparable defense-in-depth robustness requires deliberate engineering investment in (a) test coverage on cross-keypair atomicity for the single signing event per (slot, layer), (b) fault-injection testing of the operator-restart scenario, (c) optionally operational margin via larger `n` (e.g., `n ≥ 5` keeps `f = 1` while expanding the bug-budget headroom). OBFT-1's smaller surface (no cross-round atomicity, no cache persistence, no re-signing fallback) means the coordinator is closer to an EIP-3076-style per-key extension than OBFT's bigger novel coordinator.

**Summary of EKM failure modes.** A **maliciously compromised** EKM (signs requests outside protocol rules, or generates signatures the protocol layer didn't request) is byzantine-equivalent and directly consumes f-budget. A **passively buggy** EKM (fails to reject bad requests but doesn't generate signatures on its own) requires the protocol layer to also have a compounding bug for safety-violating behavior to actually occur — see the defense-in-depth analysis above. In both cases, the cluster's overall trust posture follows the standard "honest-majority cryptographic" framing — see [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic).

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model. Operational rules that handle specific faults (Defer-state deferral, equivocation detect-and-slash, cross-signing detection, head-change refresh) are described inline in the relevant Phase sections; the analyses of *what those rules buy* and *under what conditions* live here.

### Trust model

- **Byzantine bound `f`** with cluster size `n ≥ 3f+1`: up to `f` operators may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, etc.). `2f+1` honest are guaranteed. Running example: `n = 4, f = 1` (3 honest guaranteed).
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `D` (propagation P99/P999) and clock skew `δ`. The cutoffs `T_candidate_accept = T_commit − (D + δ)` and `T_round_end = T_commit + Δ_2 + Δ_3` operationalize this bound. Safety holds against arbitrary network adversaries; only liveness depends on synchrony. **OBFT-1's effective propagation envelope is exactly `D`** — single round, one D worth of re-flood tolerance. Real propagation > D for any honest receiver violates assumption 2's framing at OBFT-1 and the slot may miss cleanly (Class A "sustained partition", see §Failure modes). Multi-round OBFT (R ≥ 2) extends the envelope to `R · D` at the cost of slot budget and protocol-machinery complexity.

### Safety (cryptographic, honest-majority)

**Claim:** at most one full `V` signature is ever produced per OBFT-1 instance per slot — across any layer, on any value, across any combination of σ sources — cluster-wide, against an offline-aggregating byzantine within the f-bound, regardless of which honest aggregation rules are followed. (See [Implications of safety being honest-majority cryptographic, not 100% cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic) for what this scope does and doesn't cover.)

The proof rests on three pigeonhole arguments — the same as baseline TBFT (Pigeonholes 1, 2) extended to chained encryption at `K > 2` (Pigeonhole 3, per [TBFT.md](TBFT.md) Appendix C). All three are properties of cluster-wide signed messages, enforced cryptographically by [EKM coordination model](#ekm-coordination-model) rules at the operator level — no honest-aggregation rule required.

The pool definitions used in the arguments below:

- **σ-pool on V at L_k**: `{σ_i^V(V) partials at L_k}`, deduplicated per operator. Includes the leader's Phase-1 σ_V when valid. Once a partial is emitted, it stays on the wire — no "revocation" semantics.
- **NR-pool at L_k**: `{σ_i^{IBE}(nr_tag_k) partials}`, deduplicated per operator.

**Pigeonhole 1 — σ-vs-NR at the same layer.** σ-quorum on `V` (any V) at `L_k` and NR-quorum on `nr_tag_k` cannot both reach.

- σ-quorum on V: `h_σ + byz_σ ≥ qV = 2f+1` (where h_σ counts honest with σ partials on V at L_k from any phase, deduplicated per operator; includes the layer's leader's Phase-1 σ if they're an honest σ-side committer on V).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Cross-phase exclusivity (per "Slashing-protection scope"): `h_σ + h_NR ≤ 2f+1`. Each honest commits σ-or-NR per layer at most once, EKM-enforced. Includes the layer's leader: their Phase-1 σ counts as σ-side commitment.
- Byzantine cross-signing: `byz_σ + byz_NR ≤ 2f` (each byz contributes at most 1 to each pool).
- If both quorums reached: `h_σ + h_NR ≥ 4f+2 − 2f = 2f+2`. But `h_σ + h_NR ≤ 2f+1`. Contradiction. ∎

**Pigeonhole 2 — two σ-quorums on different values at the same layer.** Two distinct `V`'s cannot both reach σ-quorum at the same layer (e.g., via leader equivocation that some honest σ-commit on early before observing evidence):

- `(h_σ_V + h_σ_V') + (byz_σ_V + byz_σ_V') ≥ 2 · qV = 2(2f+1)`.
- Honest sign at most one V per layer (single-σ-V exclusivity, EKM-enforced — see "Slashing-protection scope"): `h_σ_V + h_σ_V' ≤ 2f+1`. The layer's leader counts here: they sign σ_V exactly once per (slot, layer), contributing to one V's pool.
- Byzantine can sign multiple V's: `byz_σ_V + byz_σ_V' ≤ 2f`.
- Bound: `(2f+1) + 2f = 4f+1 < 4f+2`. Contradiction. ∎

This is the key safety constraint underlying OBFT's "permit equivocation, slot-miss on view-divergence" framing: regardless of which V's honest σ-commit on under equivocation, at most one V can reach qV cluster-wide. There is no two-output safety failure even when honest operators split across V's; the cluster either reaches qV on a single V (some patterns recover naturally) or no V reaches qV (slot misses).

**Pigeonhole 3 — cross-layer safety under chained encryption.** Two distinct V signatures (V_k and V_{k+m} for any `m ≥ 1`) cannot both be reconstructed cluster-wide.

- For V_k sig: σ-quorum on V_k at L_k must reach.
- For V_{k+m} sig: σ partials at L_{k+m} must decrypt, which requires NR-quorum at L_k (and at every layer between L_k and L_{k+m}, per chained encryption).
- By Pigeonhole 1: if σ-quorum at L_k reaches, NR-quorum at L_k does not. So L_k's chained encryption layer at L_{k+1}, ..., L_{k+m} stays sealed.
- Therefore V_{k+m} sig is unreconstructable when V_k sig is reconstructable. ∎

The argument is symmetric: if V_{k+m} sig is reconstructable, then NR-quorum reached at L_k (to allow decryption), so by Pigeonhole 1 σ-quorum at L_k did not reach, so V_k sig is unreconstructable. Applied inductively, at most one V signature reconstructs cluster-wide across all K layers.

**Cryptographic primitive — chained IBE.** Layer-`k` σ partials are encrypted under `nr_tag_0 ∧ nr_tag_1 ∧ ... ∧ nr_tag_{k-1}`. Decryption requires NR-quorum at each prior layer. Implementation: each level of the chain is a standard threshold-IBE ciphertext (drand/tlock) using `nr_tag_j` as the tag. At K=2 the chain has only one level (single tag `nr_tag_0`); at K=3 there are two levels nested; etc. Same construction as [TBFT.md](TBFT.md) Appendix C.

The arguments above apply symmetrically to all K layers. **None of the proofs depends on honest operators excluding cross-signers from their aggregation** — they're properties of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule. Cross-phase exclusivity (σ XOR NR per layer) and single-σ-V (one V per operator per layer) are enforced cryptographically by EKM at signing time, not by aggregator-side filtering.

### Liveness (synchrony-conditional)

OBFT-1's liveness is **partial-synchrony-conditional within `T_round_end`** — the protocol's slot budget. The single-round structure absorbs network-induced failures up to one D worth of propagation tolerance via the late-σ-emit window inside Phase 2's sub-phasing. View-divergence (equivocation, validity-divergence) is NOT recovered by the protocol; some splits succeed naturally as a side-effect of leader-σ-V counting toward whichever V reaches qV first, others slot-miss. Equivocation cases are slashable (leader pays the stake-based penalty); validity-divergence is not attributable.

If propagation between honest operators stays bounded by `D`, the protocol terminates with a V signature on some layer (the first layer where σ-quorum reaches under cluster-wide V receipt, or the deepest layer reachable via NR fall-through with a valid backup leader). If propagation exceeds `D`, or more than `f` operators are byzantine/offline, the slot is missed. **Safety holds in either case.**

**Best case (healthy at L_0)**: all honest receive V_{L_0} within `D + δ`; all 3 honest σ-emit + leader's Phase-1 σ = 4 ≥ qV. Slot succeeds in 2 RTTs (Phase 1 + Phase 2). Same as baseline TBFT.

**Late-re-flood-within-Phase-2 recovery**: leader's bundle arrived at some honest before `T_candidate_accept` but at others only after, with the late arrival landing within Phase 2's late-σ-emit window `Δ_2 − (D + δ)`. Late-receiving honest σ-emit on V upon arrival; their σ-claims propagate to peers before the end-of-Phase-2 NR/Defer decision. Defer-state honest observe peer σ-emit and stay Defer (rather than NR-emit), then transition to σ on local arrival. σ-pool reaches qV. Slot succeeds in 2 RTTs. With recommended `Δ_2 ≥ 2(D + δ)`, the late-σ-emit window is `D + δ` — captures gossipsub propagation that completes within `2(D + δ)` of the leader's broadcast. Wider partition tails (real propagation > `2(D + δ)` at single-D budget) are out-of-envelope and slot-miss cleanly.

**Equivocation handling (no protocol-level recovery; outcome depends on byzantine timing)**: byzantine `L_0` equivocates, broadcasting distinct V's selectively. Case analysis at `f = 1, n = 4` (3 honest A, B, C; byz D = leader):

Recovered via σ-quorum at L_0 (when honest happen to split such that 2-of-3 σ-commit on the same V; leader's σ_L^V completes the pool):

- **D delivers V to {A, B}, V' to {C}.** σ-pool on V = A + B + D's σ_L^V(V) = 3 = qV. σ-quorum reaches. Slot outputs V.
- **D delivers V to {A}, V' to {B, C}.** σ-pool on V' = B + C + D's σ_L^V(V') = 3 = qV. Reaches. Slot outputs V'.

Recovered via NR-quorum at L_0 → fall-through to L_1 (when all honest land in Defer-due-to-equivocation; end-of-Phase-2 force-NR produces NR-quorum):

- **All-Defer outcome (byz delivers V's early enough that re-flood spreads conflicts before Phase 2 σ-emit time).** Each honest retains ≥ 2 distinct V's by Phase 2 emit time → all 3 in Defer-due-to-equivocation. σ-pools at L_0 ≤ byz partials per V < qV. End-of-Phase-2 force-NR: 3 honest force-NR + 0 byz (σ-locked from Phase 1) = 3 = qEnc → NR-quorum reaches at L_0 → in the same Phase 3 reconstruction walk, advance to L_1; if L_1 honest, σ-quorum at L_1 reaches and slot succeeds at L_1. Asymmetric-retention patterns under 3+-V flood typically land here when byz delivery timing is "too early" for grief purposes.

Not recovered — slot misses at L_0 with no fall-through (σ-locked split patterns; slashable):

- **D delivers V to {A}, V' to {B}, both to C (or neither).** A σ-locked on V; B σ-locked on V'; C in Defer-due-to-equivocation (received both) or Defer-due-to-partition (no V received). σ-pool on V = A + leader = 2 < qV. σ-pool on V' = B + leader = 2 < qV. C force-NR at end of Phase 2; NR-pool = 1 < qEnc → no fall-through. Slot misses.
- **D delivers distinct V_1 to A, V_2 to B, V_3 to C (1-1-1 split).** Each honest σ-commits on their V_i on receipt before observing equivocation. All 3 honest σ-locked on different V's. σ-pool on each V_i = 1 honest + leader's σ_L^V = 2 < qV. NR-pool = 0 (all σ-locked, cross-phase exclusivity). Slot misses.

**Byzantine timing controls which class fires.** Delivering V's early in Phase 1 leaves time for gossipsub re-flood to spread conflicts → all-Defer outcome → slot succeeds at L_1. Delivering near end-of-Phase-1 leaves insufficient re-flood time → σ-locked split outcome → slot misses at L_0. **A byzantine wanting to grief reliably picks the latter**; the persistent reputation deterrent (assumption 4) is what makes this tolerable across many slots. (This exposure is identical to OBFT(R≥2) — these are R-invariant patterns.)

In all cases, Pigeonhole 2 ensures at most one V can reach qV cluster-wide regardless of how honest σ-emissions split — there is no two-output safety violation. The TBFTR-style **Phase 2a/2b split** (broadcast-only Phase-2a where operators re-flood retained Phase-1 bundles without σ-emitting, then Phase-2b where σ-emits happen on the cluster's stabilized winner V) recovers all equivocation patterns at +1 RTT cost; documented as future improvement, not in current OBFT-1.

**Sub-partial-synchrony (real propagation > D)**: if propagation exceeds the cluster's single-D budget, late honest don't σ-emit by end of Phase 2 and slot misses. **No safety violation.** Real propagation > D is Class A "sustained partition" under R=1's framing. Multi-round OBFT (R ≥ 2) extends the envelope to `R · D` at the cost of slot budget and protocol-machinery complexity (see [OBFT.md](OBFT.md)).

**Multi-failure fall-through (K ≥ 3)**: with `K > 2` layers, the cluster can fall through past multiple silent or non-validating layers **within Phase 3's single reconstruction walk** — the walk processes layers sequentially using local decryption (no per-layer RTT). E.g., at K=4 with L_0, L_1, L_2 all silent: NR-quorum reaches at L_0 in Phase 2 (silent-leader rule fires for all 3 honest cluster-wide); same at L_1 and L_2; σ-quorum at L_3 if L_3 leader honest. All happens in Phase 2 + Phase 3 windows. Recovery scope at K layers is achieved without round-change overhead — a structural difference from QBFT (which round-changes once per failed leader). At K = n = 4, every cluster member is a leader exactly once; pigeonhole guarantees ≥3 honest leaders at f=1, providing maximum K-fall-through depth within a single round.

**Adversarial scheduling within the partial-synchrony envelope**: the network adversary delays each message by ≤ D within the synchrony bound.

- *Safety unaffected.* Pigeonholes 1, 2, 3 are properties of the cluster-wide signed-message set, not of arrival times. The adversary can delay messages but cannot forge signatures or violate EKM rules. At most one V signature reconstructs cluster-wide regardless of timing.
- *Liveness — adversary delays V to ≤ 1 honest within D.* The other 2 honest σ-emit; σ-pool = 2 + leader = 3 = qV. **Quorum reaches without the delayed operator.** At f=1, n=4 the adversary's leverage against ≤ 1 honest is wasted.
- *Liveness — adversary delays V to ≥ 2 honest beyond D.* Real propagation > D for those operators violates assumption 2 at OBFT-1's framing → Class A sustained partition. Slot misses cleanly.

OBFT-1 covers all in-envelope adversarial scheduling cases (delay ≤ D against ≤ 1 honest succeeds; ≤ D against ≥ 2 honest is in-envelope and the σ-pool still reaches qV from the unaffected operators if they receive V on time; > D is out-of-envelope). For broader adversarial-scheduling tolerance (delays in `(D, 2D]`), use [OBFT(R=2)](OBFT.md).

**OBFT-1 recovers strictly the same in-envelope cases as OBFT(R=2)** — within R=1's narrower envelope. View-divergence cases (host-validity divergence and equivocation patterns that don't naturally reach qV) are out of recovery scope at any R. The genuine difference vs OBFT(R=2) is the envelope size: OBFT-1 covers `≤ D`, OBFT(R=2) covers `≤ 2D`; cases in `(D, 2D]` are Class A under OBFT-1 and in-envelope at OBFT(R=2). The TBFTR-style **Phase 2a/2b split** (future improvement, R-orthogonal) closes the equivocation gap at +1 RTT cost.

### Liveness comparison: OBFT-1 vs OBFT(R=2) vs QBFT

The table below puts OBFT-1, OBFT(R=2), and QBFT side-by-side per scenario at the standard SSV proposer-duty configuration (n=4, f=1, K=4, ~4s relay cutoff). Timing assumes D=100ms uniform, δ=50ms (see [Timing budget](#timing-budget--concrete-configurations)). For QBFT, RT≈2s per round-change is SSV's current production tuning.

| Scenario | OBFT-1 outcome | OBFT(R=2) outcome | QBFT outcome |
|---|---|---|---|
| Healthy (all honest receive V_{L_0}) | σ-quorum reaches in 2 RTTs. ✓ at L_0 in ~600ms (Phase 2 + Phase 3 with widened Δ_2). | Same. ✓ at L_0 in ~500ms (narrower Δ_2). | PROPOSE→PREPARE→COMMIT (3 RTTs) + post-consensus (1 RTT). ~750ms. ✓ |
| Byzantine leader silent | 0 σ-emits → all honest NR (silent-leader rule) → NR-quorum at L_0 reaches in Phase 2 → automatic fall-through to L_1 in Phase 3 walk; σ-quorum at L_1 if leader honest. ✓ in ~600ms. | Same. ✓ in ~500ms. | Round 1: no PROPOSE arrives; round timeout (~2s). Round 2: new leader proposes; succeeds in ~750ms. ✓ in ~2.75s. |
| Late re-flood within Phase 2 (≤2 honest miss V at `T_candidate_accept`, V arrives via late gossipsub before end of Phase 2's late-σ-emit window) | Defer-state operators σ-emit on V upon arrival; σ-quorum reaches by end of Phase 2. ✓ in ~600ms. With recommended Δ_2 ≥ 2(D + δ), late-σ-emit window is `D + δ ≈ 150ms`. | Same — within R=1's envelope. ✓ in ~500ms. | Round 1: PREPARE-pool short; timeout (~2s). Round 2: new leader re-fetches + proposes; succeeds in ~750ms. ✓ in ~2.75s. |
| Aggressive marginal (≥1 of 3 honest miss V even after Phase 2's late-σ-emit window — real propagation > D + Phase 2 slack) | **Out of envelope under R=1** (Class A sustained partition). ✗ Slot misses cleanly. | In envelope at R=2: Round 2 re-flood delivers V; σ-quorum reaches. ✓ in ~1.15s. | Round 1: PREPARE-pool short; timeout (~2s). Round 2: new leader; succeeds in ~750ms. ✓ in ~2.75s. |
| Byzantine leader equivocates, 2-1 split where one V already has 2 honest + leader-σ_L^V | σ-pool on the 2-honest-V = 2 + leader = 3 = qV; reaches naturally. ✓ in ~600ms. | Same. ✓ in ~500ms. | Round 1: PREPARE-pool split; timeout. Round 2: new leader proposes; succeeds. ✓ in ~2.75s. |
| Byzantine leader equivocates, σ-locked split pattern (1-1-1, 1-1-Defer-C, 1-Defer-Defer) | σ-pools split below qV; NR-pool below qEnc (σ-locked operators can't NR). **✗ slot misses at L_0;** no fall-through. Equivocation slashable. | Same exposure (R-invariant). ✗ slot misses. Equivocation slashable. | Round 1: PREPARE split; timeout. Round 2: new leader proposes a fresh V; honest converge; succeeds. ✓ in ~2.75s. **QBFT recovers what OBFT-1/OBFT don't** via "new leader proposes fresh V" mode. |
| Byzantine leader equivocates, all-Defer outcome (byz delivers V's early; re-flood spreads conflicts before σ-emit) | All 3 honest in Defer-due-to-equivocation. End-of-Phase-2 force-NR → NR-quorum at L_0 → fall-through to L_1 in Phase 3 walk; if L_1 honest, ✓ at L_1 in ~600ms. Equivocation slashable. | Same recovery via Round-R force-NR. ✓ in ~500ms (round 1) or ~1.15s (round 2 if not naturally reached in round 1). | Same recovery as σ-locked split row: round-2 new leader proposes fresh V. ✓ in ~2.75s. |
| Multi-failure fall-through (multiple silent leaders) | At K=4 with L_0, L_1, L_2 silent: NR-quorum reaches at each in Phase 2; Phase 3's walk decrypts down to L_3; σ-quorum at L_3 if honest. **All in single Phase 2 + Phase 3 windows.** ✓ in ~600ms. | Same. ✓ in ~500ms. | Round 1: leader 1 silent → timeout (~2s). Round 2: leader 2 silent → timeout (~2s). Round 3: leader 3 silent → timeout (~2s). Round 4: succeeds. ✓ in ~7s — past 4s cutoff. ✗ for proposer duty. **OBFT-1's K-layer parallel fall-through beats QBFT's serial round-change**. |
| Host-validity divergence (head-change mid-slot, strict host) | Out of scope (assumption 3 — host stabilizes verdict at Phase-1 acceptance). Same as OBFT(R=2). | Same. | Round 1: validators with stale head don't PREPARE; timeout. Round 2: new leader fetches at moved head; if quorum agrees, succeeds. ✓ in ~2.75s. **QBFT recovers what OBFT-family doesn't** via fresh-V refetch on round-change. |
| Adversarial scheduling — adversary delays V to ≥2 honest beyond D | **Out of envelope** (Class A). ✗ Slot misses. | If delay ≤ 2D: in envelope at R=2; round 2 re-flood may resolve. ✓ in ~1.15s. If delay > 2D: out of envelope. ✗ | Round 1: PREPARE delayed → timeout (~2s). Round 2: new leader; if adversary delays again → round 2 timeout. ✗ Within 4s, QBFT tolerates ≤ 1 round of adversarial delay. |
| Sustained partition (real propagation > envelope) | OBFT-1 envelope is D; exceeded → ✗ slot misses. Safety holds. | OBFT(R=2) envelope is 2D; exceeded → ✗ slot misses. Safety holds. | QBFT round-budget × RT exceeded; slot misses. ✗ Safety holds. |
| > f operators offline/byzantine | Standard 3f+1 violation; slot misses. ✗ Safety holds. | Same. ✗ | Same. ✗ |

**Summary of recovery-scope differences:**

- **OBFT-1 ≡ OBFT(R=2) within OBFT-1's envelope (`real propagation ≤ D`)**: identical recovery outcomes. The R-machinery in OBFT(R=2) only matters in the partition regime `(D, 2D]`, which is Class A under OBFT-1.
- **OBFT(R=2) > OBFT-1 in `(D, 2D]`**: aggressive-marginal partitions (Class B in OBFT(R=2), Class A in OBFT-1).
- **OBFT-family > QBFT in latency and multi-leader-failure**: OBFT-1's healthy path is ~600ms (vs ~750ms QBFT); K-layer parallel fall-through is in-round (vs QBFT's serial round-change at ~2s/round, exceeding the 4s budget at K-1=3 silent leaders).
- **QBFT > OBFT-family in 1-1-1 equivocation and host-validity divergence**: QBFT's "round-change with fresh-V" handles these structurally; OBFT-family relies on assumption 3 and assumption 4.
- **All three fail equivalently** on sustained partition beyond their respective envelopes and on > f byzantine.

The choice between OBFT-1, OBFT(R=2), and QBFT for SSV proposer duty depends on (a) observed propagation tail beyond D — if `(D, 2D]` partitions are non-trivial in production, OBFT(R=2) is preferred; if the tail is thin, OBFT-1 saves spec/EKM complexity and submission headroom; (b) observed re-org rate; (c) the cluster's tolerance for 1-1-1 equivocation (handled by reputation deterrent in OBFT-family, recovered in QBFT). Detailed cost-side trade-offs (latency, bandwidth, cryptographic primitive maturity) are in [Appendix A.4](#a4--comparison-with-qbft).

### Equivocation handling

See "Phase 1 / Equivocation handling — detect and slash" for the operational rule. Summary: when honest detects equivocation (two distinct σ_V partials from the same leader on different value_roots), they:

1. Stay σ-committed if already σ-emitted at this layer (cross-phase exclusivity binds them).
2. If σ-eligible but not yet σ-emitted, transition to Defer-due-to-equivocation. The σ-emit precondition fails.
3. If already in Defer-due-to-partition, transition to Defer-due-to-equivocation upon retaining ≥ 2 distinct V's. Recovery via late-re-flood-delivers-V is foreclosed.
4. All Defer-due-to-equivocation operators force-NR at end of Phase 2 per the end-of-Phase-2 force-commit rule. The protocol does not pick a winner cluster-wide.
5. Gossip the equivocation evidence (the pair of equivocating Phase-1 bundles) for out-of-band slashing.

The leader is required to sign `σ_V` *exactly once per (slot, layer)*; refreshes in the host's fetch loop are pre-signing only (see "Application: SSV Ethereum proposer duty / Head-change handling") and don't surface multiple `σ_V` partials on the wire. Any second `σ_V` from the same leader is a protocol violation.

OBFT-1 does not provide in-protocol equivocation recovery. Some equivocation patterns naturally reach qV on a single V (when honest happen to split such that 2-of-3 σ-emit on the same V; leader's σ_L^V on that V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. See "Liveness / Equivocation handling" for the full case analysis. Equivocation is treated as a slashable byzantine fault (Phase-1 bundles signed by leader's key are self-contained slashing evidence — see "Slashing evidence"); the persistent reputation deterrent (assumption 4) is what makes the slot-miss tolerable across many slots. The TBFTR-style **Phase 2a/2b split** is the structurally correct full fix — recovers all equivocation patterns at +1 RTT cost — documented as future improvement.

**Cross-onion (operator-side) equivocation — counting and suppression.** If an operator `i` is observed with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different V, the dual partials are slashable evidence on the same logic as leader equivocation. For aggregation: by single-σ-V exclusivity (EKM-enforced — see "Slashing-protection scope"), an honest operator only ever emits σ on one V per layer, so any dual-V σ partials from the same operator are byzantine. The byzantine f-bound on per-V σ-pool contributions therefore holds without explicit suppression: byz contributes ≤ 1 partial per V regardless. Honest receivers MAY additionally elect to fully suppress `i`'s partials upon observing the equivocation evidence — this is not load-bearing for safety.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_k` is a slashable cross-signer. The pair is detected uniformly across phases:

- **σ from Phase 1 + NR/NV from Phase 2** — applies to a layer's leader specifically: a `σ_{L_k}^V(V_{L_k})` from the Phase-1 bundle paired with an `σ_{L_k}^{IBE}(nr_tag_k)` from the same operator at the same slot.
- **σ from Phase 2 onion + NR/NV from Phase 2** — any operator who included σ in their onion *and* broadcast a no-σ attestation.

Detection is straightforward — the dual partials are public. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it cryptographically — the EKM-enforced cross-phase exclusivity is what gives Pigeonhole 1 its load-bearing constraint, and a byzantine cross-signer simply consumes their f-bound contribution to one of σ-pool or NR-pool, not both effectively). The detection is purely for **attribution** and out-of-band punishment.

### Slashing evidence

Four rules surface byzantine fault evidence for *attribution and punishment*. Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to inform honest operators that a byzantine fault has occurred so the cluster's coordination layer can take action — see [Implications of the reputation deterrent](#implications-of-the-reputation-deterrent-assumption-4) for the human-supervised slashing model. The protocol surfaces the evidence; honest operators verify it and decide whether to coordinate offline (stake slashing via the SSV contract, kick-out, exclusion from future cluster formation).

- **Self-contradiction (σ + NR/NV at the same layer).** If operator `i` emitted `σ_i^V(V)` at some layer AND `σ_i^{IBE}(nr_tag_k)` at the same layer, the dual partials are slashable evidence (cross-signing).
- **Leader equivocation.** Two distinct `σ_V` partials from the same leader at the same (slot, layer) are a self-contained slashable fault proof. Any observable double-signing is protocol-violating regardless of the leader's stated intent.
- **Cross-onion partial-sig equivocation.** Operator `i` emitting `σ_i^V(V)` and `σ_i^V(V')` for different `V` at the same layer is detectable from the partial sigs alone — single-σ-V exclusivity is EKM-enforced, so any dual-V observation is a slashable byzantine fault.
- **Fake encrypted-presence (post-decryption garbage).** Operator `i` broadcasting an auth-signed `KindOnion` with an encrypted partial at layer `k > 0` that, after NR-quorum unlocks decryption, decrypts to garbage (not a valid σ partial on any leader-known V at L_k, or fails to decrypt entirely) is a slashable byzantine fault. The auth envelope binds `i` to the encrypted payload at signing time; post-decryption verification surfaces the garbage. Detection is delayed until prior layers' NR-quorums reach (so the chained encryption can be unlocked), but attribution is unambiguous once detection fires. This deters DoS attacks where byzantine operators fake encrypted-presence to push honest into Defer state with no real σ-emit ever materializing.

Each piece of evidence is verifiable in isolation (signed by the offending operator's own keys) — any observer with the published partials and the (eventually) decrypted onion contents can independently confirm the byzantine action. **Acting on the evidence (slashing transaction, cluster removal) is a human-coordinated process**, not an automated protocol step; honest operators judge whether the evidence is compelling and decide whether to act. Evidence quality varies by fault class — the four classes above are all *cryptographically self-contained* (high-confidence, low false-positive risk against honest operators). Behavioral-pattern grief (selective-delivery, σ-refusal coordinated with honest flakiness) leaves no on-wire cryptographic evidence and is correspondingly harder for humans to act on with confidence — see [Implications of the reputation deterrent / Evidence quality by fault class](#implications-of-the-reputation-deterrent-assumption-4).

### Failure modes

The slot misses (no V signature is produced) under any of the following. The cases split into two classes by relationship to OBFT-1's operating assumptions:

- **Class A — assumption violations** (the listed condition violates one of OBFT-1's assumptions; the protocol does not promise liveness when an assumption is violated). These are out-of-scope for OBFT-1's recovery guarantees by construction.
- **Class B — permitted byzantine grief within the f-bound** (occurs *under* valid assumptions; one byzantine operator within the f-byzantine bound deliberately misbehaves to cause slot-miss). These are *permitted because they are eventually accountable* — every Class B grief leaves slashable evidence on the wire (cryptographically self-contained for some classes, gossipsub-pattern-based for others), and the persistent reputation deterrent (assumption 4) holds the byzantine accountable across slots via stake slashing and cluster kick-out. The slashability is what makes Class B "permitted" rather than "fatal" — an attacker that griefs reliably pays a real cost.

**OBFT-1's failure-mode set is identical to OBFT(R≥2)'s, with one boundary shifted: real-propagation `(D, R · D]` cases are Class B-recovered at R≥2 but Class A at OBFT-1.** All other failure modes are R-invariant and the exposure is the same.

The slot misses under any of:

- **[Class A]** **Sustained partition (real propagation > D)** — violates assumption 2 (partial synchrony) under OBFT-1's framing (envelope = D). Honest who didn't receive V before end of Phase 2's late-σ-emit window stay in Defer; end-of-Phase-2 force-NR transitions them to NR. If the forced NR-pool is short of qEnc (because some honest σ-locked in Phase 1 or have an early σ-emit on V), slot misses cleanly. **No safety violation.** **OBFT-1 absorbs the smaller envelope `D` directly** rather than the multi-round `R · D` of OBFT(R≥2); deployments that need a larger envelope should use [OBFT(R=2)](OBFT.md).
- **[Class A]** **More than `f` faults** — violates assumption 1 (BFT trust bound). More than `f` operators offline/byzantine combined. Standard `3f+1` trust bound. Slot misses regardless of protocol structure.
- **[Class A]** **Assumption 3 violated (host validity diverges mid-slot)** — violates assumption 3 (host validity unanimous at decision time). See [Implications of validity-divergence not being recovered](#implications-of-validity-divergence-not-being-recovered-assumption-3). The host's stabilization workflow (validate-once-and-lock at Phase-1 acceptance) is the design's path to keeping this from occurring; if it does occur, the slot misses cleanly. Not slashable (re-orgs are real-world events, not protocol violations); reputation deterrent does not apply.
- **[Class B — cryptographically slashable]** **Equivocation σ-locked split patterns** (1-1-1 split, 1-1-Defer-C, 1-Defer-Defer at f=1 n=4 — patterns where some honest σ-commit on different V's before observing equivocation, leaving neither σ-quorum nor NR-quorum reachable at L_0 and no fall-through) — occurs under valid assumptions; one byzantine within f-bound equivocates to engineer the split. See [Implications of equivocation not being recovered](#implications-of-equivocation-not-being-recovered). **Equivocation evidence (Phase-1 bundles signed by leader's key) is self-contained cryptographically slashable**; the persistent reputation deterrent (assumption 4) makes this tolerable across many slots. The all-Defer-due-to-equivocation case (e.g., asymmetric-retention patterns where every honest retains ≥ 2 V's by σ-emit time) actually recovers via L_1 fall-through and is not a failure mode at f=1 n=4 with L_1 honest. **R-invariant** — same exposure in OBFT-1 and OBFT(R≥2).
- **[Mostly Class A]** **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates in non-recoverable patterns at every layer). At K ≥ f+1, byzantines alone (within f-bound) cannot cause this — pigeonhole guarantees ≥ 1 honest leader. So this typically requires either >f faults (Class A: violates assumption 1) or coincident non-byzantine independent failures (relay timeouts, host issues, etc. — also Class A in the sense that the cluster's operational assumptions about host availability are violated). Rare in practice. The K parameter controls the fall-through depth; **at K = n = 4 (recommended OBFT-1 default for proposer duty), pigeonhole guarantees ≥ 3 honest leaders, providing maximum fall-through redundancy**.
- **[Class A]** **Late deepest-layer leader broadcast (NR-lock at deepest layer with no fall-through).** A deepest-layer leader L_{K-1} whose Phase-1 bundle's first cluster-observation arrives after `T_candidate_accept` — e.g., the leader's fetch loop overruns Δ_1 due to slow beacon node, MEV-relay timeout, or head-change refresh — gets rejected entirely (no auth-only-retention at OBFT-1; cutoff is hard). All 3 honest at L_{K-1} are in silent-leader-NR state (no V observed cluster-wide), force-NR at end of Phase 2, NR-quorum at L_{K-1} reaches → walk advances past L_{K-1}, but no L_K layer exists. **Slot misses.**

  **Why Class A?** None of the six explicit assumptions is violated: BFT trust bound holds, partial synchrony bounds *gossipsub* propagation but not the leader's *application-internal fetch latency*, validity is orthogonal, EKM and DKGs are orthogonal. The failure relies on an *implicit* operational assumption — that honest leaders broadcast within their Phase-1 fetch window before `T_candidate_accept`. When this implicit assumption fails (legitimate operational delay, no byzantine action), the protocol cannot fall through past the deepest-layer.

  **Mitigation paths (in order of recommendation):**
  - **Use K ≥ f+2** (the recommended config; see §Setting). Adds at least 2 honest leaders so a single late-broadcasting honest leader doesn't foreclose the slot — fall-through to a deeper honest leader recovers. At f=1 n=4 this means K ≥ 3 (with K = n = 4 as the OBFT-1 default for proposer duty, providing maximum fall-through depth at minimal extra bandwidth ~3KB per onion). At f=2 n=7, K ≥ 4. **No protocol change**, just deployment configuration; this is the cleanest fix that actually recovers the slot. **The OBFT-1 default `K = n = 4` already satisfies this.**
  - **Host-side hard deadline** (defense-in-depth on top of K ≥ f+2; minor host-side discipline, no protocol change). The leader's fetch loop should abort and broadcast no bundle if it hasn't terminated by `T_candidate_accept − D − δ`. Converts "late broadcast missed cutoff" into "silent leader" (handled cleanly via NR-quorum → fall-through). At K = f+1 (BFT-min) this *cleans up the spec-tension* but doesn't recover the slot (silent + L_0-fail still falls through to non-existent layer). At K ≥ f+2 it complements the K choice by ensuring late broadcasts always fall into the silent-leader path.
  - **Phase 2a/2b** ([Path forward](#where-this-came-from)). Structural fix that handles K = f+1 too. No Phase-1 σ_V from the leader → no early commitment by anyone → late bundle observed in Phase-2a is σ-emittable in Phase-2b. Costs +1 RTT per slot. Worth adopting if the deployment also wants the validity-divergence and Class B byzantine grief recovery that Phase 2a/2b brings.
- **[Class A]** **Validity-divergence deadlock (network-induced; no byzantine action required in the cleanest case).** **This is a TBFT-family issue inherited by OBFT-1 — not introduced or worsened by the Defer-state machinery.** The same algebraic deadlock is documented in [TBFT.md "Application-validity-divergence — known liveness limit"](TBFT.md). All TBFT-family protocols (TBFT, OBFT, OBFT-1, TBFTR) share the three structural causes: per-operator independent validity verdict, leader's σ_V locked in Phase 1, and cross-phase exclusivity per operator.

  A beacon-chain re-org landing inside the gossipsub-acceptance window for the Phase-1 bundle can split honest operators between σ-eligibility (parent_root matched their pre-reorg head snapshot) and NV (parent_root didn't match their post-reorg head). The host's per-operator validate-once-and-lock workflow (§Head-change handling) preserves each operator's verdict but cannot reconcile cluster-wide divergence. At the boundary split (e.g., 2-σ vs 2-NV at f=1 n=4 with all 4 honest, where leader's Phase-1 σ_V counts as σ-side via cross-phase exclusivity): σ-pool = 2 < qV = 3; NR-pool = 2 < qEnc = 3. **No safety violation** — just no quorum on either side; slot misses cleanly. The host's stabilization workflow narrows the divergence window (typical D ≈ 100–500ms vs slot length 12s) but doesn't eliminate it.

  **The deadlock zone widens when a byzantine within f-bound exercises f-budget passively** (silent or σ-on-V — neither is independently slashable; passive silence is the natural f-budget consumption pattern for an absent operator). At f=1 n=4 with leader honest, additional deadlock configurations include:

  - "1 non-leader σ + 1 non-leader NV + byz silent" (σ-pool = leader + 1 = 2 < qV; NR-pool = 1 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz silent" (σ-pool = leader = 1 < qV; NR-pool = 2 < qEnc).
  - "0 non-leader σ + 2 non-leader NV + byz σ-on-V" (σ-pool = leader + byz = 2 < qV; NR-pool = 2 < qEnc).
  - Symmetric configs when leader's host-verdict is itself NV (Phase-1 σ_V locks leader σ-side regardless of leader's host's later opinion).

  The all-honest example captures the cleanest case; in real deployments with byz exercising f-budget, the practical slot-miss rate from validity-divergence is meaningfully higher than the all-honest example alone suggests. See [Implications of validity-divergence not being recovered (assumption 3)](#implications-of-validity-divergence-not-being-recovered-assumption-3) for the full discussion. Phase 2a/2b (see [Path forward — Phase 2a/2b](#where-this-came-from)) eliminates this deadlock structurally via late σ-emit on cluster-stabilized verdict.
- **[Class B — gossipsub-pattern slashable, weaker accountability]** **Byzantine selective-delivery grief at end of Phase 2 (h_V = 1 deadlock).** *A deliberate-byzantine grief vector — does not arise under all-honest behavior with no implementation bugs and normal network conditions.* The attack requires the L_0 leader to actively (1) **withhold the Phase-1 bundle from all honest peers** until late in Phase 1 — an honest leader broadcasts via gossipsub immediately on producing V, so withholding is a deliberate deviation; (2) **emit an auth-signed σ-claim in their Phase-2 onion despite not having broadcast Phase 1** — an honest leader's Phase-2 σ is a follow-on to their Phase-1 broadcast, not standalone; (3) **selectively deliver the Phase-1 bundle to exactly one honest operator near `T_candidate_accept`**, timed precisely so the receiving honest σ-emits but other honest don't observe the σ-emit before their NR-decision — honest leaders don't time deliveries to specific operators. Each step is a deliberate deviation from honest protocol behavior; none occurs incidentally.

  With these byzantine actions in place: per the L_0 conditional gate fallback for receivers without retained V, all honest defer until end of Phase 2. Byz selectively delivers the Phase-1 bundle to *exactly one* honest operator (A). A σ-emits; the other 2 honest force-NR (no V received, no peer σ-emit observed in time due to byz-engineered timing). Final pools: σ-pool = A + byz σ_V = 2 < qV = 3; NR-pool = 2 honest + 0 byz (σ-locked) = 2 < qEnc = 3. **Neither quorum reaches at L_0; no fall-through.** Slot misses without leaving equivocation evidence (no double-signing — the attack uses only selective delivery of a single σ_V).

  **The deadlock is fundamental at f = 1, n = 4** for the h_V = 1 case (h_V = number of honest operators with V at end of Phase 2): σ-quorum needs h_V ≥ 2 (since byz's σ_V contributes 1, total = h_V + 1); NR-quorum (with byz σ-locked) needs h_V = 0 (since 3 − h_V honest force-NR). The intermediate h_V = 1 fails both. Generalizes at higher f: the deadlock zone is `0 < h_V < 2f`. **R-invariant** — same exposure in OBFT-1 and OBFT(R≥2). The doc [OBFT.md / Failure modes](OBFT.md#failure-modes) explicitly notes "Increasing R gives byzantine more timing flexibility without strengthening cluster recovery"; OBFT-1 and OBFT(R=2) have identical exposure, just compressed into a single round at OBFT-1.

  **Defenses considered, all with trade-offs:**
  - *f+1 witness threshold for the Defer fallback* (suggested in adversarial review): require ≥ f+1 distinct σ-claims observed before the no-V-retained receiver defers; otherwise NR-emit. **Defeats the attack** (byz's lone σ-claim falls below threshold → honest NR → NR-quorum reaches → fall-through to L_1). **Breaks late-re-flood-within-Phase-2 recovery at f = 1** (1 honest with V emits 1 σ-claim, also below threshold → NR-locked, can't σ when V arrives via late re-flood). At f=1, no fixed threshold distinguishes byz selective-delivery from genuine late-delivery (information-theoretically indistinguishable to receivers without V). Higher-f deployments may have more design room.
  - *Phase 2a/2b split* (TBFTR-style, [future improvement](#where-this-came-from)): broadcast-only Phase-2a (re-flood retained Phase-1 bundles + σ-emit observation, no σ partials), then Phase-2b emits σ partials based on Phase-2a observations. This eliminates the deadlock by deferring σ-commitment until cluster-wide σ-side observability is established. Costs +1 RTT per slot.

  **Current OBFT-1 does not defend against this attack.** It is in the same class as 1-1-1 equivocation: byz can grief reliably, slot misses, no on-wire evidence beyond byz's withholding pattern (which is hard to distinguish from network failures). The persistent reputation deterrent (assumption 4) is the practical defense across many slots — repeated grief surfaces as withholding patterns observable at the gossipsub / governance layer. The TBFTR-style Phase 2a/2b split is the structurally correct fix.
- **[Class B — gossipsub-pattern slashable, weaker accountability]** **Byzantine σ-refusal coordinated with honest mesh-bounded observation / transient flakiness (NR-lock).** The Defer rule's spec wording says "peer σ-emit observed cluster-wide", but operationally each operator only observes via their own gossipsub mesh. An honest operator with poor mesh visibility (few peers, high mesh-hop latency, transient connectivity glitch, EL-CL desync, etc.) can fail to observe peer σ-emits within their NR-decision window even when peers have σ-emitted — the Defer rule's intent (cluster-wide observation) is not what the implementation can deliver (mesh-bounded observation). This is a real design fragility, not a purely adversarial edge case: under realistic gossipsub conditions, individual operators occasionally hit propagation outliers.

  Failure mode: poorly-meshed honest A applies silent-leader rule (no peer σ-emit observed in their mesh window) → A NR-emits → A is NR-committed for the slot. Combined with byzantine within f-bound refusing to σ-emit (within-f-budget passive consumption — silence is indistinguishable from honest-but-flaky, weakly slashable), σ-pool falls short of qV: at f=1 n=4, leader honest, A poorly-meshed, byz refuses → σ-pool = leader + 1 honest = 2 < 3 = qV; NR-pool = A + 0 byz = 1 < qEnc. **Deadlock.**

  The Defer rule wording "observed cluster-wide" should be read as "observed via the operator's gossipsub mesh, which approximates cluster-wide under partial synchrony with bounded D + δ but degrades under mesh-visibility outliers". When mesh visibility is poor for some honest, the Defer rule's protective effect — keeping operators uncommitted while V propagates — fails for them. **R-invariant** — same exposure in OBFT-1 and OBFT(R≥2). At OBFT(R≥2), an early NR-emit also locks the operator across rounds (cross-round exclusivity), so the recovery R≥2 promises is foreclosed in this case anyway; OBFT-1 and OBFT(R=2) have identical outcomes.

  **Mitigations:**
  - **Phase 2a/2b** ([Path forward](#where-this-came-from)) — defers commitment past mesh-visibility outliers; if honest's mesh visibility recovers before Phase-2b, they σ instead of NR-locking.
  - **Mesh diversity at deployment level** — ensure each operator has diverse gossipsub peer connections (mesh degree, geographic diversity) to reduce the probability of a single operator hitting propagation outliers.
  - **Larger Δ_2** — extends the σ-emit observation window to absorb mesh-hop latency variance. At minimum `Δ_2 = D + δ`, only direct-mesh paths propagate before NR-decision; **`Δ_2 ≥ 2(D + δ)` (recommended for OBFT-1)** allows multi-hop paths to complete (see §Phase 2 — Late σ-emit limitation).

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

OBFT-1 uses **`K-1` IBE tags per slot** (the K-1 NR tags; the deepest layer has no NR tag). Each tag is a standard threshold-IBE tag using the cluster's IBE keypair. The chained encryption at each layer-transition is implemented as a single IBE ciphertext under `nr_tag_k`, nested across layers per [TBFT.md](TBFT.md) Appendix C. At K=2 the chain has 1 level; at K=3, 2 levels; at K=4, 3 levels.

The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init. Long-lived, no per-slot rotation. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

**Chained encryption cost.** At layer K-1 (deepest), each σ partial is wrapped in `K-1` levels of IBE encryption. Per-onion size grows as `O(K)` ciphertext bytes (`K-1` levels × ~32-64 bytes ciphertext per level). At K=2 the chain has 1 level; at K=4 it has 3 levels. Concrete sizes: ~1 KB per onion at K=2, ~3 KB at K=4. Within practical SSV bandwidth budgets — same scaling as baseline TBFT extended to K > 2.

## Properties summary

| Property | OBFT-1 |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1` + EKM-enforced per-operator commitments (single-σ-V per (slot, layer), σ-XOR-NR per layer, cross-phase exclusivity), holds against offline-aggregating byzantine within the f-bound. Honest-majority cryptographic, not 100% cryptographic — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). Same trust posture as QBFT. |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition (assumption 3) |
| Termination (output guaranteed) | Conditional: terminates within `T_round_end` if propagation ≤ `D` and ≤ f operators byzantine/offline. Single-round protocol, narrower envelope than OBFT(R≥2)'s `R · D`. |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates form self-contained slashable evidence |
| Byzantine-leader-grief resistance | **Partial.** Closed under partial synchrony for selective-delivery / late-delivery within Phase 2's late-σ-emit window (`Δ_2 − (D + δ)`) via leader-σ-V-in-Phase-1 + Defer state. Equivocation: not recovered in-protocol — some patterns reach qV naturally (when leader's σ_L^V completes a 2-of-3-honest pool), but an adversarial byzantine controlling delivery timing reliably engineers slot-miss patterns. **Same exposure as OBFT(R≥2) for byzantine grief** — the R-machinery doesn't help with these patterns at any R. The persistent reputation deterrent (assumption 4) is the practical defense. Equivocation evidence is slashable. Phase 2a/2b (future improvement) closes the gap at +1 RTT. |
| Validity-divergence under strict host | **Out of scope** — see [Assumptions](#implications-of-validity-divergence-not-being-recovered-assumption-3); host stabilizes the verdict at Phase-1 acceptance |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (K-layer fall-through within Phase 3's reconstruction walk; K configurable, K = n recommended for proposer duty) |
| Round-change recovery | **No** — single-round design. Late re-flood within Phase 2 is the only within-slot partition-recovery mechanism. For larger partial-synchrony envelope, use [OBFT(R≥2)](OBFT.md) at the cost of additional slot budget and EKM cross-round atomicity. |
| Partial-synchrony envelope | `D` (single round). For `R · D` envelope, use [OBFT(R≥2)](OBFT.md). |
| Recovery scope vs OBFT(R=2) | **Identical within OBFT-1's `D` envelope.** OBFT(R=2) additionally covers real propagation in `(D, 2D]` (Class B-recovered there, Class A here). All other failure modes (byzantine grief, view-divergence) are R-invariant — same exposure. |
| Recovery scope vs QBFT | Multi-leader fall-through is in-round (vs QBFT's serial round-change), so OBFT-1 wins on K-leader-failure cases and healthy-path latency. View-divergence (validity-divergence and equivocation patterns that don't naturally reach qV) is out of scope — handled by assumptions 3 and 4. Phase 2a/2b (future improvement) would close these gaps at +1 RTT. |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block, the recommended OBFT-1 configuration is **`K = 4` (= n)** — every cluster member is a leader at exactly one layer; pigeonhole guarantees ≥ 3 honest leaders at f=1, providing maximum K-layer fall-through depth within the single round. `K = 3 = f+2` is also viable at slightly lower onion bandwidth (~3KB savings per onion, same timing). `K = 2` (BFT-min at f=1) is **not recommended** — exposes the late-deepest-layer-leader-broadcast Class A failure mode (see §Failure modes); use K ≥ 3.

Out-of-scope cases (real propagation > D, host-validity divergence, 1-1-1 equivocation splits, h_V=1 byzantine selective-delivery) are addressed by [Assumptions and implications](#assumptions-and-implications) — partial synchrony bounds propagation; host stabilizes the validity verdict at Phase-1 acceptance; equivocation falls back on the reputation deterrent. For deployments needing the larger `2D` partial-synchrony envelope, use [OBFT(R=2)](OBFT.md) at the cost of ~650ms additional slot budget and EKM cross-round atomicity.

| OBFT-1 concept | SSV mapping |
|---|---|
| `n` participants | 4 |
| `f` byzantine bound | 1 |
| `K` layers | **4 (recommended; `= n`, max fall-through depth)** or 3 (`= f+2`, smaller bandwidth) |
| `R` rounds | 1 (fixed; OBFT-1 is single-round) |
| Slot | Ethereum slot for which the cluster is proposer |
| V-signing keypair | the validator's split BLS key (already exists in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| `L_0` (primary leader) | designated MEV proposer for the slot |
| `V_{L_0}` | MEV-optimized block fetched late from the relay |
| `L_1, ..., L_{K-1}` (backup leaders) | separately designated operators, distinct from `L_0` and from each other |
| `V_{L_k}` for k ≥ 1 | safe early-fetched blocks from vanilla beacon-node payloads, refreshed on head changes (per the leader's pre-signing fetch loop) |
| `T_commit` | commit / view-fix deadline — anchor: `slot_start + 1.5s` for the configurations below; Phase 1 fetch occupies the 0–1.5s window with `T_{K-1} < ... < T_0 < T_commit` |
| `T_round_end` | reconstruction deadline — `T_commit + Δ_2 + Δ_3`; concrete value depends on `Δ_2` sizing (see configurations below) |

Cryptographic safety (`qEnc = qV` + chained encryption + EKM-enforced cross-phase/single-σ-V exclusivity) ensures only one block can ever get a valid validator signature, regardless of K. The single-round design simplifies the EKM coordinator (no cross-round atomicity) without affecting safety.

### Timing budget — concrete configurations

The slot's hard relay-submission deadline is `slot_start + 4.0s`; a minimum `T_submit ≈ 250ms` is reserved for relay submission. The consensus deadline is `T_round_end = slot_start + 4.0s − T_submit ≤ slot_start + 3.75s` — the slot's reconstruction must complete by then.

Common parameters: **D = 100ms (cluster gossipsub P99/P999), δ = 50ms, n = 4, f = 1**. Per-window minimums:
- `Δ_2 ≥ D + δ = 150ms` (BFT-minimum); **`Δ_2 = 2(D + δ) = 300ms` recommended for OBFT-1** (widens late-σ-emit window to absorb late re-flood).
- `Δ_3 ≈ 100ms` (reconstruction + local processing; propagation-independent).

Phase 1 fetch occupies the 0–`T_commit` window. With `T_commit = slot_start + 1.5s`, leaders have 1.5s to fetch and broadcast (with their per-layer windows scheduled within 0–`T_candidate_accept = 1.35s`).

#### OBFT-1(n=4, K=4) — recommended

K=4 = n; every cluster member leads exactly one layer. Maximum K-layer fall-through within the single round.

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch | 1500ms | slot_start + 1.50s | All K=4 leaders' fetch windows fit within 0–1.5s; `T_candidate_accept = 1.35s` |
| Phase 2 | 300ms | slot_start + 1.80s | Δ_2 = 2(D + δ); **late-σ-emit window = D + δ = 150ms** |
| Phase 3 | 100ms | slot_start + 1.90s | K=4 reconstruction walk (sequential, no per-layer RTT) |
| Submission | 2100ms | slot_start + 4.00s | 8.4× the 250ms minimum — comfortable headroom for relay/beacon-submit P99 tails |

**Recovery scope.** Within Phase 3's single reconstruction walk: silent L_0 → NR-quorum at L_0 → L_1 σ-quorum if honest; silent L_0 + L_1 → fall-through to L_2 (still honest by pigeonhole at f=1); silent L_0 + L_1 + L_2 → L_3. **All in one round** (sequential local decryption, no per-layer RTT). Late re-flood within Phase 2's late-σ-emit window (~150ms) is absorbed via the Defer state. Real propagation > D + late-σ-emit window is out-of-envelope (Class A sustained partition).

**Bandwidth (healthy):** ~27 KB total cluster-wide (4 onions × ~3KB at K=4 = 12KB onion traffic + Phase 1 bundles + NR partials). No round-2 overhead — single-round.

#### OBFT-1(n=4, K=3)

K=3 = f+2; satisfies BFT-min and late-leader-resilience with one fewer fall-through layer than K=4.

| Window | Length | End time | Notes |
|---|---|---|---|
| Phase 1 fetch | 1500ms | slot_start + 1.50s | All K=3 leaders' fetch windows fit within 0–1.5s |
| Phase 2 | 300ms | slot_start + 1.80s | Same as K=4 (Phase 2 timing doesn't depend on K) |
| Phase 3 | 100ms | slot_start + 1.90s | K=3 reconstruction walk |
| Submission | 2100ms | slot_start + 4.00s | Same headroom as K=4 |

**Recovery scope.** Same shape as K=4 with one fewer fall-through layer: silent L_0 + L_1 → L_2 (deepest at K=3). At K=3, the deepest-layer leader L_2 is more sensitive to late broadcast (no L_3 to fall through to); host-side hard deadline for L_2's fetch loop is recommended (see §Failure modes / Late deepest-layer leader broadcast).

**Bandwidth (healthy):** ~24 KB (~2KB per onion at K=3 = 8KB onion traffic + rest). −3KB vs K=4.

#### Comparison: OBFT-1 vs OBFT(R=2) vs QBFT (apples-to-apples at D = 100ms)

| Setup | Final phase ends | Submission headroom | Recovery scope at D=100ms | Bandwidth (healthy / failure) |
|---|---|---|---|---|
| OBFT-1(K=4) ★ | slot_start + 1.90s | 2.10s | K-layer fall-through (L_0→L_1→L_2→L_3) + late-σ-emit absorption ~150ms | ~27 KB / n/a (single round) |
| OBFT-1(K=3) | slot_start + 1.90s | 2.10s | K-layer fall-through (L_0→L_1→L_2) + late-σ-emit absorption ~150ms | ~24 KB / n/a |
| OBFT(K=4, R=2) | slot_start + 2.65s | 1.35s | Same K-layer fall-through + extended envelope `2D` (~150–550ms additional partition tolerance) | ~27 KB / ~50 KB on round-1 failure |
| OBFT(K=3, R=2) | slot_start + 2.65s | 1.35s | Same K-layer fall-through (one fewer layer) + extended envelope `2D` | ~24 KB / ~45 KB on round-1 failure |
| QBFT (RT=2s, SSV production) | Round 1 only fits | 1.75s if R1 succeeds | Round-1 healthy; round-2 round-change exceeds 4s budget | ~14 KB / n/a (round 2 doesn't fit) |

★ = recommended default for OBFT-1.

**Key observations:**

- **OBFT-1's submission headroom is significantly larger than OBFT(R=2)'s** (2.10s vs 1.35s; +750ms) — useful when relay/beacon-submit P99 tails are non-trivial, or when the cluster wants margin for local CPU variance on Phase-3 reconstruction or certificate gossip.
- **OBFT-1's partial-synchrony envelope is `D` (100ms here)**, OBFT(R=2)'s is `2D`. Real propagation in `(D, 2D]` is recovered at OBFT(R=2) (Class B partition recovery), out-of-envelope at OBFT-1 (Class A). Whether this gap matters depends on the cluster's gossipsub propagation tail beyond P99/P999.
- **OBFT-1 is simpler operationally**: no L_C consensus, no Phase 2.5, no per-round acceptance widening, no auth-only-retention state, no cross-round σ-or-NR exclusivity, no cached σ-partial persistence requirement, no deterministic re-signing fallback. The EKM coordinator is closer to a per-key extension than a novel multi-round atomicity engine.
- **K=4 vs K=3 trades bandwidth for recovery depth.** Same timing fit at this D (Phase 2/3 don't depend on K); K=4 adds one more fall-through layer at +3 KB per onion. K=4 = n is the OBFT-1 default — maximum fall-through with all cluster members participating as leaders.
- **OBFT-1 fits at higher D where OBFT(R=2) doesn't.** At D = 500ms: Δ_2 minimum becomes 550ms, recommended `Δ_2 = 2(D + δ) = 1100ms`; Phase 2 + Phase 3 = 1200ms; total OBFT-1 consensus ends at slot_start + 2.7s with 1.3s submission headroom. OBFT(R=2) at the same D fails to fit the 4s relay cutoff. **High-D networks running proposer duty should use OBFT-1.**

The deadline-tuning rule: `T_round_end − T_commit ≥ Δ_2 + Δ_3`, with `Δ_2 ≥ D + δ` (minimum) or `Δ_2 ≥ 2(D + δ)` (recommended for OBFT-1). Concrete numbers should come from production telemetry (P99 / P999 tails of gossip propagation, EKM signing latency, beacon submit latency, relay submission latency).

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_{L_k}` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_{L_k}`. The protocol consumes the verdict; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_{L_k})` *exactly once per slot/layer*, on the final `V_{L_k}` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_{L_k}` is locked on the originally-signed value. The layer's leader cannot subsequently emit NR/NV on `nr_tag_k` — the Phase-1 σ is their σ-side commitment per cross-phase exclusivity.

**Receiver-side validity stabilization — host best-effort toward assumption 3.** OBFT-1 requires host validity to be unanimous at decision time (see [Assumptions](#assumed)). For SSV's proposer duty, the host narrows the divergence window via per-operator validity stabilization: each operator runs the validity check (including `parent_root`-vs-head) **once at Phase-1 acceptance** against a stable head snapshot, then locks the verdict for the remainder of the slot. Subsequent head movements within that operator do not flip the verdict.

**This per-operator locking does NOT guarantee cluster-wide convergence.** Operators accept Phase-1 bundles at different `t` within the gossipsub propagation window (operator-local first-observation timestamps, plus clock skew δ). If a re-org happens during this acceptance window, operator A may snapshot pre-reorg (verdict = valid) while operator B snapshots post-reorg (verdict = invalid). Both locks hold; cluster views diverge. The locking ensures *per-operator* stability across the slot, narrowing the divergence window to "re-org during gossipsub-acceptance window" — typical D ≈ 100–500ms — but does not eliminate it.

When the residual window fires (re-org lands inside the acceptance window AND operators split across the boundary), assumption 3 is violated and the slot may miss in-protocol — neither σ-quorum (capped at honest-σ-validators + leader's σ_L^V) nor NR-quorum (capped at honest-NV-count) reaches threshold under adversarial byz that withholds NR. This deadlock is the cost of strict re-validation. The looser alternative (validate once at acceptance, never re-check) avoids the in-protocol deadlock but commits on a V whose parent may become orphaned (relay/beacon submission rejection at submit time, also a slot miss). Hosts pick between the two failure modes based on observed re-org rates.

The "permit and slot-miss" framing parallels OBFT-1's equivocation handling: validity-divergence is a view-divergence pattern that the protocol does not recover from. Unlike equivocation, it's not attributable to anyone (re-orgs are real-world events, not protocol violations), so no slashing applies; it relies on assumption 3 being approximately true. **Same exposure as OBFT(R≥2)** — R-machinery doesn't help with validity-divergence at any R.

**Backup-leader re-org resistance.** Fetching `V_{L_k}` for k ≥ 1 from a deeper-confirmed parent (the asymmetric `T_{K-1} < ... < T_1 < T_0` schedule already accommodates this) reduces the likelihood that the backup's parent becomes orphaned. Backups are structurally re-org-resistant by construction.

Implementation notes:

- Each operator validates `parent_root` against a stable head snapshot taken at Phase-1 acceptance time; the verdict is locked into the operator's local state for the remainder of the slot.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" and "single V per operator per layer" — a second signing attempt with a different V at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing AND operator-side double-signing detectable cluster-wide.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same. **Same DKG cost as baseline TBFT and OBFT.**

2. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. Cutoffs derived from `D` (propagation P99/P999) and `δ`:

   - **`T_candidate_accept = T_commit − (D + δ)`** for Phase-1-bundle acceptance. Bundles first-observed past this cutoff are rejected entirely (no auth-only-retention, no later-acceptance path — single round, single cutoff).
   - **`Δ_2 ≥ D + δ`** is the Phase-2 minimum; **`Δ_2 ≥ 2(D + δ)` is recommended for OBFT-1** to widen the late-σ-emit window so late re-flood within Phase 2 can be absorbed (see §Phase 2 — Late σ-emit limitation).
   - **`Δ_3 ≈ 100ms`** for Phase 3 reconstruction (propagation-independent local processing).

   All are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

3. **Choosing K (layer count).** K is per-duty:

   - `K = 2`: BFT-min at f=1; **not recommended for OBFT-1** — exposes the late-deepest-layer-leader-broadcast Class A failure mode (see §Failure modes).
   - `K = 3..n`: provides multiple fall-through layers within Phase 3's single reconstruction walk. At `n = 4`, max K is 4 (one layer per operator). Each extra layer adds a level of chained encryption (~1 KB per onion at K=3, ~3 KB at K=4 — within practical bandwidth).

   **Two K bounds (see §Setting for the full discussion):**
   - **`K ≥ f+1`** — BFT-liveness minimum (≥ 1 honest leader exists by pigeonhole over the f-byz bound).
   - **`K ≥ f+2`** — late-leader-resilience recommendation (≥ 2 honest leaders, so a single late-broadcasting honest leader doesn't foreclose the slot via the deepest-layer NR-lock pathology — see §Failure modes / Late deepest-layer leader broadcast).

   Recommended for OBFT-1 proposer duty: **`K = n = 4`** (maximum fall-through depth at f=1, every cluster member leads exactly one layer). `K = f+2 = 3` is also viable at slightly lower bandwidth.

4. **R is fixed at 1.** OBFT-1 is single-round by design; for deployments needing the larger `R · D` partial-synchrony envelope, use [OBFT(R≥2)](OBFT.md). The trade-off: OBFT-1 saves ~650ms of slot budget vs OBFT(R=2), drops cross-round EKM atomicity / cached-partial-persistence / re-signing-fallback requirements, and fits at higher D where OBFT(R=2) doesn't (e.g., D = 500ms) — at the cost of narrower partition envelope.

5. **Tag construction and replay.** Each NR tag must uniquely bind `(slot, cluster, layer)` to prevent replay across slots/layers/clusters. Structure: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Same construction as OBFT.

6. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one OBFT-1 instance and assumes:
   - Single OBFT-1 instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between OBFT-1 (`protocol_tag = "OBFT-1-v1"`) and any other path that signs against the V-signing share (TBFT, OBFT, QBFT, etc.).
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`), not just submission.

7. **Equivocation is permitted, not recovered.** OBFT-1 does not provide an in-protocol equivocation recovery mechanism. Some patterns reach qV naturally (when 2-of-3 honest happen to σ-commit on the same V at f=1 n=4; leader's σ_L^V completes the qV-quorum). Other patterns slot-miss. Pigeonhole 2 ensures at most one V reaches qV cluster-wide regardless of split — there is no two-output safety failure. Equivocation is treated as a slashable byzantine fault; the persistent reputation deterrent (assumption 4) makes this tolerable across many slots. **Same exposure as OBFT(R≥2)**. The TBFTR-style **Phase 2a/2b split** is the structurally correct full fix; +1 RTT per slot, preserves cryptographic safety AND f-tolerant liveness. Documented as future improvement.

## Where this came from

OBFT-1 is the simpler-spec point in the OBFT family, derived from [OBFT](OBFT.md) by fixing R = 1 and stripping the round-retry machinery. The motivation: under analysis, OBFT's R-round structure was found to add real partial-synchrony envelope (`R · D` vs `D`) but introduce significant spec/EKM complexity (cross-round σ-or-NR exclusivity, cross-round atomicity in EKM, persistent partial-sig caching, deterministic re-signing fallback, L_C cluster-consensus signaling, per-round acceptance widening, auth-only-retention state, cross-round dedup logic) — most of which adds nothing for the failure modes that R-machinery was originally introduced to address (byzantine grief patterns are R-invariant; only network-partition tails in `(D, R · D]` benefit from R ≥ 2).

OBFT-1 is the design point that asks: **what's the minimum machinery needed to get K-layer parallel fall-through plus late-σ-emit-within-Phase-2 partition recovery, without rounds?** The answer:

1. **Drop L_C cluster-consensus.** Its only purpose in OBFT is round-transition coordination; with R = 1, no transitions to coordinate. (At R ≥ 2, L_C consensus is itself a cosmetic optimization — falls back to the round timer when honest views diverge, which is precisely when rounds are needed for partition recovery.)
2. **Drop cross-round σ-or-NR exclusivity.** With R = 1, no cross-round to enforce; cross-phase exclusivity (within Phase 1 + Phase 2) is sufficient for Pigeonhole 1.
3. **Drop cross-round σ-partial dedup, retention widening, auth-only-retention.** Single cutoff `T_candidate_accept`, no later-acceptance path; bundles past cutoff are rejected entirely.
4. **Drop EKM cross-round atomicity, cached-partial persistence, deterministic re-signing fallback.** Single signing event per (slot, layer) per operator; standard transactional sign-and-log.
5. **Keep Phase 2 sub-phasing.** Load-bearing for the Defer rule; lets late re-flood within Phase 2 deliver V to Defer-state operators who σ-emit before NR-decision.
6. **Keep K-layer fall-through, chained encryption, equivocation detect-and-slash, four slashing-evidence rules.** These are R-orthogonal — same value at R = 1 as R ≥ 2.

The result is a protocol that strictly preserves [OBFT](OBFT.md)'s in-envelope recovery (within `D`), drops machinery whose only purpose was extending the envelope to `R · D`, and pays in narrower envelope for spec/EKM simplicity, more submission headroom, and high-D fit. The Phase 2 `Δ_2 ≥ 2(D + δ)` recommendation captures the late-σ-emit-within-Phase-2 case, which is the sole within-slot partition-recovery mechanism at R = 1.

**Path forward — Phase 2a/2b is the family-level structural fix (R-orthogonal).** Phase 2a/2b is not OBFT-specific — it comes from [TBFTR](TBFTR.md) and addresses a TBFT-family limitation (validity-divergence + adversarial byzantine deadlock) that current TBFT, OBFT, and OBFT-1 all inherit. The TBFTR-style Phase-2 split (broadcast-only Phase-2a where operators re-flood retained Phase-1 bundles without σ-emitting, then Phase-2b where σ-emits happen on a deterministically-chosen V after Phase-2a observation completes) is the structural fix for the limitations that current TBFT and OBFT-family document in their respective failure-mode sections. **Smaller variants (relax cross-phase exclusivity, allow NV → σ flip via aggregator filtering, separate cryptographic tags for tentative-vs-final commitment, etc.) all either break safety against an offline-aggregating byzantine or are isomorphic to Phase 2a/2b under a different name.** Phase 2a/2b's structural shape is essentially forced: synchronous consensus on validity (or σ-commitment more generally) requires explicit cluster-wide coordination, which means an observation phase before commitment.

The mechanism — deferring σ-commitment until after cluster-wide Phase-2a observation — has three effects on the failure-mode taxonomy that apply equally to OBFT-1 and OBFT(R≥2):

1. **Recovers the Class A validity-divergence deadlock** (assumption 3 violated by re-org during acceptance window). Phase-2a's observation step lets operators see cluster-wide σ-eligibility state and converge on a stabilized validity verdict; Phase-2b σ-emit happens on that verdict rather than each operator's local at-acceptance snapshot. This brings validity-divergence into recovery scope rather than leaving it as an out-of-scope assumption violation. (The "leader σ_L^V locking on stale V" concern collapses into this same fix — without a Phase-1 σ_L^V, the leader doesn't pre-lock; Phase-2b's σ-emit is on the post-observation stabilized V.)

   **Same fix also resolves the Class A late-deepest-layer-leader-broadcast pathology** at K = f+1 (see §Failure modes). Without Phase-1 σ_V from the leader, no early operator-side σ/NR commitment; a late-arriving Phase-1 bundle observed in Phase-2a is σ-emittable in Phase-2b without any pre-locked NR commitments to override.

2. **Removes the Class B byzantine grief surface** for byzantine actions that exploit early σ-commitment:
   - *Equivocation σ-locked split patterns* (1-1-1, 1-1-Defer, all-pattern). By deferring σ-emit, no honest carries an "initial" σ partial on a non-winner V; single-σ-V exclusivity stays intact; Pigeonhole 2 holds; at most one V reaches qV cluster-wide *regardless of equivocation pattern*.
   - *Byzantine selective-delivery grief* (h_V = 1 deadlock). Phase-2a's observation establishes cluster-wide σ-side eligibility before any σ-commitment; byz cannot manipulate h_V to 1 because honest don't σ-commit until Phase-2b sees the post-observation pool.
   - *Byzantine refusal coordinated with honest transient flakiness*. Honest operator who would NR due to transient flakiness can defer their commitment to Phase-2b; if the condition resolves before Phase-2b, they σ instead of NR-locking themselves out for the slot.

   These Class B grief patterns are currently "permitted" because they're eventually accountable via reputation deterrent, but with weak (gossipsub-pattern / operator-unreliability) slashability. Phase 2a/2b removes the protocol-level grief surface so the deterrent doesn't need to do this work.

3. **Bonus: improves per-operator participation efficiency** under transient errors — operators with brief flakiness during Phase 2 don't lose their slot contribution.

Costs **+1 RTT per slot** at OBFT-1 (Phase 2 grows by ~D+δ for the Phase-2a observation window). Preserves cryptographic safety AND f-tolerant liveness — strictly better than the qV-bump alternative which trades f-tolerance for safety.

**Status: future improvement → near-term for serious deployments.** Current OBFT-1 trades the recovery properties above for spec simplicity and the +1 RTT savings. The trade-off is acceptable when (a) the deployment's re-org rate is low (assumption 3 holds in practice), (b) byzantine operators value future participation enough to be deterred by the reputation mechanism, and (c) the cluster's governance SLA is short enough that grief is bounded across slots. **For deployments under realistic re-org rates or with adversarial network operators, Phase 2a/2b should be considered near-term, not future** — the +1 RTT cost is small relative to the recovery properties gained.

OBFT-1 generalizes baseline TBFT (`K=2`) to K-layer fall-through and incorporates structural ideas from TBFTR's chained encryption (used unchanged at K > 2) and bid-routing variants (which OBFT-1 can compose with as host-supplied leader-determination). The relationship across the TBFT family:

| Protocol | R | K | Phase-2 split | Role |
|---|---|---|---|---|
| baseline TBFT | 1 | 2 | no | minimal viable point |
| OBFT-1 | 1 | configurable | no | this protocol — minimum machinery for K-layer fall-through |
| [OBFT](OBFT.md) | configurable (typically 2) | configurable | no | OBFT-1 + R-round retry for `(D, R · D]` partition coverage |
| TBFTR | 1 | configurable | yes (Phase 2a/2b) | OBFT-1 + Phase 2a/2b for view-divergence recovery |
| TBFTR + R | configurable | configurable | yes | full recovery-scope point in the family (not yet specified) |

## Appendix A — Protocol comparisons

This appendix gives high-level structural comparisons against the protocols OBFT-1 relates to: baseline [TBFT](TBFT.md), [OBFT](OBFT.md) (the multi-round generalization), [TBFTR](TBFTR.md) (the Phase-2-split spec), and QBFT (SSV's existing consensus protocol).

### A.1 — Comparison with baseline TBFT

OBFT-1 is a **strict superset** of baseline TBFT at `K = 2` and reduces exactly to TBFT there. The K-generalization (chained IBE at K > 2 per [TBFT.md](TBFT.md) Appendix C) and the Defer state for late-σ-emit-within-Phase-2 are OBFT-1's additions to baseline.

| Aspect | Baseline TBFT | OBFT-1 (K = 4) |
|---|---|---|
| Phase 1 bundle | `(V, σ^V_L, σ^op_L(envelope))` | Same |
| Phase 2 onion at layer k | `E_{nr_tag_0}(σ_i^V(V_{L_k}))` (single-tag IBE at K=2) | Chained IBE: layer-k σ encrypted under `nr_tag_0 ∧ ... ∧ nr_tag_{k-1}` (K=4 has 3-level nesting at deepest layer) |
| Operator commitment states | σ, NR, NV (3 states) | σ, NR-silent, NV, **Defer** (4 states; Defer is OBFT-1's addition) |
| Tag count per slot | 1 (`nr_tag_0`) | K−1 (`nr_tag_0`, ..., `nr_tag_{K-2}`) |
| Round structure | Single-shot | Single-shot (R = 1) — same |
| Equivocation handling | Honest who detect equivocation emit NR; single-V receivers can deadlock | Detect-and-slash; honest stay in current commitment state (no winner-pick). Some patterns reach qV naturally; others slot-miss (equivocation slashable; reputation deterrent makes this tolerable). |
| Aggressive marginal (≥ 1 of 3 honest miss V at acceptance) | Slot misses | Recovered if late re-flood arrives within Phase 2's late-σ-emit window (`Δ_2 − (D + δ)`) via Defer state |
| Multi-leader fall-through | At most L_0 → L_1 (K=2) | Up to L_{K-1}; sequential within Phase 3's reconstruction walk (no per-layer RTT) |
| Validity-divergence under strict host | Slot misses | Out of scope — assumed unanimous (see [Assumptions](#assumed)) |
| Healthy-path latency | 2 RTTs | 2 RTTs (unchanged) |
| Bandwidth (healthy, n=4) | ~21 KB at K=2 | ~24 KB at K=3, ~27 KB at K=4 |
| DKG ceremony | 2 keypairs (V-share, IBE-share) | Same |

**Migration path**: a cluster running baseline TBFT can adopt OBFT-1 incrementally by (1) enabling the Defer-state rule (decision-time only; no wire change at K=2 since layer-0 σ is plaintext); (2) bumping K to 3 or 4 (gains the K-layer fall-through and chained-encryption nesting at deeper layers — same chained-IBE construction as [TBFT.md](TBFT.md) Appendix C). The Phase-1 protocol-tag (`OBFT-1-v1` vs `TBFT-v1`) provides envelope domain separation.

### A.2 — Comparison with [OBFT](OBFT.md) (R ≥ 2)

OBFT-1 is OBFT with R fixed at 1 and the round-retry machinery stripped. They share Phase 1 / Phase 2 / Phase 3 structure, K-layer fall-through, chained encryption, the four commitment states, and the four slashing-evidence rules. OBFT-1 differs from OBFT by what's *removed* from the spec rather than by adding anything.

| Aspect | [OBFT](OBFT.md) (R = 2) | OBFT-1 |
|---|---|---|
| Round structure | Up to R rounds with re-flood retry; round transitions on timer or L_C-quorum promotion | Single round, no retry, no transitions |
| L_C cluster-consensus signaling | `KindLCClaim` message kind in Phase 2.5 for round-transition coordination | **Removed** (no rounds to transition between) |
| Phase 2.5 | Present (carries `KindLCClaim`) | **Removed** entirely |
| Per-round acceptance widening | `T_candidate_accept_r` widens across rounds; late bundles auth-only-retained for next round | **Removed** — single hard cutoff `T_candidate_accept`; bundles past it rejected entirely |
| Cross-round σ-or-NR exclusivity | EKM enforces across rounds + cross-phase | **Cross-phase only** — no rounds to span |
| Cross-round σ-partial dedup | Phase 3 deduplicates per-operator across rounds | **Removed** — single round, no cross-round duplicates possible |
| EKM cross-share atomicity | Required across rounds; sign-and-log atomic across V-share + IBE-share for re-emission semantics | Required per single signing event only; standard transactional behavior |
| EKM persistent partial-sig cache | Required (cached σ partial must survive operator restart for cross-round re-emission) | **Not required** — single signing event per (slot, layer) |
| EKM deterministic re-signing fallback | Required (allow re-sign if log row matches same `(slot, layer, side, value_root)`) | **Not required** |
| Final-round force-commit | At round R: Defer operators force-commit (σ if Defer-due-to-partition resolved, else NR) | **Becomes end-of-Phase-2 force-commit** (same logic, single round = final round) |
| Partial-synchrony envelope | `R · D` (e.g., `2D` at R=2) | `D` (single round) |
| Slot budget at K=4, D=100ms | Ends at slot_start + 2.65s (consensus + reconstruction) | Ends at slot_start + 1.90s — saves ~750ms |
| Submission headroom (4s relay cutoff) | 1.35s | 2.10s |
| Bandwidth (healthy, n=4, K=4) | ~27 KB | ~27 KB (same) |
| Bandwidth (worst case at R=2 with round-1 failure) | ~50 KB | n/a (no round 2) |
| High-D fit (D = 500ms) | Does not fit 4s relay cutoff | Fits with ~1.3s submission headroom |

**Recovery scope:**

- **Within OBFT-1's `D` envelope**: identical to OBFT(R=2). Same K-layer fall-through, same Defer-state late-σ-emit recovery, same equivocation natural-recovery + all-Defer fall-through, same byz-grief exposure (R-invariant patterns).
- **Outside OBFT-1's envelope but inside OBFT(R=2)'s `2D` envelope**: aggressive-marginal partition tails in `(D, 2D]` are recovered at OBFT(R=2) (Class B partition recovery via round-2 re-flood), out-of-envelope at OBFT-1 (Class A sustained partition).
- **Outside OBFT(R=2)'s envelope**: both fail equivalently (slot misses, safety holds).

**When to choose which:**

- Use **OBFT-1** when the cluster's gossipsub propagation tail beyond P99/P999 is thin enough that the `D` envelope is sufficient; or when the deployment runs at higher D (≈ 300–500ms) where OBFT(R=2) doesn't fit; or when spec/EKM simplicity and submission headroom outweigh the extra envelope.
- Use **[OBFT(R=2)](OBFT.md)** when the propagation tail is meaningfully wider than P99/P999 D and within-envelope coverage of `(D, 2D]` partitions is needed; the extra ~750ms of slot budget and EKM cross-round atomicity are acceptable costs.

**Migration in either direction is straightforward** because the protocols share the same wire format for Phase 1, Phase 2 onions, and NR partials (modulo the Phase-2 envelope `protocol_tag`). A cluster could even mix-and-match per duty: OBFT-1 for proposer (where headroom matters and slot is short), OBFT(R≥3) for attestation (where slot budget is generous and envelope width is the priority).

### A.3 — Comparison with TBFTR

[TBFTR](TBFTR.md) is the K-generic spec with V-plaintext + Phase-2-split machinery, designed for `n ≥ 7`. OBFT-1 and TBFTR share the K-layer onion structure and chained encryption, but diverge on commitment timing.

| Aspect | TBFTR | OBFT-1 |
|---|---|---|
| K | K-generic (typically `K = ⌈n/2⌉`) | K-generic, configurable (recommended K = n for proposer at f=1) |
| Phase 2 split | 2a (broadcast/observation only) + 2b (σ-emit on stabilized V) | Single Phase 2 with sub-phasing (σ-emit at start, NR/Defer at end of window) |
| V-plaintext at deeper layers | Yes — onion carries `V ‖ C_k(σ_partial)` | No — only σ partial encrypted (V_{L_k} learned via Phase-1 broadcast retention; not in onion) |
| Recovery mechanism | Phase 2b late σ-emit on cluster-stabilized V (from Phase-2a observation) | Defer state + late re-flood within Phase 2's late-σ-emit window |
| Equivocation single-V | Phase-2 split recovers all patterns (σ-emit on deterministically-chosen V after observation) | Not recovered in-protocol; some patterns reach qV naturally; rest slot-miss (equivocation slashable) |
| Round structure | Single-shot per slot (no rounds) | Same (single-shot) |
| Bandwidth | Larger (V-plaintext per layer × n operators) | Smaller per-onion |
| Healthy-path latency | 3 RTTs (Phase 1 + Phase 2a + Phase 2b) | 2 RTTs (Phase 1 + Phase 2) |

**TBFTR's Phase 2a/2b is OBFT-1's natural extension path** for closing the equivocation and validity-divergence gaps. The Phase-2 split adds +1 RTT per slot but removes the Class B byz-grief surface (1-1-1 equivocation, h_V=1 selective-delivery, mesh-flakiness NR-lock) and brings Class A validity-divergence into recovery scope. See [Where this came from](#where-this-came-from) for the discussion of why Phase 2a/2b is the structurally correct fix at the TBFT-family level.

For new SSV deployments, OBFT-1 supersedes baseline TBFT for K-generic deployments. TBFTR remains a useful reference for the K-generic onion structure analysis and the Phase-2-split design.

### A.4 — Comparison with QBFT

QBFT is SSV's existing consensus protocol. The key structural difference: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus partial-sig collection)**; OBFT-1 (and the rest of the TBFT family) fuses the two by embedding partial signatures inside Phase-2 onions. Most observable trade-offs trace back to this structural difference.

For per-scenario liveness behavior (recovery scope, mechanism, outcome) see [Liveness comparison: OBFT-1 vs OBFT(R=2) vs QBFT](#liveness-comparison-obft-1-vs-obftr2-vs-qbft). This appendix covers the structural / cost dimensions: protocol shape, latency, bandwidth, safety posture, primitive complexity, and deployment maturity.

| Aspect | QBFT | OBFT-1 (K=4 for proposer) |
|---|---|---|
| Protocol shape | Multi-round (PROPOSE → PREPARE → COMMIT) with per-round leader rotation; round-change on timeout | Single round, K-layer onion fall-through |
| Consensus and signing | Decoupled: consensus reaches a decided value, then a separate post-consensus phase collects 2f+1 partial sigs | Fused: Phase-2 onions carry σ partials directly, Phase 3 reconstructs |
| RTTs to signed output (min, healthy) | 3 (consensus) + 1 (post-consensus) ≈ 4 | 2 (Phase 1 + Phase 2) |
| Termination guarantee | Eventually-terminating across rounds (under partial synchrony) | Conditional on `D ≥ real_propagation`; for larger envelope, use [OBFT(R≥2)](OBFT.md) |
| Safety posture | Honest-majority (`2f+1` honest) + correct quorum-certificate verification | Honest-majority cryptographic via `qEnc = qV = 2f+1` + chained IBE + EKM-enforced per-operator commitments. Same trust posture as QBFT — see [Implications of safety being honest-majority cryptographic](#implications-of-safety-being-honest-majority-cryptographic-not-100-cryptographic). |
| Bandwidth (healthy n=4) | ~14 KB | ~27 KB at K=4 |
| Latency (healthy, n=4, D=100ms) | ~750 ms | ~600 ms (Phase 2 + Phase 3 with Δ_2 = 2(D+δ)) |
| Latency (1 round failure, n=4) | ~3.0 s (round-1 timeout 2s + round-2 success ~750 ms) | n/a (single round; failure → slot miss) |
| Slashing-protection scope | Single block sig per slot, gated at submission time | Multiple per-share sigs per slot at candidate-signing (Phase-1 leader, Phase-2 onion) gated by EKM cross-keypair coordination — see [EKM coordination model](#ekm-coordination-model) |
| Cryptographic primitive | BLS threshold signatures only | BLS threshold signatures + threshold IBE / SWE (drand/tlock-style) |
| Application contract | Decides on any value the application proposes | K-layer with primary + backup-leader fetches |
| Production maturity | What SSV runs today; corner cases hit and fixed | New codebase; deployment confidence to be derived |

**Cost-side trade-off summary:**

- **Latency.** OBFT-1 wins on healthy-path (~600ms vs ~750ms). On round-1 failure, QBFT can still recover via round-change (at ~3s total), while OBFT-1 single-round failures are slot-misses; OBFT(R=2) covers round-1-failure cases at ~1.15s within the same envelope. OBFT-1's recovery scope is narrower than QBFT's but available much faster within scope.
- **Bandwidth.** QBFT lower healthy-path; OBFT-1 higher due to onion encryption. On round failure, QBFT's round-change has its own bandwidth cost (~12KB extra round + a full additional consensus round); OBFT-1 doesn't recover, so no failure-case bandwidth.
- **Cryptography.** QBFT only needs BLS threshold signatures. OBFT-1 additionally needs threshold IBE / SWE (drand/tlock-style; audited, deployed since 2023). The IBE primitive is more novel; for risk-averse deployments, this is a real consideration.
- **Spec surface.** OBFT-1 is meaningfully smaller spec than [OBFT(R≥2)](OBFT.md) (no rounds, no L_C consensus, no Phase 2.5, simpler EKM). Comparable in size to QBFT once you account for QBFT's view-change protocol and prepared-certificate verification.
- **Maturity.** QBFT is production. OBFT-1 is a new codebase — deployment confidence has to be derived.

**Where QBFT genuinely wins for proposer duty:**

- **Validity-divergence recovery.** Head re-org mid-slot invalidates parent_root for L_0 candidate; honest verdicts genuinely diverge. QBFT round-changes through with new leader fetching at new head. OBFT-1 requires the host to stabilize the verdict at Phase-1 acceptance (assumption 3). (Same gap as OBFT(R≥2).)
- **1-1-1 equivocation recovery.** QBFT's round-change with new leader proposing a fresh V breaks the deadlock. OBFT-1 relies on the reputation deterrent (assumption 4). (Same gap as OBFT(R≥2).)
- **Cryptographic primitive simplicity.** BLS-only, no IBE.
- **Production maturity.** QBFT is what SSV runs today.

**Where OBFT-1 wins:**

- **Healthy-path latency.** ~600ms vs ~750ms.
- **Multi-leader-failure recovery.** OBFT-1's K-layer parallel fall-through resolves K-1 silent layers within Phase 3's reconstruction walk (sequential local decryption, no per-layer RTT). For K=4 with 3 silent leaders, OBFT-1 recovers in ~600ms; QBFT round-changes 3 times serially, exceeding the 4s budget.
- **All-Defer equivocation recovery.** When byz delivers V's early enough for re-flood to spread conflicts before Phase 2 σ-emit, all 3 honest land in Defer-due-to-equivocation; end-of-Phase-2 force-NR produces NR-quorum at L_0 → fall-through to L_1. Same recovery as QBFT but in single round (~600ms vs ~2.75s).
- **Spec/EKM simplicity vs OBFT(R≥2).** No cross-round atomicity, no L_C consensus, no per-round widening — see [§A.2](#a2--comparison-with-obft-r--2).

**The operational bottom line:** QBFT covers more failure modes (its round-change-with-fresh-V handles validity-divergence and 1-1-1 equivocation that OBFT-family doesn't). OBFT-1 wins on common-case latency and multi-leader-failure recovery. For SSV proposer duty under a 4s relay cutoff, the choice depends on observed re-org rate (favors QBFT), cluster's tolerance for the 1-1-1 equivocation case via the reputation deterrent (favors OBFT-family), and deployment complexity tolerance (favors OBFT-1 over OBFT(R≥2) in the family).

## Appendix B — Composable extensions

OBFT-1's K-generic structure composes cleanly with **host-supplied leader-determination extensions** — selecting which operator gets which layer based on application-supplied criteria (rather than just a deterministic rotation table). The extensions live outside the OBFT-1 core: they affect *which* leaders get assigned to layers L_0, L_1, ..., L_{K-1}, while OBFT-1's safety/liveness machinery (Defer state, K-layer fall-through, chained encryption) operates uniformly across whatever leader assignment the host supplies.

Three example extensions, originally sketched for baseline TBFT and applicable to OBFT-1 with the natural K-generic adaptation. Full design sketches live in [TBFT.md](TBFT.md) Appendix B; this section summarizes their composition with OBFT-1.

**B.1 — Bid-ordered leader selection.** Each leader attaches a bid value to their Phase-1 envelope; operators commit to whichever layer's bid is highest among locally-validated candidates. Originally specified at K=2 with per-operator commit-tags replacing OBFT-1's σ-or-NR machinery. Composes with OBFT-1 by using commit-tags as the per-layer commitment primitive (replacing nr_tag_k); OBFT-1's Defer state and K-layer fall-through apply uniformly. Trade-off: per-operator hedging across layers is sacrificed (each operator commits to *exactly one* layer per slot, the argmax-bid one). For SSV proposer duty, the bid is the relay's `SignedBuilderBid` value — see [TBFT.md](TBFT.md) Appendix B.1 for the full sketch including bid-equivocation handling and post-hoc attribution.

**B.2 — Parent-root-based "ordering" (negative result).** A natural-sounding alternative — letting operators commit to whichever layer's `parent_root` matches their canonical chain — turns out to be a non-extension at K=2 (collapses to baseline behavior) and fragments under head-divergence (the very scenario it would purportedly address). [TBFT.md](TBFT.md) Appendix B.2 has the full negative analysis. The key takeaway carries to OBFT-1: parent-root-as-filter (used to filter envelopes within a bid layer's input set, as in B.3 below) is productive; parent-root-as-ordering (per-operator routing rule) is not.

**B.3 — L_Bid-prepended OBFT-1 (bid-routing as a top layer).** Prepends an opportunistic bid-routing layer (`L_Bid`) on top of OBFT-1's rotation-determined K layers, producing a `K' = K + 1` configuration. Layer 0 is bid-determined (highest-bid envelope from any operator who broadcasted in Phase 1); layers 1..K' are baseline OBFT-1's rotation-determined leaders. Composes with OBFT-1 by using OBFT-1's chained encryption uniformly across the K'+1 layers. The bid layer's σ-eligibility is conditioned on a cluster-state predicate (saw all bidders' envelopes with parent-root majority filter, OR saw `n-1` with parent-root unanimity), making σ-side participation cluster-consistent. [TBFT.md](TBFT.md) Appendix B.3 has the full sketch including relay-attestation bid binding, cluster-recognition rules for trusted builders, and timing implications.

**Liveness inheritance is partial, not automatic.** A naive composition does NOT inherit OBFT-1's full liveness-recovery scope. The "saw all bidders" σ-eligibility predicate from baseline TBFT B.3 introduces new deadlock surfaces under selective bid-withholding: a byzantine non-rotation bidder can deliver their bid envelope to some honest but not others, causing some honest to σ_LBid (saw all bids) and others to NR_LBid (saw n−1 bids), splitting both pools below quorum at L_Bid. Cross-phase exclusivity then prevents fall-through to layers 1..K'. See [TBFT-audit.md](TBFT-audit.md) for the full adversarial review of the baseline construction. **A production L_Bid composition would need a different σ-eligibility predicate or its own quorum/witness design**; the sketch above is a starting point only, not a drop-in extension. Until that design is specified, B.3 should be treated as a research direction rather than a recommended OBFT-1 configuration.

Under OBFT-1's application-agnostic framing, these extensions are best read as **examples of plugging an application-supplied selection criterion into OBFT-1's leader-determination slot**. The criterion (B.1: bid via commit-tags; B.2: parent-root via commit-tags — ruled out; B.3: bid via L_Bid prepended layer) is host-supplied. OBFT-1's protocol body doesn't enumerate or interpret the criteria; it consumes the resulting layer-to-leader mapping and runs uniformly.

