# OBFT F1 + F5 Implementation Plan — skip redundant BLS verifies

Concrete implementation plan for the two findings that together account for **~88 ms/slot at n=7, K=4** — the dominant share of the audit's total saving. Both rest on the same principle: **trust the upstream insertion-time BLS verify; don't repeat it in the consensus-critical path**.

See [OBFT-PERFORMANCE-AUDIT-PLAN.md](OBFT-PERFORMANCE-AUDIT-PLAN.md) for the broader audit context, the per-finding investigations, and the open-questions analysis that informs the safety risks below.

## Goal

| Finding | Mechanism | Saving |
|---|---|---|
| **F5** | Gate `Instance.verifyCommitNRPartials` behind a `SkipNRPartialReverify` config flag; runner layer sets it true (the production `Verifier.VerifyCommitNRPartials` already did the verifies upstream); consensustest leaves default false. | ~18 ms/slot |
| **F1** | Add a per-Instance verify-cache map. Populate at every existing insertion-time BLS-verify success site (Phase-1 retain, peer L_0 onion observe, witness harvest, first L_k>0 decrypt+verify). Resolve consults the cache before each `signer.VerifyPartial`; cache hit skips the verify. | ~70 ms/slot |

Combined: **~88 ms/slot wall-clock saving** (benched). Both fixes mirror identically into the 2abOBFT package.

## Safety invariant

The single invariant the implementation must preserve:

> Every partial signature that contributes to σ-quorum, NR-quorum, certificate aggregation, or evidence emission inside `Resolve` was BLS-verified at least once against its claimed `(operator pub-share, message)` tuple — either at insertion time or in `Resolve` itself.

Stated negatively: a partial that bypasses BLS verification on all paths must never end up in a `sigGroup` reaching qV, in an `addToGroup` count, in a `Certificate.Signature` aggregation, or as the trigger for slashing evidence.

The fix satisfies this by:

- **F5**: relying on the runner-layer `Verifier.VerifyCommitNRPartials` running before `ObserveCommit` is called. The skip only fires in code paths where that upstream verify is guaranteed. The consensustest framework does NOT route through `Verifier` (confirmed in [§Q-Open-1](OBFT-PERFORMANCE-AUDIT-PLAN.md#q-open-1-does-consensustests-path-go-through-the-production-verifier)), so the config flag MUST default false; only the runner construction sets it true.
- **F1**: cache populates exclusively from "I just BLS-verified this and the verify returned true" sites; never from "I assume this is fine". Cache HIT proves a prior verify happened. Cache MISS falls through to the full verify path unchanged.

If the cache key collides between distinct partials (different bytes hashing to the same key), the worst case is a false-positive verify-skip on a malformed partial — that's a safety violation. Key choice must therefore be collision-resistant. We use `sha256(partial-sig-bytes)` (32-byte key) which gives 2^128 collision resistance — comfortably beyond practical concern.

## Design overview

### F5 — gate verifyCommitNRPartials behind a config flag

The simplest possible change. The verify call already lives in `Instance.ObserveCommit` at [base/phase2.go:324](protocol/v2/obft/base/phase2.go:324). Wrap it in an `if !i.cfg.SkipNRPartialReverify` guard.

Two code touches:

- Add `SkipNRPartialReverify bool` to `Config` ([obft/base/types.go:85](protocol/v2/obft/base/types.go:85)).
- Wrap the call at [base/phase2.go:324](protocol/v2/obft/base/phase2.go:324) with the flag check.

Two consumer touches:

- Runner construction in [runner/obft/controller.go](protocol/v2/ssv/runner/obft/controller.go) — wherever `NewInstance` is called — set `SkipNRPartialReverify: true`.
- (No consensustest touch — defaults to false, preserving the existing in-Instance verify.)

### F1 — per-Instance verify-cache for σ-side partials

A `map[verifyCacheKey]struct{}` lives on `*Instance`. Populated at every insertion-time verify-success site; consulted at every verify in `Resolve`.

**Cache key:**

```go
type verifyCacheKey struct {
    op          OperatorID
    layer       int
    valueRoot   [32]byte // sha256(value)
    partialRoot [32]byte // sha256(partial sig bytes)
}
```

Both `valueRoot` and `partialRoot` are load-bearing disambiguators:

- `partialRoot` makes byzantine equivocation safe: under the same (op, layer) emitting distinct partial-byte sequences, each caches independently.
- `valueRoot` makes the cache safe against the cross-V leakage attack: byzantine emits two L_k>0 onion entries from the same (op, layer) — entry A claims `V_a` and decrypts to σ_a (the leader's real σ on V_a), entry B claims `V_b` but its ciphertext decrypts to the same σ_a bytes (IBE allows distinct ciphertexts that decrypt to the same plaintext). Without `valueRoot` in the key, walking entry A first populates `(op, layer, sha256(σ_a))`; walking entry B then cache-hits and skips verify, admitting σ_a to V_b's σ-pool incorrectly. BLS partials bind to a single (msg, share) pair mathematically, so a cache hit on `(op, layer, v, σ)` is the only safe disambiguator that doesn't admit cross-V leakage. Caught during F1's post-commit self-review; the original plan had only `partialRoot`.

Using sha256 of value+partial avoids any wire-format coupling — the bytes themselves are the canonical identifiers.

**Cache populate sites** — every line where a BLS verify currently returns true:

1. [base/phase1.go:167](protocol/v2/obft/base/phase1.go:167) — Phase-1 bundle retention: `i.signer.VerifyPartial(leaderShare, b.Value, b.LeaderSigma)`. Key: `(leaderID, layer, sha256(b.LeaderSigma))`.
2. [base/phase2.go:859](protocol/v2/obft/base/phase2.go:859) — `peerSigmaAtL0Verdict`: `i.signer.VerifyPartial(pubShare, el.Value, el.Ciphertext)`. Key: `(op, 0, sha256(el.Ciphertext))`.
3. [base/phase2.go:692](protocol/v2/obft/base/phase2.go:692) — `harvestWitness`: `i.signer.VerifyPartial(pubShare, v, w.Sigma)`. Key: `(w.Leader, w.Layer, sha256(w.Sigma))`.
4. [base/phase3.go:247](protocol/v2/obft/base/phase3.go:247) — L_k>0 post-decrypt verify (the one verify in Resolve that ISN'T redundant — it's the first opportunity). Populate cache on success so the *next* Resolve call's lookup hits. Key: `(op, layer, sha256(partial))` where partial is the decrypted bytes.

**Cache check sites** — every verify in Resolve:

1. [base/phase3.go:181](protocol/v2/obft/base/phase3.go:181) — leader bundle σ_V re-verify. Always redundant; cache should always hit after first observation.
2. [base/phase3.go:247](protocol/v2/obft/base/phase3.go:247) — peer-onion entry verify. At L_0 the cache hits (populated at observation); at L_k>0 the cache miss-then-populate keeps the verify but ensures subsequent Resolve calls hit.

Algorithmic structure for the check:

```go
key := verifyCacheKey{op: opID, layer: layer, partialRoot: sha256.Sum256(partialBytes)}
if _, ok := i.verifiedPartials[key]; ok {
    // Cached verify — safe to skip the BLS call.
} else if !i.signer.VerifyPartial(pubShare, value, partial) {
    // Verify failed — usual error path (Rule 4 evidence, etc.).
} else {
    // Verify passed — populate cache so subsequent Resolves skip.
    i.verifiedPartials[key] = struct{}{}
}
```

## Implementation — file by file

### Commit 1: F5 — config flag + gate

**[protocol/v2/obft/base/types.go](protocol/v2/obft/base/types.go)** — add field to `Config`:

```go
type Config struct {
    // ... existing fields ...

    // SkipNRPartialReverify, when true, skips Instance.verifyCommitNRPartials
    // in ObserveCommit. Safe to set true ONLY when the caller guarantees an
    // upstream BLS verify of every NR partial in the Commit — specifically,
    // the runner-layer Verifier.VerifyCommitNRPartials before dispatch. The
    // consensustest framework drives Instance directly without that upstream
    // verify and MUST leave this false; production runner construction sets
    // it true. See docs/OBFT-PERFORMANCE-AUDIT-PLAN.md F5.
    SkipNRPartialReverify bool
}
```

**[protocol/v2/obft/base/phase2.go:324](protocol/v2/obft/base/phase2.go:324)** — wrap the call:

```go
if !i.cfg.SkipNRPartialReverify {
    if err := i.verifyCommitNRPartials(c); err != nil {
        return err
    }
}
```

**Runner construction** — where the production `obft.Config` is built (look in [protocol/v2/ssv/runner/obft/controller.go](protocol/v2/ssv/runner/obft/controller.go) for `NewInstance` calls):

```go
cfg := obftcore.Config{
    // ... existing fields ...
    SkipNRPartialReverify: true,
}
```

**Tests** — extend [protocol/v2/obft/base/phase2_test.go](protocol/v2/obft/base/phase2_test.go) (or wherever ObserveCommit tests live):

- `TestObserveCommit_DefaultStillVerifiesNRPartials` — feed a Commit with one tampered NR partial; assert ObserveCommit returns the verify error (current behaviour preserved).
- `TestObserveCommit_SkipNRPartialReverify_BypassesVerify` — same Commit, `Config.SkipNRPartialReverify=true`; assert ObserveCommit accepts it (intent: in production the upstream Verifier would have rejected it; here we just confirm the flag actually skips the call).

### Commit 2: F1 — verify-cache + check sites

**[protocol/v2/obft/base/instance.go](protocol/v2/obft/base/instance.go)** — add field + init:

```go
type Instance struct {
    // ... existing fields ...

    // verifiedPartials caches "partial X was BLS-verified at insertion time"
    // so Resolve can skip the redundant re-verify. Populated by every site
    // where signer.VerifyPartial returned true on a partial that will later
    // appear in Resolve's σ-walk (Phase-1 retention, peer L_0 onion observe,
    // witness harvest, first L_k>0 decrypt+verify inside Resolve itself).
    // Read in Resolve; the verify is skipped on cache hit. Single-threaded
    // by Instance's controller-mu serialization (Q-Open-2), so no locking.
    verifiedPartials map[verifyCacheKey]struct{}
}

type verifyCacheKey struct {
    op          OperatorID
    layer       int
    partialRoot [32]byte // sha256 of the partial sig bytes
}
```

Initialize in `NewInstance`:

```go
i.verifiedPartials = make(map[verifyCacheKey]struct{}, 4*cfg.K()*len(cfg.Operators))
```

(Capacity hint = "every op at every layer might emit, plus a few extras for equivocation". Cheap upper bound.)

Helper methods:

```go
// markVerified records that (op, layer, value, partial) passed BLS verify,
// so Resolve can skip the re-verify next time it walks this entry. Value-
// binding is load-bearing — see verifyCacheKey doc for the safety argument.
func (i *Instance) markVerified(op OperatorID, layer int, value, partial []byte) {
    i.verifiedPartials[verifyCacheKey{
        op:          op,
        layer:       layer,
        valueRoot:   sha256.Sum256(value),
        partialRoot: sha256.Sum256(partial),
    }] = struct{}{}
}

// alreadyVerified reports whether (op, layer, value, partial) has been
// BLS-verified before. Cache HIT lets Resolve skip a redundant
// signer.VerifyPartial call; cache MISS falls through to the full verify
// (and the caller populates on success via markVerified).
func (i *Instance) alreadyVerified(op OperatorID, layer int, value, partial []byte) bool {
    _, ok := i.verifiedPartials[verifyCacheKey{
        op:          op,
        layer:       layer,
        valueRoot:   sha256.Sum256(value),
        partialRoot: sha256.Sum256(partial),
    }]
    return ok
}
```

**Cache population sites:**

[base/phase1.go:167](protocol/v2/obft/base/phase1.go:167) — Phase-1 bundle retention. After the existing `if !i.signer.VerifyPartial(...) { ... }` block:

```go
if !i.signer.VerifyPartial(leaderShare, b.Value, b.LeaderSigma) {
    // ... existing error / evidence path ...
}
i.markVerified(b.OperatorID, b.Layer, b.Value, b.LeaderSigma)
```

[base/phase2.go:859](protocol/v2/obft/base/phase2.go:859) — `peerSigmaAtL0Verdict`. After the L_0 verify success path:

```go
if !i.signer.VerifyPartial(pubShare, el.Value, el.Ciphertext) {
    return l0SigmaCryptoFake
}
i.markVerified(op, 0, el.Value, el.Ciphertext)
```

[base/phase2.go:692](protocol/v2/obft/base/phase2.go:692) — `harvestWitness`. After the witness verify success:

```go
if !i.signer.VerifyPartial(pubShare, v, w.Sigma) {
    return
}
i.markVerified(w.Leader, w.Layer, v, w.Sigma)
```

**Cache check sites:**

[base/phase3.go:181](protocol/v2/obft/base/phase3.go:181) — leader bundle σ_V re-verify:

```go
for _, b := range i.bundles[layer][leaderID] {
    if i.alreadyVerified(leaderID, layer, b.Value, b.LeaderSigma) ||
        i.signer.VerifyPartial(pubShare, b.Value, b.LeaderSigma) {
        if !i.alreadyVerified(leaderID, layer, b.Value, b.LeaderSigma) {
            i.markVerified(leaderID, layer, b.Value, b.LeaderSigma)
        }
        addToGroup(&groups, b.Value, leaderID, b.LeaderSigma)
    }
}
```

(The double-lookup is ugly; consider refactoring to a helper that returns `(verified bool, cachedHit bool)` to populate only on first-time success. Or simpler: re-lookup is cheap — sha256 + map lookup is sub-microsecond.)

A cleaner helper:

```go
// verifyOrCached returns true if (op, layer, value, partial) is known-
// verified (either cache hit, or fresh BLS verify succeeds and cache
// populated). Returns false only if a fresh verify failed.
func (i *Instance) verifyOrCached(op OperatorID, layer int, pubShare, value, partial []byte) bool {
    if i.alreadyVerified(op, layer, value, partial) {
        return true
    }
    if !i.signer.VerifyPartial(pubShare, value, partial) {
        return false
    }
    i.markVerified(op, layer, value, partial)
    return true
}
```

Then both check sites become a single call.

[base/phase3.go:247](protocol/v2/obft/base/phase3.go:247) — peer-onion entry verify. Same pattern, but layer>0 entries are populated on first Resolve walk (their partial bytes weren't available at observation):

```go
if !i.verifyOrCached(opID, layer, i.pubKeyShares[opID], el.Value, partial) {
    if layer > 0 {
        // ... Rule 4 evidence ...
    }
    continue
}
addToGroup(&groups, el.Value, opID, partial)
```

**Tests** — extend [protocol/v2/obft/base/phase3_test.go](protocol/v2/obft/base/phase3_test.go) (and any Resolve test file):

- `TestResolve_CachedVerifyOnSecondCall` — observe a Phase-1 bundle (forces a cache populate at retention); spy on the Signer's `VerifyPartial` call count; call `Resolve()` once and confirm zero VerifyPartial calls for that bundle's σ_V (cache hit on the leader-bundle path). Call Resolve again, still zero.
- `TestResolve_LayerKGreaterThanZero_VerifiesOnceCachesAfter` — set up an Instance with a peer-onion entry at L_k>0 that decrypts to a valid partial. First Resolve: 1 VerifyPartial call. Second Resolve: 0 (cache hit).
- `TestResolve_FailedVerifyNotCached` — feed an entry that decrypts to garbage; first Resolve: verify fails, Rule 4 evidence fires. Second Resolve: another verify fails (cache should NOT have populated on failure).
- `TestResolve_EquivocationDistinctPartialsCachedIndependently` — same (op, layer, value), two distinct partial-byte sequences. Both verify independently; cache distinguishes by `sha256(partial)`. Verifies the partialRoot disambiguator is load-bearing.
- `TestResolve_ValueBoundCacheKey_NoCrossVLeak` — populate cache for `(op, layer, V_a, σ)`; assert a lookup for `(op, layer, V_b, σ)` MISSES. Verifies the `valueRoot` disambiguator blocks the cross-V leakage attack (entry A claims V_a with σ_a, entry B claims V_b with bytes that decrypt to the same σ_a). Without this, a byzantine could admit σ_a to V_b's σ-pool via a phantom cache hit.

**Sanity test for the safety invariant** — `TestResolve_TamperedPartialDoesNotPassViaCache`:
- Observe a Commit, populate cache for one of the partials.
- Subsequent Resolve sees a malformed partial (different bytes, same op+layer+value) — confirm it goes through the full verify path (cache miss because partialRoot differs) and is rejected.

### Mirroring into 2abOBFT

Both fixes mirror identically into [protocol/v2/obft/twoab/](protocol/v2/obft/twoab/). The twoab Instance has the same shape — Resolve at [twoab/phase3.go](protocol/v2/obft/twoab/phase3.go), the same insertion-time verify sites in twoab phase1/2a/2b. The verify-cache + flag-gate carries 1:1.

Suggested order: land base first (commits 1+2), then mirror to twoab (commit 3), so we don't have an asymmetric state.

## Testing strategy

Unit tests listed inline per file above. Additionally:

- **Existing `consensustest` stress suite** must continue to pass with `SkipNRPartialReverify=false` (the default), exercising the in-Instance verify path that catches adapter bugs. Run after each commit.
- **Existing runner tests** (with `SkipNRPartialReverify=true` set in the runner construction) must pass — these confirm the runner-layer Verifier upstream of Instance is correctly invoked and the skip-verify path doesn't admit anything the Verifier would have rejected.
- **Benchmark re-run**: after F1+F5 land, re-run B1/B2/B3/B4 to confirm baseline-numbers unchanged (the fixes should not change *per-verify* cost, only call count). Then add a new benchmark `BenchmarkResolve_CachedVsCold` that times a second Resolve call against the first — should show the predicted ~10× speedup on the leader-bundle + L_0 onion paths.
- **Race-stress runner test** ([protocol/v2/ssv/runner/obft/race_safety_bridge_test.go](protocol/v2/ssv/runner/obft/race_safety_bridge_test.go) and twoab counterpart) — confirms the change doesn't introduce data races under `-race -count=N`.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| Cache populate misses an insertion-time verify path → Resolve skips a verify that should have run → bad partial admitted. | Exhaustive enumeration of populate sites (4 listed; verify nothing else verifies σ-partials). Code review with the safety invariant in hand. |
| F5's `SkipNRPartialReverify=true` set in a path that doesn't run the upstream Verifier first. | Default false. Only the runner construction (verified in [Q-Open-1](OBFT-PERFORMANCE-AUDIT-PLAN.md#q-open-1-does-consensustests-path-go-through-the-production-verifier)) sets true. Consensustest framework leaves default. New tests assert default-false stays at default-false. |
| Cache map grows unbounded under byzantine flood (many distinct partials at same (op, layer)). | Cap bounded by `MaxRetainedPerOpLayer × K × n` — at n=7, K=4, max-retained=2 → at most ~56 entries per Instance lifetime. Instance is per-slot scoped, so the map is GC'd at slot end. No explicit eviction needed. |
| Concurrent access to the cache. | Instance is single-threaded under `r.instanceMu` per [Q-Open-2](OBFT-PERFORMANCE-AUDIT-PLAN.md#q-open-2-is-resolve-ever-called-concurrently-on-the-same-instance). No locks needed. New tests can run under `-race` for confirmation. |
| Wire-format coupling (adding fields to `EncryptedLayer` / `Phase1Bundle`). | We don't — the cache is a separate `Instance` field, not a struct extension. Wire format unchanged. |
| F5's flag could be confused with a more aggressive "skip all verify" flag in future. | Name is specific: `SkipNRPartialReverify`. Doc-comment explicitly says NR-partial only. Code review enforces. |
| sha256 collision on `partialRoot`. | 2^128 collision resistance — not a practical concern. Standard cryptographic identifier. |

## Sequencing

Two commits in `obft/base/`, one mirror commit in `obft/twoab/`:

1. **Commit 1 — F5: config flag + gate.** Add `SkipNRPartialReverify` to `Config`; wrap the verify call in `ObserveCommit`; set the flag true in the runner construction; tests for both paths. ~50 lines net. Lowest risk; biggest single-finding saving (~18 ms/slot, 100% reliable).

2. **Commit 2 — F1: per-Instance verify-cache.** Add field + helpers to `Instance`; populate at 3 observation sites; check at 2 Resolve sites; tests covering the invariant + equivocation + L_k>0 first-walk. ~150-200 lines net. Higher risk (touches the consensus-critical Resolve path); biggest absolute saving (~70 ms/slot).

3. **Commit 3 — mirror to 2abOBFT.** Same shape in [obft/twoab/](protocol/v2/obft/twoab/) — flag in twoab `Config`, gate, cache, populate sites, check sites, tests. ~250 lines net.

Each commit CI-green on its own. After commit 3, re-run the full consensustest stress matrix (`make stresstest`) to confirm the safety invariant holds across all scenarios + cluster sizes. Re-run benchmarks (B1-B4) to confirm baseline-call costs unchanged.

## Open questions

(All resolved during the audit; documented here for traceability.)

- **Q1 — Does consensustest go through the runner-layer Verifier?** No (Q-Open-1). So the `SkipNRPartialReverify` flag defaults false and consensustest leaves it that way.
- **Q2 — Is Resolve called concurrently on the same Instance?** No (Q-Open-2). Verify-cache map needs no locking.
- **Q3 — Does the runner-layer Verifier cache verified-partial bits already?** No (Q-Open-4). F1 must add its own cache; no cross-layer cache desync risk.

None of the above blocks implementation.

## Status

- Plan: this document.
- Implementation: not started — next step.
- Benchmarks for regression detection: B1-B4 already landed in [9644c8a12](https://github.com/ssvlabs/ssv/commit/9644c8a12).
