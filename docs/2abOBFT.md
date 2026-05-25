# 2abOBFT — Two-Phase Witness BFT for Distributed Validators

A single-round agreement protocol for SSV clusters that produces one collective threshold-signed value per slot against a hard relay-submission deadline. 2abOBFT reaches σ-quorum on a leader's candidate value in the healthy case roughly **one broadcast-trip-time after the leader's Phase-1 bundle arrives**, and falls through to a sequence of backup-leader layers — within a single Phase-3 reconstruction walk, no per-layer round-trip — when the primary layer cannot decide.

The "2ab" in the name reflects a Phase-2 split. **Phase 2a** is the σ-side terminal emission and the NR-side coordination signal: each operator emits exactly one of `KindValue` (carrying its own σ partial on the value, inline), `KindNoValue` (a non-binding "I have no value to sign" coordination message), or `KindCommit-NRDirect` (an immediate NR commit when leader equivocation is observed). **Phase 2b** is the dynamic NR-side commit — operators that coordinated `KindNoValue` and cannot σ-commit emit a `KindCommit-NR` partial once the cluster's no-value cohort reaches quorum, unlocking fall-through to the next layer. Cryptographic safety is identical to single-emission BFT designs (chained threshold-IBE + EKM-enforced per-operator commitments + `qV = qEnc = 2f+1`); the split, together with per-layer leader witnesses, buys a fast healthy path and a structured fall-through ladder.

2abOBFT operates with `K` configurable layers (`f+1 ≤ K ≤ n`), each with its own deterministically-derived leader, falling through within a single Phase-3 reconstruction walk (sequential local decryption, no per-layer RTT). The running example throughout is `n = 4, f = 1, K = 2` (`K = f+1`, the BFT-liveness minimum at `f=1`, and the recommended SSV default — see [§Application](#application-ssv-ethereum-proposer-duty)); the algebra generalizes uniformly across `f+1 ≤ K ≤ n` and to the larger SSV cluster sizes (`n ∈ {7, 10, 13}`, where `K = (n-1)/3 + 1 ∈ {3, 4, 5}`).

## When to use it

**Suited for:**
- SSV proposer duty under healthy-network partial synchrony, where σ-quorum forms ~1·BTT after the leader's bundle arrives. The leader's Phase-1 witness gives σ-pool a one-partial head-start at every layer, so a single honest peer's σ partial completes the L_0 quorum.
- Deployments that want OBFT-family fall-through recovery for the common adversarial and network-flakiness patterns: silent/crashed leaders, byzantine σ-refusal within the f-bound, σ-locked-split and 2-1 leader equivocation, h_V=1 byzantine selective-delivery, late-leader broadcast, mesh flakiness, and validity-divergence within an honest majority (3-1 / 1-3 at f=1 n=4).

**Not suited for:**
- Deployments where the residual byzantine grief patterns 2abOBFT does **not** recover dominate the threat profile. Re-introducing the leader's Phase-1 σ-witness (for the healthy-path head-start) means an honest leader is σ-locked to its value the moment it builds its bundle; it cannot later join an NR-quorum. As a consequence the following **miss** (slot-miss, no fall-through), recovering only on the next slot under the rational-byzantine deterrent (assumption 4):
  - **1-1-1 leader equivocation** (`Equivocate_111`): the leader hands each honest operator a distinct value; each σ-locks on its own value before re-flood surfaces the conflict, so every σ-pool fragments below `qV` and no operator can pivot to NR. (A design without the Phase-1 witness recovers this via NR fall-through; 2abOBFT trades that recovery for the head-start.)
  - **Validity-divergence at the 2-2 boundary** (`ValidityDivergence_AlgebraicLimit`) and **validity-divergence combined with a passive byzantine** (`ValidityDivergence_PassiveByz_*`, `ValidityDivergence_LeaderNV_PassiveByz`): when the honest σ-cohort is below `qV` and the no-value cohort is below `qEnc`, no trigger fires and the slot stalls at L_0. This is the f=1 n=4 algebraic limit (Pigeonhole 1) plus the σ-locked-leader effect.
- General-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. 2abOBFT (like the OBFT family) gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.
- Sustained partition tails beyond the absorption window (the σ-side path tolerates ~`1·BTT + SafetyBuffer` of re-flood lag at L_0; deeper layers wider). Real propagation beyond that is Class A "sustained partition" — out of scope by definition. Multi-round extensions are a future direction; see [OBFTR(R≥2)](OBFTR.md).

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n = 3f+1` (the BFT-tight setting; matches SSV's deployment configurations `n ∈ {4, 7, 10, 13}`). Running example: `n = 4, f = 1`. The threshold formula `qV = qEnc = 2f+1` equals `n − f` exactly at this setting; this equality is what makes the bare Pigeonhole arguments in [§Safety](#safety-cryptographic-honest-majority) hold.
- **Two threshold BLS keypairs** from independent DKGs run once at cluster init over the same `n` operators at the same threshold `qV = qEnc = 2f+1`:
  - **V-signing keypair** (`qV = 2f+1`): produces the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** (`qEnc = 2f+1`): used (a) as the threshold-signing scheme for no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (the same primitive as `drand/tlock`). Decrypting a ciphertext under tag `T` requires `qEnc` partial sigs on `T`. The two keypairs use distinct cryptographic backends (so the IBE primitive can use its expected DST) but share the operator set and threshold — this shared threshold is what makes [Pigeonhole 1](#pigeonhole-1--σ-vs-nr-at-the-same-layer)'s algebra work across the cross-keypair signing log.
- An **operator-identity signature scheme** for the outer authentication envelope on every message. Distinct from the two threshold keypairs; practical choice is each operator's long-term P2P/SSV identity key.
- **K layers** (`f+1 ≤ K ≤ n`, configurable) with deterministically-derived distinct leaders: layer 0 with primary leader `L_0`, layer 1 with backup `L_1`, ..., `L_{K-1}`. The default `K = (n-1)/3 + 1` gives `K ∈ {2, 3, 4, 5}` at `n ∈ {4, 7, 10, 13}`.

  **K bounds:** `K ≥ f+1` is the BFT-liveness minimum — pigeonhole over the f-byz bound guarantees at least one honest leader; below it all leaders could be byzantine and no σ-quorum reaches at any layer. `K ≥ f+2` guarantees ≥ 2 honest leaders (late-leader resilience) at the cost of an extra layer's leader-broadcast budget. **Recommended default: `K = f+1`** (K=2 at n=4) — production testing shows `K > f+1` doesn't materially improve outcomes once the common grief patterns are handled at L_0.

- **Single agreement round per slot.** One Phase 1 → Phase 2a → Phase 2b → Phase 3 sequence; no retry, no cross-round re-flood. The slot's only hard deadline is the relay-submission cutoff (`T_relay_cutoff − T_submit`), enforced at the runner level — the consensus core itself has no hard wall and never emits a "give-up" default.

- **Time unit `BTT` (broadcast trip time).** `1 BTT = P99 + δ`, where `P99` is the gossipsub propagation budget at the deployment's chosen tail percentile and `δ` is the clock-skew bound — the time for one one-way message to reach all honest receivers under partial synchrony. Concrete sizing at the reference configuration: `P99 = 150ms, δ = 50ms, 1 BTT = 200ms`.

- **`BFT_start`** — slot-relative offset at which Phase 1 begins. Pre-fetch and pre-consensus (block-builder calls, RANDAO aggregation, etc.) occupy `[slot_start, BFT_start]`. In the pure-timing model `BFT_start = 0`; SSV's proposer duty sets `BFT_start > 0` (see [§Application](#application-ssv-ethereum-proposer-duty)). Leader broadcasts cannot land before `BFT_start`.

- **Key derived offsets** (computed by the runner from the relay cutoff backward):
  - `resolveBudget = 1·BTT + max(SafetyBuffer, 1·BTT) + ε_3 + phase3JitterBuffer + HeaderSubmitHeadroom` — the time reserved before the relay cutoff for the slot to resolve and a certificate to land, where the last two terms are small fixed reserves (a Phase-3 jitter cushion and the ~100ms relay-submission headroom).
  - `TPhase2a = T_relay_cutoff − resolveBudget` — the Phase-2a **backstop** instant (see Phase 2a; operators usually fire earlier).
  - `T_0_broadcast = TPhase2a − 1·BTT` — the primary leader's broadcast target (its bundle gets `1·BTT` to propagate before the backstop).
  - `fetchAt[k] = T_0_broadcast − B_k`, where `B_k` is the per-layer broadcast budget — `(k+2)·BTT` for the shallower layers and `T_0_broadcast` for the deepest (so its fetch deadline clamps to `BFT_start`). Non-decreasing in `k`: deeper layers fetch earlier, from deeper-confirmed parents, making their values re-org-resistant.
  - `resolveDeadline = TPhase2a + max(1·BTT + SafetyBuffer, 2·BTT) + ε_3`, clamped to `T_relay_cutoff − HeaderSubmitHeadroom − phase3JitterBuffer`. See [§Timing](#timing-parameters).

- **`SafetyBuffer`** — the absorption budget for σ-pool fill via gossipsub IHAVE/IWANT recovery when the initial `KindValue` eager-push doesn't reach all honest peers in one hop. Protocol-level configurable, **independent of** the gossipsub `HeartbeatInterval`. Default `SafetyBuffer = 700ms` (matching SSV's `RefloodDelay`); see [§Timing](#timing-parameters) for the lean/loose spectrum and the crossover behavior.

- **`ε_3 ≈ 50ms`** — local Phase-3 processing (BLS aggregation + IBE decryption walk + certificate construction); propagation-independent.

- **K−1 NR tags:** `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. When `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt layer-`(k+1)` σ partials. The deepest layer `L_{K-1}` has no NR tag — it either σ-decides or the slot misses.

## Assumptions and implications

2abOBFT's claims hold conditional on six explicit assumptions, the same six as [OBFT](OBFT.md). The rest of the spec assumes them and refers back here rather than re-deriving the trade-offs.

### Assumed

1. **Standard BFT trust bound at the tight setting.** `n = 3f+1`, up to `f` byzantine. Honest operators run protocol-conformant software; byzantine operators may deviate arbitrarily within their f-bound. The threshold `qV = qEnc = 2f+1 = n − f` requires this tightness for the bare Pigeonhole arguments.

2. **Partial synchrony for liveness.** Messages eventually deliver within bounded propagation `P99` and clock skew `δ`. Safety is unconditional on timing; only liveness depends on it. The σ-side absorption window at L_0 is ~`1·BTT + SafetyBuffer` (re-flood lag tolerance for a late bundle to still seed a σ-quorum); deeper layers are wider. Real propagation exceeding the window is Class A "sustained partition" — out of scope.

3. **Host validity is best-effort unanimous at decision time.** The protocol consumes the host application's `valid` / `not-valid` verdict on each `V_{L_k}` at Phase-1 acceptance, at Phase-2a fire time, and at upgrade time. Operators may transiently diverge (e.g., a head change observed by some but not others); the host's job is to make divergence rare via per-operator stabilization.

   **Validity-divergence recovery is partial and asymmetric**, a consequence of the leader's Phase-1 σ-witness (assumption 6 / [§Phase 1](#phase-1--candidate-broadcast)). When an honest majority agrees, the slot decides (3-σ vs 1-NV → L_0; 1-σ vs 3-NV → NR fall-through to L_1). But once an operator σ-commits — including a leader, which σ-locks the moment it builds its bundle — a later host flip to NV has **no effect** (flipping to NR would self-equivocate; cluster-quorum semantics dominate single-operator disagreement). So a leader whose host flips post-fetch cannot join an NR-quorum, and the 2-2 boundary split, as well as validity-divergence combined with a passive byzantine, **miss cleanly** at f=1 n=4 (insufficient cluster majority on either side; neither quorum reaches). See [§Liveness](#liveness-synchrony-conditional).

4. **Persistent operator set with rational-byzantine deterrent.** Same shape as the OBFT deterrent — see [OBFT.md / Implications of the rational-byzantine deterrent](OBFT.md). Deployed today via SSV's fee model (continuous per-validator operator fees regardless of per-slot contribution, plus staker-migration response to slot-miss rates); a manual-blacklist extension (restoring `Byzantine ≡ Down` per-slot on observed evidence) is planned but not yet deployed. 2abOBFT's residual surfaces that the deterrent must absorb are the slot-miss patterns above (1-1-1 equivocation; validity-divergence-with-passive-byz) — recovered on the following slot, not in-protocol.

5. **Coordinated EKM across both keypair shares.** The "EKM" is a coordinated signing service spanning the operator's V-keypair share and IBE-keypair share, backed by a single slashing-protection log keyed on `(slot, layer, side, value_root)`. 2abOBFT's EKM is the simplest in the OBFT family — at most one signing event per `(slot, layer)` per operator, no cross-round atomicity, no persistent partial-sig cache.

   **In-slot operator restart is out of scope.** An operator that crashes mid-slot is treated as silent for the remainder of the slot and resumes cleanly at the next slot boundary. The EKM slashing-protection log is the only state required to be durable across restarts.

6. **Independent V-keypair and IBE-keypair DKGs over the same operator set at the same threshold `qV = qEnc = 2f+1`.** The shared operator set and threshold make Pigeonhole 1's algebra work (`h_σ + h_NR ≤ 2f+1` across the cross-keypair signing log).

### Implications of safety being honest-majority cryptographic, not 100% cryptographic

Safety holds against an offline-aggregating byzantine *given the protocol rules are correctly enforced by honest operators*. Enforcement happens at two independent layers per operator: the **trigger/emission layer** (operator software deciding which message to emit and when) and the **EKM** (the slashing-protection log that rejects signing requests violating single-σ-V and σ-XOR-NR). For a single honest operator to produce safety-violating behavior (e.g., σ on two values at one layer), *both* layers must be buggy in compounding ways. A single such operator consumes f-budget directly; two violate the trust bound. This is the same trust posture as QBFT.

"Cryptographic safety" here means: against a partially-synchronous network adversary plus up to `f` byzantine operators, honest operators following the protocol cannot produce two contradictory outputs. It does not mean safety against arbitrary bugs in honest operator software.

## Protocol

2abOBFT runs a single agreement round per slot: Phase 1 → Phase 2a → Phase 2b → Phase 3. The slot's hard wall is the runner-level relay-submission deadline; a slot that does not reach σ-quorum at any layer in time is missed.

### Phase 1 — Candidate broadcast

Each layer `L_k` (`k ∈ {0, ..., K-1}`), fetching by its per-layer deadline `fetchAt[k] = T_0_broadcast − B_k` (deeper layers earlier, since `B_k` is non-decreasing):

1. Independently produces and host-validates its candidate `V_{L_k}` (the leader's local fetch loop — see [§Preconditions](#preconditions-on-the-host-application)).
2. Signs a **threshold σ partial** on `V_{L_k}` with its V-keypair share — the **leader witness** `LWitness = σ_{L_k}^V(V_{L_k})`. Signing the witness acquires the leader's σ-side EKM lock at layer `k` on `V_{L_k}` (so the leader cannot later NR at that layer — see below).
3. Broadcasts the bundle `Phase1Bundle{cluster, operator = L_k, height, layer = k, value = V_{L_k}, LWitness}`, op-identity-signed at the outer envelope.

**The leader witness gives σ-pool a head-start at every layer.** A receiver that observes the bundle verifies `LWitness` against the leader's pubKeyShare on `V_{L_k}` and, on success, pools it into `σ-pool[layer][value_root]` — one real σ partial from the moment the bundle arrives, at every layer (parity with bare OBFT's witness section). At L_0, this means an operator that retains the bundle, host-validates it, and emits its own σ partial reaches `2` of `qV = 3` immediately; a single honest peer completes the quorum. The witness is **plaintext** at every layer (a head-start), distinct from the chained-encrypted Phase-2a σ partials at deeper layers; safety holds because a single witness is `1 < qV` and cannot itself reach σ-quorum (Pigeonhole, [§Safety](#safety-cryptographic-honest-majority)).

**The head-start has a cost (the σ-lock trade-off).** Because the leader σ-locks at fetch time, it is committed to `V_{L_k}` for the rest of the slot. If the leader's host flips `V_{L_k}` to invalid post-fetch (a re-org), the leader cannot retract and cannot join an NR-quorum at that layer. This is the structural source of the validity-divergence-with-passive-byz and 1-1-1-equivocation slot-misses (assumption 3, [§Liveness](#liveness-synchrony-conditional)). A design without the witness recovers those cases via NR fall-through but loses the head-start; 2abOBFT takes the head-start.

Receivers run two layers of validation, in order:

1. **Protocol-level** (structural + auth): verify the outer op-identity envelope against the claimed leader's pubkey, check `(cluster, height, layer, claimed-leader = the layer's designated leader, non-empty value, non-empty LWitness)`, then verify `LWitness` against the leader's pubKeyShare on `value`. A bundle failing the envelope check is dropped. A bundle whose `LWitness` fails to verify is retained for its value but its witness is rejected and **Rule 5** (fake plaintext σ) fires against the leader at that layer.
2. **Application-level** (host-supplied): the host returns `valid` / `not-valid` on `V_{L_k}`. The protocol consumes the verdict without interpreting it.

**Retention bounds.** Retention is keyed by `(slot, layer, leader_id)`; an operator keeps at most **two distinct `value_root`s** per key (sufficient for both a Phase-2a emission on the chosen value *and* leader-equivocation evidence). Further distinct bundles from the same leader are dropped. A second distinct `value_root` is leader equivocation (**Rule 2**) — self-contained slashable evidence — and forces the receiver NR-side at that layer.

**Bundle propagation.** Honest receivers re-flood every bundle on first observation via gossipsub; re-flood continues through Phase 2a so late bundles are absorbed into the σ-pool's fill window.

### Phase 2a — σ-side terminal emission and NR-side coordination

There is **no synchronized fire instant**. Each operator fires its single Phase-2a emission **as soon as its L_0 emission is determinable** (the *L0Ready* signal), and the synchronized `TPhase2a` serves only as a backstop for operators still waiting (see [Async fire](#async-fire-on-l0ready)). Each operator emits exactly one of three messages, by its local L_0 state:

- **`KindValue`** — the operator has `V_0` retained, host-valid, and not equivocation-observed. This is the **σ-side terminal emission**: the operator signs its own plaintext σ partial `L0Partial = σ_i^V(V_0)` (acquiring the σ-side EKM lock at L_0 on `V_0`) and carries it inline, alongside the forwarded leader witnesses (see below) and its `K-1` deeper-layer entries. There is no separate later σ commit — `KindValue` is the operator's entire σ-side contribution.
- **`KindNoValue`** — the operator has no `V_0`, or its host says NV. This is a **coordination message, not a commitment**: it carries the operator's deeper-layer entries but **no L_0 payload and no L_0 lock**. The operator stays in the L_0 `coordination` EKM state, free to go either direction later (the upgrade or an NR commit).
- **`KindCommit-NRDirect`** — the operator observed leader equivocation at L_0 (≥ 2 distinct `V_0`) before emitting anything else. This is an immediate NR commit: it carries the `nr_tag_0` partial plus the deeper-layer entries, and acquires the L_0 NR-lock in one step. It is a sole emission (no second message from that operator at L_0).

**Forwarded leader witnesses.** A `KindValue` carries, in addition to the emitter's own `L0Partial`, a `Witnesses []LayerWitness` section: byte-for-byte forwarded copies of leader witnesses the emitter has retained, one per layer the emitter is σ-side on. Each `LayerWitness = {Layer, ValueRoot, Witness}`; the value bytes needed to verify the witness ride alongside in the same message (`V` at L_0, the layer's `SigmaChained` entry at `k > 0`). A receiver that missed a leader's bundle can harvest the leader's σ partial — and the value — from any σ-side peer's `KindValue`, verify it against the layer leader's pubKeyShare, and pool it. This is the peer-reflood-V recovery vector (it recovers h_V=1 selective-delivery in-protocol, modulo a degraded-mesh tail; see [§Liveness](#liveness-synchrony-conditional)). On verify failure the witness is silently discarded (it is signed-for-forwarding by the emitter, not the leader — firing Rule 5 against the leader on a forwarded forgery would open a framing attack; the leader-signed-garbage case stays covered by Phase-1's direct Rule 5).

**Deeper-layer entries.** Every `KindValue` / `KindNoValue` / `KindCommit-NRDirect` carries `K-1` `LayerEntry` records, one per layer `k ∈ {1, ..., K-1}`, each = `{Layer, Kind ∈ {SigmaChained, NRPlaintext, Empty}, V, Payload}`:
- **SigmaChained** — the operator is σ-side at layer `k` (it retained `V_k`, host-valid). `Payload` is `σ_i^V(V_k)` chained-IBE-encrypted under `nr_tag_0 ∧ ... ∧ nr_tag_{k-1}` (k levels, innermost-first). Acquires the σ-side EKM lock at layer `k`.
- **NRPlaintext** — the operator is NR-side at layer `k` (`k ≤ K-2`). `Payload` is the plaintext `nr_tag_k` IBE partial. Acquires the NR-side lock at layer `k`.
- **Empty** — NR-side at the deepest layer `K-1` (no `nr_tag_{K-1}` exists), or no commitment.

  An operator that is the leader of layer `k > 0` (and so σ-locked there by its own witness) stays SigmaChained at layer `k` even if its host later flips `V_k` invalid — flipping to NRPlaintext would publish both a σ witness and an NR partial at the same layer, which is self-equivocation. (This is the deeper-layer analog of the L_0 σ-lock-freezes-the-decision rule.)

#### Async fire on L0Ready

Under healthy mesh, an operator's L_0 emission becomes determinable as soon as the leader's bundle arrives and the host validates — typically `~1·BTT` after broadcast, before the `TPhase2a` backstop. The operator fires its `KindValue` (or `KindCommit-NRDirect`) at that moment rather than waiting, shaving the bundle-arrival-to-backstop gap off the healthy path. The *L0Ready* signal closes once the operator's local L_0 state is `Value` or `NRDirect` (i.e., determinable as σ-side or equivocation-NR). It does **not** close for the `NoValue` state: a V-drop or host-NV operator waits for the `TPhase2a` backstop, giving the value its full re-flood window before declaring no-value (and leaving the upgrade path open). L0Ready is a best-effort, non-monotone hint (the host can flip valid→NV), so the emission kind is always re-derived from local state at fire time.

#### Upgrade (KindNoValue → KindValue)

A `KindNoValue`-path operator that *later* obtains `V_0` — via bundle re-flood, or by harvesting a forwarded witness from a peer's `KindValue` — and whose host re-validates it as valid, emits a single upgrade `KindValue`. The upgrade carries the operator's own σ partial inline (it *is* the σ-side commit; there is no separate later message), and acquires the L_0 σ-lock. The upgrade must precede any NR commit; once the operator has NR-locked at L_0, the upgrade is no longer authorized. Receivers recognize the upgrade sequence (`KindNoValue` then `KindValue` from the same operator) by the presence of both messages, in any arrival order. The upgrade's `K-1` deeper-layer entries must match those of the prior `KindNoValue` (an L_0 upgrade changes only the L_0 emission, not the deeper commitments); a receiver that observes a mismatch records Rule 6 and keeps the prior entries.

### Phase 2b — dynamic NR-side commit

There is no hard Phase-2b deadline and no scheduled commit-build step. The only Phase-2b emission is the **NR-side commit**, fired dynamically by the operator's per-state-delta evaluation:

- **NR-eligibility trigger.** An operator emits a `KindCommit-NR` (the `nr_tag_0` partial) when the cluster's L_0 no-value cohort reaches quorum — `|noValuePool[0]| ≥ qEnc` — **and** the operator cannot σ at L_0 (the *cannot-σ gate*). It acquires the L_0 NR-lock.

  The **cannot-σ gate** is essential: an operator that holds a verified leader witness + host-valid `V_0` is σ-eligible and must take the upgrade instead of NR. Without the gate, σ-eligible operators would forfeit their σ contribution under byzantine no-value-flooding and break the L_0 σ-quorum at marginal `h_V`.

Receivers verify every NR partial against the emitter's IBE-keypair share before counting it toward the NR-pool (anti-pollution); an unverifiable partial is dropped, so a byzantine cannot inflate NR-quorum with garbage. (Symmetrically, σ partials are verified before σ-pool inclusion — see Phase 1 and §Slashing evidence.)

(The equivocation trigger fires at Phase 2a as `KindCommit-NRDirect`, not here. There is **no σ-eligibility trigger** — the σ-side is terminal in `KindValue`.)

The dynamic model means the cluster commits only the messages it needs: under healthy mesh, σ-quorum forms from `KindValue`s and no `KindCommit-NR` is ever emitted; under a silent/NV cohort, the `KindCommit-NR`s accumulate `nr_tag_0` partials until `qEnc` is reached and the chain unlocks layer 1.

### Phase 3 — reconstruction (observer-on-arrival)

Each operator runs a stateless, idempotent reconstruction walk on **every state delta** (after each observed message), ascending layers from 0:

```
for k in [0, K):
    # σ-pool at layer k:
    #   L_0: plaintext partials — emitters' KindValue.L0Partial, leaders' Phase-1 LWitness,
    #        and forwarded witnesses harvested from peer KindValues.
    #   k>0: the layer leader's plaintext LWitness (head-start) PLUS peers' SigmaChained
    #        partials, decrypted via the accumulated nr_tag_0..nr_tag_{k-1} keys (peeled
    #        outermost-first). The leader's witness and its own decrypted chained entry key
    #        the same operator and coalesce — no double-count.
    if exists V with |σ-pool[k][V]| ≥ qV:
        reconstruct S = aggregate; output (k, V, S); halt
    if k == K-1: break                       # deepest layer has no NR tag
    if |nr_tag_k-pool| ≥ qEnc:
        derive the nr_tag_k key; unlock layer k+1; continue
    else:
        break                                # neither quorum; wait for next delta or slot deadline
```

Resolve re-runs safely on late arrivals (Pigeonhole guarantees at most one `V` reconstructs cluster-wide regardless of timing). On success the operator gossips a `Certificate{cluster, height, value, signature}`; any operator (whether or not it reconstructed locally) can verify the certificate against the cluster V-pubkey and submit `(V, S)` downstream, protecting against the lone-reconstructor-submit-fails failure mode. Receivers SHOULD re-run host validity before submitting (a post-quorum re-org can render the agreed `V` unsubmittable — a slot-miss outcome, not a safety violation).

### EKM coordination model

The EKM is a coordinated signing service over the operator's V-keypair and IBE-keypair shares, backed by a single slashing-protection log: one row per signing event, `(slot, layer, side ∈ {σ, NR}, value_root)` (value_root set on σ rows, null on NR). No round dimension (single round); Phase-2a coordination (`KindNoValue`) does not log.

Per `(slot, layer)`, each operator transitions through a three-state machine:

| State | Meaning | Entered by |
|---|---|---|
| `coordination` | initial; no commitment | slot start; `KindNoValue` keeps this state at L_0 |
| `σ-locked(V)` | σ-committed to `V` | leader: signing its Phase-1 `LWitness`; any operator: emitting `KindValue` (incl. upgrade) or a `SigmaChained` deeper-layer entry |
| `NR-locked` | NR-committed | `KindCommit-NR`; `KindCommit-NRDirect` (coordination → NR-locked in one step); an `NRPlaintext` deeper-layer entry |

Per-request EKM checks enforce **single-σ-V** (a second σ on `V' ≠ V` at the same `(slot, layer)` is rejected) and **σ-XOR-NR** (NR rejected if σ-row exists, and vice versa). A byzantine that publishes both σ and NR at one layer is publicly attributable (Rule 1) but, under `qEnc = qV`, has no safety impact (Pigeonhole 1). Once σ-locked, a later host re-validate-NV verdict has **no protocol effect** (the cluster's quorum semantics are binding; flipping to NR would self-equivocate).

### Slot structure

1. **Phase 1** `[BFT_start, T_0_broadcast]`: each leader fetches and broadcasts its bundle (with `LWitness`) per its per-layer window; the deepest layer broadcasts at `BFT_start`.
2. **Phase 2a** (async, backstop at `TPhase2a = T_0_broadcast + 1·BTT`): each operator fires `KindValue` / `KindNoValue` / `KindCommit-NRDirect` as soon as L_0 is determinable, else at the backstop. The upgrade window stays open afterward.
3. **Phase 2b** (dynamic): operators that coordinated `KindNoValue` and cannot σ emit `KindCommit-NR` once `noValuePool[0] ≥ qEnc`.
4. **Phase 3** (observer-on-arrival from the first Phase-2a emission): each operator runs Resolve on every delta; on σ-quorum at any layer it outputs and gossips a certificate. The only hard stop is the runner-level relay-submission deadline.

### Wire format

Five envelope kinds, each wrapped in the outer op-identity-signed envelope with a 16-byte `ProtocolTag` (`"2abOBFT"` + NUL padding) domain separator and a per-message version byte:

| Kind | Version | Payload |
|---|---|---|
| `Phase1Bundle` | V3 | `ClusterID[32], OperatorID, Height, Layer, Value, LWitness` |
| `KindValue` | V4 | `ClusterID[32], OperatorID, Height, V, ValueRoot[32], L0Partial, Witnesses []LayerWitness, LayerEntries []LayerEntry` |
| `KindNoValue` | V1 | `ClusterID[32], OperatorID, Height, LayerEntries []LayerEntry` |
| `KindCommit` | V2 | `ClusterID[32], OperatorID, Height, Side ∈ {NR, NRDirect}, L0Partial, LayerEntries (only when Side = NRDirect)` |
| `KindCertificate` | V1 | `ClusterID[32], Height, Value, Signature` |

where `LayerWitness = {Layer int, ValueRoot[32], Witness Signature}` and `LayerEntry = {Layer int, Kind ∈ {Empty, SigmaChained, NRPlaintext}, V Value, Payload []byte}`. A cluster commits to one wire version per slot; mixed-version clusters cannot decide (there is no cross-version compatibility layer). The `KindValue` content-hash used for re-broadcast dedup (as distinct from Rule 6 distinct-emission detection) **excludes both** the forwarded `Witnesses` and the emitter's own `L0Partial`: Phase-2 equivocation (Rule 6) is a *value*-level fault (a second distinct V-claim from the same op), not a partial-level one, and `OperatorID` is already in the hash, so emitters never collide. Excluding the partials means a byzantine that byte-mutates its own partial across re-broadcasts of the same V-claim dedups cleanly instead of false-firing Rule 6.

### Timing parameters

The slot's only hard deadline is the runner-level relay-submission cutoff. All other instants are derived backward from it (see [§Setting](#setting)).

**The Instance is timing-agnostic.** The protocol state machine consumes no wall-clock — it acts on observed messages and host verdicts and exposes hints (e.g. the L0Ready signal) to its driver. All the offsets below are derived and applied by the *runner*. In the reference design they live in the consensustest discrete-event simulator and are validated there across the scenario catalog; the production runner integration is a separate effort not yet built, so the timing here is **simulation-validated, not production-wired**. The same applies to the "SHOULD re-run host validity before submitting" step in [§Phase 3](#phase-3--reconstruction-observer-on-arrival): a runner-layer recommendation, not yet implemented.

**`TPhase2a` is a backstop, not the primary fire-instant.** Under async fire, σ-eligible operators emit `KindValue` as soon as their bundle arrives + host validates (`~1·BTT` post-broadcast); `TPhase2a` only bounds the `KindNoValue` emission for operators still in `coordination`.

**The resolve window is a `max`, not a sum.** A slot resolves σ-ward XOR NR-ward — never both sequentially — so the post-`TPhase2a` budget reserves the maximum of the two paths:

```
resolveWindow = max( 1·BTT + SafetyBuffer ,  2·BTT )  =  1·BTT + max(SafetyBuffer, 1·BTT)
```

- **σ-ward** (the common case): `1·BTT` for `KindValue` propagation + `SafetyBuffer` for σ-pool fill via IHAVE/IWANT recovery on a late peer.
- **NR-ward** (fall-through): `2·BTT` for the genuinely 2-hop NR cascade — `KindNoValue` → `noValuePool ≥ qEnc` → `KindCommit-NR` → `nr_tag_0-pool ≥ qEnc` → decrypt layer 1.

Reserving the `max` (rather than the sum) reclaims `min(1·BTT, SafetyBuffer)` of MEV-fetch headroom (a later `TPhase2a` ⇒ later leader fetch).

**SafetyBuffer crossover.** Because the window is `1·BTT + max(SafetyBuffer, 1·BTT)`, `SafetyBuffer` only widens it *above* the `1·BTT` crossover. Below `1·BTT`, the `2·BTT` NR-fall-through path dominates and a smaller `SafetyBuffer` reclaims MEV headroom for nothing extra (at `BTT ≥ 300ms` a 300ms `SafetyBuffer` ≡ 0). The recommended spectrum: **lean** (`SafetyBuffer = 0`; max MEV headroom, no mesh-tail tolerance), **default** (`= RefloodDelay = 700ms`; one IHAVE/IWANT cycle), **loose** (`= RefloodDelay + 1·BTT`; one cycle + jitter tail). All are absolute milliseconds — sized to the reflood (IHAVE/IWANT) cycle rather than as BTT multiples — and tunable independently of the gossipsub `HeartbeatInterval` (the default merely matches `RefloodDelay`).

## Preconditions on the host application

2abOBFT is application-agnostic: it reaches consensus on a value `V` proposed by a leader, with the host application supplying a `valid` / `not-valid` verdict on each `V_{L_k}`. The protocol does not interpret the host's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role.** The leader's local fetch loop runs entirely host-internal: fetch a candidate, validate against application rules, re-fetch on state changes if needed, then commit the final `V_{L_k}` for broadcast.
- **Validating `V_{L_k}` at the receiver role.** The host's validity check is invoked at three points: Phase-1 bundle acceptance (drives retention), Phase-2a fire time (drives the `KindValue` vs `KindNoValue` choice), and upgrade time on a harvested value. Each check is independent; a `not-valid` at any point steers the operator away from σ.

For SSV's Ethereum proposer duty, host checks include slot/proposer-index/fork-domain match, parent-root match against the operator's head view, relay-metadata validity, slashing-protection, and block well-formedness. The protocol body neither enumerates nor interprets these.

**Slashing-protection scope.** Each operator's V-share signs *up to K* values per slot (one per layer it σ-committed). EKM enforces single-σ-V and σ-XOR-NR per `(slot, layer)` by coordinating across the V-share and IBE-share (see [§EKM](#ekm-coordination-model)). Phase-2a `KindNoValue` coordination does not pass through the EKM; only σ partials (V-share) and NR partials (IBE-share) do.

## Fault tolerance

### Trust model

- **Byzantine bound `f`** with `n = 3f+1`: up to `f` operators arbitrarily malicious (collude, equivocate, cross-sign, withhold); exactly `2f+1` honest.
- **Partial synchrony for liveness**: messages eventually deliver within `P99` + clock skew `δ`. Safety is unconditional on timing. The σ-side absorption window at L_0 is ~`1·BTT + SafetyBuffer`; the slot's hard wall is the runner-level relay-submission deadline. Real propagation beyond the absorption window is Class A "sustained partition" — out of envelope.

### Safety (cryptographic, honest-majority)

**Claim:** at most one full `V` signature is ever produced per slot — across any layer, on any value, against an offline-aggregating byzantine within the f-bound, regardless of which honest aggregation order is used. The proof rests on three pigeonhole arguments over the cluster-wide signed-message set, each enforced cryptographically by the EKM (no honest-aggregation rule required). Pool definitions: **σ-pool on V at L_k** = `{σ_i^V(V) partials at L_k}` deduplicated per operator; **NR-pool at L_k** = `{σ_i^{IBE}(nr_tag_k) partials}` deduplicated per operator.

The leader witness does not change this. A leader's `LWitness` contributes **one** σ partial to `σ-pool[V_k]` (deduplicated with the leader's own later emission); `1 < qV`, so a witness alone can never reach σ-quorum.

#### Pigeonhole 1 — σ vs NR at the same layer

σ-quorum on any `V` at `L_k` and NR-quorum on `nr_tag_k` cannot both reach.
- σ-quorum: `h_σ + byz_σ ≥ 2f+1`; NR-quorum: `h_NR + byz_NR ≥ 2f+1`.
- σ-XOR-NR (EKM): each honest commits σ-or-NR at most once per layer, so `h_σ + h_NR ≤ n − f = 2f+1`.
- Byzantine: `byz_σ + byz_NR ≤ 2f` (one each).
- Both reaching ⇒ `h_σ + h_NR ≥ (4f+2) − 2f = 2f+2 > 2f+1`. Contradiction. ∎

#### Pigeonhole 2 — two σ-quorums on different values at the same layer

Two distinct `V`'s cannot both reach σ-quorum at one layer.
- `(h_σV + h_σV') + (byz_σV + byz_σV') ≥ 2·qV = 4f+2`.
- Single-σ-V (EKM): `h_σV + h_σV' ≤ 2f+1`. Byzantine cross-V: `≤ 2f`.
- Bound: `(2f+1) + 2f = 4f+1 < 4f+2`. Contradiction. ∎

This is what makes leader equivocation safe: regardless of how many values the byzantine leader hands out (and how many honest operators σ-lock on which), at most one value reaches `qV` cluster-wide. (At `n = 3f+1` the tie is moot: `Σ_V |σ-pool[V]| ≤ n = 3f+1 < 2·qV`, so two σ-quorums are arithmetically impossible.)

#### Pigeonhole 3 — cross-layer safety under chained encryption

Two distinct `V` signatures (at `L_k` and `L_{k+m}`, `m ≥ 1`) cannot both reconstruct. Layer-`(k+m)` σ partials are chained-encrypted under `nr_tag_k ∧ ... ∧ nr_tag_{k+m-1}`; decryption requires NR-quorum at every intermediate layer. By Pigeonhole 1, σ-quorum at `L_j` ⇒ no NR-quorum at `L_j`, so if `V_k` σ-reconstructs the chain stays sealed and `V_{k+m}` is inaccessible; symmetrically if `V_{k+m}` reconstructs, NR-quorum reached at `L_k` so `V_k` did not. Applied pairwise across all layers: at most one `V` reconstructs cluster-wide. ∎

`KindNoValue` coordination messages and forwarded witnesses contribute no threshold partials to either pool, so they cannot affect safety — only liveness convergence.

### Liveness (synchrony-conditional)

Liveness is **partial-synchrony-conditional within the slot's relay deadline**. The recovery profile below is the protocol's behavior as exercised by the reference test catalog (`protocol/v2/consensustest/`, protocol `2abOBFT`). Running example: `f = 1, n = 4, K = 2`; honest A, B, C; byzantine D. The protocol terminates with a V signature on the first layer whose bundle reaches a σ-quorum within its σ-side absorption window (`~1·BTT + SafetyBuffer` at L_0; deeper layers wider), else falls through via NR-quorum to a deeper backup, else — past the deepest layer's window (Class A sustained partition) — misses cleanly; **safety holds in either case**. View-divergence is recovered in-protocol where an honest majority resolves it (most equivocation splits, honest-majority validity-divergence); the residual splits slot-miss — equivocation cases are slashable, validity-divergence is not attributable.

**Relation to OBFT and QBFT.** 2abOBFT removes bare OBFT's commitment-deadlock: `KindNoValue` is non-binding, so a value-drop operator stays upgrade-eligible and σ-signs whenever the value arrives — there is no `T_commit` hard-lock. In the all-honest case its liveness matches QBFT: both finalize unless the network fails to deliver the value to a quorum within the slot's relay deadline (2abOBFT via reflood + upgrade; QBFT via in-round delivery or round-change), and a degraded-mesh straggler tail is the shared cost. The one liveness cost relative to QBFT is the **validity-divergence 2-2 boundary** (a mid-slot re-org splitting host verdicts) — and, with a faulty node at n=4, a narrowed NR-fall-through — where 2abOBFT can stall while QBFT round-changes to a fresh value. This is the leader-witness head-start trade-off ([assumption 3](#assumed); see [§Failure modes](#failure-modes)).

**Healthy path — decides at L_0, ~1·BTT post-bundle.** All operators retain `V_0` + host-valid → all emit `KindValue` with σ partials. The leader witness seeds σ-pool, so a single honest peer completes `qV = 3`. **Success (fastest).**

**Network-flakiness and silent/crash patterns recovered:**
- **Non-leader crash, byzantine σ-refusal, deepest-layer withhold, cert-withholding** — L_0's `2f+1` honest σ partials still reach `qV`. **Success (fastest).**
- **Primary leader silent or crashed** — no `V_0` reaches the cluster; honest operators coordinate `KindNoValue`, `noValuePool[0] = 3 ≥ qEnc`, all emit `KindCommit-NR`, `nr_tag_0`-quorum unlocks L_1, the backup leader's value σ-decides. **Success (fall-through).**
- **Late L_0 leader broadcast** (bundle arrives in the absorption window past the nominal target) — re-flood + the SafetyBuffer fill window let σ-pool reach `qV` at L_0. **Success (fastest).**
- **Mesh-flaky honest + byzantine σ-refusal** — IHAVE/IWANT recovery within the SafetyBuffer fills the flaky operator's σ-pool. **Success (fastest).**
- **h_V=1 byzantine selective-delivery** (the leader delivers `V_0` to only one honest operator) — the V-drop operators harvest `V_0` + the leader's witness from the recipient's `KindValue` (peer-reflood-V), host-validate, and upgrade to σ → σ-pool reaches `qV` at L_0. **Success (fastest)** under healthy mesh — peer-reflood-V largely addresses the worst propagation sub-case; the degraded-mesh tail slot-misses, deterred via assumption 4.

**Leader equivocation:**
- **σ-locked f-f split** (the leader gives `V_a` to `f` honest, `V_b` to `f` honest, ∅ to the rest) — the leader's per-value witnesses + the silent operators harvesting `V_a` via forwarded witnesses push one value's σ-pool past `qV`. **Success (fastest).** (Pigeonhole 2 guarantees only one value reaches `qV`.)
- **2-1 partial equivocation** — `V_a` to `2f` honest, `V_b` to one: `σ-pool[V_a]` = `2f` recipients + the leader's witness = `2f+1 = qV`. **Success (fastest).**
- **All-NR equivocation** (both values flooded to everyone) — every honest operator observes ≥ 2 values → `KindCommit-NRDirect` → `nr_tag_0`-quorum → **Success (fall-through)** under uniform delivery. (Under heavy jitter, async fire can σ-lock some operators on the first value before the second arrives; the slot then mostly decides fast at L_0 with a minority miss tail — safety-preserving, since finalizing one of the two equivocated values is a valid decision and the equivocation stays slashable.)
- **1-1-1 equivocation** (a distinct value to each honest operator) — each σ-locks on its own value before re-flood surfaces the conflict; every `σ-pool[V_i]` = the recipient + the leader's witness = `2 < qV`, and no σ-locked operator can pivot to NR. **Miss** (no fall-through; recovers next slot). This is the headline cost of the leader-witness head-start.

Pigeonhole 2 ensures at most one value can reach `qV` cluster-wide regardless of how honest σ-emissions split — there is no two-output safety violation in any equivocation case above.

**Validity-divergence** (a re-org splits honest host verdicts mid-slot; the leader's Phase-1 witness locks it σ-side regardless of a later host flip):
- **3-σ vs 1-NV** — `σ-pool[V] = 3 = qV`. **Success (fastest).**
- **1-σ vs 3-NV** — `noValuePool[0] = 3 = qEnc` (the NV operators can NR since they never σ-locked); fall-through. **Success (fall-through).**
- **2-σ vs 2-NV (boundary)** — `σ-pool = 2 < qV` and `noValuePool = 2 < qEnc`; no trigger fires; **Miss** (stalls at L_0). The f=1 n=4 algebraic limit.
- **Validity-divergence + passive byzantine** (the honest split is below quorum and the byzantine stays silent or σ-locks on a single value, including a leader whose host flips post-fetch) — neither quorum reaches; **Miss**. These are the σ-locked-leader regressions (assumption 3).

**Deepest-layer / multi-failure fall-through.** With `K > 2` layers the cluster falls through past multiple silent or non-validating layers **within Phase 3's single reconstruction walk** (sequential local decryption, no per-layer RTT) — a structural difference from QBFT (which round-changes once per failed leader). If all `K` leaders are silent, or every layer's leader fails non-recoverably, the slot **misses** — at `K ≥ f+1`, byzantines alone cannot cause this (pigeonhole guarantees ≥ 1 honest leader); it requires `> f` faults or coincident independent failures.

**Adversarial scheduling.** A network adversary delaying messages by up to `1 BTT` cannot affect safety (the pigeonholes are over the cluster-wide signed-message set, not arrival times). Its liveness effect reduces to the propagation split: delaying `V` to ≤ 1 honest still leaves `σ-pool = 2 + leader witness = qV` (the quorum reaches without the delayed operator); delaying to ≥ 2 honest is absorbed by the `KindNoValue` no-lock — the delayed operators upgrade to σ if `V` or a forwarded witness re-floods before they NR-commit (the `h_V=1` shape, under healthy mesh), else fall through if `noValuePool` reaches `qEnc`, else slot-miss on the degraded-mesh / dormant-byz tail.

### Slashing evidence

Six rules surface byzantine faults for *attribution and out-of-band punishment*; under the safety guarantees above none is load-bearing for safety. Honest operators MUST log observed evidence for later aggregation (the planned manual-blacklist extension and on-chain slashing are the consumers). Each piece is verifiable in isolation (signed by the offender's own keys).

- **Rule 1 — Cross-signing.** An operator emitted both a σ partial and an `nr_tag_k` partial at the same layer (σ-XOR-NR violation). Cryptographic, self-contained.
- **Rule 2 — Leader equivocation.** Two distinct auth-valid Phase-1 bundles from the same leader at the same `(slot, layer)`. Cryptographic, self-contained. (Detection is capped at two retained values per `(layer, leader)` — two suffice for evidence.)
- **Rule 3 — Cross-σ-V equivocation.** An operator emitted σ partials on two distinct values at the same layer (e.g., two `KindValue`s on different `V_0`). Cryptographic, self-contained.
- **Rule 4 — Fake encrypted-presence (`k > 0`).** A `SigmaChained` entry that, after NR-quorum unlocks decryption, decrypts to a non-verifying partial (or fails to decrypt). Cryptographic but delayed — conditional on NR-quorum reaching every prior layer; stays sealed when the slot misses cleanly at L_0.
- **Rule 5 — Fake plaintext σ.** A plaintext σ partial (a leader's Phase-1 `LWitness`, or an operator's `KindValue.L0Partial`) that does not verify against the signer's pubKeyShare on the claimed value. Keyed on the **signer**: a bad `LWitness` in a directly-observed bundle is attributed to the **leader**; a bad `L0Partial` to the **emitter**. A *forwarded* witness inside a peer's `KindValue` that fails to verify is **silently discarded, not slashed** — it is signed-for-forwarding by the emitter, not the leader, so firing Rule 5 against the leader here would open a framing attack: a byzantine forwarder can trivially place non-verifying garbage in the forwarded witness, and an honest leader would be slashed for it. The genuine leader-signed-garbage case instead stays covered by the direct-observation path, where the bundle's own op-identity envelope binds the bytes to the leader. Cryptographic, self-contained for the direct paths.
- **Rule 6 — Phase-2 equivocation.** An operator's emission sequence is not in the authorized set `{KindNoValue→KindValue (upgrade), KindNoValue→KindCommit-NR, KindCommit-NRDirect-alone}` — e.g., a `KindValue` followed by any NR partial at L_0 (σ-XOR-NR). Cryptographic, self-contained. (There is no verdict-vs-action rule: `KindValue` *is* the σ-side action, so there is no separate non-binding claim for a later action to contradict.)

Evidence is **retroactively cross-fired at L_0**: Rule 3 re-evaluates on every L_0 σ-pool insertion — a leader's Phase-1 witness, an emitter's own `KindValue.L0Partial`, or a harvested forwarded witness — so a single op that holds σ partials on two distinct values at L_0 is caught in any arrival order (bundle-then-`KindValue`, `KindValue`-then-bundle, forwarded-witness-vs-bundle), not just when two `KindValue`s arrive. Rule 5 likewise fires per partial-arrival on a verify-fail. The re-walk is over the L_0 σ-pool only (≤ n operators, each capped at two retained value-roots) and Rule 3 fires at most once per op (deduped); each forwarded witness verifies once per `(leader, value_root)` regardless of how many peers forward it. (Deeper layers have no plaintext cross-σ-V re-eval — k>0 σ partials are chained-encrypted.)

### Failure modes

The slot misses (no `V` signature) under:

- **[Class A] Sustained partition** — real propagation exceeds the absorption window; violates assumption 2. Clean miss, no safety violation.
- **[Class A] More than `f` faults** — violates assumption 1.
- **[Class A] Validity-divergence at the 2-2 boundary** — re-org produces a 2-σ vs 2-NV honest split; neither quorum reaches; clean miss. (In practice backup leaders fetch from deeper-confirmed parents and rarely share L_0's re-org exposure, so L_1 usually decides.)
- **[Class A] All-leaders-silent / backup-leader cascade** — every layer's leader fails; requires `> f` faults or coincident independent failures.
- **[Class B — leader-witness trade-off]** **1-1-1 leader equivocation** and **validity-divergence + passive byzantine** (incl. the leader-host-flip case) — a value's σ-pool and the no-value cohort both fall below quorum because σ-locked operators (including the leader, locked by its own witness) cannot pivot to NR. These are slashable where the byzantine emits contradicting partials (Rule 3 / Rule 6 / Rule 2), behavioral-only where it stays silent. They recover on the next slot under the rational-byzantine deterrent. A design without the Phase-1 leader witness recovers these via NR fall-through, at the cost of the healthy-path head-start; 2abOBFT takes the head-start.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)** (equivalently, signature-based witness encryption). Production-grade implementations exist: `drand/tlock` (Go, audited, on Drand mainnet since 2023) and Shutter Network (Gnosis Chain). 2abOBFT uses `K-1` IBE tags per slot (the NR tags; the deepest layer has none). The chained encryption at each layer transition is a single IBE ciphertext under `nr_tag_k`, nested across layers: a σ partial at layer `k` is wrapped in `k` levels (`nr_tag_0` outermost). Per-`LayerEntry` ciphertext grows `O(k)`; at K=2 the chain has one level, at K=5 it has four. The V-signing DKG reuses SSV's operator-share setup; the IBE keypair needs a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init (long-lived, no per-slot rotation; distinct backend from the V-keypair so the IBE primitive can use its expected DST).

## Properties summary

| Property | 2abOBFT |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qV = qEnc = 2f+1` + chained IBE + EKM-enforced single-σ-V and σ-XOR-NR, against an offline-aggregating byzantine within the f-bound. Honest-majority cryptographic (depends on honest software correctly enforcing the rules), not 100% cryptographic. Same trust posture as QBFT. |
| Validity (output ∈ proposed values, host-valid) | Yes, conditional on the host precondition (assumption 3). |
| Termination | Conditional on partial synchrony within the slot's relay deadline and ≤ f faults. |
| Healthy-path latency | Fast — σ-quorum ~`1·BTT` after the leader's bundle arrives (leader-witness head-start + σ-inline `KindValue`; async fire, no synchronized fire instant). |
| Equivocation detection | Yes — leader-equivocation (Rule 2), cross-σ-V (Rule 3), fake-σ (Rule 5), Phase-2 sequence (Rule 6) are self-contained slashable evidence. |
| Equivocation recovery | σ-locked-split and 2-1 partial recover fast at L_0 (witness head-start); all-NR equivocation falls through to L_1. **1-1-1 equivocation misses** (σ-locked operators cannot pivot to NR) — the cost of the leader-witness head-start. |
| Validity-divergence recovery | Honest-majority recovers (3-σ vs 1-NV → L_0; 1-σ vs 3-NV → L_1). **2-2 boundary and validity-divergence-with-passive-byz miss** (the σ-locked-leader limit). |
| Network-flakiness tolerance | Good — `SafetyBuffer` absorbs the IHAVE/IWANT cycle; h_V=1 selective-delivery recovers via peer-reflood-V; late-leader and mesh-flaky cases decide at L_0. |
| Built-in leader fallback | Yes — `K`-layer fall-through within Phase 3's single reconstruction walk (no per-layer RTT); `K = f+1` recommended default. |
| Round-change recovery | No — single-round. Within-slot recovery is the σ-pool fill window + the NR fall-through ladder. |
| EKM complexity | Lowest in the OBFT family — at most one signing event per `(slot, layer)` per operator. |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide (OBFT-family property). |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block, the adapter's default is **`K = f+1`** (K=2 at n=4: one fall-through layer `L_0 → L_1`), motivated by production testing showing `K > f+1` doesn't materially improve outcomes once the common grief patterns decide at L_0. The adapter also supports `K = f+2` (one extra layer) and `K = n`.

| 2abOBFT concept | SSV mapping |
|---|---|
| `n`, `f` | 4, 1 (also 7/2, 10/3, 13/4) |
| `K` layers | `(n-1)/3 + 1` by default (2 at n=4) |
| V-signing keypair | the validator's split BLS key (already in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| `L_0` (primary) | designated MEV proposer; `V_{L_0}` = MEV-optimized block fetched late from the relay |
| `L_1..L_{K-1}` (backups) | distinct operators; `V_{L_k}` = safe early-fetched payloads from deeper-confirmed parents, refreshed on head changes |

**Timing budget (reference config: P99 = 150ms, δ = 50ms, 1 BTT = 200ms; slot relay cutoff `T_relay_cutoff = slot_start + 4.0s`).** All instants derive backward from the relay cutoff:

- `resolveBudget = 1·BTT + max(SafetyBuffer, 1·BTT) + ε_3 + phase3JitterBuffer + HeaderSubmitHeadroom` — at default `SafetyBuffer = 700ms`, the core terms are `200 + 700 + 50 = 950ms`, plus the small fixed reserves (`HeaderSubmitHeadroom ≈ 100ms` + the Phase-3 jitter cushion).
- `TPhase2a = T_relay_cutoff − resolveBudget` (the Phase-2a backstop).
- `T_0_broadcast = TPhase2a − 1·BTT`; `fetchAt[k] = T_0_broadcast − B_k` with `B_k = (k+2)·BTT` for shallower layers and `T_0_broadcast` for the deepest (clamping its fetch to `BFT_start`).
- `resolveDeadline = TPhase2a + max(1·BTT + SafetyBuffer, 2·BTT) + ε_3`, clamped to `T_relay_cutoff − HeaderSubmitHeadroom − phase3JitterBuffer`.

Under healthy mesh the slot decides ~`1·BTT` after the L_0 bundle arrives — well inside `TPhase2a` — leaving the bulk of the slot as submission headroom. A later `TPhase2a` anchor (smaller `SafetyBuffer`) trades mesh-tail tolerance for a wider MEV-relay fetch window for `L_0`.

**Head-change handling.** The host's `valid` / `not-valid` verdict includes a `parent_root`-vs-head check. The leader's pre-broadcast fetch loop (fetch → validate → re-fetch on head change) runs internally; it broadcasts the final `V_{L_k}` plus the σ-witness on it. Each receiver re-checks validity at bundle acceptance, Phase-2a fire, and upgrade time; a flip to NV before σ-lock steers it away from σ, but a flip *after* σ-lock has no effect (the σ commitment is binding). Backup leaders fetch from deeper-confirmed parents and are structurally re-org-resistant, so an L_0 re-org rarely repeats at L_1.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster (V-signing and IBE), one DKG each at cluster init; long-lived, no per-slot rotation.
2. **Clock skew** across operators must be bounded by `δ` and known (the timing derivation assumes it).
3. **Choosing K.** `f+1 ≤ K ≤ n`; default `K = f+1`. Larger `K` adds fall-through layers (deeper recovery) at the cost of larger onions and longer Phase-3 IBE walks; smaller `K` has fewer backstops.
4. **R is fixed at 1.** Multi-round extension (Phase-2 split + R-round retry) composes cleanly but is not specified here; see [OBFTR(R≥2)](OBFTR.md).
5. **Tag replay.** Each `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` uniquely binds `(slot, cluster, layer)`.
6. **"At most one full sig" is per-instance** — assumes a single 2abOBFT instance per slot and domain separation (via the `ProtocolTag`) from any other path that signs the V-share.

## Comparison with the OBFT family and QBFT

QBFT is SSV's existing consensus protocol; bare [OBFT](OBFT.md) is the spec-simplest OBFT-family ancestor (single Phase 2, single `KindCommit`); [OBFTR(R≥2)](OBFTR.md) is OBFT with R-round retry. The structural axes:

- **QBFT vs the OBFT family:** QBFT separates "decide on a value" from "sign the decided value"; the OBFT family fuses them by embedding threshold partials inside the consensus phases. QBFT recovers a silent leader via serial round-change (`RT` per round); the OBFT family recovers `K-1` silent leaders in a *single* Phase-3 reconstruction walk (parallel fall-through, no per-layer RTT) — decisive when `K-1` is large relative to the slot budget.
- **2abOBFT vs bare OBFT:** both carry a per-layer leader σ-witness, reach σ-quorum fast at L_0, and recover honest-majority validity-divergence (3-1 / 1-3) and 2-1 partial equivocation. 2abOBFT adds the Phase-2 split (the `KindNoValue` coordination signal + the dynamic NR-eligibility commit) and the peer-reflood-V harvest of forwarded witnesses. The net effect over bare OBFT: the no-lock recovers **σ-locked-split leader equivocation** (the silent operators harvest the value via forwarded witnesses and upgrade) and **mesh-flakiness with a byzantine σ-refusal** (the flaky operator upgrades instead of hard-NR-locking) — both of which bare OBFT misses — and decides a **late L_0-leader broadcast** at L_0 rather than falling through. They are otherwise close on the healthy path and share the same residual misses (1-1-1 equivocation, 2-2 validity-divergence).
- **The per-layer propagation deadlock** (bare OBFT's [§Liveness](#liveness-synchrony-conditional) box names this its `(b)` condition): bare OBFT carries the widest band — a receiver without `V` at `T_commit` hard-NR-locks, so the partial-propagation middle (`n − qEnc < receivers < qV`) deadlocks unless peer-reflood-V lifts it at L_0. 2abOBFT **narrows it to a recoverable stall**: the `KindNoValue` no-lock keeps late-`V` receivers upgrade-eligible, so a **late L_0-leader broadcast decides at L_0** (where bare OBFT falls through to L_1) and late multi-receiver propagation tails upgrade to σ rather than NR-lock; the residual is the shared degraded-mesh / non-delivery tail. QBFT has **no analogous per-layer deadlock** — round-change re-proposes — which is also why it recovers the 1-1-1 equivocation and 2-2 validity-divergence cases the OBFT family misses (those are separate from propagation, but trace to the same absence of an in-protocol pivot in the family).

**Recovery coverage** (✓ recover, ✗ miss; at equal total slot budget):

| Failure class | bare OBFT | 2abOBFT | QBFT |
|---|---|---|---|
| Healthy path | ✓ | ✓ | ✓ |
| Network flakiness / σ-refusal / non-leader fault (any one alone, within f) | ✓ | ✓ | ✓ |
| Primary leader silent / crashed | ✓ fall-through | ✓ fall-through | ✓ round 2 |
| h_V=1 selective-delivery | ✓ at L_0 (peer-reflood-V) | ✓ at L_0 (peer-reflood-V) | ✓ round 2 |
| Late L_0-leader broadcast | ✓ fall-through (L_1) | ✓ at L_0 | ✓ round 2 |
| Mesh-flaky honest + byz σ-refusal | ✗ (flaky op hard-NR-locks at `T_commit`; with the σ-refusal, σ-pool stays < qV) | ✓ at L_0 (no-lock — flaky op upgrades once its mesh delivers V) | ✓ round 2 |
| Validity-divergence, honest majority (3-1 / 1-3) | ✓ | ✓ | ✓ |
| 2-1 partial leader equivocation | ✓ at L_0 | ✓ at L_0 | ✓ round 2 |
| σ-locked-split leader equivocation | ✗ | ✓ at L_0 (witness head-start + peer-reflood-V) | ✓ round 2 |
| All-NR leader equivocation | ✓ fall-through | ✓ fall-through | ✓ |
| 1-1-1 leader equivocation | ✗ | ✗ (σ-locked, no NR pivot) | ✓ round 2 |
| Validity-divergence 2-2 boundary | ✗ | ✗ | ✓ round 2 |
| Validity-divergence + passive byzantine | ✗ | ✗ | ✓ round 2 |
| All leaders silent / sustained partition / `> f` faults | ✗ | ✗ | ✗ |

(QBFT cells marked "round 2" recover by timing out round 1 and re-proposing a fresh value in round 2 — which costs a round-timeout `RT` of latency. The OBFT family instead falls through `K` layers within a *single* Phase-3 walk, no per-layer RTT, so it scales better when many leaders are silent; QBFT's serial round-change does not fit the slot budget once several round-changes are needed.)

The "Late L_0-leader broadcast" and "Mesh-flaky honest + byz σ-refusal" rows are the all-honest faces of bare OBFT's **`(b)` per-layer propagation deadlock**: a late or partial Phase-1 delivery leaves the layer in the `n − qEnc < receivers < qV` middle band, where bare OBFT hard-NR-locks at `T_commit` while 2abOBFT's `KindNoValue` no-lock keeps receivers upgrade-eligible — so 2abOBFT decides a late leader at L_0 (not L_1) and lets a mesh-flaky operator upgrade once V arrives rather than NR-lock into a miss. "σ-locked-split leader equivocation" is the byzantine sibling — separate from propagation but recovered by the same no-lock (the value-less operators harvest a forwarded witness and upgrade). Under a healthy mesh h_V=1 recovers the same in both protocols (peer-reflood-V) and the deep non-delivery tail misses in both; 1-1-1 (no NR pivot) is the residual the no-lock can't reach.

2abOBFT's distinctive wins over bare OBFT all come from the `KindNoValue` no-lock + upgrade — **σ-locked-split leader equivocation**, **mesh-flakiness with byz σ-refusal**, and deciding a **late L_0-leader at L_0**. Its distinctive losses relative to QBFT are the patterns a fresh-value round 2 recovers but a single round cannot re-propose: **1-1-1 equivocation, the 2-2 validity boundary, and validity-divergence with a passive byzantine**. Bandwidth (n=4, K=2, healthy) is ~20–22 KB cluster-wide, comparable to bare OBFT and modestly above QBFT.
