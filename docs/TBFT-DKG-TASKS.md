# TBFT-DKG — Pedersen DKG for IBE Keypair (Option B)

This document is the implementation plan for T5 in [TASKS.md](TASKS.md) — upgrading TBFT from Option A (reuse the validator threshold key for IBE under the DST-trick) to Option B (separate IBE keypair at threshold `qEnc = f+1`, established via Pedersen DKG between operators). The T5 entry in [TASKS.md](TASKS.md) is now a pointer to this document.

## Scope

In scope:
- Pedersen DKG ceremony between an SSV cluster's operators, run once per cluster, at threshold `f+1`, producing a per-cluster IBE keypair `(s_IBE, P_IBE)` with each operator holding a share `s_IBE_i`.
- Persistence of the resulting share material on the SSV node alongside the validator share, with restart durability.
- Wiring this material into the existing TBFT controller construction so the IBE-tag signing path uses the IBE keypair at threshold `f+1` rather than the validator keypair at `2f+1`.
- Realization of the protocol-level threshold separation [TBFT.md](TBFT.md) describes — `Config.QV() = 2f+1` for σ-quorum, `Config.QEnc() = f+1` for layer-unlock — coincident with this work (T4 lands here).
- End-to-end test proving `f+1` IBE-share partials decrypt a layer-1 ciphertext, where `2f+1` validator-share partials would not (i.e. threshold separation is *real*, not symbolic).

Explicitly out of scope (tracked under "Future work" below): remote-signer parity, kyber resharing on cluster reconfig, in-flight DKG recovery on restart, migration of any pre-existing Option-A cluster.

## References

- [TBFT.md](TBFT.md) — protocol spec; "Setting", "Cryptographic primitive", "Why it's safe", and "DKG cost" caveats are the load-bearing sections for Option B.
- [TASKS.md](TASKS.md) — current implementation status; this work resolves T5 and lands T4 alongside.
- [IBE-INTEGRATION.md](IBE-INTEGRATION.md) — the DST-trick rationale that underpins Option A; carries over unchanged under Option B (same primitives, different shares).
- [github.com/ssvlabs/ssv-dkg](https://github.com/ssvlabs/ssv-dkg) — SSV's existing initiator-coordinated DKG ceremony for *validator* shares. Useful prior art for kyber DKG integration: `pkgs/board` shows the kyber `Board` adapter pattern; `pkgs/dkg/drand.go` shows `kyber_dkg.Config` setup with `drand_bls.NewSchemeOnG2(suite)` as the `Auth` scheme; `design.md` describes the exchange-then-deal-then-response-then-justification round structure. Architecture differs (HTTP+initiator there; P2P+peer-to-peer here), but the kyber-side wiring is directly informative.
- [github.com/drand/kyber/share/dkg](https://github.com/drand/kyber/tree/v1.3.2/share/dkg) — already a transitive dep at `v1.3.2`. Provides `Protocol`, `Board` interface, `Config`, `DistKeyShare`, FastSync mode.

## Architectural decisions

These were reviewed in conversation prior to writing this doc; capturing them here for permanence.

- **D1 — DKG trigger: on `ValidatorAdded` contract event.** Same control point as validator-share registration ([eth/eventhandler/handlers.go](../eth/eventhandler/handlers.go) `handleShareCreation`). Each operator processes the same event and starts the DKG independently; deterministic across the cluster.
- **D2 — Cluster scope: per-committee, not per-validator.** The cluster identity is `clusterID = share.CommitteeID()` (the canonical hash of the sorted operator-ID set). One DKG amortizes across every validator the same committee operates. This matches how `clusterID` is already used at [setup_tbft.go:63](../operator/validator/setup_tbft.go) and how committee identity is structured throughout SSV.
- **D3 — Transport: separate SSV message type, not extending the TBFT envelope.** Add `SSVDKGMsgType` alongside the existing `SSVTBFTMsgType` ([protocol/v2/ssv/runner/proposer_tbft.go:198](../protocol/v2/ssv/runner/proposer_tbft.go)). DKG is once-per-cluster-lifetime and the runtime TBFT path is per-slot; keeping the wire kinds disjoint avoids dispatch confusion and lets the DKG path evolve independently. Within `SSVDKGMsgType`, sub-kinds for `Exchange`, `Deal`, `Response`, `Justification`.
- **D4 — Kyber long-term keys: fresh per ceremony, distributed via "Exchange" pre-phase.** Match ssv-dkg's approach: each operator generates a fresh kyber scalar at DKG start, broadcasts the corresponding G1 point in a signed `Exchange` message before the deal phase begins. All operators wait until they have an `Exchange` from every cluster member, then start `kyber_dkg.NewProtocol` with `NewNodes` populated from the exchanged points. Cleaner than KDF-based derivation; long-term key compromise across clusters is forward-secure for free; matches existing prior art.
- **D5 — Nonce derivation: deterministic from `H(clusterID || generation)`.** Every operator computes the same nonce; cross-cluster replay is structurally prevented; generation increments on re-DKG (D8).
- **D6 — FastSync mode.** Closes as soon as all responses are in (typically <1 RTT after deals); falls back to timeout only if a complaint occurs. Bandwidth is `O(n²)` for the response phase, well within budget for SSV cluster sizes (n ≤ 13).
- **D7 — Duty processing blocked until DKG completes.** Following the QBFT-removal commit on this branch, the proposer duty has no fallback path; today, a missing IBE share at startup means the proposer runner is silently skipped (see [operator/validator/controller.go](../operator/validator/controller.go) `case spectypes.RoleProposer`). Under Option B this changes shape: when the SSV node starts up and finds an own-validator share whose committee has no persisted IBE share, it runs DKG **before** marking the validator ready for duties. No QBFT fallback; no per-message gate; simply: validators with incomplete cluster IBE state don't participate in any duty until DKG concludes.
- **D8 — Reconfig: full re-DKG on any committee change; new generation counter.** First-cut behavior: any change to the operator set ⇒ discard existing IBE share, re-run DKG with `generation+1`. Kyber resharing (`OldNodes`/`NewNodes` `Config` fields) is a follow-up optimization (out-of-scope for v1).
- **D9 — Restart durability: persist only at `FinishPhase`. No mid-DKG resumption.** State machine is "either complete-and-persisted, or restart from scratch". On startup, a clean slate is the only state the system needs to recognize beyond "share present". Significantly simplifies the lifecycle code; downside is wasted bandwidth if a long DKG is interrupted late, which is an acceptable trade for v1.
- **D10 — Remote-signer parity: out of scope.** IBE share lives on the SSV node regardless of where the validator share lives. Remote-signer support tracked under "Future work" with a note describing the eventual shape.
- **D11 — No migration story.** Greenfield clusters only. No existing Option-A clusters to upgrade.

## Phased task breakdown

Status conventions follow [TASKS.md](TASKS.md): `[ ]` not started · `[~]` in progress · `[x]` done · `[?]` blocked or under investigation.

### Phase A — Design alignment

- [x] **A1.** This document. D1–D11 captured above; phases below committed.
- [x] **A2.** Wire format draft for DKG envelopes. Landed at `protocol/v2/dkg/wire/{envelope,exchange,bundles}.go` with round-trip tests; the `[version][kind][body]` framing was factored to `protocol/v2/wire/framing.go` and now also backs the existing TBFT-wire envelope.
- [x] **A3.** Storage schema sketch + interface stub at `ssvsigner/ekm/ibe_share_storage.go` (`IBEShareBytesProvider` / `IBEShareWriter`, bytes-only so kyber stays out of ssvsigner; top-of-file schema sketch covers BadgerDB layout, encryption-at-rest, generation pointer).

### Phase B — DKG core in-process

- [x] **B1. `protocol/v2/dkg/board.go`** — kyber `Board` adapter over an injected broadcast function. Channel-buffered `dealCh` / `responseCh` / `justificationCh`; outbound bundles wrapped in DKG envelopes (with cluster/generation routing) and broadcast; inbound bundles routed via `Receive(*Envelope)` after cluster/generation filter.
- [x] **B2. `protocol/v2/dkg/keys.go`** — `Keypair` struct + `GenerateKeypair(group)` (fresh scalar, computed pubkey). Throwaway per ceremony.
- [x] **B3. `protocol/v2/dkg/coordinator.go`** — concrete `Coordinator` type with the planned `Run` signature. Drives Exchange phase synchronously, then constructs `kyber_dkg.NewProtocol` in FastSync mode with `Auth = bdn.NewSchemeOnG2(suite)`, pumps the inbox into the Board until completion. Nonce derivation per D5: `H(clusterID || generation)`.
- [~] **B4. Unit tests with synthetic transport.** Four of the five planned tests landed at `protocol/v2/dkg/coordinator_test.go`:
    - [x] Happy-path 7-of-7 — DKG completes; `Commits[0]` matches across operators.
    - [x] Threshold property — exactly `f+1 = 3` shares Lagrange-interpolate to a scalar whose public matches `Commits[0]`.
    - [x] Below-threshold — `f = 2` shares fail to recover.
    - [x] Liveness limit — 5/7 online ⇒ exchange phase times out cleanly on every survivor.
    - [ ] **Byzantine-dealer test deferred.** Constructing a kyber `DealBundle` with internally-inconsistent shares (the case that triggers the complaint→justification path) requires bypassing kyber's signing or hooking into internals; not trivial without forking. Will be exercised naturally in Phase G3 over the real transport via fault injection. Tracked here so it isn't lost.

  Phaser period note: in fresh DKG (`OldNodes == NewNodes`, every node both issues and receives), kyber's FastSync auto-advance condition `deals.Len() == oldN` is unreachable — each node misses its own broadcast. The phaser-period timeout therefore drives phase advancement; tests use 1s, production wires the existing 2s default. Worth keeping in mind when tuning C3.

### Phase C — Wire & transport

- [ ] **C1. `protocol/v2/dkg/wire/`** — versioned envelope + kind enum + encoders. Outer envelope analogous to [tbft/wire/envelope.go](../protocol/v2/tbft/wire/envelope.go) but for `SSVDKGMsgType`. Inner kinds `KindExchange = 0x01`, `KindDeal = 0x02`, `KindResponse = 0x03`, `KindJustification = 0x04`. Round-trip tests for each.
- [ ] **C2. SSV-side `Board` adapter** — a `protocol/v2/dkg.Board` impl that broadcasts via [protocolp2p.Network.Broadcast](../protocol/v2/p2p/network.go) over the validator subnet, signing the outbound message with the operator identity key (matching how TBFT envelopes are signed today). Inbound: hooked into the existing message router via a new `MsgType` dispatch arm; the validator's running coordinator (Phase E) consumes the inbound channels.
- [ ] **C3. Phaser** — kyber's `TimePhaser` is fine in FastSync mode; only `DealPhase` start needs explicit signaling. Custom phaser only if we need to coordinate phase boundaries with the receive-Exchange barrier (which precedes the kyber DKG proper, so it sits outside kyber's phaser anyway).
- [ ] **C4. End-to-end transport tests.**
    - 4-operator cluster over the in-process P2P stub used by other TBFT tests, DKG completes.
    - 7-operator cluster, one byzantine dealer, complaint+justification path runs over real wire envelopes; DKG completes with QUAL=6.
    - Network-loss resilience: drop X% of DKG packets randomly; assert DKG either completes or fails cleanly within a bounded retry window.

### Phase D — Persistence & EKM accessor

- [ ] **D1. `ssvsigner/ekm/ibe_share_storage.go`** — store `DistKeyShare` (`Commits[]` + `Share.PriShare`) keyed by `(clusterID, generation)`. Encrypted-at-rest using the operator key, mirroring the wallet storage at [signer_storage.go](../ssvsigner/ekm/signer_storage.go). Atomic write at FinishPhase only (per D9).
- [ ] **D2. Extend `LocalKeyManager`** with:
    - `AddIBEShare(clusterID [32]byte, generation uint64, share *kyber_dkg.DistKeyShare) error`
    - `GetIBEShareBytes(clusterID [32]byte) ([]byte, error)` — returns the operator's serialized scalar share.
    - `GetClusterIBEPubKey(clusterID [32]byte) ([]byte, error)` — returns `Commits[0]` marshaled to bytes.
    - `GetClusterIBEPolyCommits(clusterID [32]byte) ([]kyber.Point, error)` — full `Commits` array. Useful if Phase E1 below adds per-NR-partial verification.
- [ ] **D3. Add `IBEShareBytesProvider` interface** in [ssvsigner/ekm/key_manager.go](../ssvsigner/ekm/key_manager.go), parallel to `ShareBytesProvider`. Only `LocalKeyManager` implements; `RemoteKeyManager` returns `ErrNotImplemented` (tracked under "Future work" below).

### Phase E — Trigger & lifecycle

- [ ] **E1. `operator/validator/dkg_orchestrator.go`** — per-node coordinator that:
    - On startup, walks all own-validator shares and identifies any whose `clusterID` lacks a persisted IBE share.
    - For each, kicks off a `Coordinator.Run` (Phase B3) and blocks the validator's duty registration until completion.
    - Idempotent: if a share is already persisted, skip.
    - Timeout / retry policy: a configured number of attempts with backoff before logging a fatal-ish error and refusing to start the validator.
- [ ] **E2. Hook into `handleShareCreation`** ([eth/eventhandler/handlers.go](../eth/eventhandler/handlers.go)) — when a *new* validator share lands at runtime (post-startup) for a committee whose IBE share is absent, kick the orchestrator. While DKG is in flight, the validator does not register a proposer runner; on DKG completion, the proposer runner is created and the validator becomes proposer-active.
- [ ] **E3. Update [operator/validator/setup_tbft.go](../operator/validator/setup_tbft.go)** to bind:
    - `Signer:        blsbackend.New(validatorShareBytes)` — unchanged (the value-signing share is still the validator share).
    - `TagSigner:     blsbackend.NewKyberSigner(ibeShareBytes)` — NEW source: `LocalKeyManager.GetIBEShareBytes(clusterID)`.
    - `ClusterPubKey: clusterIBEPubKey` — NEW source: `LocalKeyManager.GetClusterIBEPubKey(clusterID)`. The validator pubkey is no longer used inside TBFT; it's still used by the runner's beacon-submission path, which lives outside.
    - `PubKeyShares:  pubKeyShares` — unchanged for value-signing verification (validator-side shares). If we add per-NR-partial verification (E5 below), a separate `IBEPubKeyShares` map enters here, computed from `Commits` evaluated at each operator's index.
- [ ] **E4. Land [T4](TASKS.md)** — `Config.QV() = 2f+1` and `Config.QEnc() = f+1`; switch the σ-quorum check at [tryReconstructLayer](../protocol/v2/tbft/instance.go) to `cfg.QV()` and the NR-quorum check at [tryDeriveNextLayerKey](../protocol/v2/tbft/instance.go) to `cfg.QEnc()`. Lands here because under Option B these counts are now meaningful (`f+1` IBE-share partials genuinely decrypt; under Option A they don't).
- [ ] **E5. (Optional, decide during implementation) Per-NR-partial verification.** Today, NR partials are aggregated without individual verification ([instance.go `tryDeriveNextLayerKey`](../protocol/v2/tbft/instance.go)) — a byzantine peer's garbage NR partial silently corrupts the aggregate, which then fails to decrypt (a liveness loss but not a safety loss). Under Option B we have per-operator IBE-share pubkeys (from `Commits` evaluated at index `i`) available for free, so we can verify each partial cheaply. If we land it: extend `Instance` with an `ibePubKeyShares` map and call `tagSigner.VerifyPartial(ibePubKeyShares[opID], tag, partial)` before counting an NR. If skipped: defer; track as a follow-up. Decide based on whether the asymmetry vs σ-partials (which *are* verified) feels worth resolving in this PR.

### Phase F — Reconfig

- [ ] **F1.** Detect committee change in `handleShareCreation` (committee for an existing validator's clusterID differs from the persisted committee). The clusterID itself derives from the committee, so a "committee change" is really a new cluster appearing — but the *prior* cluster's IBE share is now stale and should be retired.
- [ ] **F2.** First-cut behavior: discard the old IBE share, run a fresh DKG with `generation+1`. Same orchestrator path as Phase E1.
- [ ] **F3.** Tests: cluster reconfig triggers re-DKG; old IBE share removed atomically alongside the new IBE share landing; validator stays on a "no proposer runner" state until new DKG completes.

### Phase G — End-to-end devnet validation

- [ ] **G1.** Adapt or fork [end_to_end_real_ibe_test.go](../protocol/v2/tbft/blsbackend/end_to_end_real_ibe_test.go) — a setup where the IBE master scalar is *distinct* from the validator master scalar (i.e. truly Option B, not the DST-trick-on-validator-share that today's test exercises). Same DST machinery applies, just keyed differently. Critical assertions:
    - With exactly `f+1 = 3` (n=7) IBE-share partials on a no-quorum tag, the layer-1 ciphertext decrypts.
    - With `f = 2` IBE-share partials, decryption fails — proving threshold separation is real (not symbolic).
    - With `2f+1 = 5` *validator-share* partials on the same tag, decryption fails — proving the IBE keypair is genuinely distinct from the validator keypair.
    - The reconstructed validator-output signature still byte-equals what the master herumi key would sign directly (i.e. the σ-side path is unaffected).
- [ ] **G2.** Multi-node devnet: 4-node and 7-node clusters complete DKG over real P2P, run TBFT proposer slots. Observe layer-1 fall-through under simulated layer-0 leader silence.
- [ ] **G3.** Failure scenarios:
    - Kill an operator mid-DKG; restart it; verify it cleanly restarts the DKG from scratch (per D9) and the cluster eventually converges.
    - Kill an operator mid-TBFT under the DKG-derived IBE share; verify σ + NR behavior matches Phase 5/6 expectations from [TBFT.md](TBFT.md).
    - Two-faulty scenario at n=7 (f=2): 2 operators offline ⇒ remaining 5 honest can still complete DKG (threshold 3) and run TBFT.

## Open risks / things to revisit during implementation

- **Restart-mid-DKG interaction with `handleShareCreation`'s transactional model.** The contract event handler runs inside a database transaction. DKG is a long-lived async operation and must not block the txn. The orchestrator (E1/E2) needs to enqueue DKG work and return; the txn commits with a "DKG pending" marker for the cluster. On startup, the orchestrator scans these markers and resumes (per D9, "resume" means "run from scratch").
- **DKG bandwidth under FastSync at large `n`.** `DealBundle.Public` has `threshold` G2 points (~96 B each), and there are `n × n` deals (~96 B encrypted share each). For n=13, f=4, threshold=5: roughly `13 × (96·5 + 13·96) ≈ 23 KB` of dealing, plus FastSync's `O(n²)` responses (~30 KB). Comfortably within GossipSub message limits, but worth profiling on devnet to confirm no propagation issues.
- **kyber `Auth` scheme alignment.** ssv-dkg uses `drand_bls.NewSchemeOnG2(suite)` with the freshly-generated kyber long-term scalar as the auth key. We mirror this: the same fresh-per-ceremony kyber scalar from the Exchange phase serves as both `Longterm` (deal decryption) and the auth key. Standard kyber setup; no SSV-side adapter needed.
- **Generation counter semantics.** A monotonically increasing per-cluster counter persisted alongside the share. Initial DKG: generation=0. Reconfig (F2): generation++. Important the counter is part of the nonce so a re-DKG of the same cluster doesn't replay the previous one.
- **Per-NR-partial verification (E5).** The today-no-tomorrow-yes asymmetry. Decide during E1-E3 whether to land it in this work or defer.

## Future work — not on the v1 critical path

- **FW1. Remote-signer parity for IBE share.** The current plan persists the IBE share material on the SSV node (Phase D) regardless of where the validator share lives — IBE share has different blast radius than the validator share, so the security domains can differ for v1. For full parity with the existing SSV remote-signer architecture, ssv-signer/Web3Signer would need to expose drand-DST signing endpoints (so the IBE-side scalar never leaves the remote signer), and the `IBEShareBytesProvider` interface would route through `RemoteKeyManager` rather than `LocalKeyManager`. Multi-repo work; coordinate with [ssv-signer CLAUDE.md](../ssvsigner/CLAUDE.md). Tracked here so it doesn't get lost.
- **FW2. Kyber resharing on cluster reconfig.** Under D8/F2, any committee change runs a fresh DKG. Kyber's `OldNodes`/`NewNodes` resharing protocol can preserve continuity of the IBE keypair across reconfigs (so the cluster IBE pubkey stays stable). Operationally nicer (fewer DKG ceremonies, no re-publication of the IBE pubkey), but not safety-critical. Land if reconfig frequency in production warrants it.
- **FW3. In-flight DKG resumption on restart.** Per D9, restart mid-DKG drops the in-flight session and starts fresh. For v1 this is acceptable; if DKG bandwidth becomes a measurable cost in operations, persist phase-boundary state (deals received, responses sent) and resume from the last persisted boundary on restart.
- **FW4. Migration path for clusters running Option A.** Per D11, no migration story is needed for v1 (greenfield only). If Option-A clusters ever exist in the wild, a migration would need: a way to re-run DKG for an existing cluster without disrupting in-flight slots, an atomic switchover from "Option-A `KyberSigner` keyed on validator share" to "Option-B `KyberSigner` keyed on IBE share", and a backfill mechanism for the cluster IBE pubkey publication.
- **FW5. DKG observability.** Phase-state metrics, byzantine-event counters, time-per-phase histograms. Useful for production diagnosis. Defer until v1 is in operation and we know what to instrument.
- **FW6. Re-DKG-on-suspected-compromise runbook.** If an IBE share is suspected leaked, operators need a procedure to trigger a fresh DKG (generation++) without changing the committee. Same code path as F2 but triggered manually rather than by reconfig event. Document and add a CLI / admin endpoint.
