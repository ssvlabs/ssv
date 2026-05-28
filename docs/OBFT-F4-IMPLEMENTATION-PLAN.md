# OBFT F4 Implementation Plan — BLS batch-verify for σ-walk loops

Concrete implementation plan for the last Tier-1 audit finding: batch the per-operator BLS verifies that Resolve walks at deeper layers, via herumi's `bls.MultiVerify` random-linear-combination primitive.

See [OBFT-PERFORMANCE-AUDIT-PLAN.md](OBFT-PERFORMANCE-AUDIT-PLAN.md) for the broader audit context, the §F4 investigation, and the B4 benchmark that grounds the saving estimate. F4 is more invasive than F1/F3/F5 because it touches the `obft.Signer` interface — hence this focused plan.

## Goal

| Finding | Mechanism | Saving |
|---|---|---|
| **F4 (base)** | Batch the per-operator σ-walk verifies at L_k > 0 inside `Instance.Resolve` into one `MultiVerify` call (BLSSigner path); KyberSigner / StubSigner fall back to sequential. | ~3-5 ms/slot |
| **F4 (twoab mirror)** | Same shape at twoab's `extractSigmaFromEntries` L_k > 0 walk. | ~3-5 ms/slot |

Combined: **~6-10 ms/slot wall-clock saving**, on top of F1's ~70 ms/slot. The win is modest because F1's cache already covers the warm path — F4 only helps the *first* walk of each L_k>0 layer (cache miss). The implementation is paid for by the fact that the same insertion-point + cache-key infrastructure F1 added makes the batching slot in cleanly.

The win materialises only on slots where Resolve walks L_k > 0 entries — primarily attack-mode slots and slots where L_0 σ-quorum fails. On healthy slots that converge at L_0, F4 is a no-op (the L_0 path is pre-verified at observation, never touches Resolve's verify).

## Safety invariant

The single invariant the implementation must preserve:

> A successful batch verify (`VerifyPartialBatch(shares, msgs, sigs) == true`) implies that, with overwhelming probability, every individual tuple `(shares[i], msgs[i], sigs[i])` would have verified under `VerifyPartial`. A failed batch verify (`false`) implies AT LEAST ONE tuple would have failed individually, but does not identify which.

Stated against herumi: `bls.MultiVerify` uses random-linear-combination — it samples random 64-bit scalars `r_i` and verifies `∏_i e(sigs[i], -G_2)^(r_i) · ∏_i e(pubs[i], H(msgs[i]))^(r_i) == 1` in one pairing equation. A forged tuple passes this with probability ~2^-64 per random sample; the standard security argument that has the same forgery-resistance as N individual verifies. Caller-side this means a batch-true result is as load-bearing as N true results from `VerifyPartial`.

For Rule-4 evidence attribution at L_k > 0 (the σ-walk's only consequence on byzantine partials), the implementation MUST fall back to per-sig verify on batch failure to identify which `(op, layer)` to attribute. The fallback re-runs `signer.VerifyPartial` over the candidate set and records evidence per failing tuple, preserving the existing per-(op, layer) Rule-4 dedup and evidence path exactly.

## Why not the Verifier NR-side paths?

The audit doc's original F4 sketch listed `verifyCommitNRPartials` ([base/phase2.go:983](protocol/v2/obft/base/phase2.go:983)) as a batch target. After investigating the production wiring, that path is NOT worth touching:

- **Production NR-side verifies go through `KyberSigner`**, not `BLSSigner`. The runner-layer `Verifier` constructed at [runner/obft/verifier.go:69](protocol/v2/ssv/runner/obft/verifier.go:69) sets `TagSigner = blsbackend.NewKyberSigner(nil)`. Same wiring in [runner/obft/twoab/verifier.go:62](protocol/v2/ssv/runner/obft/twoab/verifier.go:62).
- **drand/kyber-bls12381 has no equivalent batch primitive.** `kyber/sign/bls.BatchVerify` verifies an *already-aggregated* signature against many (pub, msg) pairs ([kyber@v1.3.1/sign/bls/bls.go:128](https://github.com/drand/kyber/blob/master/sign/bls/bls.go#L128)). The σ-walk and NR-partials loops need the opposite: many *individual* sigs verified as a batch. Implementing the random-linear-combination scheme on kyber primitives is non-trivial and out of F4's scope.
- **F5 already moved the in-Instance `verifyCommitNRPartials` off the production critical path** via `SkipNRPartialReverify=true` in the runner. So that loop only runs in consensustest, where wall-clock saving doesn't matter.
- **The upstream `Verifier.VerifyCommitNRPartials` runs unconditionally on production, but uses kyber** — no batch primitive available without writing one.

So F4's practical scope is **the σ-walk's BLSSigner verifies inside `Instance.Resolve`**. KyberSigner + StubSigner implementations of the new method sequentially loop `VerifyPartial`, preserving the F1 + F3 caches transparently.

## Design overview

### Interface extension

```go
// obft/signer.go

type Signer interface {
    // ... existing methods ...

    // VerifyPartialBatch is the batch form of VerifyPartial: it returns true
    // iff EVERY (pubKeyShares[i], msgs[i], sigs[i]) tuple would individually
    // verify. All three input slices MUST have the same length N ≥ 1.
    //
    // msgs[i] follows the same shape rules as VerifyPartial's msg argument
    // for the receiver type: inner backends (BLSSigner, KyberSigner) require
    // each msgs[i] to be the 32-byte raw signing target; wrapper signers
    // (proposerSigner) accept V bytes and translate each msg internally
    // before delegating to the inner backend.
    //
    // Returns false if N is zero, any length mismatches, any inner msg isn't
    // 32 bytes, or any tuple fails to verify under the same security argument
    // as VerifyPartial. A false return does NOT identify which tuple failed —
    // callers that need per-tuple attribution (Rule-4 evidence at the σ-walk)
    // MUST fall back to a per-tuple verify loop on failure.
    //
    // Backed by herumi's MultiVerify (random-linear-combination) in BLSSigner;
    // KyberSigner and StubSigner fall back to sequential VerifyPartial loops
    // because kyber-bls12381 doesn't expose an equivalent batch primitive and
    // the stub is for protocol-level tests where realism isn't a concern.
    VerifyPartialBatch(pubKeyShares [][]byte, msgs [][]byte, sigs []Signature) bool
}
```

The msg-shape rule mirrors VerifyPartial's existing two-layer contract: inner backends sign 32-byte signing targets (OBFT signing roots and NR tags), while wrapper signers like proposerSigner accept V bytes and translate per-call. Tying VerifyPartialBatch to the same convention keeps the layering intact without forking a separate "raw" interface.

### BLSSigner implementation (herumi)

```go
// blsbackend/signer.go

func (s *BLSSigner) VerifyPartialBatch(pubs [][]byte, msgs [][]byte, sigs []obft.Signature) bool {
    n := len(sigs)
    if n == 0 || len(pubs) != n || len(msgs) != n {
        return false
    }
    concat := make([]byte, 0, n*32)
    sigVec := make([]bls.Sign, n)
    pubVec := make([]bls.PublicKey, n)
    for i := 0; i < n; i++ {
        if len(msgs[i]) != 32 || len(pubs[i]) == 0 || len(sigs[i]) == 0 {
            return false
        }
        if err := pubVec[i].Deserialize(pubs[i]); err != nil {
            return false
        }
        if err := sigVec[i].Deserialize(sigs[i]); err != nil {
            return false
        }
        concat = append(concat, msgs[i]...)
    }
    return bls.MultiVerify(sigVec, pubVec, concat)
}
```

Allocations: 1 backing slice for `concat` (n*32 bytes), 2 backing slices for the sig/pub vectors. herumi internally allocates the random scalars (`randVec`) plus per-thread workspace at N ≥ 16 (the parallelization threshold). For N=5-7 typical clusters, it's the single-threaded `else` branch — no goroutine overhead.

### KyberSigner implementation (sequential fallback)

```go
// blsbackend/kyber_signer.go

func (k *KyberSigner) VerifyPartialBatch(pubs [][]byte, msgs [][]byte, sigs []obft.Signature) bool {
    n := len(sigs)
    if n == 0 || len(pubs) != n || len(msgs) != n {
        return false
    }
    for i := 0; i < n; i++ {
        if len(msgs[i]) != 32 {
            return false
        }
        if !k.VerifyPartial(pubs[i], msgs[i], sigs[i]) {
            return false
        }
    }
    return true
}
```

Trivially correct — each call goes through the existing F3 pub-cache, so warm calls benefit from `cachedPubkeyPoint`. No batch speedup; the method exists to keep the interface uniform.

### StubSigner implementation (sequential fallback)

Same shape as KyberSigner — loop calling `VerifyPartial`. The stub doesn't model real-BLS cost; no batching is needed for protocol-level test realism.

### proposerSigner wrapper

```go
// runner/obft/proposer_signer.go and runner/obft/twoab/proposer_signer.go

func (s *proposerSigner) VerifyPartialBatch(pubs [][]byte, msgs [][]byte, sigs []obftcore.Signature) bool {
    if len(msgs) == 0 {
        return false
    }
    srs := make([][]byte, len(msgs))
    for i, m := range msgs {
        sr, err := s.signingRootFor(m)
        if err != nil {
            return false
        }
        srs[i] = sr
    }
    return s.inner.VerifyPartialBatch(pubs, srs, sigs)
}
```

Translates each msg through `signingRootFor` then delegates. The translation cost (B2 measured at ~100 µs per call for a 17 KB block) dominates the per-msg overhead — but the same V is reused across all N tuples in a σ-walk batch (every operator's partial at one layer signs the same V). A future optimization (F2) caches the signing-root per V; until then, callers that batch over a single V should compute the signing root once and skip the wrapper. **For F4 itself, we accept the redundant per-msg translation** since the σ-walk's batch is always at a single V — the proposerSigner's batch implementation is correctness-preserving but not optimal. F2 closes the gap.

### Call sites — base/phase3.go σ-walk

Current code at [base/phase3.go:213-279](protocol/v2/obft/base/phase3.go:213) walks `peerOnions[layer]` per-op, decrypts at L_k > 0, then calls `verifyOrCached` per entry. Refactored:

```go
// Collect cache-miss verify candidates into a batch.
type pendingVerify struct {
    op       OperatorID
    pubShare []byte
    value    Value
    partial  Signature
    el       EncryptedLayer // for evidence path
}
var pending []pendingVerify

for opID, entries := range i.peerOnions[layer] {
    if opID == leaderID {
        continue
    }
    for _, el := range entries {
        var partial Signature
        if layer == 0 {
            partial = Signature(el.Ciphertext)
        } else {
            pt, err := i.chainDecryptForLayer(layer, el.Ciphertext, chainedKeys)
            if err != nil {
                // ... existing Rule-4 (decrypt-failure) path unchanged ...
                continue
            }
            partial = Signature(pt)
        }
        pubShare := i.pubKeyShares[opID]
        if pubShare == nil {
            continue
        }
        // F1: cache hit → add directly, skip batch.
        if i.alreadyVerified(opID, layer, el.Value, partial) {
            addToGroup(&groups, el.Value, opID, partial)
            continue
        }
        pending = append(pending, pendingVerify{opID, pubShare, el.Value, partial, el})
    }
}

// Batch-verify all pending tuples in one MultiVerify call.
if len(pending) > 0 {
    if i.batchVerifyAndPopulate(layer, pending, &groups) {
        // Happy path: all pending verified.
    } else {
        // At least one failed — fall back to per-sig to attribute Rule-4.
        i.sequentialVerifyAndAttribute(layer, pending, &groups)
    }
}
```

The two helpers:

```go
// batchVerifyAndPopulate returns true iff the batch verify succeeded; on
// success, every pending tuple is cache-populated and added to its group.
func (i *Instance) batchVerifyAndPopulate(layer int, pending []pendingVerify, groups *[]*sigGroup) bool {
    pubs := make([][]byte, len(pending))
    msgs := make([][]byte, len(pending))
    sigs := make([]Signature, len(pending))
    for k, pv := range pending {
        pubs[k], msgs[k], sigs[k] = pv.pubShare, pv.value, pv.partial
    }
    if !i.signer.VerifyPartialBatch(pubs, msgs, sigs) {
        return false
    }
    for _, pv := range pending {
        i.markVerified(pv.op, layer, pv.value, pv.partial)
        addToGroup(groups, pv.value, pv.op, pv.partial)
    }
    return true
}

// sequentialVerifyAndAttribute runs per-sig verify on each pending tuple. On
// success: cache-populate + addToGroup. On failure at L_k > 0: record Rule-4
// evidence. Identical attribution to the pre-F4 per-sig loop.
func (i *Instance) sequentialVerifyAndAttribute(layer int, pending []pendingVerify, groups *[]*sigGroup) {
    for _, pv := range pending {
        if i.signer.VerifyPartial(pv.pubShare, pv.value, pv.partial) {
            i.markVerified(pv.op, layer, pv.value, pv.partial)
            addToGroup(groups, pv.value, pv.op, pv.partial)
            continue
        }
        if layer > 0 {
            if i.recordRule4(pv.op, layer) {
                i.recordEvidence(Evidence{ /* ... same as current ... */ })
            }
        }
    }
}
```

The leader-bundle path at [base/phase3.go:180-188](protocol/v2/obft/base/phase3.go:180) stays as-is — it iterates ≤ 2 retained bundles per layer (Pigeonhole 2 bound), and F1's cache hits the common case. Batching 2 entries gains nothing.

### Call sites — twoab/phase3.go σ-walk

Same shape, slightly different structure. `aggregatePeerLayerEntries` at [twoab/phase3.go:218](protocol/v2/obft/twoab/phase3.go:218) loops over three peer-message stores (`peerValueMsg`, `peerNoValueMsg`, `peerCommit` with `Side=NRDirect`), calling `extractSigmaFromEntries` at [twoab/phase3.go:239](protocol/v2/obft/twoab/phase3.go:239) once per (op, store). The per-op helper decrypts the op's SigmaChained entry at `layer`, verifies, and adds to `groups` or fires Rule-4 evidence.

Refactor: split `extractSigmaFromEntries` into a "decrypt + classify" helper that pushes (cache-hit → addToGroup) or (cache-miss → pending) instead of doing the verify inline. `aggregatePeerLayerEntries` becomes:

```go
var pending []pendingVerify
for op, vm := range i.peerValueMsg {
    i.classifySigmaFromEntries(op, layer, vm.LayerEntries, chainedKeys, groups, &pending)
}
for op, nv := range i.peerNoValueMsg {
    i.classifySigmaFromEntries(op, layer, nv.LayerEntries, chainedKeys, groups, &pending)
}
for op, c := range i.peerCommit {
    if c.Side != CommitSideNRDirect {
        continue
    }
    i.classifySigmaFromEntries(op, layer, c.LayerEntries, chainedKeys, groups, &pending)
}
if len(pending) > 0 {
    if !i.batchVerifyAndPopulate(layer, pending, groups) {
        i.sequentialVerifyAndAttribute(layer, pending, groups)
    }
}
```

`batchVerifyAndPopulate` / `sequentialVerifyAndAttribute` mirror base's helpers (same shape, twoab's `addToGroup`-equivalent uses the `groups[vRoot]` map directly per [twoab/phase3.go:286-292](protocol/v2/obft/twoab/phase3.go:286)). The Rule-4 evidence shape is identical to base's (same `EvidenceFakeEncryptedPresence` type, same `recordRule4` dedup).

## Implementation — commit-by-commit

### Commit 1: Signer interface extension + 3 backends + race-detector handling

**Files:**
- [protocol/v2/obft/signer.go](protocol/v2/obft/signer.go) — add `VerifyPartialBatch` to the `Signer` interface; implement on `StubSigner` as sequential.
- [protocol/v2/obft/blsbackend/signer.go](protocol/v2/obft/blsbackend/signer.go) — implement on `BLSSigner` via `bls.MultiVerify`.
- [protocol/v2/obft/blsbackend/kyber_signer.go](protocol/v2/obft/blsbackend/kyber_signer.go) — implement on `KyberSigner` as sequential.
- [protocol/v2/ssv/runner/obft/proposer_signer.go](protocol/v2/ssv/runner/obft/proposer_signer.go) — implement on `proposerSigner` (msg → signing-root translate, delegate).
- [protocol/v2/ssv/runner/obft/twoab/proposer_signer.go](protocol/v2/ssv/runner/obft/twoab/proposer_signer.go) — same for twoab.
- New test file [protocol/v2/obft/blsbackend/multiverify_batch_test.go](protocol/v2/obft/blsbackend/multiverify_batch_test.go) — direct tests of the new method on all 3 backends.

**Tests:**

```
TestBLSSigner_VerifyPartialBatch_AllValid           — happy path, N=3,6,13
TestBLSSigner_VerifyPartialBatch_OneTampered        — N=6 with one bad sig → returns false
TestBLSSigner_VerifyPartialBatch_LengthMismatch     — len(pubs)≠len(sigs) → false
TestBLSSigner_VerifyPartialBatch_NonStandardMsgLen  — msgs32[i] not 32 bytes → false
TestBLSSigner_VerifyPartialBatch_EmptyBatch         — N=0 → false (contract)
TestKyberSigner_VerifyPartialBatch_AllValid         — sequential fallback exercises F3 cache too
TestKyberSigner_VerifyPartialBatch_OneTampered      — false on first bad sig
TestStubSigner_VerifyPartialBatch_AllValid          — sequential fallback parity
TestStubSigner_VerifyPartialBatch_OneTampered       — false on first bad sig
TestProposerSigner_VerifyPartialBatch_AllValid      — translates msgs, delegates
TestProposerSigner_VerifyPartialBatch_DecodeError   — bad V bytes → false (one bad msg fails the batch)
```

The BLSSigner tests AND the existing `TestMultiVerify_Fixture` must skip under `-race` due to the herumi `checkptr` issue (see below). A helper:

```go
func skipIfRace(t *testing.T) {
    t.Helper()
    if race.Enabled {
        t.Skip("herumi/bls MultiVerify trips Go's -race checkptr; production builds are unaffected — " +
            "see docs/OBFT-F4-IMPLEMENTATION-PLAN.md §race-detector")
    }
}
```

Lives next to the bench/test setup. Each `bls.MultiVerify`-touching test calls it as its first line.

### Commit 2: base Instance σ-walk batch wiring + tests

**Files:**
- [protocol/v2/obft/base/phase3.go](protocol/v2/obft/base/phase3.go) — refactor the σ-walk peer-onion loop to collect pending → batch-verify → fall back to sequential.
- [protocol/v2/obft/base/phase3_batch_test.go](protocol/v2/obft/base/phase3_batch_test.go) — new test file for the batch path.

**Tests:**

```
TestResolve_LkGreaterThanZero_BatchHappy        — all decrypted partials valid → batch succeeds → all cache-populated + added
TestResolve_LkGreaterThanZero_BatchFailureFallback — one partial bad → batch fails → sequential fires Rule-4 for bad op, cache-populates good
TestResolve_LkGreaterThanZero_CacheHitBypassesBatch — pre-populate cache for one op → that op's entry skips the batch entirely
TestResolve_LkGreaterThanZero_AllCacheHits_NoBatchCall — all pending list empty after cache check → no MultiVerify call
TestResolve_LkGreaterThanZero_DecryptFailureNotInBatch — Rule-4 on decrypt-fail still fires per-(op, layer); doesn't enter the batch path
```

The "no batch call when all cache-hit" test uses a custom signer that asserts `VerifyPartialBatch` was never called when every entry hits the cache (count-tracking signer).

### Commit 3: twoab Instance σ-walk batch wiring + tests

**Files:**
- [protocol/v2/obft/twoab/phase3.go](protocol/v2/obft/twoab/phase3.go) — same refactor at `extractSigmaFromEntries` / `aggregatePeerLayerEntries`.
- [protocol/v2/obft/twoab/phase3_batch_test.go](protocol/v2/obft/twoab/phase3_batch_test.go) — same test set, twoab-flavored.

Tests mirror base 1:1 (same scenarios, same assertions, twoab Resolve harness).

## Testing strategy

- Unit tests listed above per commit.
- **Existing `consensustest` stress suite must continue to pass** end-to-end. The batch path is a refactor of the σ-walk; behaviour-equivalent. Run `make stresstest` after commit 3 to confirm.
- **Existing protocol tests under `-race`**: skip the new BLSSigner-batch tests under `-race` (herumi `checkptr` issue), but the σ-walk batch wiring tests in commits 2 & 3 use a custom signer that doesn't go through `bls.MultiVerify` — they run under `-race` freely.
- **B4 re-run**: confirm batch vs sequential numbers are unchanged for raw `bls.MultiVerify`.
- **New benchmark `BenchmarkResolve_LkGreaterThanZero_BatchVsSequential`** (planned, NOT landed — see §Status): end-to-end σ-walk timing comparing pre-F4 (sequential) and post-F4 (batch). Should show the ~1.5-1.8× speedup predicted by B4 at typical N.
- **Race-stress runner test** ([protocol/v2/ssv/runner/obft/race_safety_bridge_test.go](protocol/v2/ssv/runner/obft/race_safety_bridge_test.go) and twoab counterpart) — confirms no data races introduced. The L_k > 0 batch path triggers only under specific protocol states (L_0 quorum fail); the stress test exercises both happy and degraded paths.

## Race-detector issue with `bls.MultiVerify`

`bls.MultiVerify` at [bls-eth-go-binary@v1.29.1/bls/eth.go:32-33](https://github.com/herumi/bls-eth-go-binary/blob/v1.29.1/bls/eth.go#L32) does:

```go
msg := uintptr(unsafe.Pointer(&concatenatedMsg[0]))
rp := uintptr(unsafe.Pointer(&randVec[0]))
// ... msg += uintptr(msgSize * m) etc. then ...
C.blsMultiVerifySub(&e, &aggSig.v, &sigs[0].v, &pubs[0].v, (*C.char)(unsafe.Pointer(msg)), ...)
```

Storing a slice-pointer in `uintptr` and converting it back to `unsafe.Pointer` later is the [unsafe.Pointer pattern (4)](https://pkg.go.dev/unsafe#Pointer) that Go's `checkptr` (enabled with `-race`) considers invalid: the GC tracks `unsafe.Pointer`s but loses the relationship through a `uintptr`. Production builds (no `-race`, no `checkptr`) are unaffected because the slices are still live on the stack frame and the `unsafe.Pointer` reconversion + immediate C call is correct in practice — but the runtime check can't prove that.

Confirmed via direct repro: `go test -race -run TestMultiVerify_Fixture ./protocol/v2/obft/blsbackend/...` panics with:

```
fatal error: checkptr: pointer arithmetic result points to invalid allocation
... at bls/eth.go:83
```

**Mitigation:** tests that exercise `bls.MultiVerify` (directly or via the BLSSigner backend) gate themselves on `runtime/race.Enabled`. Production code is unaffected. The KyberSigner / StubSigner / proposerSigner tests don't trigger the issue (no path through `bls.MultiVerify`).

**Long-term:** upstream herumi could rewrite the inner loop using `unsafe.Add(ptr, msgSize*m)` instead of `uintptr` arithmetic, which `checkptr` accepts. Tracking this is out of F4 scope; for now we live with the `-race` skip.

This is not new in F4 — the existing `TestMultiVerify_Fixture` from B4 ([protocol/v2/obft/blsbackend/multiverify_bench_test.go:135](protocol/v2/obft/blsbackend/multiverify_bench_test.go:135)) already needs the skip but doesn't currently have it. The first commit adds the skip both to the new tests and retroactively to the B4 fixture.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| Batch verify returns true on a forged tuple due to weak random-linear-combination randomness. | herumi samples 64-bit randomness per sub-batch from `randVec`; security argument is standard. The forgery probability is ~2^-64 per attack attempt — the same level production already accepts for any single verify under the underlying pairing assumption. |
| Fall-back path fails to record Rule-4 evidence for a bad σ-walk tuple. | The sequential-fallback helper mirrors the pre-F4 per-sig loop exactly (same `recordRule4` dedup, same `EvidenceFakeEncryptedPresence` shape). Test `TestResolve_LkGreaterThanZero_BatchFailureFallback` asserts evidence still fires per failing tuple. |
| Cache populated on batch success that includes a tuple a future protocol change would want to reject. | Cache populate-on-batch-success is gated by `signer.VerifyPartialBatch` returning true. By contract, that's equivalent to N successful `VerifyPartial` calls — the F1 safety invariant ("populate only on verify-success") holds. |
| Interface change to `obft.Signer` breaks external implementations. | The interface lives in `protocol/v2/obft/`; the only implementations are the three in-tree backends (BLSSigner, KyberSigner, StubSigner) plus the proposerSigner wrapper. No external consumers. All four get implementations in commit 1. |
| Test suite gets harder to run under `-race`. | Only the BLSSigner direct tests skip under `-race`. The σ-walk wiring tests (commits 2 & 3) use a custom signer for the batch behaviour — they run normally under `-race`. The race-stress runner suite is unaffected (it doesn't directly exercise `bls.MultiVerify` outside of any cluster-state where L_k > 0 walks fire, which the stress suite exercises in degraded modes). |
| Per-msg signing-root translation in `proposerSigner.VerifyPartialBatch` duplicates work across N tuples that share the same V. | All callers of the batch method in F4 build batches at a single layer over a single V (the σ-walk's "many ops sign the same V" pattern). The wrapper still translates N times. F2 (cache signing-root per V on the proposerSigner) closes this; in F4 we accept the redundancy since it's still correctness-preserving and the wall-clock cost (≤ ~100 µs × N) is dwarfed by the BLS verify cost. The σ-walk batch's N is small (5-7 typical), so the worst case is ~600 µs of redundant translation per batch — already paid by the pre-F4 code in the same per-sig pattern. |
| Allocations in `VerifyPartialBatch` add GC pressure. | Per call: 1 concat buffer (n*32 bytes ≤ ~400 bytes typical), 1 `[]bls.Sign` (n*256 bytes), 1 `[]bls.PublicKey` (n*200 bytes). At n=6: ~3 KB / 3 allocs. Compared to N individual verifies (each 432 B / 2 allocs per B1 = N×432 B / 2N allocs), batch is ~50% fewer allocs for n ≥ 3 and saves the per-call overhead of deserialise inside `VerifyPartial`. Net allocs reduction. |
| Future call site batches over multiple V's via proposerSigner → cache desync if F2 lands later. | Outside F4 scope. F4's only callers are single-V σ-walk batches; no multi-V case exists today. Documenting the constraint in `VerifyPartialBatch`'s doc-comment ("each msgs[i] is independently translated by wrapper signers") makes any future change deliberate. |

## Sequencing

Three commits, all CI-green standalone:

1. **Commit 1 — Signer interface + 3 backend implementations + race-detector skip.** Adds the method to the interface; implements on BLSSigner (herumi `MultiVerify`), KyberSigner (sequential), StubSigner (sequential), proposerSigner (translate + delegate). Adds the `skipIfRace` helper and applies it to the new BLSSigner tests and retroactively to the existing `TestMultiVerify_Fixture`. ~250 lines net (mostly tests). Lowest risk; no behaviour change to consensus code.

2. **Commit 2 — base Instance σ-walk batch wiring.** Refactors the L_k > 0 walk in [base/phase3.go](protocol/v2/obft/base/phase3.go) to collect pending → batch → fall back to sequential on failure. Tests assert cache-hit bypass, batch happy-path, batch failure → Rule-4 attribution preserved. ~200 lines net.

3. **Commit 3 — twoab mirror.** Same refactor at [twoab/phase3.go](protocol/v2/obft/twoab/phase3.go). Same test set, twoab-flavored. ~200 lines net.

Each commit CI-green on its own. After commit 3, re-run the full consensustest stress matrix (`make stresstest`) to confirm the safety invariant holds across cluster sizes + attack modes. Re-run B4 (raw `bls.MultiVerify`) to confirm the per-call numbers; the planned end-to-end `BenchmarkResolve_LkGreaterThanZero_BatchVsSequential` was descoped (§Status).

## Open questions

(All resolved during the audit and this plan's drafting; documented here for traceability.)

- **Q1 — Does kyber-bls12381 have a `MultiVerify`-style primitive?** No. `kyber/sign/bls.BatchVerify` verifies one aggregated sig against many (pub, msg) pairs — not many individual sigs as a batch. KyberSigner falls back to sequential.
- **Q2 — Does the herumi `-race`/`checkptr` issue affect production?** No. Production builds run without `-race`/`-d=checkptr`. The issue is a Go runtime-check false-positive on a `uintptr` storage pattern the C-backed library uses; the underlying C call is correct. Tests skip under `-race`; bench runs (which don't enable `-race`) work fine.
- **Q3 — Does F4 conflict with F1's verify-cache?** No — F4 layers on top. Cache hits bypass the batch entirely; only cache-miss entries enter the pending list. The batch's success populates the cache for each tuple, so subsequent Resolve calls warm-hit per-entry as before.
- **Q4 — Should the runner-layer `Verifier` paths also batch?** Not in F4. `Verifier.VerifyCommitNRPartials` (base) and `Verifier.VerifyCommit` / `VerifyValueMsg` / `VerifyNoValueMsg` (twoab) all loop verifies, but their TagSigner is `KyberSigner` per the runner construction — kyber has no batch primitive, so the batch falls back to sequential anyway. A future kyber random-linear-combination implementation would close this; out of F4 scope. The V-side `VerifyPhase1Bundle` and `VerifyCertificate` each do a single verify per call — no batching opportunity.
- **Q5 — Should batching consider tuples ACROSS multiple Resolve attempts (slot-level batching)?** No. Resolve is per-Instance, single-threaded under `r.instanceMu`. Cross-Resolve batching would require buffering verifies across opportunistic-Resolve fires — significant complexity for marginal benefit since F1's cache already collapses repeated work.

None of the above blocks implementation.

## Status

- Plan: this document.
- Implementation: **complete**, landed across three CI-green commits:
  - `0e9481412` — Signer interface extension + 4 backend implementations
    (BLSSigner via herumi `MultiVerify`; KyberSigner / StubSigner / both
    proposerSigner wrappers as sequential / translate-then-delegate);
    `countingSigner` test mock picked up the pass-through; `skipIfRace`
    helper + build-tag pair gated the BLSSigner-batch tests AND was
    applied retroactively to the existing `TestMultiVerify_Fixture`.
  - `6b32dfef1` — base/phase3.go σ-walk wired to collect cache-miss
    tuples into a single batch with sequential fallback for Rule-4
    attribution. 5 helper-level tests in `phase3_batch_test.go`.
  - `ad3b0eb42` — twoab/phase3.go mirror, splitting
    `extractSigmaFromEntries` into a classify helper that pushes pending
    tuples to the batch entry-point in `aggregatePeerLayerEntries`.
    5 mirror tests in `twoab/phase3_batch_test.go`.
- Follow-up cleanup commit lifts `proposer_signer_bench_test.go::makeBenchV`
  to `testing.TB` so it's reusable, adds 4 direct unit tests for each of
  the two `proposerSigner.VerifyPartialBatch` wrappers covering happy-path
  delegation, bad-V short-circuit, length mismatch, and empty batch. The
  tests use a recording mock inner signer so the wrapper-translate logic
  is exercised without touching `bls.MultiVerify` (no `-race` skip needed
  for the wrapper paths).
- Benchmarks: B4 already landed in [9644c8a12](https://github.com/ssvlabs/ssv/commit/9644c8a12).
  The planned `BenchmarkResolve_LkGreaterThanZero_BatchVsSequential` was
  not added — direct benching would require manually staging an L_k>0
  Resolve walk against a control signer, which adds substantial test
  infrastructure for a marginal signal beyond B4's raw `MultiVerify`
  speedup. The end-to-end win is observable instead via `make stresstest`
  variance against the F1+F3+F5 baseline. Out of F4 scope; revisit if
  profile data shows the σ-walk dominating a hot slot.
