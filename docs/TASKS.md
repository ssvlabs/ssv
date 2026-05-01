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

## Unified TBFT design

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
  - `T_commit` (deadline): default `slot_start + 3s`, configurable per cluster.
  - `Δ_1` (block-fetch window): default `1s`.
  - `Δ_2` (onion-gossip window): default `500ms`.
  - For TBFT2 (n=4): `T_b` (early backup fetch) at `slot_start − 4s`.
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
- [x] [protocol/v2/ssv/runner/tbft/config.go](protocol/v2/ssv/runner/tbft/config.go) — `ConfigForCluster(slot, committee, clusterID, overrides) → *tbft.Config` with cluster-size-aware TBFT selection: n=4 → K=2 with split fetch times (TBFT2); n≥7 → K=max(3, f+1) with uniform late fetch (TBFT). Leader rotation matches SSV's existing `RoundRobinProposer` convention.
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
- **Block-fetch coordination.** Top-K leaders each fetch in parallel. The Scheduler's hooks make this straightforward — `FetchCandidate` is the only callback that needs to call out to the beacon node / relay.
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
- [ ] **Cluster-size matrix.** Run protocol-scenario tests for n ∈ {4, 7, 10, 13}.
- [ ] **`make unit-test` and `make lint` clean** for the new TBFT package and its integration points.
- [ ] **Existing QBFT spec-tests untouched.** They still pass because we don't modify the QBFT codepath; TBFT lives in a separate package.

**Exit criteria:** TBFT package's own test suite green at all four cluster sizes; `make unit-test` and `make lint` clean.

---

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
| `T_commit = 3s` is too aggressive (squeezes block fetch) | Medium | Medium | Configurable per cluster; default conservative; tune from production data |

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
- [config.go](protocol/v2/ssv/runner/tbft/config.go) — `ConfigForCluster` with cluster-size-aware TBFT selection, leader rotation matching SSV's existing `RoundRobinProposer`
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

**Last updated:** Phase 4a + 4b + 4c + 4d complete. `proposer.go` integration committed. Real cryptographic IBE working end-to-end via the DST-trick approach (no DKG changes). **Spec rewrite landed; new spec-alignment work tracked in the "Spec-alignment tasks" section below.**

## Spec-alignment tasks (post-rewrite — current work)

This section enumerates the implementation work needed to bring `protocol/v2/tbft/` up to date with the post-rewrite [TBFT.md](TBFT.md) (n=4) and [TBFTR.md](TBFTR.md) (n≥7). The work picks up from the Phase 4d baseline. Audit residual: [TBFT-audit.md](TBFT-audit.md).

### ⚠ T0 (highest priority) — Constrain current implementation to n=4

**Before any other implementation work, the existing `protocol/v2/tbft/` must be locked to n=4 only.**

The current code is generic over K and n (parameterized through [`Config`](../protocol/v2/tbft/types.go)). With the spec rewrite, [TBFT.md](TBFT.md) is the n=4-only protocol; [TBFTR.md](TBFTR.md) is the separate n≥7 protocol that requires *additional* implementation work (V plaintext in onions, Phase 2a/2b composition, head-change refresh, worst-of-K timing — see T9, T17, T21, T22 below). **Allowing the current code to instantiate at n ≥ 7 would deploy a vulnerable protocol** — the byzantine-leader selective-delivery grief residual `[f+1, 2f-1]` is non-empty at f ≥ 2 without TBFTR's additions, which the current code doesn't have.

The code can/should stay generic in shape (parameterized by K, n, threshold) — *but only allow instance creation at n=4*. This makes future TBFTR support a smaller diff (lift the n=4 restriction once T9/T17/T21/T22 land).

- [ ] **T0. n=4-only instantiation guard.**

  - In [`Config.Validate`](../protocol/v2/tbft/types.go), add a check: `if len(c.Operators) != 4 { return errors.New("tbft: only n=4 clusters supported; for n≥7 see TBFTR (separate impl track)") }`. Remove later when TBFTR is implemented.
  - In [`ConfigForCluster`](../protocol/v2/ssv/runner/tbft/config.go) (or wherever the runner builds TBFT configs), add the same guard before constructing `*tbft.Config` for non-n=4 clusters. Surface a clear error message.
  - Update package doc-comments to reflect that the implementation is n=4-only until TBFTR support lands.
  - Tests: `n=4` succeeds; `n=7`, `n=10`, `n=13` rejected at instance creation with the expected error message.

  This task is not a long-term constraint — it's a guard rail until the TBFTR-specific tasks (T9, T17, T21, T22) land, at which point the runner can route n=4 to TBFT-mode and n≥7 to TBFTR-mode based on cluster size.

### Resolved design decisions

- **D1 — Threshold separation: keep `qEnc = f+1` in [TBFT.md](TBFT.md); upgrade to Option B is future work.** Spec stays as written (`qV = 2f+1` σ-quorum, `qEnc = f+1` unlock). Implementation today runs Option A from Phase 0 (one DKG, threshold 2f+1, DST-trick role separation), so the cluster is effectively at `qEnc = qV = 2f+1` cryptographically — the threshold-separation liveness benefit (TBFT.md "Liveness profile / What threshold separation buys") is documented in spec but not yet realized in code. T5 below tracks the upgrade. Safety is unconditional in either configuration once T6's σ+NR exclusion rule lands at aggregation time: the σ+NR mutual exclusion is algebraic regardless of byz behavior. The σ+NR slashing rule is for attribution, not safety.

- **D2 — V-plaintext in onion: cluster-size-conditional per the new TBFT/TBFTR split.** TBFT (n=4) doesn't include V plaintext in onions — the leader-σ-V mechanism + algebra at f=1 closes P0.1 without it. TBFTR (n≥7) requires V plaintext in onions as part of its core (recovery channel for missing-V honest in Phase 2a, fed into Phase 2b late σ). Implementation should be conditional on cluster config: at n=4, omit V plaintext from `EncryptedLayer`; at n≥7, include it (or include hash + leader-V per the hash variant). T9 below tracks the implementation; T17 tracks the broader TBFTR composition.

### P0 — correctness / safety (blocks deployment)

- [ ] **T1. Leader-authenticated candidates with leader's σ-on-V** ([TBFT.md](TBFT.md) Phase 1; closes audit P1.2 first action AND P0.1/P0.2 at n=4).

  Phase-1 bundle becomes `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(V_{L_k}))` — the leader signs V with both their V-keypair share (a partial threshold sig that gives the cluster a head-start partial toward qV) and their operator-identity key (leader-auth proof). At n=4 (f=1), the leader's σ^V plus the f+1=2 honest partials in Phase 2 sum to qV=3, closing the byzantine-leader selective-delivery grief mechanically (P0.1/P0.2). At n≥7 it narrows the grief window by one without closing it.

  - Add `LeaderSigOp Signature` and `LeaderSigV Signature` fields to [`CandidateBroadcast`](../protocol/v2/tbft/types.go):
    - `LeaderSigOp` is the operator-identity-key signature over `(ClusterID, Height, Layer, Value)`.
    - `LeaderSigV` is the leader's V-keypair partial sig on `Value` (same domain / DST as Phase-2 onion threshold partials, so the aggregator at [`tryReconstructLayer`](../protocol/v2/tbft/instance.go) treats it as one of the σ partials).
  - Update [`wire/`](../protocol/v2/tbft/wire/) encoder/decoder for both new fields.
  - Leader signs at fetch time in [`tbftFetchCandidate`](../protocol/v2/ssv/runner/proposer_tbft.go) (or its broadcast wrapper) — produces both signatures alongside V.
  - Verify both signatures in [`Controller.ProcessCandidate`](../protocol/v2/ssv/runner/tbft/controller.go) before `ObserveCandidate`. Reject bundles missing or failing either sig.
  - On the receiver side, when the bundle is accepted, the `LeaderSigV` partial is fed into the layer-`k` σ pool of the receiver's local [`Instance`](../protocol/v2/tbft/instance.go) (extend `ObserveCandidate` or add a sibling method to record the leader's σ partial). [`tryReconstructLayer`](../protocol/v2/tbft/instance.go) then includes it in σ aggregation alongside Phase-2 onion partials — same value `V_{L_k}`, same threshold qV.
  - Tests: candidate from non-leader rejected; bad operator-sig rejected; bad V-sig rejected; happy-path with both sigs verifies; **n=4 P0.1/P0.2 closure end-to-end** (byz layer-0 leader delivers to 2 honest, refuses to vote in Phase 2 — cluster still reconstructs at qV=3 because leader's σ-on-V from Phase 1 is the third partial); **n=7 residual grief** (byz delivers to exactly 3 honest at f=2, σ count = 4 < qV=5, NR count = 2 < qEnc=3, slot stuck).

- [ ] **T2. Equivocation-to-non-receipt rule** ([TBFT.md](TBFT.md) Phase 1; closes audit P1.2 second action).
  - Replace [`Instance.candidates map[int]Value`](../protocol/v2/tbft/instance.go) with a structure that records multiple validly-signed observations per layer.
  - On second distinct validly-signed observation at the same layer from the same leader: mark layer as equivocated locally.
  - [`BuildOwnOnion`](../protocol/v2/tbft/instance.go) skips equivocated layers; [`BuildOwnNonReceipts`](../protocol/v2/tbft/instance.go) emits NR for them.
  - Keep the two signed candidates as fault evidence (see T7).
  - Tests: two distinct candidates from same leader → that layer's onion slot empty + matching NR generated.

- [ ] **T3. Sender / authenticity check at envelope dispatch** ([TBFT.md](TBFT.md); closes audit P1.2 third action).
  - In [`ProcessTBFTEnvelopeMsg`](../protocol/v2/ssv/runner/proposer_tbft.go), before dispatch:
    - `KindOnion`: `senderID == env.Onion.OperatorID`.
    - `KindNonReceipt`: `senderID == env.NonReceipt.OperatorID`.
    - `KindCandidate`: `senderID == env.Candidate.OperatorID` AND `senderID == cfg.Layers[env.Candidate.Layer].Leader`.
  - With T1, the leader sig is the load-bearing check; sender-equality is defense-in-depth.
  - Tests: each kind with mismatched sender rejected.

### P1 — spec compliance

- [ ] **T4. Threshold separation in protocol counting — naming-only refactor** ([TBFT.md](TBFT.md) Setting + Why it's safe). Coupled with T5 below; lands together cryptographically but the naming change can land independently as a small refactor.
  - Add `Config.QV() = 2f+1` and `Config.QEnc() = f+1`; update callers.
  - σ-quorum check in [`tryReconstructLayer`](../protocol/v2/tbft/instance.go): `< QV()`.
  - NR-quorum check in [`tryDeriveNextLayerKey`](../protocol/v2/tbft/instance.go): `< QEnc()`.
  - **Effectively a no-op until T5 lands**: under Option A the IBE primitive still needs `2f+1` partials to decrypt, so counting at `f+1` for unlock doesn't enable actual fall-through. Lands the spec's naming in code; cryptographic effect comes with T5.
  - Tests: protocol-level counting at the new thresholds; mutual-exclusion preserved at the impl-level (still 2f+1 effective until T5).

- [ ] **T6. Aggregator-level fault exclusion (σ+NR + cross-onion σ+σ')** ([TBFT.md](TBFT.md) "Why it's safe" + caveat 2; closes audit P1.5 first bullet AND replaces the load-bearing-slashing safety assumption with a structural guard).

  Two related exclusions in the [`tryReconstructLayer`](../protocol/v2/tbft/instance.go) and [`tryDeriveNextLayerKey`](../protocol/v2/tbft/instance.go) aggregation paths:

  - **σ+NR exclusion**: an operator that has both a non-empty σ partial at layer `k` (in their onion) AND an NR attestation on `nr_tag_k` (broadcast separately) has *both* contributions excluded from their respective quorum pools at layer `k`. Implementation: in `tryReconstructLayer`, when iterating onions, skip operators that appear in `i.nonReceipts[layer]`. Symmetrically in `tryDeriveNextLayerKey`, skip operators whose onion at `layer` has a non-empty σ partial. **This is what makes the threshold-separation safety argument hold structurally** (TBFT.md "Why it's safe"), rather than depending on slashing-deterred byzantine behavior.

  - **Cross-onion σ+σ' exclusion**: an operator's partial sig appearing in two different value-groups at the same layer (signed two different `V`'s) has both contributions excluded. Implementation: in `tryReconstructLayer`'s grouping loop, when adding `(opID, partial)` to a sigGroup, check if `opID` already appears in any *other* group at this layer. If yes, record an `InconsistencyFault` of kind `CrossOnionPartial` and remove `opID` from both groups.

  Both exclusions feed a shared `InconsistencyFault` `Kind` enum: `SigmaPlusNR`, `LeaderEquivocation`, `CrossOnionPartial` (T7 carries the evidence representation). The existing [`detectInconsistencyAt`](../protocol/v2/tbft/instance.go) detector remains for attribution-time recording; the new behavior is *applying the exclusion at aggregation time* (not just recording faults silently).

  Tests:
  - **σ+NR exclusion**: byz operator broadcasts σ at layer k AND NR on nr_tag_k → both contributions excluded from their pools; with byz attempting σ+NR equivocation, at most one of {σ-quorum, NR-quorum} reaches at the same layer regardless of byz behavior. **No two outputs cluster-wide even under hostile byz.**
  - **Cross-onion σ+σ'**: byz operator's σ on V and σ on V' at same layer → recorded as fault; neither value-group counts that contribution; standard σ-quorum still reachable for honest-only V.
  - **Liveness regression check**: under the standard P0.1 attack (byz silent on Phase-2 votes), exclusion is a no-op (byz didn't σ+NR), slot outcome unchanged from current behavior.

- [ ] **T7. Slashable fault-proof representation** ([TBFT.md](TBFT.md) caveat 2).
  - Extend [`InconsistencyFault`](../protocol/v2/tbft/instance.go) to carry the cryptographic evidence per Kind:
    - `SigmaPlusNR`: σ partial + NR attestation, both signed.
    - `LeaderEquivocation`: two leader-signed candidates.
    - `CrossOnionPartial`: two distinct σ partials on different values.
  - Decide: in-protocol gossip (new `KindFaultProof` envelope) vs out-of-band export. Default: out-of-band.
  - Persistence so faults aren't lost on restart.
  - Tests: each kind round-trips; verifying evidence in isolation reproduces the contradiction.

- [ ] **T8. Application-validity gates candidate signing** ([TBFT.md](TBFT.md) Preconditions + caveat 8; closes audit P1.3 + P1.4 implementation halves).
  - Audit the runner onion-build path: before signing `σ_i^V(V_{L_k})` at any layer, run app-level checks (slot, proposer index, fork/domain, parent root, relay metadata, doppelganger, slashing protection, encoding).
  - Move `IsBeaconBlockSlashable` + `UpdateHighestProposal` to also gate **candidate signing** (in addition to today's submit-time check). Submit-time check stays as defense-in-depth.
  - Tests: a slashable candidate doesn't make it into the operator's onion.

### P2 — polish

- [ ] **T9. Cluster-size-conditional V-plaintext in onions.** With the new TBFT (n=4) / TBFTR (n≥7) split:

  - **TBFT mode (n=4)**: `EncryptedLayer` should NOT carry `V_{L_k}` plaintext (TBFT.md Phase 2 doesn't include it; the leader-σ-V mechanism + algebra at f=1 closes P0.1 without a recovery channel).
  - **TBFTR mode (n≥7)**: `EncryptedLayer` carries `V_{L_k}` plaintext (or hash, with full V at leader's own layer per hash variant) — see [TBFTR.md](TBFTR.md) Phase 2a. Required for the late-σ recovery in Phase 2b.

  Implementation: parameterize `EncryptedLayer` and onion construction by mode. The runner picks mode based on cluster size at config time. Same instance.go aggregation path handles both — operators that haven't received V from any source contribute null at that layer regardless of mode. Tests for both modes.

  Today the impl carries V-plaintext at every layer (legacy from before the cluster-size split). For TBFT n=4 mode, strip the `Value` field from `EncryptedLayer` (or leave it but ignore it during aggregation). For TBFTR mode, keep it — but TBFTR support is gated on T0 lifting, so this task primarily delivers the TBFT-mode-strips-V part now.

- [ ] **T10. EKM behavior under multi-block-signing-per-slot** ([TBFT.md](TBFT.md) caveat 8).
  - Verify EKM allows K candidate-value signatures per slot inside onion build (one per layer). Today's EKM check is at submit time on the winning block; partial-sig signing in onion build should not collide with duplicate-block protection.
  - If EKM dedupes by slot only and would flag partial-sig signing, adjust to dedupe by (slot, layer) or use a separate signing API for partials.
  - Tests: end-to-end onion build at K=3 layers without EKM rejection; submit still applies the proper full-block slashing check.

- [ ] **T11. Aggregate test pass for the new rules** (integrates with T1–T8).
  - Many tests are listed under each task; this is the consolidated coverage commitment.

- [ ] **T12. Code comment cleanup**.
  - Update comments in [`instance.go`](../protocol/v2/tbft/instance.go) (notably the punt-to-host comment around lines 319–323, now actually wired up by T2), [`types.go`](../protocol/v2/tbft/types.go) (Quorum vs QV/QEnc), [`tag.go`](../protocol/v2/tbft/tag.go) (post-rewrite terminology).

### P3 — backlog (don't gate spec-alignment; tracked here)

- [ ] **T5. Upgrade Option A → Option B: separate IBE keypair at threshold `qEnc = f+1` via Pedersen DKG between operators.** Detailed plan tracked in [TBFT-DKG-TASKS.md](TBFT-DKG-TASKS.md). Lands T4 (protocol-counting refactor) coincidentally — under Option B those new thresholds are cryptographically meaningful for the first time.

- [ ] **T15. Final-certificate gossip — `KindCertificate(slot, V, S)`** ([TBFT.md](TBFT.md) Phase 3 / [TBFTR.md](TBFTR.md) Phase 3). **Applies to both protocols.** After successful Phase-3 reconstruction, the operator broadcasts a `KindCertificate` envelope carrying `(slot, V, S)`. Receivers verify `S` against the cluster's V-keypair pubkey on `V`; valid certificates can be submitted downstream by anyone, mitigating the "lone-reconstructor's beacon path fails" failure mode. Implementation:
  - Add `KindCertificate = 0x04` to [wire/envelope.go](../protocol/v2/tbft/wire/envelope.go) along with encode/decode.
  - In the runner, after `Resolve()` returns a non-nil `Output`, broadcast the certificate in parallel with the local submit attempt.
  - On receiving a certificate, verify `S` and submit if not already submitted (idempotent; downstream dedupes).
  - Replay/cache: dedupe on `(slot, V hash)` to prevent gossip storm.
  - Tests: lone-reconstructor scenario — only one operator reaches qV in their local view, but the certificate gossip lets every operator submit, slot succeeds even when the reconstructor's own submit fails.

- [ ] **T16. End-to-end timing budget with telemetry** ([TBFT.md](TBFT.md) and [TBFTR.md](TBFTR.md) "Timing budget" subsections). Framework specified in spec; numbers TBD. Production data needed:
  - Pre-consensus (RANDAO partial-sig collection) tail latency.
  - Block-fetch latency (worst-of-K for TBFTR).
  - Gossipsub propagation (P99/P999) for onion + late-σ broadcasts.
  - EKM signing latency.
  - Beacon submit + relay submission tails.
  - Once collected, commit per-leg budget defaults; tighten Δ_1, Δ_2 (and Δ_2a, Δ_2b for TBFTR) accordingly.

- [ ] **T17. TBFTR composition (Phase 2a/2b split, late σ)** ([TBFTR.md](TBFTR.md) Phase 2a + 2b). **Required for any n≥7 deployment** — gated on T0 lifting. Foundations come from T1 (leader-auth) + T9 (V-plaintext for TBFTR mode).

  Implementation:
  - Phase 2a: each operator broadcasts onion (with V-plaintext per T9 TBFTR mode) at `T_commit`. No NR yet.
  - Phase 2b at `T_commit + Δ_2a`: for each layer where the operator hasn't yet broadcast σ — if they recovered V from a peer's Phase-2a onion and validated, broadcast a late `σ_i^V(V_{L_k})` plaintext (separate message, not encrypted). Otherwise broadcast NR attestation.
  - Phase 3 starts at `T_commit + Δ_2a + Δ_2b`. Aggregator pool at layer `k` = leader's σ from Phase 1 ∪ onion partials at layer k ∪ late-σ broadcasts at layer k, with σ+NR exclusion (T6).
  - New scheduler / timing logic for Phase 2 split. New message kind for late σ broadcasts (or reuse onion format with a flag).
  - Tests: P0.1 worst-case attack at n=7 (byz selective delivery to 3 honest + dark on Phase-2a votes) → cluster reaches qV=5 via late σ in Phase 2b, slot succeeds. Same shape at n=10, n=13.

  At n=4 (TBFT) the composition is not used.

- [ ] **T21. Head-change handling during Phase 1.** Both [TBFT.md](TBFT.md) Phase 1A and [TBFTR.md](TBFTR.md) "Head-change handling" specify that leaders refresh their candidate (and re-broadcast `(V', σ^V', σ^op')`) if the head changes during their fetch window. Implementation:
  - **For TBFT (n=4)**: `L_b` (backup) and `L_p` (primary) detect head-change between `T_b`/`T_p` and `T_commit`, refresh candidate from new head, re-broadcast bundle. Honest receivers accept the latest validly-signed candidate matching the current head; older candidates fail `parent_root` validity and are silently dropped.
  - **For TBFTR (n≥7)**: same shape applied to top-K leaders during their parallel fetch windows. Gated on T0 lifting.
  - Implementation lives in the runner's fetch path ([proposer_tbft.go](../protocol/v2/ssv/runner/proposer_tbft.go) `tbftFetchCandidate`). Subscribe to head-change events; on head-change-during-fetch, abort current fetch, re-fetch from new head, re-broadcast.
  - Tests: head changes mid-fetch → cluster receives the refreshed candidate; old candidate is dropped by `parent_root` check.

- [ ] **T22. Worst-of-K beacon-fetch timing** ([TBFTR.md](TBFTR.md) "Timing budget" / "Worst-of-K beacon-fetch latency"). TBFTR-only (TBFT K=2 doesn't have meaningful worst-of-K, since `L_b` and `L_p` fetch at different times anyway). Gated on T0 lifting.
  - Configure `Δ_1` to accommodate the slowest-of-K parallel beacon fetches (P99/P999 over all K fetchers in parallel, not single-fetch P99).
  - Production telemetry on K-parallel-fetch tails feeds into this; ties to T16.

### Sequencing recommendation

**🚧 PR-Z (must land before any other implementation work)**: T0. Constrain the current implementation to n=4 only. Defensive guard rail; all other tasks below assume this is in place.

**Shared (both protocols, n=4 deployment first)**:

- **PR-A**: T1 + T2 + T3 + their tests. Closes audit P1.2 end-to-end. The right *first* PR after T0.
- **PR-B**: T6 + T7 + tests. Aggregator-level fault exclusion (σ+NR + cross-onion σ+σ'). Builds on T1's evidence shape.
- **PR-C**: T8 (candidate-signing slashing gate). Touches runner + EKM behavior.
- **PR-D**: T9 (TBFT-mode V-plaintext stripping). Aligns onion shape with TBFT.md spec for n=4.
- **PR-E**: T15 (KindCertificate). Real liveness improvement. Both protocols use it.
- **PR-F**: T21 (head-change handling, TBFT n=4 part). Runner-side; TBFTR n≥7 part is gated.
- **PR-G**: T12 (comment cleanup); rides along with any of the above.

**Lighter-touch (in parallel with above)**:

- T10 (EKM compatibility audit) — verify; act if EKM dedupe-by-slot-only would flag partial-sig signing.
- T11 (aggregate test pass) — integrates with all PRs above.

**TBFTR-specific (gated on T0 lifting + TBFTR-spec implementation)**:

- T9 (TBFTR-mode V-plaintext path) + T17 (Phase 2a/2b composition) + T21 (TBFTR head-change) + T22 (TBFTR worst-of-K timing). All these work together to deliver n≥7 support per [TBFTR.md](TBFTR.md).

**Backlog**:

- T5 (Option B upgrade — separate IBE DKG at threshold f+1). Blocks T4's cryptographic realization (currently both protocols run "Option A" with effective qEnc=qV until T5 lands). Multi-PR effort; tracked separately in [TBFT-DKG-TASKS.md](TBFT-DKG-TASKS.md).
- T16 (end-to-end timing budget). Production telemetry needed.

## Where this came from

This plan is the implementation track of the design exploration documented in:

- [TBFT.md](TBFT.md) — the n-layer protocol design
- [TBFT-comparison.md](TBFT-comparison.md) — failure-mode comparison vs QBFT

The motivation traces back to [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829), specifically the MEV-driven concern that QBFT's round-change time can blow past relays' 4 s cutoff for proposer duty.
