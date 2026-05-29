# OBFT Validation-Layer Verifier Caching — implementation plan

Concrete plan for the follow-up surfaced during F2's lifetime survey: the message-validation layer constructs a fresh `Verifier` on **every inbound OBFT/2abOBFT envelope**, which re-pays the share parse and — more importantly — defeats the F2 (signing-root) and F3 (kyber-pubkey) caches that only help when the signer instances persist across calls.

See [OBFT-PERFORMANCE-AUDIT-PLAN.md](OBFT-PERFORMANCE-AUDIT-PLAN.md) §Status "Remaining → Validation-layer Verifier caching" for where this sits. This is **not** an original F-finding; the per-envelope construction wasn't probed until F2's survey of `proposerSigner` lifetimes.

> ⚠️ **Security boundary.** Unlike F1-F5, this touches the message-validation DoS-prevention layer. A stale cached Verifier — one whose committee/pub-shares no longer match the validator's current share — would verify inbound consensus messages against the **wrong** operator pub-key shares: it could accept signatures from operators no longer in the committee, or reject signatures from newly-added ones. The invalidation strategy is therefore a correctness/safety decision, not just a performance knob, and is the central question this plan resolves.

## Where the cost is

Both validation entry points build a Verifier per envelope:

- [obft_validation.go:128](message/validation/obft_validation.go:128) — `obftadapter.NewVerifierFromShare(&share.Share, nil, mv.netCfg.Beacon)`
- [twoab_validation.go:93](message/validation/twoab_validation.go:93) — `twoabadapter.NewVerifierFromShare(&share.Share, nil, mv.netCfg.Beacon)`

Each `NewVerifierFromShare` ([runner/obft/verifier.go:29](protocol/v2/ssv/runner/obft/verifier.go:29), twoab mirror) builds:

- A `PubKeyShares map[OperatorID][]byte` — copied from `share.Committee` (n entries, ~48 B each).
- A V-side `proposerSigner` wrapping `blsbackend.New(nil)` — with a **fresh, empty F2 signing-root cache** (the shared `proposersig.Cache`).
- A tag-side `blsbackend.NewKyberSigner(nil)` — with a **fresh, empty F3 pubCache**.
- `ClusterPubKey` copy + (obft only) a `LeaderForLayer` closure over the committee.

The construction itself is cheap (µs). The real waste is downstream, on the verify calls that follow:

| Envelope kind | Verify work that re-pays a cold cache |
|---|---|
| Phase1Bundle | 1 V-side `VerifyPartial` → 1 cold `signingRootFor` (~100 µs, 336 allocs — F2 would cache) |
| Commit (obft) | K-1 tag-side `VerifyPartial` on NR partials → K-1 cold kyber pubkey parses (~114 µs each — F3 would cache) + witness σ checks |
| Value/NoValue/Commit (twoab) | NR-plaintext `VerifyPartial` per LayerEntry → cold kyber parses; ValueMsg also 1 cold `signingRootFor` |
| Certificate | 1 V-side `VerifyAggregate` → 1 cold `signingRootFor` |

The validation layer sees **every** gossiped OBFT envelope for **every** validator the node tracks — a higher aggregate volume than the runner's own Instance (which only processes envelopes routed to its active slots). So persisting the Verifier (and with it the warm F2 + F3 caches) across a validator's envelopes is plausibly a larger aggregate win than F2's runner-side win, though it's spread across many validators rather than concentrated in one slot.

**Honest sizing caveat:** the per-validator benefit is bounded by how many envelopes a validator's proposal generates in its hot window (~2(n-1) + certs). The aggregate win scales with the number of *concurrently-proposing* validators on the node. No micro-benchmark exists yet; B6 (below) will measure cold-vs-warm Verifier reuse before/after.

## The invalidation problem

A `Verifier` is a snapshot of `(committee pub-shares, validator pubkey)`. From the share-lifecycle investigation:

1. **`validatorStore.Validator(pubkey)` returns the live store pointer**, not a copy ([validatorstore.go:123](registry/storage/validatorstore.go:123) → `byPubKey`). Same pointer across calls until the share is replaced.
2. **`sharesStorage.Save` stores whatever pointer the caller passes** ([shares.go:282](registry/storage/shares.go:282): `s.shares[key] = share`). The store never mutates an existing share object itself.
3. **A live validator's committee + share-pubkeys are immutable.** A `ValidatorAdded` event for an already-registered validator (same owner) is a **no-op** — it does not rebuild or re-Save the share ([handlers.go:190-216](eth/eventhandler/handlers.go:190)). Changing a validator's committee requires `ValidatorRemoved` (delete from store) + `ValidatorAdded` (fresh `*SSVShare` via `handleShareCreation` → new pointer).
4. **No generation counter / share version exists.** `CommitteeID` is derived from operator IDs only ([ssvshare.go ComputeCommitteeID](protocol/v2/types/ssvshare.go)), so it would *not* change if the same operator set reshared with new SharePubKeys — making it unsafe as the sole invalidation key.

**Consequence:** the Verifier-relevant fields (`Committee`, `ValidatorPubKey`) change only via remove+re-add, which produces a new share pointer. So **pointer identity is a correct invalidation signal today** — but it depends on a non-local invariant in `eth/eventhandler` that a future refactor (e.g. switching the update path to in-place committee mutation) could silently break, turning this perf cache into a consensus-safety bug.

## Recommended design: TTL-bounded cache + content fingerprint

Two independent mechanisms, each handling one concern:

- **Memory bound → TTL cache.** Store the cache as a `ttlcache.Cache[string, *cachedVerifier]` on `messageValidator`, mirroring the existing `states` and `validationLockCache` ([validation.go:63,70](message/validation/validation.go:63)). TTL = `maxStoredSlots × SlotDuration` (= `(SlotsPerEpoch + LateSlotAllowance)` slots ≈ ~7 min), same as `states`. A validator that stops gossiping has its Verifier evicted automatically. Bounds memory to ~active-validator-count Verifiers.

- **Correctness → content fingerprint.** The cache value carries a `fingerprint [32]byte` = `sha256` over the exact fields the Verifier depends on: each committee member's `(Signer, SharePubKey)` (sorted by Signer for determinism) ‖ `ValidatorPubKey`. On every lookup, recompute the fingerprint from the *current* share and compare. Match → reuse the cached Verifier. Mismatch → rebuild + replace. This is **locally correct regardless of how the eventhandler mutates shares** — pointer swap, in-place mutation, or anything else — because validity is re-derived from the live share content, not from an external invariant.

Why both: TTL alone leaves a staleness window (a remove+re-add within the TTL could serve a stale Verifier for up to ~7 min — unacceptable for a security boundary). Fingerprint alone is correct but unbounded over node lifetime. Together: correct *and* bounded.

The fingerprint costs ~1-5 µs (a few sha256 over n×48 B + 48 B) per envelope — negligible against the ~100-340 µs of cold cache work it unlocks, and it removes the fragile cross-package coupling that pointer-identity would introduce.

### Sketch

```go
// message/validation/verifier_cache.go (new)

type cachedOBFTVerifier struct {
    fingerprint [32]byte
    verifier    *obftcore.Verifier
}

// on messageValidator:
obftVerifiers  *ttlcache.Cache[string, *cachedOBFTVerifier]
twoabVerifiers *ttlcache.Cache[string, *cachedTwoabVerifier]

func shareVerifierFingerprint(share *spectypes.Share) [32]byte {
    h := sha256.New()
    members := append([]*spectypes.ShareMember(nil), share.Committee...)
    sort.Slice(members, func(i, j int) bool { return members[i].Signer < members[j].Signer })
    var idbuf [8]byte
    for _, m := range members {
        binary.BigEndian.PutUint64(idbuf[:], uint64(m.Signer))
        h.Write(idbuf[:])
        h.Write(m.SharePubKey)
    }
    h.Write(share.ValidatorPubKey[:])
    var out [32]byte
    h.Sum(out[:0])
    return out
}

func (mv *messageValidator) obftVerifierFor(share *ssvtypes.SSVShare) (*obftcore.Verifier, error) {
    key := string(share.ValidatorPubKey[:])
    fp := shareVerifierFingerprint(&share.Share)
    if item := mv.obftVerifiers.Get(key); item != nil {
        if cv := item.Value(); cv != nil && cv.fingerprint == fp {
            return cv.verifier, nil // hit — committee unchanged
        }
        // fingerprint mismatch → committee changed; fall through to rebuild
    }
    v, err := obftadapter.NewVerifierFromShare(&share.Share, nil, mv.netCfg.Beacon)
    if err != nil {
        return nil, err
    }
    mv.obftVerifiers.Set(key, &cachedOBFTVerifier{fingerprint: fp, verifier: v}, ttlcache.DefaultTTL)
    return v, nil
}
```

`validateOBFTMessage` then calls `mv.obftVerifierFor(share)` instead of `obftadapter.NewVerifierFromShare(...)` directly; `validateTwoabMessage` the twoab twin.

### Concurrency

The validation pool calls `Validate` concurrently across goroutines. `jellydator/ttlcache` Get/Set are mutex-safe. The get→fingerprint-check→maybe-rebuild→set sequence is a benign read-modify-write: two goroutines that both miss will both build an equivalent Verifier and the last `Set` wins — wasteful but correct (identical Verifiers; the F2/F3 sub-caches inside each are independently valid). No extra locking needed; matches F2/F3's double-checked-but-racy-tolerant pattern.

One subtlety: once a Verifier is shared from the cache, **multiple validation goroutines call its methods concurrently**. That is already required to be safe — the production path shares a single Verifier per envelope today only because each envelope gets its own, but the *runner* already shares one signer across goroutines (Q-Open-2 / F3's concurrency note). F2 made the proposer signing-root cache (`proposersig.Cache`) RWMutex-safe and F3 made `KyberSigner.pubCache` RWMutex-safe precisely for shared concurrent use, so the cached Verifier is concurrency-ready. This must be explicitly re-confirmed in review (it's the load-bearing reason this is safe to share).

## Alternatives considered

| Option | Correctness | Memory | Coupling | Verdict |
|---|---|---|---|---|
| **TTL + fingerprint** (recommended) | Locally correct, no staleness window | Bounded by TTL | None | Robust; ~1-5 µs/envelope fingerprint overhead |
| Pointer identity + TTL | Correct *today*; depends on eventhandler "new object on committee change" invariant | Bounded by TTL | Fragile cross-package | Cheaper (O(1) compare) but unsafe to rely on for a security boundary |
| TTL only | Up-to-TTL staleness window on reshare | Bounded | None | Rejected — staleness window unacceptable at a security boundary |
| No cache (status quo) | Trivially correct | None | None | The thing we're trying to fix |

## Implementation — commit plan

Single commit (it's contained to `message/validation/`):

1. **New file `message/validation/verifier_cache.go`**: the two `cached*Verifier` types, `shareVerifierFingerprint`, and the `mv.obftVerifierFor` / `mv.twoabVerifierFor` helpers + the two `ttlcache` fields and their init in `New` (+ `go cache.Start()` for expiry, mirroring `states`).
2. **Swap the two call sites** in `obft_validation.go` / `twoab_validation.go` to use the helpers.
3. **Tests** `verifier_cache_test.go`:
   - miss-then-hit: same share → second call returns the *same* `*Verifier` pointer (cache hit).
   - committee change → fingerprint mismatch → new `*Verifier` returned (no stale reuse). Build two shares with same ValidatorPubKey but different committee SharePubKeys; assert the second lookup rebuilds.
   - distinct validators → distinct entries.
   - concurrent lookups on the same share → race-clean (run under `-race`), converge to a usable Verifier.
   - fingerprint determinism: committee member order in the slice doesn't change the fingerprint (sort is load-bearing).
   - end-to-end: an inbound envelope that previously verified still verifies through the cached path (wire it through `validateOBFTMessage` with a crafted share + envelope, both variants).
4. **B6 benchmark** (optional, in the same commit): cold `NewVerifierFromShare` + verify vs warm cached Verifier + verify, to quantify the unlocked F2/F3 savings.

No change to `NewVerifierFromShare`, the `Verifier` types, or any protocol-layer code. The cache is purely a validation-layer memoisation in front of the existing constructor.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| **Stale Verifier accepts wrong-committee sigs** (the core safety risk). | Content fingerprint re-derived from the live share every lookup; mismatch forces rebuild. Locally correct, independent of eventhandler behavior. Dedicated test: committee-change → rebuild. |
| Cached Verifier shared across goroutines isn't concurrency-safe. | F2 (signing-root cache, `proposersig.Cache`) + F3 (pubCache) already RWMutex-guarded for shared use; `PubKeyShares`/`ClusterPubKey` are read-only after construction. Re-confirmed in review; `-race` test on concurrent lookups + verifies. |
| Unbounded memory over node lifetime. | `ttlcache` with `maxStoredSlots` TTL + background expiry, identical to the existing `states` cache. Bounded by active-validator count. |
| Fingerprint collision admits a stale Verifier. | sha256 over the committee + cluster key — 2^128 collision resistance, standard cryptographic identifier (same argument as F1's cache key). |
| Fingerprint omits a Verifier-relevant field (e.g. a future Option-B `NRPubKeyShares`). | The validation layer constructs with `ibePubKeyShares = nil` (Option A) at both call sites today, so NR shares are derived from the committee already covered by the fingerprint. If Option B is wired into validation later, the fingerprint MUST add the IBE shares — flagged in a code comment on `shareVerifierFingerprint`. |
| TTL too short → thrashing; too long → memory. | Match `states`' TTL (~7 min). Envelopes only land within ±4 slots (`obftAllowed{Past,Future}Slots`), so a ~7 min TTL comfortably covers a validator's hot window with margin; eviction is lazy + background. |

## Open questions for sign-off

1. **Invalidation strategy** — confirm TTL + content-fingerprint (recommended) vs the cheaper pointer-identity. This is the load-bearing safety decision.
2. **Scope** — both variants (obft + twoab) in one commit, as planned? (They're 1:1 mirrors.)
3. **B6 benchmark** — include the cold-vs-warm micro-benchmark in this commit, or defer?

## Status

- Investigation: complete (construction sites, share lifecycle, mutation model, mv lifetime, existing ttlcache pattern — all verified against code with citations above).
- Plan: this document.
- Sign-off: **TTL + content fingerprint** confirmed; both variants in one commit; **B6 included.**
- Implementation: **complete.** Single commit:
  - `message/validation/verifier_cache.go` — `cachedOBFTVerifier`/`cachedTwoabVerifier`, `shareVerifierFingerprint` (sorted committee + cluster pubkey), `obftVerifierFor`/`twoabVerifierFor` with nil-cache graceful fallback (mirrors the `consensusAdmissions` nil-guard precedent).
  - `validation.go` — two `ttlcache` fields + init in `New()` (TTL = `maxStoredSlots`, mirroring `states`) + background `Start()`.
  - Swapped both call sites (`obft_validation.go`, `twoab_validation.go`) to the cached helpers; dropped the now-unused adapter imports.
  - `verifier_cache_test.go` — miss-then-hit (same `*Verifier`), **committee-change → rebuild** (the safety test), distinct-validators, nil-cache fallback, **concurrent lookups + shared-verify under -race**, and fingerprint determinism / order-independence / field-sensitivity.
  - `verifier_cache_bench_test.go` — B6.
- **B6 measured** (Apple M3 Pro, minimal-block fixture): cold `NewVerifierFromShare`+`VerifyPhase1Bundle` = **105 allocs / 7.8 KB/op**; cached = **4 allocs / 533 B/op** — a **96% per-envelope allocation cut**. Wall-clock delta ~27 µs here (the ~1 ms BLS pairing is common to both and unchanged); on realistic 10-25 KB production blocks the cold path is ~336+ allocs (per B2), so the alloc + cold signing-root saving scales up and is paid on every gossiped envelope. The Commit path additionally unlocks F3 (kyber pubkey-parse) on NR partials — not separately benched here (B3 covers it standalone).
