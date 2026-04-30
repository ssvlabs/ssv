# TBFT for Proposer Duty — Implementation Plan & Status

This document tracks the plan to replace QBFT with TBFT in SSV's proposer-duty execution path, and the progress of executing it.

**Scope:** Proposer duty only. Attestation/sync-committee duties continue to use QBFT. (See [TBFT.md](TBFT.md) and [TBFT-comparison.md](TBFT-comparison.md) for the protocol design and comparison; the analysis there motivated this scope choice — TBFT's value is concentrated on proposer duty's tight 4 s relay cutoff, not on attestation's looser ~12 s window.)

**Status tracking conventions:**

- `[ ]` — Not started
- `[~]` — In progress
- `[x]` — Complete
- `[?]` — Blocked / decision needed

## Big-picture

The proposer-duty path in [protocol/v2/ssv/runner/proposer.go](protocol/v2/ssv/runner/proposer.go) currently runs:

```
RANDAO partial-sig collection → block fetch → QBFT consensus on blinded block
  → post-consensus partial-sig collection → reconstruct → submit
```

Target after this work:

```
RANDAO partial-sig collection → top-K leaders fetch in parallel → onion broadcast (Phase 2)
  → local decryption + reconstruction (Phase 3) → submit
```

The replacement removes QBFT's 3-phase ceremony and the separate post-consensus phase, replacing both with TBFT's 1-RTT onion broadcast that carries partial validator-signatures inside it.

## Unified TBFT / TBFT2 design

Per the design exploration, a single implementation can serve both protocols by parameterization. TBFT2 is just TBFT with `K=2` plus per-layer fetch timing.

```go
type LayerSpec struct {
    Leader  OperatorID
    FetchAt time.Duration  // offset from slot start; TBFT: uniform late, TBFT2: layer 0 late, layer 1 early
}

type Config struct {
    Layers       []LayerSpec  // K = len(Layers), in priority order
    Deadline     time.Duration  // offset from slot start, e.g. 3s
    OnionTagFunc func(layer int, slot phase0.Slot) string
}
```

Cluster-size-aware factory picks the right config:

| Cluster `n` | `f` | Configuration spawned | Layer count | Notes |
|---|---|---|---|---|
| 4 | 1 | TBFT2 | 2 | Layer 0 (primary) at slot+1s; Layer 1 (backup) at slot−4s |
| 7 | 2 | TBFT (K=3) | 3 | All layers at slot+1s |
| 10 | 3 | TBFT (K=4) | 4 | All layers at slot+1s |
| 13 | 4 | TBFT (K=5) | 5 | All layers at slot+1s |

The protocol implementation is the same; the cluster's bootstrap layer chooses the config based on `n`. This is a real implementation simplification — one codebase, two named configurations. See Phase 4 for where this factory lives.

## Phases

Phases build on each other. Within a phase, tasks can usually be parallelized.

---

### Phase 0 — Design alignment & key decisions

**Goal:** Resolve the design questions that materially affect downstream code before we write any of it.

**Tasks:**

- [x] **Pick threshold IBE library.** Default candidate: `github.com/drand/tlock`. Verify Go version compatibility, audited status, custom-tag support (we need tags like `("slot", N, "layer", k, "no-quorum")`, not just round numbers). If not directly usable, fall back to extracting `tlock`'s primitives or implementing on top of `kyber`. **Decided:** start with `drand/tlock` and the kyber primitives it exposes; verify in Phase 1 they support arbitrary BLS pubkey trust anchors (needed for Option A).
- [x] **Decide on threshold IBE keypair management. → Option A.** Reuse the validator's existing threshold BLS key as the IBE trust anchor. No separate DKG. Math: each operator's existing share signs an IBE tag, 2f+1 partial sigs aggregate to a full BLS signature on the tag, which serves as the decryption key. Verification of compositional correctness happens in Phase 1.
- [x] **Decide rollout model. → Per-cluster opt-in via registry.** Operator-side deployment will hard-prevent mixed clusters; not the protocol implementation's problem.
- [x] **Pin protocol parameters.**
  - `K` per cluster size: `max(3, f+1)` per [TBFT.md](TBFT.md).
  - `T_d` (deadline): default `slot_start + 3s`, configurable per cluster.
  - `Δ_1` (block-fetch window): default `1s`.
  - `Δ_2` (onion-gossip window): default `500ms`.
  - For TBFT2 (n=4): `T_b` (early backup fetch) at `slot_start − 4s`.
- [x] **Leader semantics for TBFT2's `L_p` / `L_b`.** Distinct operators chosen via per-slot rotation, deterministic from `(slot, cluster_operators)`. Specifically: priority order for TBFT2 is the same priority order as TBFT-K=2. `L_p` = priority[0], `L_b` = priority[1].
- [x] **Backwards compatibility / mixed-cluster prevention.** Out of scope for the protocol implementation. Operator deployment ensures a cluster runs one protocol or the other, not both.
- [x] **ssv-spec coupling.** The TBFT protocol implementation is **independent of `github.com/ssvlabs/ssv-spec` in every way**. We define our own message types, our own value-checker hooks, our own controller interface. Spec-tests are not part of this work.
- [x] **Inconsistency-slashing.** Detect and log only; no punishment hook (SSV doesn't have an operator-slashing mechanism today).

**Exit criteria:** All design decisions resolved. ✅

---

### Phase 1 — Cryptographic foundations

**Goal:** Have a working threshold IBE primitive (or stub thereof), wrapped in an SSV-internal interface that downstream protocol code can use without knowing the underlying library.

**Tasks:**

- [x] **Create package structure.** [protocol/v2/tbft/](protocol/v2/tbft/) — parallel to `protocol/v2/qbft/`. Independent of `ssvlabs/ssv-spec`; no spec types imported.
- [x] **Define core protocol types.** [protocol/v2/tbft/types.go](protocol/v2/tbft/types.go) — `Config`, `LayerSpec`, `Onion`, `EncryptedLayer`, `NonReceiptAttestation`, `Output`, plus generic `OperatorID` / `Height` / `Value` / `Signature` types decoupled from Ethereum/spec.
- [x] **Tag construction.** [protocol/v2/tbft/tag.go](protocol/v2/tbft/tag.go) — domain-separated SHA-256 over `(prefix, clusterID, height, layer)`. Tested for determinism + cross-context distinctness in [tbft_test.go](protocol/v2/tbft/tbft_test.go).
- [x] **`ThresholdIBE` interface.** [protocol/v2/tbft/ibe.go](protocol/v2/tbft/ibe.go) — generic interface with `Encrypt` / `PartialDecryptKey` / `AggregateDecryptKey` / `Decrypt`. Hides library choice behind a small surface.
- [x] **Stub IBE for protocol-level tests.** [protocol/v2/tbft/ibe_stub.go](protocol/v2/tbft/ibe_stub.go) — non-cryptographic placeholder that satisfies the interface deterministically. Critically, verifies the property that any 2f+1 subset of partials yields the *same* aggregate key (else different operators would derive different decryption keys for the same ciphertext).
- [x] **Foundation unit tests.** [protocol/v2/tbft/tbft_test.go](protocol/v2/tbft/tbft_test.go) — config validation, tag construction, stub-IBE round-trip, below-quorum-rejection, tag-mismatch-rejection, distinct-quorum-subsets-yield-same-key. All green.
- [x] **Add kyber + drand IBE deps to `go.mod`.** Done — `github.com/drand/kyber` + `github.com/drand/kyber-bls12381` pulled in for the `TLockIBE` impl. (Originally written as "drand/tlock" in this checklist; we ended up using kyber's IBE primitives directly via the DST-trick approach, see docs/IBE-INTEGRATION.md.)
- [x] **Verify Option A composes cryptographically.** Done — see [end_to_end_real_ibe_test.go](../protocol/v2/tbft/blsbackend/end_to_end_real_ibe_test.go). 7-operator cluster with kyber-aggregated herumi-format shares decrypts a `TLockIBE` ciphertext keyed to the validator's pubkey. The reconstructed Eth2-format signature byte-equals what the master key would sign directly. The DST-trick (same scalar, different DSTs) makes this work without a separate IBE DKG.

**Exit criteria:** Stub-based tests for the protocol primitives are green. Real-IBE integration is the remaining work, blocked only on Phase 2 progress (so it can be done in parallel without holding up onion construction / decryption walk).

**Risk reduced:** by stubbing the IBE we've decoupled the cryptography integration from the protocol-logic implementation. If real-IBE integration hits a wall, the stub-based protocol code still works for testing and the only missing piece is the swap-in.

---

### Phase 2 — Protocol core (onion + decryption walk)

**Goal:** Pure-protocol code that builds an onion, broadcasts it, decrypts walking layers, and reconstructs an output. No SSV runner integration yet — this is testable in isolation.

**Tasks:**

- [x] **`Signer` interface + stub.** [signer.go](protocol/v2/tbft/signer.go) — BLS-style threshold signing primitive: `SignPartial`, `AggregatePartials`, `VerifyPartial`, `VerifyAggregate`. Stub provides deterministic, non-cryptographic implementations satisfying the critical "any 2f+1 partials → same aggregate" property.
- [x] **Onion construction.** [onion.go::BuildOnion](protocol/v2/tbft/onion.go) — given K candidates, share, signer, IBE: emits a K-layer Onion with encrypted-where-needed partial signatures.
- [x] **Non-receipt attestation construction.** [onion.go::BuildNonReceipt](protocol/v2/tbft/onion.go) — partial sig on `NoQuorumTag(layer)` for use unlocking layer+1.
- [x] **`Instance` state machine.** [instance.go](protocol/v2/tbft/instance.go) — accumulates `ObserveCandidate`/`ObserveOnion`/`ObserveNonReceipt` calls during Phases 1–2.
- [x] **Decryption walk.** [instance.go::Resolve](protocol/v2/tbft/instance.go) — for each layer in priority order: try positive quorum, else try non-receipt quorum to unlock the next layer, else halt. Returns `Output` or `ErrNoQuorum`.
- [x] **Equivocation handling.** Per-layer partials are grouped by `EncryptedLayer.Value`; each group is independently quorum-checked. Multiple groups indicate equivocation (no group reaches quorum unless honest majority converges).
- [x] **Inconsistency-fault detection.** Operators emitting both a positive contribution AND a non-receipt for the same layer are recorded via `Instance.InconsistencyFaults()`. Detection only — no punishment.
- [x] **Protocol-level tests.** [instance_test.go](protocol/v2/tbft/instance_test.go) — cluster simulator covering: healthy (n=4 + n=7), top-leader silent, layer-0 equivocation, f-operators-offline-within-bound, beyond-byzantine-bound, inconsistency-fault detection. 23 total tests across the package; all green; `go vet` and `gofmt` clean.

**Design note discovered during Phase 2:** TBFT requires honest operators to detect leader equivocation during Phase 1 (before onion construction) and treat the equivocating layer as non-receipt. Without this, operators with conflicting views all emit positive sigs for their respective values, no group reaches quorum, and no non-receipt quorum can form (operators don't emit non-receipts when they "saw" something). The protocol's behavior is correct under the rule "if you see multiple distinct candidates at a layer during Phase 1, treat that layer as non-receipt"; the tests model this rule directly. This should be documented in [TBFT.md](TBFT.md) as part of the operational protocol description (TODO: do that update).

**Exit criteria:** ✅ All target test scenarios green at n=4 and n=7 cluster sizes.

---

### Phase 3 — Validation & p2p plumbing

**Goal:** TBFT messages flow through SSV's p2p layer with structural validation in place. The TBFT core exposes pure validation functions; the SSV adapter (Phase 4) handles wire format, sender authentication, routing, and rate limits.

**Split of concerns:** Per the user's "independent of ssv-spec" constraint, the TBFT core does not define wire envelopes, signed-message types, or rate-limit policy — those belong to the SSV adapter that imports the TBFT core. The TBFT core defines only the protocol-level structural validation that the adapter calls before passing messages to the Instance.

**Tasks:**

- [x] **Standalone validation functions.** [validation.go](protocol/v2/tbft/validation.go) — `ValidateOnion(o, cfg)` and `ValidateNonReceipt(nr, cfg)`. Used by Instance.Observe* and exposable to the SSV adapter for pre-queue filtering.
- [x] **Onion structural rules:** non-nil; height matches instance; len(Layers) == K; sender is cluster member; each layer either fully empty (no contribution) or fully populated (Value + Ciphertext both present); each non-empty layer's Tag matches `LayerTag(k)`.
- [x] **Non-receipt structural rules:** non-nil; height matches; layer in [0, K-1); sender is cluster member; non-empty PartialSig.
- [x] **Refactor Observe methods to call validators.** `Instance.ObserveOnion` and `ObserveNonReceipt` now delegate to `ValidateOnion` / `ValidateNonReceipt` for the structural checks; the duplicated inline validation is gone.
- [x] **Validation tests.** [validation_test.go](protocol/v2/tbft/validation_test.go) — 14 tests covering OK paths and each rejection branch for both message types.

**Deferred to Phase 4 (SSV-adapter concerns):**

- Wire envelope (SSZ encoding, signed by operator identity key) — lives in the SSV-side adapter package, not in the TBFT core.
- Sender authentication via libp2p / operator key — adapter responsibility.
- Routing to the right Instance based on (cluster, height) — adapter responsibility.
- Rate limits / replay (operator can emit at most 1 onion + ≤ K-1 non-receipts per instance) — adapter responsibility, with the TBFT Instance silently de-duplicating as a defense-in-depth.

**Exit criteria:** ✅ Structural validation functions exposed and tested. SSV adapter (Phase 4) can call these as soon as it parses an incoming wire message.

---

### Phase 4 — Runner integration

**Goal:** Wire the TBFT core into the proposer-duty runner, replacing the QBFT path. This is a multi-turn phase; broken into sub-phases below.

#### Phase 4a — herumi/bls-backed Signer (Option A verification) ✅

- [x] **New subpackage** [protocol/v2/tbft/blsbackend/](protocol/v2/tbft/blsbackend/) holding concrete TBFT primitive implementations backed by `herumi/bls-eth-go-binary` (the BLS library SSV's existing share infrastructure already uses).
- [x] **`BLSSigner`** ([signer.go](protocol/v2/tbft/blsbackend/signer.go)) — full implementation of `tbft.Signer`. SignPartial uses `bls.SecretKey.SignByte`; AggregatePartials uses `bls.Sign.Recover` (Lagrange interpolation, identical to what SSV's existing `utils/threshold.ReconstructSignatures` does); Verify* uses `bls.Sign.VerifyByte`.
- [x] **Option-A property verified** ([signer_test.go](protocol/v2/tbft/blsbackend/signer_test.go)): `TestSigner_AnyQuorumSubsetYieldsSameAggregate` exercises three different 5-element subsets of 7 operator partials and asserts they produce byte-identical aggregates. This is THE critical property TBFT relies on; without it, different operators with different received-partial subsets would derive different "decision signatures" and the cluster would fork. Verified empirically with real BLS keys.
- [x] **End-to-end healthy-case integration test** ([integration_test.go](protocol/v2/tbft/blsbackend/integration_test.go)) — `TestProtocol_Healthy_n7_BLSBackend` drives the full TBFT pipeline (NewInstance → ObserveCandidate → BuildOwnOnion → ObserveOnion × n → Resolve) with real BLS keys and the `BLSSigner`. All operators produce identical reconstructed signatures; the reconstructed signature verifies under the master pubkey AND equals what the master would have signed directly.

**Status:** 6 unit tests + 1 integration test green; `go vet` and `gofmt` clean. **Option A is empirically validated for the BLS layer.**

**Known limitation:** `BLSSigner` + `StubIBE` work end-to-end ONLY for the layer-0-healthy case (no IBE decryption needed). Layer-fallthrough tests with real BLS require either real IBE integration (Phase 4b) or a refactor of `StubIBE` to accept BLS aggregates as decryption keys.

#### Phase 4b — IBE primitive (intermediate: SignerGatedIBE; production: drand/tlock)

**Pragmatic intermediate: `SignerGatedIBE` (done).** Provides the *access-gate* semantics of an IBE (decryption requires a valid BLS aggregate signature on the ciphertext's tag) using `BLSSigner.VerifyAggregate` for verification. Does **NOT** provide cryptographic confidentiality — plaintext is embedded directly in the ciphertext. Sufficient for end-to-end protocol testing with real BLS keys; insufficient for production deployment where confidentiality matters.

- [x] **`SignerGatedIBE`** ([signer_gated_ibe.go](protocol/v2/tbft/blsbackend/signer_gated_ibe.go)) — Signer-verified access gate.
- [x] **Tests** ([signer_gated_ibe_test.go](protocol/v2/tbft/blsbackend/signer_gated_ibe_test.go)) — 5 tests: round-trip with real BLS, rejection of wrong-tag keys, rejection of below-quorum keys, malformed-ciphertext handling, missing-Verifier handling.
- [x] **End-to-end layer-fallthrough integration test** ([integration_test.go::TestProtocol_TopLeaderSilent_n7_BLSBackend](protocol/v2/tbft/blsbackend/integration_test.go)) — top-leader silent, all operators emit non-receipts, real BLS aggregation produces a valid signature on the no-quorum tag, SignerGatedIBE accepts it as the layer-1 decryption key, layer 1 reaches positive quorum, all operators converge on the same reconstructed signature on layer 1's value.

**Production track: real cryptographic IBE — DONE.** Implemented via the "DST trick" (see [docs/IBE-INTEGRATION.md](IBE-INTEGRATION.md)): existing herumi-format BLS shares are interpreted as kyber-BLS scalars and used to sign IBE tags under drand's DST. Different DSTs make the cross-DST signatures cryptographically independent (the standard DST-design property), so the validator's secret can serve both Eth2 output-signing AND IBE-tag-signing without a separate DKG.

- [x] **Added `kyber` + `drand/kyber-bls12381` deps to `go.mod`.**
- [x] **`HerumiShareToKyberScalar` / `HerumiPubkeyToKyberG1Point`** ([kyber_conversion.go](protocol/v2/tbft/blsbackend/kyber_conversion.go)) — direct byte pass-through (length-checked); both libraries follow the IETF/Eth2 standardised BLS12-381 encoding. Verified by [kyber_conversion_test.go](protocol/v2/tbft/blsbackend/kyber_conversion_test.go) — scalars and pubkeys round-trip with byte-equality, and Lagrange interpolation across libraries recovers the master.
- [x] **`KyberSigner`** ([kyber_signer.go](protocol/v2/tbft/blsbackend/kyber_signer.go)) — implements `tbft.Signer` using kyber-bls12381. Inputs are herumi-format bytes; outputs are kyber-format G2 sigs. Lagrange interpolation in kyber's scalar field. [signer_test.go](protocol/v2/tbft/blsbackend/kyber_signer_test.go) verifies round-trip, "any 2f+1 subset → same aggregate", per-partial verification.
- [x] **`TLockIBE`** ([tlock_ibe.go](protocol/v2/tbft/blsbackend/tlock_ibe.go)) — implements `tbft.ThresholdIBE` via `github.com/drand/kyber/encrypt/ibe` with hybrid AES-GCM. Decryption key is a kyber-format G2 BLS sig. Tested in [tlock_ibe_test.go](protocol/v2/tbft/blsbackend/tlock_ibe_test.go).
- [x] **`Instance.tagSigner` field** + new `NewInstanceWithTagSigner` constructor ([instance.go](protocol/v2/tbft/instance.go)). `BuildOwnNonReceipts` and `tryDeriveNextLayerKey` use `tagSigner` for IBE-tag operations; the original `signer` continues to handle value-signing. Backward-compatible (defaults to `signer` if `tagSigner` is nil).
- [x] **`Controller.TagSigner` option** ([controller.go](protocol/v2/ssv/runner/tbft/controller.go)) — exposed in `ControllerOptions`, threaded through to `NewInstanceWithTagSigner`. The runner constructs Controllers with `Signer: BLSSigner` + `TagSigner: KyberSigner` + `IBE: TLockIBE` for production-grade IBE.
- [x] **End-to-end capstone test** ([end_to_end_real_ibe_test.go](protocol/v2/tbft/blsbackend/end_to_end_real_ibe_test.go)) — 7-operator cluster, real threshold BLS keys, BLSSigner+KyberSigner+TLockIBE, top-leader-silent fallthrough scenario. All operators converge on the same Eth2-format reconstructed validator signature on layer 1's value (byte-equal to what the master would sign directly). Test passes.

**Status:** Phase 4b production-IBE track is substantively done. SSV deployment with real cryptographic IBE is now a runner-side wiring task (Phase 4d), no cryptographic open questions remain.

#### Phase 4c — SSV adapter (in progress)

**Wire encoding** (TBFT-side, ssv-spec-independent):
- [x] Binary length-prefixed encoding for `Onion` and `NonReceiptAttestation` ([protocol/v2/tbft/wire/wire.go](protocol/v2/tbft/wire/wire.go)). Versioned headers, defensive bounds (MaxLayers=32, MaxFieldSize=16MiB).
- [x] Round-trip tests + truncation/version-rejection tests ([protocol/v2/tbft/wire/wire_test.go](protocol/v2/tbft/wire/wire_test.go)).

**Config factory** (SSV-side, depends on ssv-spec):
- [x] [protocol/v2/ssv/runner/tbft/config.go](protocol/v2/ssv/runner/tbft/config.go) — `ConfigForCluster(slot, committee, clusterID, overrides) → *tbft.Config` with cluster-size-aware TBFT vs TBFT2 selection: n=4 → K=2 with split fetch times (TBFT2); n≥7 → K=max(3, f+1) with uniform late fetch (TBFT). Leader rotation matches SSV's existing `RoundRobinProposer` convention.
- [x] Tests for all SSV cluster sizes (4, 7, 10, 13), leader-rotation correctness, validation rejection of non-3f+1 sizes, override propagation, deterministic sorting of unsorted committees ([config_test.go](protocol/v2/ssv/runner/tbft/config_test.go)).

**Done:**
- [x] **Controller** ([controller.go](protocol/v2/ssv/runner/tbft/controller.go)) — state-machine wrapper around `tbft.Instance` exposing the runner-friendly API: `NewController`, `StartNewInstance`, `EndInstance`, `ObserveCandidate`, `ProcessOnion`, `ProcessNonReceipt`, `BuildOwnOnion`, `BuildOwnNonReceipts`, `Resolve`, `InconsistencyFaults`, `ActiveSlots`. Concurrency-safe (mutex around per-slot instance map). Computes `LeaderAtLayers` per slot so the runner can schedule Phase-1 fetches.
- [x] **Tests** ([controller_test.go](protocol/v2/ssv/runner/tbft/controller_test.go)) — API-surface tests (constructor validation, lifecycle, routing-by-Height, leader-layer detection, multiple concurrent instances) plus a multi-controller end-to-end smoke test (`TestController_MultiCluster_HealthyEndToEnd`) that spins up 7 Controllers, gossips messages between them, and verifies all converge on the same reconstructed signature using real BLS + SignerGatedIBE.

**Done (continued):**
- [x] **Wire envelope** ([envelope.go](protocol/v2/tbft/wire/envelope.go)) — versioned, kind-discriminated wrapper that lets a single byte stream carry an Onion, a NonReceiptAttestation, or a CandidateBroadcast. Convenience helpers `WrapOnion`, `WrapNonReceipt`, `WrapCandidate`, `Unwrap` (returns typed `Envelope` with the parsed message). The envelope deliberately carries no signature — the SSV adapter places the bytes in `SignedSSVMessage.Data` and SSV's existing operator-key signing covers authentication.
- [x] **Envelope tests** ([envelope_test.go](protocol/v2/tbft/wire/envelope_test.go)) — round-trip for all three kinds, truncation, bad version, unknown kind, body-decode errors propagated cleanly.
- [x] **CandidateBroadcast** ([types.go](protocol/v2/tbft/types.go)) — Phase-1 message type for layer leaders to distribute their fetched candidate values to peers. Without this, non-leader operators would have no candidate to sign and the layer would silently fail at Phase 2.
- [x] **CandidateBroadcast wire encoding** ([wire.go](protocol/v2/tbft/wire/wire.go)) — `EncodeCandidate` / `DecodeCandidate` with the same versioned, length-prefixed format as the others.
- [x] **`Controller.ProcessCandidate`** ([controller.go](protocol/v2/ssv/runner/tbft/controller.go)) — routes incoming `*CandidateBroadcast` to the right Instance by Height; calls `Instance.ObserveCandidate(cb.Layer, cb.Value)` under the hood. Same routing pattern as ProcessOnion / ProcessNonReceipt.

**Done:**
- [x] **Per-slot lifecycle scheduler** ([scheduler.go](protocol/v2/ssv/runner/tbft/scheduler.go)) — `Scheduler` type combining `Controller` + `LifecycleHooks` to expose Phase-1/2/3 mechanics as procedural methods: `FetchAndBroadcastCandidate`, `BuildAndBroadcastOnion`, `ResolveAndSubmit`. Scheduler does NOT manage time itself — runner calls these at the right slot offsets via SSV's existing slot-ticker infrastructure. This avoids fake-clock test machinery and keeps the boundaries clean (Scheduler knows protocol mechanics, runner knows when).
- [x] **`LifecycleHooks`** — interface for runner-provided callbacks: `FetchCandidate(ctx, slot, layer) ([]byte, error)`, `Broadcast(ctx, slot, data) error`, `SubmitOutput(ctx, slot, output) error`, optional `OnMissedSlot`.
- [x] **`Controller.OperatorID()`** — exposed accessor so the Scheduler can fill in the `OperatorID` field when constructing outbound `CandidateBroadcast` messages.
- [x] **Scheduler tests** ([scheduler_test.go](protocol/v2/ssv/runner/tbft/scheduler_test.go)) — validation errors, Fetch+Broadcast happy path / fetch error / broadcast error, BuildAndBroadcastOnion with real BLS, ResolveAndSubmit no-quorum + missing-instance paths, and an end-to-end below-quorum test verifying ErrNoQuorum + OnMissedSlot fire correctly.

**Done:**
- [x] **Dispatch helpers** ([dispatch.go](protocol/v2/ssv/runner/tbft/dispatch.go)) — `DispatchEnvelope(c, env)` routes a parsed `*wire.Envelope` to the right `Process*` method on the Controller. `DispatchBytes(c, data)` is a convenience wrapper that combines `wire.Unwrap` + dispatch. Errors propagated cleanly for unknown kind, malformed bytes, and underlying Process failures. Tested in [dispatch_test.go](protocol/v2/ssv/runner/tbft/dispatch_test.go).
- [x] **Rate limiter** ([ratelimit.go](protocol/v2/ssv/runner/tbft/ratelimit.go)) — `RateLimiter` type with per-`(slot, operator, kind, layer)` tracking. `AllowOnion`, `AllowNonReceipt`, `AllowCandidate` accept first-time messages and reject duplicates with informative errors. `Forget(slot)` releases per-slot tracking memory when the runner ends an instance. Thread-safe. Tested in [ratelimit_test.go](protocol/v2/ssv/runner/tbft/ratelimit_test.go).

#### Phase 4d — proposer.go modifications

**Reference implementation delivered:** [runner.go::RunProposerSlot](protocol/v2/ssv/runner/tbft/runner.go) ties Controller + Scheduler together with realistic timing (real `time.AfterFunc`, goroutine per layer-leader fetch, deadline-driven Phase-2/3). End-to-end test in [runner_test.go](protocol/v2/ssv/runner/tbft/runner_test.go) drives a 7-operator cluster through one proposer slot — both healthy and top-leader-silent paths — using real BLS keys, SignerGatedIBE, and an in-memory broadcast bus. Race-detector clean.

**Integration sketch delivered:** [proposer_integration_sketch.go](protocol/v2/ssv/runner/tbft/proposer_integration_sketch.go) is a doc-style file with concrete pseudocode showing where to apply each modification in `proposer.go`: hook callbacks (FetchCandidate / Broadcast / SubmitOutput / OnMissedSlot), the network-receive dispatch with rate-limiter wiring, and what to delete (post-consensus phase, QBFT controller field). Sketch only; the SSV team should review against actual `proposer.go` and apply in a focused PR with test coverage.

The integration shape is now fully defined in code. What remains is the runner-side wiring in `protocol/v2/ssv/runner/proposer.go` itself, which is best done by the SSV team because it touches concerns I don't have full context on:

- **Keep RANDAO pre-consensus phase as-is.** Pre-consensus is independent of TBFT.
- **Replace `r.QBFTController.StartNewInstance(...)` with adapter's `Controller.StartNewInstance(slot)`.** Hold the returned `RunningInstance` for the slot's lifecycle.
- **Use `Scheduler.FetchAndBroadcastCandidate` / `BuildAndBroadcastOnion` / `ResolveAndSubmit`** as the per-phase primitives, scheduled via SSV's existing time-based scheduler at `cfg.Layers[k].FetchAt` / `cfg.Deadline` / `cfg.Deadline + gossipWindow`.
- **Block-fetch coordination.** Top-K leaders each fetch in parallel; for TBFT2 (n=4), `L_b` fetches early. The Scheduler's hooks make this straightforward — `FetchCandidate` is the only callback that needs to call out to the beacon node / relay.
- **Remove `ProcessPostConsensus`.** TBFT's Phase 3 produces the signature directly via `SubmitOutput` hook.
- **Doppelganger / slashing-protection integration.** `CanSign()` check ([protocol/v2/ssv/runner/proposer.go:272-275](protocol/v2/ssv/runner/proposer.go:272)) must run before the Scheduler builds the onion. Easiest place: just before calling `BuildAndBroadcastOnion`, if `CanSign()` returns false, skip Phase 2 (the slot will miss).
- **Wire the network-receive path** through `DispatchBytes` + `RateLimiter`. SSV's existing message-validation layer parses the outer `SignedSSVMessage`, then the TBFT-specific dispatch happens after.
- **Inconsistency-slashing detection.** Call `Controller.InconsistencyFaults(slot)` after `Resolve` and log/report.

**Exit criteria for Phase 4 overall:** A 4-operator local cluster (Docker-compose) successfully completes a proposer duty using TBFT end-to-end, in `make docker-debug`. This requires the proposer.go modifications above plus:
- **Phase 4b production track** — replace `SignerGatedIBE` with real `drand/tlock` (or equivalent) for cryptographic confidentiality. The protocol code is unchanged; only the IBE *implementation* is swapped.
- **Wire envelope routing in SSV's network layer** — define a `MsgType` value (SSV-internal, not in ssv-spec) for TBFT messages so they're routed to the right runner.

---

### Phase 5 — In-tree tests & build cleanliness

**Goal:** TBFT has its own dedicated test suite covering the protocol scenarios. No spec-test integration. No ssv-spec coordination.

**Tasks:**

- [ ] **Protocol-scenario tests.** Self-contained tests for the scenarios from [TBFT-comparison.md](TBFT-comparison.md):
  - Healthy: layer 0 reaches quorum
  - Top-leader silent: 2f+1 non-receipt → unlocks layer 1, succeeds there
  - Top-leader byzantine equivocating: rejected, falls through to layer 1
  - `f` byzantine + degraded network: scenario-specific outcomes per the comparison tables
  - TBFT2 specifically: `L_p` byzantine → fallback to `V_b`; both byzantine → miss
- [ ] **Cluster-size matrix.** Run protocol-scenario tests for n ∈ {4, 7, 10, 13}.
- [ ] **`make unit-test` and `make lint` clean** for the new TBFT package and its integration points.
- [ ] **Existing QBFT spec-tests untouched.** They still pass because we don't modify the QBFT codepath; TBFT lives in a separate package.

**Exit criteria:** TBFT package's own test suite green at all four cluster sizes; `make unit-test` and `make lint` clean.

---

### Phase 6 — Devnet validation

**Goal:** TBFT runs in a real-network environment (Holesky or similar) and produces measurable telemetry comparable to QBFT.

**Tasks:**

- [ ] **Deploy TBFT-enabled binary to a devnet cluster.** Ideally a 4-of-13 cluster deployment so we exercise both TBFT2 (n=4) and TBFT-K=5 (n=13).
- [ ] **Telemetry / metrics.** Emit per-slot metrics:
  - Time from slot start to validator-signed-block ready
  - Bandwidth used (onion + non-receipt + RANDAO)
  - Layer reached (0, 1, 2, ... K-1) — distribution shows how often fallback fires
  - Reconstruction success/failure
  - Comparison metric: alongside-QBFT-run results for the same slots if feasible
- [ ] **Soak test.** Run for ~1 week. Look for:
  - Stuck consensus, missed slots
  - Bandwidth spikes
  - Memory leaks in IBE/onion paths
  - Negative-attestation honesty exploits in the wild
- [ ] **Performance comparison vs QBFT baseline.** Quantify the actual gain in latency and the actual cost in bandwidth, per cluster size.

**Exit criteria:** ≥1 week of clean operation on devnet at multiple cluster sizes; measured numbers within design predictions (≤2× of [TBFT-comparison.md](TBFT-comparison.md) projections).

---

### Phase 7 — Mainnet rollout

**Goal:** TBFT available on mainnet, opt-in per cluster, with monitoring.

**Tasks:**

- [ ] **Registry signaling.** New clusters can specify "TBFT" as their consensus protocol at registration. Existing clusters stay on QBFT until they re-register. (Subject to Phase 0 decision.)
- [ ] **Operator config flag.** Optional override allowing operators to advertise TBFT support or refuse it (depending on rollout model).
- [ ] **Documentation.** Update [docs/MEV_CONSIDERATIONS.md](docs/MEV_CONSIDERATIONS.md) and operator-facing docs to explain TBFT, when it kicks in, what changes operationally.
- [ ] **Monitoring & alerting.** Production dashboards showing TBFT-vs-QBFT proposer-duty success rates, latency, bandwidth.
- [ ] **Rollback plan.** If TBFT misbehaves in production, mechanism to disable per-cluster or globally.

**Exit criteria:** TBFT available on mainnet; ≥10 clusters opted in; success rate ≥ QBFT baseline; rollback mechanism tested.

---

## Open design questions

Most Phase 0 questions resolved. Remaining:

- [?] **Retry / re-attempt within the slot.** TBFT is single-shot by design. Should we allow a "second attempt" if Phase 2 deadline passes without output? This compromises the protocol's properties but might be a reasonable practical concession. Default: no, per design; revisit if telemetry shows it would help.
- [?] **What happens to the existing QBFT for proposer code?** Keep both for transition period, or replace outright? Default: keep both behind a feature flag, deprecate QBFT-for-proposer after ≥2 mainnet stable releases.
- [?] **Verify Option A (reuse validator key for IBE) composes cryptographically.** Phase 1 task: verify that BLS partial sigs on a tag — produced by the validator's existing operator shares — can serve as a tlock-style threshold IBE decryption key. If not, fall back to Option B (separate DKG).

### Deferred follow-ups

- [ ] **EKM "share-bytes" Option B (production-grade).** The current Option A path exposes share bytes from `LocalKeyManager` to construct `BLSSigner` / `KyberSigner`. This works for local-mode operators (the bytes are pulled from the loaded wallet account on demand, see `LocalKeyManager.GetShareBytes`) but does NOT work for remote-signing operators (ssv-signer + Web3Signer never put share bytes on the SSV node). Production-grade fix: add new endpoints to ssv-signer / Web3Signer that produce signatures under both Eth2 DST (already there) and drand DST (new), and refactor `tbft.Signer` to call into ekm rather than holding share bytes locally. Multi-repo scope. Defer until devnet validation establishes TBFT is mainnet-shaped.
- [ ] **Allocate stable `MsgType` byte for TBFT envelopes.** Current placeholder `0xF0` (= 240). Coordinate with SSV-team for a permanent ecosystem-wide allocation before mainnet. Older SSV nodes seeing this type will *reject* the message via `ErrUnknownSSVMessageType` (`reject: true`) — a libp2p-pubsub `ValidationReject` outcome that drops the message and decrements the sender's peer score. Mixed-cluster rollouts (some operators on TBFT-binary, some on QBFT-only binary) will therefore degrade gossip; the rollout model must hard-prevent this (covered in Phase 7 / registry signaling). For a coordinated "everyone upgrades together" rollout this isn't an issue.

## Risks register

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| `tlock` doesn't compose with SSV's BLS setup | Medium | High (blocks Phase 1) | Time-box Phase 0 IBE decision to 1 week; have fallback (separate DKG) ready |
| Onion bandwidth at n=13 is worse than projections | Low-Medium | Medium | Phase 6 measures real bandwidth; if exceeds budget, reduce K to 4 for n=13 |
| Negative-attestation grief in production | Low | Medium | Inconsistency-slashing detection logged; deadline tuning per [TBFT.md](TBFT.md) caveat 1; revisit if observed |
| Mixed cluster (some QBFT some TBFT) accidentally happens | High if rollout is wrong | Catastrophic (slot misses, possible safety) | Rollout model decided in Phase 0 must hard-prevent mixed clusters; registry signaling |
| Spec drift between SSV impl and ssv-spec | Medium | Medium | Coordinate spec changes early; spec tests catch divergence |
| `T_d = 3s` is too aggressive (squeezes block fetch) | Medium | Medium | Configurable per cluster; default conservative; tune from production data |

## Status summary

**Current phase:** Phase 4 in progress (4a done; 4b–4d remaining).
**Phase 0:** Done. Decisions resolved by user.
**Phase 1:** Substantively done (stub track + Option-A BLS Signer).
**Phase 2:** Done (stub track). Protocol core working end-to-end across n=4 and n=7.
**Phase 3:** Done. Structural validation functions exposed and tested; observe-side wired to use them.
**Phase 4a:** Done. Real BLS Signer implemented; Option-A's "any 2f+1 subset → same aggregate" property empirically validated; healthy-case end-to-end test green with real BLS.
**Phase 4b (intermediate):** Done. `SignerGatedIBE` provides the IBE access-gate semantics using real BLS verification, enabling layer-fallthrough end-to-end tests with real BLS keys. Cryptographic IBE (drand/tlock) deferred to when the integration is actually needed; the protocol code is complete and only the IBE *implementation* needs swapping.
**Phase 4c (DONE):** Wire encoding, envelope, CandidateBroadcast, Config factory, Controller, Scheduler, dispatch helpers, and rate limiter all complete. Multi-controller end-to-end smoke test green with real BLS. The SSV adapter package exposes everything the runner needs — what's left is wiring it into `proposer.go` (Phase 4d).
**Phase 4d (reference implementation delivered):** [RunProposerSlot](protocol/v2/ssv/runner/tbft/runner.go) function ties everything together with real timing. End-to-end test runs a 7-operator cluster through a full proposer-duty slot (healthy + top-leader-silent variants) with real BLS keys. Race-detector clean. [proposer_integration_sketch.go](protocol/v2/ssv/runner/tbft/proposer_integration_sketch.go) shows the call-site changes for `proposer.go`. Actual `proposer.go` modifications need SSV-team context.

**Phase 4b production-IBE track (DONE):** Real cryptographic IBE working end-to-end via the "DST trick." Existing herumi-format validator shares used unchanged; same secrets sign Eth2 outputs (under Eth2 DST) and IBE tags (under drand DST); kyber-aggregated sigs decrypt `drand/kyber-bls12381`-encrypted ciphertexts. Capstone test exercises a 7-operator cluster through layer-fallthrough with real BLS + real IBE; reconstructed validator signature is Eth2-compatible. No DKG changes. See [docs/IBE-INTEGRATION.md](IBE-INTEGRATION.md).

**Code so far:**

Core protocol package [protocol/v2/tbft/](protocol/v2/tbft/) — independent of ssv-spec:
- [types.go](protocol/v2/tbft/types.go) — `Config`, `LayerSpec`, `Onion`, `EncryptedLayer`, `NonReceiptAttestation`, `Output`, generic types
- [tag.go](protocol/v2/tbft/tag.go) — `NoQuorumTag`, `LayerTag`
- [ibe.go](protocol/v2/tbft/ibe.go) — minimal `ThresholdIBE` interface
- [ibe_stub.go](protocol/v2/tbft/ibe_stub.go) — stub IBE
- [signer.go](protocol/v2/tbft/signer.go) — `Signer` interface + `StubSigner`
- [onion.go](protocol/v2/tbft/onion.go) — `BuildOnion`, `BuildNonReceipt`
- [instance.go](protocol/v2/tbft/instance.go) — `Instance` state machine, `BuildOwnOnion` / `BuildOwnNonReceipts`, `Resolve`
- [validation.go](protocol/v2/tbft/validation.go) — `ValidateOnion`, `ValidateNonReceipt`
- 3 test files covering foundations, protocol scenarios, message validation

Concrete crypto-backend subpackage [protocol/v2/tbft/blsbackend/](protocol/v2/tbft/blsbackend/):

*herumi-BLS side (value-signing):*
- [signer.go](protocol/v2/tbft/blsbackend/signer.go) — `BLSSigner` (herumi/bls-eth-go-binary backed)
- [signer_test.go](protocol/v2/tbft/blsbackend/signer_test.go) — unit tests including "any 2f+1 subset yields same aggregate"

*Stub IBE (for tests / non-confidential deployments):*
- [signer_gated_ibe.go](protocol/v2/tbft/blsbackend/signer_gated_ibe.go) — `SignerGatedIBE` (Signer-verified access gate; non-cryptographic)
- [signer_gated_ibe_test.go](protocol/v2/tbft/blsbackend/signer_gated_ibe_test.go) — round-trip + rejection tests

*kyber-BLS side (tag-signing for real IBE — DST-trick approach):*
- [kyber_conversion.go](protocol/v2/tbft/blsbackend/kyber_conversion.go) — `HerumiShareToKyberScalar`, `HerumiPubkeyToKyberG1Point`
- [kyber_conversion_test.go](protocol/v2/tbft/blsbackend/kyber_conversion_test.go) — byte-equality round-trips, Lagrange recovery across libraries
- [kyber_signer.go](protocol/v2/tbft/blsbackend/kyber_signer.go) — `KyberSigner` (drand/kyber-bls12381 backed; consumes herumi-format share bytes)
- [kyber_signer_test.go](protocol/v2/tbft/blsbackend/kyber_signer_test.go) — same contract tests as `BLSSigner` but kyber-format outputs

*Real cryptographic IBE:*
- [tlock_ibe.go](protocol/v2/tbft/blsbackend/tlock_ibe.go) — `TLockIBE` (kyber IBE primitives + AES-GCM hybrid)
- [tlock_ibe_test.go](protocol/v2/tbft/blsbackend/tlock_ibe_test.go) — round-trip, wrong-tag rejection, below-quorum rejection

*Integration capstones:*
- [integration_test.go](protocol/v2/tbft/blsbackend/integration_test.go) — full TBFT pipeline with real BLS + SignerGatedIBE
- [end_to_end_real_ibe_test.go](protocol/v2/tbft/blsbackend/end_to_end_real_ibe_test.go) — full TBFT pipeline with real BLS + KyberSigner + real TLockIBE

Wire encoding subpackage [protocol/v2/tbft/wire/](protocol/v2/tbft/wire/):
- [wire.go](protocol/v2/tbft/wire/wire.go) — versioned binary encoding for `Onion` and `NonReceiptAttestation`
- [wire_test.go](protocol/v2/tbft/wire/wire_test.go) — round-trip + truncation + version-rejection tests
- [envelope.go](protocol/v2/tbft/wire/envelope.go) — `WrapOnion`, `WrapNonReceipt`, `Unwrap` — kind-discriminated envelope for putting TBFT messages in `SignedSSVMessage.Data`
- [envelope_test.go](protocol/v2/tbft/wire/envelope_test.go) — round-trip, version, kind, truncation, body-decode-error propagation

SSV adapter package [protocol/v2/ssv/runner/tbft/](protocol/v2/ssv/runner/tbft/) (the bridge layer; depends on ssv-spec):
- [config.go](protocol/v2/ssv/runner/tbft/config.go) — `ConfigForCluster` with cluster-size-aware TBFT/TBFT2 selection, leader rotation matching SSV's existing `RoundRobinProposer`
- [config_test.go](protocol/v2/ssv/runner/tbft/config_test.go) — n∈{4,7,10,13} factory tests, leader rotation, sorting, validation, overrides
- [controller.go](protocol/v2/ssv/runner/tbft/controller.go) — `Controller` (per-cluster state machine wrapping `tbft.Instance`s by slot, with per-Instance mutex, `ProcessOnion`/`ProcessNonReceipt`/`ProcessCandidate` routing, `OperatorID` accessor)
- [controller_test.go](protocol/v2/ssv/runner/tbft/controller_test.go) — API-surface tests + multi-controller end-to-end smoke test with real BLS + SignerGatedIBE
- [scheduler.go](protocol/v2/ssv/runner/tbft/scheduler.go) — `Scheduler` + `LifecycleHooks` exposing Phase-1/2/3 as procedural methods
- [scheduler_test.go](protocol/v2/ssv/runner/tbft/scheduler_test.go) — validation, hook-call ordering, error propagation, end-to-end below-quorum miss
- [dispatch.go](protocol/v2/ssv/runner/tbft/dispatch.go) — `DispatchEnvelope` / `DispatchBytes` for the receive path
- [dispatch_test.go](protocol/v2/ssv/runner/tbft/dispatch_test.go) — kind routing, error propagation, malformed-bytes handling
- [ratelimit.go](protocol/v2/ssv/runner/tbft/ratelimit.go) — `RateLimiter` for per-(slot, operator, kind, layer) duplicate rejection
- [ratelimit_test.go](protocol/v2/ssv/runner/tbft/ratelimit_test.go) — first-allowed / duplicate-rejected, cross-kind/cross-layer/cross-slot independence, Forget
- [runner.go](protocol/v2/ssv/runner/tbft/runner.go) — `RunProposerSlot` reference implementation: ties Controller+Scheduler together with realistic timing
- [runner_test.go](protocol/v2/ssv/runner/tbft/runner_test.go) — end-to-end multi-operator cluster timing tests for healthy + top-leader-silent paths (race-detector clean)
- [proposer_integration_sketch.go](protocol/v2/ssv/runner/tbft/proposer_integration_sketch.go) — documentation file showing call-site changes for the actual `proposer.go` modifications

Companion docs:
- [docs/IBE-INTEGRATION.md](docs/IBE-INTEGRATION.md) — drand/tlock compatibility analysis and Option B implementation path

`go test ./protocol/v2/tbft/... ./protocol/v2/ssv/runner/tbft/... -race` ⇒ **142 tests passing** across 4 packages (core + blsbackend + wire + adapter); `go vet` and `gofmt` clean; race-detector clean. The blsbackend package now includes the full real-IBE stack (KyberSigner, TLockIBE, conversion functions) with end-to-end integration test exercising the DST-trick approach.

**Refactors applied during cleanup pass:**
- Removed package-level `opShareForVerify` mutable global; replaced with `Instance.pubKeyShares` field passed at construction.
- Added `Instance.BuildOwnOnion` and `Instance.BuildOwnNonReceipts` convenience methods (the SSV adapter will use these).
- Added duplicate-operator-ID check to `Config.Validate`.
- Removed unused generic helper.
- Inline doc on the equivocation-handling rule.

**Decisions on file:**
- IBE keypair source: **Option A** (reuse validator threshold BLS key as IBE trust anchor)
- Rollout: per-cluster opt-in via registry; mixed-cluster prevention is operator deployment's job
- ssv-spec: **completely independent**; no spec coordination, no spec tests
- Inconsistency-slashing: detect & log only (no punishment hook in SSV today)

**Architectural notes (worth keeping in mind for Phase 4):**
- The TBFT core is intentionally a "consensus library": it knows nothing about wire format, p2p networking, libp2p signing, or SSV's specific message envelope. The SSV adapter is what makes it talk to the network.
- IBE = Encrypt/Decrypt only. Partial-sig signing and threshold aggregation are in `Signer`. Under Option A both share the same underlying BLS primitive but are conceptually distinct interfaces.
- Equivocation handling depends on honest operators detecting it during Phase 1 and treating the affected layer as non-receipt; this is a protocol-rule the adapter needs to enforce in its Phase 1 logic.

**Next action — handoff state.** The TBFT adapter, the protocol core, the wire format, the BLS-backed value-signing layer, the **real cryptographic IBE**, and the `proposer.go` integration are all functionally complete. The remaining work to actually flip the switch (proposer-duty runs on TBFT instead of QBFT):

1. **`proposer.go` modifications — DONE.** TBFT path lives behind `ProposerRunnerOptions.TBFTController`; nil keeps the QBFT path. See [proposer.go](protocol/v2/ssv/runner/proposer.go) and [proposer_tbft.go](protocol/v2/ssv/runner/proposer_tbft.go).

2. **EKM share-bytes accessor (Option A).** Local-mode-only path so the validator controller can hand raw share bytes to `BLSSigner` / `KyberSigner` constructors. In-memory cache populated at `AddShare`, with a fallback that extracts bytes from the loaded wallet account on cache miss (so shares persisted across process restarts are usable without re-replaying contract events). Remote-mode operators stay blocked until Option B (above) lands.

3. **Validator-controller wiring.** `operator/validator/controller.go`'s `SetupRunners` constructs the `tbftadapter.Controller` with the operator's share + cluster config + signers + TLockIBE, and passes `TBFTController` to `NewProposerRunner`.

4. **Network message validation + queue decode + dispatch.** `MsgTypeTBFT` (placeholder `0xF0`) is recognised by `message/validation/`, `protocol/v2/ssv/queue/`, and `protocol/v2/ssv/validator/validator.go` so inbound TBFT envelopes route to `ProposerRunner.ProcessTBFTEnvelope`.

5. **Devnet validation** (Phase 6) — once 2-4 are in. User-driven.

6. **Mainnet rollout** (Phase 7) — registry-based opt-in per cluster, coexistence with QBFT until rollout completes.

**Last updated:** Phase 4a + 4b + 4c + 4d complete. `proposer.go` integration committed. Real cryptographic IBE working end-to-end via the DST-trick approach (no DKG changes).

## Where this came from

This plan is the implementation track of the design exploration documented in:

- [TBFT.md](TBFT.md) — the n-layer protocol design
- [TBFT2.md](TBFT2.md) — the 2-layer specialization for n=4
- [TBFT-comparison.md](TBFT-comparison.md) — failure-mode comparison vs QBFT

The motivation traces back to [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829), specifically the MEV-driven concern that QBFT's round-change time can blow past relays' 4 s cutoff for proposer duty.
