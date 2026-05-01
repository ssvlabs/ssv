# TBFT-DKG — Pedersen DKG for IBE Keypair (Option B)

This document is the implementation plan for T5 in [TASKS.md](TASKS.md) — upgrading TBFT from Option A (reuse the validator threshold key for IBE under the DST-trick) to Option B (separate IBE keypair at threshold `qEnc = 2f+1`, established via Pedersen DKG between operators). The T5 entry in [TASKS.md](TASKS.md) is now a pointer to this document.

> **Threshold note (post-audit-P0).** This doc was originally written assuming `qEnc = f+1` (threshold separation between σ and NR sides). Post-audit-P0, the protocol design moved to a unified `qEnc = qV = 2f+1` for cryptographic safety against byzantine cross-signing — see [TASKS.md](TASKS.md) D1 and [TBFT.md](TBFT.md) "Why it's safe". Option B is still required (the IBE keypair must be distinct from the V-keypair so the IBE primitive can use its expected DST), but the DKG threshold is now `2f+1`. References to `f+1` below are kept where they describe historical decisions or completed code; references to the *target* threshold are `2f+1`. Code/test deltas to apply this change are tracked in T4/T5 of [TASKS.md](TASKS.md).

## Scope

In scope:
- Pedersen DKG ceremony between an SSV cluster's operators, run once per cluster, at threshold `2f+1`, producing a per-cluster IBE keypair `(s_IBE, P_IBE)` with each operator holding a share `s_IBE_i`.
- Persistence of the resulting share material on the SSV node alongside the validator share, with restart durability.
- Wiring this material into the existing TBFT controller construction so the IBE-tag signing path uses the IBE keypair (distinct cryptographic backend; same `2f+1` threshold as the V-keypair).
- Coincident landing of the unified-threshold protocol counting `Config.QV() = Config.QEnc() = 2f+1` (T4 in [TASKS.md](TASKS.md)).
- End-to-end test proving `2f+1` IBE-share partials decrypt a layer-1 ciphertext, where `2f` IBE-share partials do not, and `2f+1` *validator-share* partials on the same tag do not (proving keypair distinctness — the IBE keypair is genuinely separate from the V-keypair, not just the same key under a different DST).

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
    - [x] Threshold property — exactly `f+1 = 3` shares Lagrange-interpolate to a scalar whose public matches `Commits[0]`. **Note:** these tests were written for the original `qEnc = f+1` design. Under the post-audit unified threshold (`qEnc = 2f+1`), the threshold-property test should be updated to use `2f+1 = 5` shares for n=7, and the below-threshold test should use `2f = 4` shares — see T4 in [TASKS.md](TASKS.md).
    - [x] Below-threshold — `f = 2` shares fail to recover. (See note above.)
    - [x] Liveness limit — 5/7 online ⇒ exchange phase times out cleanly on every survivor.
    - [ ] **Byzantine-dealer test deferred.** Constructing a kyber `DealBundle` with internally-inconsistent shares (the case that triggers the complaint→justification path) requires bypassing kyber's signing or hooking into internals; not trivial without forking. Will be exercised naturally in Phase G3 over the real transport via fault injection. Tracked here so it isn't lost.

  Phaser period note: in fresh DKG (`OldNodes == NewNodes`, every node both issues and receives), kyber's FastSync auto-advance condition `deals.Len() == oldN` is unreachable — each node misses its own broadcast. The phaser-period timeout therefore drives phase advancement; tests use 1s, production wires the existing 2s default. Worth keeping in mind when tuning C3.

### Phase C — Wire & transport

- [x] **C1.** Subsumed by A2: the wire format is at `protocol/v2/dkg/wire/` with the four kinds (`KindExchange = 0x01`, `KindDeal = 0x02`, `KindResponse = 0x03`, `KindJustification = 0x04`), built on the shared framing in `protocol/v2/wire/`.
- [x] **C2. P2P transport adapter** at `protocol/v2/dkg/p2p/transport.go`. `Broadcast(envelope)` wraps in a `SignedSSVMessage` with `MsgType = SSVDKGMsgType` (new constant in [protocol/v2/message/msg.go](../protocol/v2/message/msg.go), placeholder value `0xF1`), signs with the supplied `ssvtypes.OperatorSigner`, and publishes via `protocolp2p.Broadcaster.Broadcast`. `Inbox()` returns a buffered channel; `Deliver(envelope)` pushes inbound bytes in (caller is the per-node DKG dispatcher in Phase E). MsgID is supplied per ceremony — typically derived from one of the cluster's validator pubkeys so the message lands on a subnet every cluster operator is already subscribed to.
- [x] **C3. Phaser.** Kyber's `TimePhaser` works as-is. The exchange-barrier sits outside kyber's phaser (it's our pre-DKG addition) so no custom phaser is needed. See the phaser-period note under B4 — fresh-DKG FastSync auto-advance never fires, so phase advancement is phaser-driven and the period must allow round propagation under load.
- [~] **C4. Tests at `protocol/v2/dkg/p2p/transport_test.go`.**
    - [x] Unit: New() validation, BroadcastShape (correct MsgType / MsgID / signed wrapper), BroadcastEmpty, BroadcastNetworkError, DeliverInbox, DeliverEmpty, DeliverFull.
    - [x] End-to-end: 4-of-4 DKG completes through `dkgcore.Coordinator` instances wired via `*p2p.Transport` and a fanout test network, asserting `Commits[0]` matches across operators.
    - [ ] Network-loss resilience and byzantine-dealer-over-real-transport tests deferred to Phase G3 (devnet fault injection); both depend on real-transport plumbing that's Phase E's domain (per-cluster dispatcher + validator-side validation arm).

  Phase E will also wire the inbound side end-to-end: `SSVDKGMsgType` decoder in [queue/messages.go](../protocol/v2/ssv/queue/messages.go), a per-node DKG dispatcher routing by clusterID, and the corresponding case in `Validator.ProcessMessage` (or a node-level handler that bypasses the per-validator router, since DKG is per-committee not per-validator).

### Phase D — Persistence & EKM accessor

- [x] **D1. `ssvsigner/ekm/ibe_share_storage.go`** — `IBEShareRecord` struct (Generation, ShareBytes, ClusterIBEPubKey, PolyCommits). `Storage` interface gains `SaveIBEShare` / `GetIBEShare` / `RemoveIBEShare`; the `*storage` impl JSON-encodes the record and wraps it through the existing `encryptData` / `decryptData` (same encryption-at-rest as wallet accounts). One record per clusterID under the new prefix `signer_data-ibe_share-`. Generation lives inside the value, so a successful save for a new generation atomically supersedes any prior record.
- [x] **D2. `LocalKeyManager` IBE methods.** `AddIBEShare(clusterID, generation, shareBytes, clusterIBEPubKey, polyCommits)`, `RemoveIBEShare(clusterID)`, `GetIBEShareBytes(clusterID)`, `GetClusterIBEPubKey(clusterID)`, `GetClusterIBEPolyCommits(clusterID)`. Bytes-only contract — kyber stays out of ssvsigner; the orchestrator (Phase E) serializes the kyber `DistKeyShare` to bytes before calling. All getters return defensive copies. The signerStore reference is now captured on `LocalKeyManager` at construction.
- [x] **D3. Interfaces in [ibe_share_storage.go](../ssvsigner/ekm/ibe_share_storage.go).** `IBEShareBytesProvider` (read-side) and `IBEShareWriter` (write-side) declared from Phase A3; only `LocalKeyManager` implements. `RemoteKeyManager` does not (FW1: ssv-signer extension exposes drand-DST signing remotely).
- [x] **D4. Tests at `ssvsigner/ekm/ibe_share_storage_test.go`** — round-trip (with and without encryption-at-rest), missing → `ErrIBEShareNotFound`, overwrite, idempotent remove, distinct clusters, nil-record rejection, plus `LocalKeyManager`-level round-trip + empty-input rejection.

### Phase E — Trigger & lifecycle

- [x] **E0. Base wiring.** New `RoleDKG = 0xF0` constant in [protocol/v2/message/msg.go](../protocol/v2/message/msg.go) (placeholder); `committeeRole` extended to include it ([common_checks.go](../message/validation/common_checks.go)); validation type-switch + `validRole` arm ([signed_ssv_message.go](../message/validation/signed_ssv_message.go)); minimal `validateDKGMessage` (single-signer + body-non-empty; orchestrator decodes the envelope) at [dkg_validation.go](../message/validation/dkg_validation.go); dispatcher arm in [validation.go](../message/validation/validation.go); queue decoder pass-through in [queue/messages.go](../protocol/v2/ssv/queue/messages.go). Plus the kyber-DKG indexing fix at [protocol/v2/dkg/coordinator.go](../protocol/v2/dkg/coordinator.go) (`Index(opID-1)` → kyber's `Eval` adds 1 internally → share at `x = opID` matches `KyberSigner.AggregatePartials`).
- [x] **E1. `operator/validator/dkg_orchestrator.go`** — `*DKGOrchestrator` type. Owns per-cluster `*p2p.Transport` instances; `EnsureClusterIBE(ctx, clusterID, committee, generation)` is synchronous and idempotent (returns immediately if a share is already persisted), kicks `dkgcore.Coordinator.Run`, persists the resulting `*kyber_dkg.DistKeyShare` via `IBEShareWriter.AddIBEShare`. `Receive(envelope)` routes inbound DKG envelopes to the right cluster's Transport, with a bounded inbound buffer to handle the small startup race (peer A broadcasts before peer B has registered its own ceremony Transport). Tests: 4-of-4 happy path, idempotent second call, malformed-envelope rejection, threshold computation.
- [x] **E2. Lifecycle hooks in `*Controller`** ([operator/validator/controller.go](../operator/validator/controller.go)):
    - New `dkgOrchestrator *DKGOrchestrator` field, constructed in `NewController` if `BeaconSigner` implements `ibeShareStore` (LocalKeyManager); nil for remote-signer setups.
    - Bypass arm at the top of `handleRouterMessages` for `SSVDKGMsgType` → `c.dkgOrchestrator.Receive(...)`. Skips per-validator/per-committee dispatch (DKG is per-cluster, not per-validator).
    - `onShareInit` calls `EnsureClusterIBE` *before* `SetupRunners` so the proposer runner is built only after the cluster's IBE share is on disk. Per D7, sequential — duties don't proceed until DKG completes.
- [x] **E3. [operator/validator/setup_tbft.go](../operator/validator/setup_tbft.go)** — type-asserts the signer to both `ekm.ShareBytesProvider` (validator share) AND `ekm.IBEShareBytesProvider` (IBE share). Binds:
    - `Signer:        blsbackend.New(shareBytes)` — validator share, value signing unchanged.
    - `TagSigner:     blsbackend.NewKyberSigner(ibeShareBytes)` — NEW source: `LocalKeyManager.GetIBEShareBytes(clusterID)`.
    - `ClusterPubKey: clusterIBEPubKey` — NEW source: `LocalKeyManager.GetClusterIBEPubKey(clusterID)`.
    - `IBEPubKeyShares: ...` — NEW: computed from `GetClusterIBEPolyCommits(clusterID)` via kyber's `share.NewPubPoly(...).Eval(opID-1).V`, marshaled to bytes per operator. Used by E5 verification.
- [x] **E4. T4 — `Config.QV()` / `Config.QEnc()`** ([types.go](../protocol/v2/tbft/types.go)). `tryReconstructLayer` σ-quorum check uses `cfg.QV()` (2f+1); `tryDeriveNextLayerKey` NR-quorum check uses `cfg.QEnc()`. Existing `Config.Quorum()` retained as a backward-compat alias for `QV()`. **Originally landed with `QEnc() = f+1` per the threshold-separation design; the post-audit unified-threshold refactor (T4 in [TASKS.md](TASKS.md)) sets `QEnc() = 2f+1` to match `QV()` cryptographically.** The DKG (B3, E1) is updated alongside via `ibeThresholdForCommitteeSize` returning `2f+1`.
- [x] **E5. Per-NR-partial verification at observe time** ([instance.go](../protocol/v2/tbft/instance.go)):
    - `Instance.ibePubKeyShares map[OperatorID][]byte` field + `SetIBEPubKeyShares` setter (post-construction).
    - `ObserveNonReceipt` verifies the partial sig against `ibePubKeyShares[op]` via `tagSigner.VerifyPartial(pub, NoQuorumTag(...), partial)` BEFORE storing — catches byzantine garbage NRs at observe time rather than letting them silently corrupt the aggregate. nil-safe: when unset, falls back to existing aggregate-time-only behavior.
    - Plumbed via `tbftadapter.ControllerOptions.IBEPubKeyShares` → `Controller.ibePubKeyShares` → `inst.SetIBEPubKeyShares` in `StartNewInstance`.
    - Tests: accepts valid partial, rejects forged partial, skips verification when shares unset.

### Phase F — Reconfig

- [x] **F1. Stale IBE-share cleanup.** Each SSV `clusterID` (= committee hash) corresponds to a distinct IBE share record; a committee change in SSV is naturally surfaced as a new validator with a new clusterID, not an in-place mutation, so the existing E2 path handles "new clusterID → new DKG" automatically. The orphaned-share concern is on the *removal* side: when the last validator on a committee is removed, we delete the cluster's IBE share so a future cluster with the same committee composition starts fresh. Implementation:
    - `*DKGOrchestrator.RemoveClusterIBE(clusterID)` — thin wrapper around `IBEShareWriter.RemoveIBEShare`.
    - `*Controller.onShareStop` removes the IBE share when `len(vc.Shares) == 0` for the share's committee — the existing committee-cleanup branch.
- [x] **F2. Re-DKG path.** Naturally falls out of F1 + the orchestrator's idempotency check: after a `RemoveClusterIBE`, a subsequent `EnsureClusterIBE(clusterID, ...)` re-runs DKG (no persisted share found). The `generation` parameter is available for monotonic bumping when an explicit re-DKG of the same clusterID is wanted (e.g. compromise recovery — tracked under FW6).
- [x] **F3. Tests** — `TestOrchestrator_RemoveClusterIBE` at [operator/validator/dkg_orchestrator_test.go](../operator/validator/dkg_orchestrator_test.go): a 4-of-4 cluster runs DKG, every store has a share; `RemoveClusterIBE` clears every store; idempotent on a second call; a follow-up `EnsureClusterIBE` (with `generation = 1`) re-runs DKG and re-populates the stores.

### Phase G — End-to-end devnet validation

- [ ] **G1.** Adapt or fork [end_to_end_real_ibe_test.go](../protocol/v2/tbft/blsbackend/end_to_end_real_ibe_test.go) — a setup where the IBE master scalar is *distinct* from the validator master scalar (i.e. truly Option B, not the DST-trick-on-validator-share that today's test exercises). Same DST machinery applies, just keyed differently. Critical assertions (under the unified threshold `qEnc = qV = 2f+1`):
    - With exactly `2f+1 = 5` (n=7) IBE-share partials on a no-quorum tag, the layer-1 ciphertext decrypts.
    - With `2f = 4` IBE-share partials, decryption fails — proving the threshold is genuinely `2f+1`.
    - With `2f+1 = 5` *validator-share* partials on the same tag, decryption fails — proving the IBE keypair is genuinely distinct from the validator keypair (the original "threshold separation is real" assertion is now repurposed as "keypair distinctness is real": the IBE keypair has its own DKG-derived polynomial, not the V-keypair re-tagged).
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
