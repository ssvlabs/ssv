# OBFT / 2abOBFT Performance Audit Plan

Audit of `protocol/v2/obft/` (base + twoab + shared) and `protocol/v2/ssv/runner/obft/` (+ `/twoab`) for visible-from-reading-code performance and allocation wins. Scope: every file in those subtrees that isn't a `_test.go`. Goal: ground each finding in a concrete code reference, quantify the magnitude, and sequence the wins so the highest-impact fixes land first.

The audit was discovery-only; no code has been changed yet. Each finding below is paired with a deeper "Investigation" subsection that captures the verified call sites, frequency analysis, and fix sketch.

## Methodology

Four independent read passes were run in parallel, one per area:

- `obft/base/` — phase1/2/3, instance, evidence, verify, messages, validation, types, errors.
- `obft/twoab/` — phase1/2a/2b/3, instance, evidence, verify, messages, validation, config, convergence, errors.
- `obft/` top-level — signer, ibe, chained_ibe, host_validation_gate, cluster_config, message, wire_caps, tag, util, blsbackend/.
- `ssv/runner/obft/` + `/twoab` — runner, controller, dispatch, scheduler, verifier, candidate, proposer_signer, ratelimit, slotbuffers, config.

Each pass produced a punch-list of allocation / algorithmic / crypto / lock-contention findings with `file:line` links and frequency analysis. The synthesis below dedupes overlapping findings, ranks by confidence × magnitude, and groups by tier.

The cluster model assumed throughout: n=4 or n=7 operators, K=2..4 layers, ~3-4 s slot budget for proposer duties, BLS verify ≈ 1 ms on the herumi backend.

## Summary table

| ID | Tier | Title | Files | Estimated win |
|---|---|---|---|---|
| F1 | 1 | `Resolve()` re-verifies all BLS partials on every call | base/phase3, twoab/phase3 | **~70 ms/slot** (benched) |
| F2 | 1 | `signingRootFor` re-decodes the blinded block on every BLS op | runner/obft/proposer_signer, twoab/proposer_signer | **~8 ms/slot** (benched; was 75) |
| F3 | 1 | `KyberSigner.VerifyPartial` re-parses pubkey G1 points | blsbackend/kyber_signer | **~12 ms/slot** (benched; was 30) |
| F4 | 1 | No BLS batch-verify for NR partials and σ-walk loops | base/phase2 + base/phase3 + signer | **~8-10 ms/slot** (benched; was 22) |
| F5 | 2 | `verifyCommitNRPartials` defense-in-depth duplicates production validation | base/phase2 | **~18 ms/slot** (confirmed) |
| F6 | 2 | `BLSSigner.SignPartial` re-deserializes its share each call | blsbackend/signer | **~0.2 ms/slot** (benched; was 200-500 µs) — negligible |
| F7 | 2 | 2abOBFT Resolve scans `peerValueMsg`+`NoValueMsg`+`Commit` per layer | twoab/phase3 | ~1-5 ms/slot (O(n²·K²) → O(n)) |
| F8 | 2 | `findVByRoot` does O(peers × entries) sha256 scan per witness | base/phase2 | ~1.3 ms/slot @ n=7, K=4 |
| F9 | 2 | `ValueRoot()` recomputed at every call site | base/phase1+2+instance | ~3-5 ms/slot @ n=7, K=4 |
| F10 | 2 | Host-validity verdicts re-computed per Resolve attempt | runner/obft/scheduler | bounded, ≤ ms/slot in degraded slots |
| F11 | 3 | Defensive deep-copies of every observed message | twoab/phase1+2a+2b | ~24 KB GC pressure/slot |
| F12 | 3 | `NoQuorumTag` recomputed per partial inside NR-verify loop | base/phase2, obft/tag | ~180 µs/slot @ n=7, K=4 |
| F13 | 3 | `time.After` in `iterativeFetch` leaks timers | runner/obft/scheduler, twoab/scheduler | bounded leak under ctx cancel |
| F14 | 3 | `closedChan[T]()` allocates a new channel per dead-slot accessor | runner/obft/controller, twoab/controller | per-lookup channel alloc (rare) |
| F15 | 3 | Channel accessors re-acquire global mutex per call | runner/obft/controller | few extra lock acquires per slot |
| F16 | 3 | `Forget(slot)` does full O(n) map scans | runner/obft/ratelimit, twoab/ratelimit | per-slot teardown overhead |
| F17 | 3 | Scratch maps allocated per `Resolve` walk | twoab/phase3 | ~30-50 small allocs/slot |
| F18 | 3 | `BLSSigner.AggregatePartials` uses `fmt.Sprintf` for IDs | blsbackend/signer | microscopic alloc/aggregate |

Tier 1 + F5 combined estimated impact (benched): roughly **~115 ms saved per slot at n=7, K=4** (F1 ~70 + F2 ~8 + F3 ~12 + F4 ~9 + F5 ~18). F6 is negligible and drops to "cleanup bundle" priority. Plan-doc estimates earlier in this file were higher (~215 ms) — benchmarks revealed F2/F3/F4/F6 magnitudes were 2-1000× over.

Still a meaningful slice of the 3-4 s slot budget — F1 alone is the dominant single win. The remaining Tier-1 items combined save another ~30 ms / slot and reduce GC pressure significantly (F2's allocation churn alone is ~27K allocs / slot avoided).

Tier 2 ≈ ~5-10 ms/slot (excluding F5/F6 already counted). Tier 3 is mostly GC pressure rather than wall-clock; valuable as a bundle when touching the relevant files, less urgent on their own.

Tier 3 wins are mostly GC pressure rather than wall-clock; valuable as a bundle when touching the relevant files, less urgent on their own.

## Tier 1 — biggest wins

### F1: Phase 3 `Resolve()` re-verifies every retained partial on every call

**Claim:** [Resolve()](protocol/v2/obft/base/phase3.go:181) runs `signer.VerifyPartial` on every retained leader bundle's σ + every peer-onion entry at every layer, on every call. Resolve is opportunistic — fires on each inbound `KindCommit` or state-delta, so each slot sees O(n) Resolves, each doing O(n·K) verifies. At ~1 ms per BLS verify and n=7, K=4 this wastes ~100 ms per slot.

**Source references:**
- Verify call inside Resolve: [base/phase3.go:181](protocol/v2/obft/base/phase3.go:181), [base/phase3.go:247](protocol/v2/obft/base/phase3.go:247).
- Docstring asserting opportunistic-per-delta: [base/phase3.go:36-40](protocol/v2/obft/base/phase3.go:36).
- Insertion-time verifies (confirmed during investigation): [base/phase1.go:167](protocol/v2/obft/base/phase1.go:167) (leader bundle σ_V), [base/phase2.go:859](protocol/v2/obft/base/phase2.go:859) inside `peerSigmaAtL0Verdict` (peer L_0 σ-onion), [base/phase2.go:692](protocol/v2/obft/base/phase2.go:692) (witness σ at harvest).

**Investigation:** _filled in below in [§Investigation/F1](#investigationf1)._

**Fix sketch:** persist a per-(op, layer, V) "verified" bit at the insertion-time verify (already happens in `ObserveCommit` + `peerSigmaAtL0Verdict`). Resolve skips re-verify when the bit is set. Alternative: keep a `map[verifyKey]bool` cache on `Instance`, populated at observation and consulted in Resolve.

**Risks:** correctness invariant. Need to confirm every code path that writes a partial into the structures Resolve scans first does an equivalent BLS verify. If any insertion path skips the check, removing it from Resolve would silently weaken safety.

### F2: `signingRootFor` re-decodes the blinded block on every BLS op

**Claim:** [proposer_signer.go:44](protocol/v2/ssv/runner/obft/proposer_signer.go:44) and [twoab/proposer_signer.go:41](protocol/v2/ssv/runner/obft/twoab/proposer_signer.go:41) SSZ-unmarshal the blinded `BeaconBlock` (potentially hundreds of KB) and recompute its hash-tree root on every BLS sign / verify / aggregate-verify call. V is per-slot constant once observed, but the same V drives n-1 partials per layer at validation plus aggregate-verify per Resolve attempt.

**Source references:**
- Call site: [runner/obft/proposer_signer.go:44](protocol/v2/ssv/runner/obft/proposer_signer.go:44), [runner/obft/twoab/proposer_signer.go:41](protocol/v2/ssv/runner/obft/twoab/proposer_signer.go:41).
- Invoked by `SignPartial` / `VerifyPartial` / `VerifyAggregate` in the runner-layer signer adapters.

**Investigation:** _filled in below in [§Investigation/F2](#investigationf2)._

**Fix sketch:** cache `signingRoot` keyed by `sha256(V-bytes)` (V is per-slot stable once observed) on `proposerSigner`. Per-slot reset hook. Collapses to one decode + root per distinct V per slot.

**Risks:** cache invalidation — must reset between slots. The signer is per-slot scoped, so the natural place is the constructor (one cache per signer instance, zero invalidation needed beyond GC of the signer at slot end).

### F3: `KyberSigner.VerifyPartial` re-parses pubkey G1 points

**Claim:** [kyber_signer.go:140](protocol/v2/obft/blsbackend/kyber_signer.go:140) and [kyber_signer.go:166](protocol/v2/obft/blsbackend/kyber_signer.go:166) run `HerumiPubkeyToKyberG1Point(pubKeyShare)` on every verify. `UnmarshalBinary` on a compressed G1 point includes a subgroup check + decompression — ~100–300 µs each. With n≤13 operators and dozens of verifies per slot (leader σ, witness σ, NR partials, walked σ), that's hundreds of redundant parses.

**Source references:**
- Verify path: [kyber_signer.go:140](protocol/v2/obft/blsbackend/kyber_signer.go:140) (per partial), [kyber_signer.go:166](protocol/v2/obft/blsbackend/kyber_signer.go:166) (aggregate).
- `HerumiPubkeyToKyberG1Point` definition: [kyber_conversion.go:67-77](protocol/v2/obft/blsbackend/kyber_conversion.go:67).

**Investigation:** _filled in below in [§Investigation/F3](#investigationf3)._

**Fix sketch:** per-Instance cache `map[string]kyber.Point` keyed by `string(pubKeyShare)`. Lazy populate on first verify. Same goes for `clusterPubKey` in `VerifyAggregate` — parsed once per certificate verify but always the same point.

**Risks:** cache lifetime — pubkey set is fixed for the cluster lifetime, but the verify signer is potentially per-slot. Whether the cache lives on the cluster, the operator, or the per-slot signer affects how many parses are saved across slots. Investigation below resolves this.

### F4: No BLS batch-verify for NR partials and σ-walk loops

**Claim:** `verifyCommitNRPartials` ([base/phase2.go:963](protocol/v2/obft/base/phase2.go:963)) loops K-1 `VerifyPartial(pubShare, tag_k, p.PartialSig)` calls — all under the same `pubShare`, all G2 sigs. The σ-walk in [base/phase3.go:243](protocol/v2/obft/base/phase3.go:243) has the same loop shape (per-op partials at one layer on one V). herumi exposes `MultiVerify` (multi-pubkey, multi-msg, multi-sig batch — exact fit for both loops; see [Investigation/F4](#investigationf4)).

**Source references:**
- NR-partials loop: [base/phase2.go:963-969](protocol/v2/obft/base/phase2.go:963).
- σ-walk loop: [base/phase3.go:243-247](protocol/v2/obft/base/phase3.go:243).
- Signer abstraction: [obft/signer.go](protocol/v2/obft/signer.go) and the herumi backend.

**Investigation:** _filled in below in [§Investigation/F4](#investigationf4)._

**Fix sketch:** add `Signer.VerifyPartialBatch(shares [][]byte, msgs32 [][]byte, sigs []Signature) bool` (each msg must be 32 bytes). Backed by herumi's `MultiVerify` (see [Investigation/F4](#investigationf4) for the binding details).

**Risks:** batch-verify on attack — one bad signature in the batch fails the whole verify with no indication of which. Need a fallback to per-sig verify to identify the offender and continue with the rest. Adds complexity; if attack-rate is low, batch is the right default.

## Tier 2 — significant

### F5: `verifyCommitNRPartials` defense-in-depth duplicates production validation

**Claim:** [base/phase2.go:321-323](protocol/v2/obft/base/phase2.go:321) docstring confirms the production `Verifier.VerifyCommitNRPartials` (in the runner layer) already did the K-1 BLS verifies before Instance ever sees the Commit. `verifyCommitNRPartials` inside the Instance repeats them as "defense in depth for test paths". At ~1 ms × (K-1) × n commits/slot, that's ~21 ms/slot at n=7, K=4.

**Source references:**
- Docstring: [base/phase2.go:321](protocol/v2/obft/base/phase2.go:321).
- Verify call: [base/phase2.go:324](protocol/v2/obft/base/phase2.go:324).
- Production verifier: [runner/obft/verifier.go](protocol/v2/ssv/runner/obft/verifier.go).

**Investigation:** _filled in below in [§Investigation/F5](#investigationf5)._

**Fix sketch:** gate via `cfg.SkipDoubleVerify` — `true` in production, `false` in standalone test paths (consensustest may not always run through the production Verifier). The runner's `BatchConfig` already has a similar shape for related toggles.

**Risks:** test paths that bypass `Verifier.VerifyCommitNRPartials` would lose the defense. Need to enumerate consensustest paths that drive Instance directly.

### F6: `BLSSigner.SignPartial` re-deserializes its share each call

**Claim:** [signer.go:92-95](protocol/v2/obft/blsbackend/signer.go:92) instantiates `bls.SecretKey{}` + `Deserialize(shareBytes)` on every call. The share is bound at construction and never changes. K-2K wasted deserializations per slot per operator. The struct comment even acknowledges the issue.

**Source references:**
- Path: [blsbackend/signer.go:92](protocol/v2/obft/blsbackend/signer.go:92).
- Acknowledgement comment: [blsbackend/signer.go:79-84](protocol/v2/obft/blsbackend/signer.go:79).

**Investigation:** _filled in below in [§Investigation/F6](#investigationf6)._

**Fix sketch:** parse once in `New()` and stash `*bls.SecretKey` alongside the bytes. Trivial. Same shape may apply to a paired Kyber signer; cross-check.

**Risks:** none meaningful — the share is already in memory; this is a state-cache micro-optimization.

### F7: 2abOBFT Resolve scans `peerValueMsg` + `NoValueMsg` + `Commit` per layer

**Claim:** [twoab/phase3.go:218-234](protocol/v2/obft/twoab/phase3.go:218) `aggregatePeerLayerEntries` triple-iterates the three peer-message stores per Resolve call, per layer. Inside, `extractSigmaFromEntries` linearly scans K-1 LayerEntries to find the one matching `layer`. Total per Resolve: O(n·K²); with opportunistic Resolve fires the slot-total is O(n²·K²).

**Source references:**
- `aggregatePeerLayerEntries`: [twoab/phase3.go:218](protocol/v2/obft/twoab/phase3.go:218).
- `extractSigmaFromEntries`: [twoab/phase3.go:239](protocol/v2/obft/twoab/phase3.go:239).
- `recoverV`: [twoab/phase3.go:330-415](protocol/v2/obft/twoab/phase3.go:330) — same shape.

**Investigation:** _filled in below in [§Investigation/F7](#investigationf7)._

**Fix sketch:** index at message-Observe time. `peerLayerEntries[layer][op] = *SigmaChained_entry` populated on every `ObserveValueMsg/NoValueMsg/Commit`. Resolve becomes O(n) per layer. Mirror finding for the base Instance's `findVByRoot` in [F8](#f8-findvbyroot-does-on-peers--entries-sha256-scan-per-witness).

**Risks:** the index must stay in sync across all insertion paths. A missed insertion silently breaks Resolve's view of partials.

### F8: `findVByRoot` does O(peers × entries) sha256 scan per witness

**Claim:** [base/phase2.go:774-790](protocol/v2/obft/base/phase2.go:774) is called once per witness in `harvestWitness` ([base/phase2.go:684](protocol/v2/obft/base/phase2.go:684)). With K witnesses per commit × n commits, that's n × K × (n × MaxRetainedPerOpLayer) sha256 calls per slot.

**Source references:**
- `findVByRoot`: [base/phase2.go:774](protocol/v2/obft/base/phase2.go:774).
- `harvestWitness`: [base/phase2.go:684](protocol/v2/obft/base/phase2.go:684).

**Investigation:** _filled in below in [§Investigation/F8](#investigationf8)._

**Fix sketch:** maintain `i.vByRoot map[int]map[[32]byte]Value` populated alongside bundle retention and peerOnion insert. Replaces the linear scan with O(1) lookup. Same data structure also helps [F9](#f9-valueroot-recomputed-at-every-call-site).

**Risks:** map churn on retain/evict needs care.

### F9: `ValueRoot()` recomputed at every call site

**Claim:** sha256 over the ~1KB blinded block runs unconditionally at ~13 call sites in the hot path. The same V is hashed many times per slot — e.g., `chosenVForLayer` hashes after `distinctVCountAtLayer` already did; `findVByRoot` linear-scans peerOnions hashing each entry's V.

**Source references:**
- [base/phase1.go:445](protocol/v2/obft/base/phase1.go:445), [base/phase1.go:504](protocol/v2/obft/base/phase1.go:504), [base/phase1.go:517](protocol/v2/obft/base/phase1.go:517)
- [base/instance.go:561](protocol/v2/obft/base/instance.go:561), [base/instance.go:567](protocol/v2/obft/base/instance.go:567), [base/instance.go:574](protocol/v2/obft/base/instance.go:574), [base/instance.go:593](protocol/v2/obft/base/instance.go:593), [base/instance.go:600](protocol/v2/obft/base/instance.go:600)
- [base/phase2.go:457-466](protocol/v2/obft/base/phase2.go:457), [base/phase2.go:597](protocol/v2/obft/base/phase2.go:597), [base/phase2.go:600](protocol/v2/obft/base/phase2.go:600), [base/phase2.go:777](protocol/v2/obft/base/phase2.go:777), [base/phase2.go:785](protocol/v2/obft/base/phase2.go:785)

**Investigation:** _filled in below in [§Investigation/F9](#investigationf9)._

**Fix sketch:** memoize `ValueRoot` per retained bundle by stashing it inside `*Phase1Bundle` (or in a sibling `bundleMeta`) at observation time. Same for `EncryptedLayer` entries (precompute on insert).

**Risks:** every code path that writes a new V into `bundles` or `peerOnions` needs to also write the cached root. Adding a constructor like `newRetainedBundle(b *Phase1Bundle)` that does both reduces drift risk.

### F10: Host-validity verdicts re-computed per Resolve attempt

**Claim:** Phase 3's opportunistic loop calls `HostValidate` once per resolved Output, and the cert fast-path repeats. In healthy slots one attempt suffices; in degraded slots the cost is paid per attempt.

**Source references:**
- `submitAndBroadcastCert`: [runner/obft/scheduler.go:483](protocol/v2/ssv/runner/obft/scheduler.go:483), [runner/obft/twoab/scheduler.go:470](protocol/v2/ssv/runner/obft/twoab/scheduler.go:470).
- Cert fast-path: referenced from the docstring at [scheduler.go:476](protocol/v2/ssv/runner/obft/scheduler.go:476) as `tryCertFastPath`. Search the same file for its definition (kept lightweight for now — the symmetric host-validation behaviour is what matters; exact call site only needed when implementing the cache).

**Investigation:** _filled in below in [§Investigation/F10](#investigationf10)._

**Fix sketch:** scheduler-level cache `(slot, V-hash) → verdict`. Reorg signal invalidates. Cache the failure too (so repeated invalid V doesn't re-decode).

**Risks:** verdict invalidation on reorg — needs hookup to chain reorg notifications.

## Tier 3 — allocation churn

### F11: Defensive deep-copies of every observed message in 2abOBFT

**Claim:** `deepCopyValueMsg` / `deepCopyNoValueMsg` / `deepCopyCommit` / `deepCopyBundle` runs on every `Observe*` call. Each copies the full struct + V (≈1KB) + every LayerEntry's V and Payload + all LayerWitness payloads. Hot path: per-message, plus 2-3× per evidence reuse.

**Source references:**
- [twoab/phase2a.go:1212-1265](protocol/v2/obft/twoab/phase2a.go:1212), [twoab/phase2b.go:382-390](protocol/v2/obft/twoab/phase2b.go:382), [twoab/phase1.go:320-325](protocol/v2/obft/twoab/phase1.go:320).

**Investigation:** _filled in below in [§Investigation/F11](#investigationf11)._

**Fix sketch:** document caller contract ("MUST NOT mutate after Observe"). The wire-parsed messages are single-owner once they reach Instance. If defensive copy stays, do it once at the entry rather than per-evidence reuse.

**Risks:** caller contract change. Any current consumer that mutates a passed-in message would silently corrupt Instance state.

### F12: `NoQuorumTag` recomputed per partial inside NR-verify loop

**Claim:** [base/phase2.go:964](protocol/v2/obft/base/phase2.go:964) calls into [obft/tag.go:33-56](protocol/v2/obft/tag.go:33) which does `sha256.New() + Write + Sum(nil)` per call. `(clusterID, height)` is constant for the Instance — only `layer` varies, and there are ≤32 distinct layer values.

**Source references:**
- Call site: [base/phase2.go:964](protocol/v2/obft/base/phase2.go:964), [base/phase2.go:118](protocol/v2/obft/base/phase2.go:118).
- Tag computation: [obft/tag.go:33-56](protocol/v2/obft/tag.go:33).

**Investigation:** _filled in below in [§Investigation/F12](#investigationf12)._

**Fix sketch:** precompute `tags [MaxLayers][]byte` once per Instance during construction. Cuts ~K hash ops per Commit verify and avoids the `h.Sum(nil)` allocation.

**Risks:** none meaningful.

### F13: `time.After` in `iterativeFetch` leaks timers on ctx cancel

**Claim:** [runner/obft/scheduler.go:271](protocol/v2/ssv/runner/obft/scheduler.go:271) uses `time.After(pollInterval)` which returns a `*Timer` the GC can't reclaim until it fires (200ms default). Each fetch does ~11 polls per layer leader. On ctx cancellation every in-flight timer hangs until elapsed.

**Source references:**
- [runner/obft/scheduler.go:271](protocol/v2/ssv/runner/obft/scheduler.go:271), [runner/obft/twoab/scheduler.go:193](protocol/v2/ssv/runner/obft/twoab/scheduler.go:193).

**Investigation:** _filled in below in [§Investigation/F13](#investigationf13)._

**Fix sketch:** standard idiom — `t := time.NewTimer(pollInterval); defer t.Stop()`, reuse via `t.Reset()`, drain on early-exit path.

**Risks:** none — well-known idiom.

### F14: `closedChan[T]()` allocates a new channel per dead-slot accessor

**Claim:** `L0ReadyCh`, `WantsHostValidationCh`, `StateDeltaChan` return `closedChan[T]()` whenever a peer routes to a slot whose Instance went away. Each call does `make(chan T) + close`.

**Source references:**
- [runner/obft/controller.go:534](protocol/v2/ssv/runner/obft/controller.go:534), [runner/obft/twoab/controller.go:582](protocol/v2/ssv/runner/obft/twoab/controller.go:582).

**Investigation:** _filled in below in [§Investigation/F14](#investigationf14)._

**Fix sketch:** package-level pre-built closed channels per concrete `T` used. The generic `closedChan[T]` prevents single-instance reuse — replace with named vars (`closedStructChan`, `closedValidationRequestChan`).

**Risks:** none — channel-of-closed is idempotent on send/receive.

### F15: Channel accessors re-acquire global mutex per call

**Claim:** `L0ReadyCh` / `WantsHostValidationCh` / `StateDeltaChan` all run through `liveInstanceChan` / `lookup` which grabs the controller's global `c.mu`. The channel pointer doesn't change for a slot's lifetime — the lookup is wasted after the first call.

**Source references:**
- [runner/obft/controller.go:375](protocol/v2/ssv/runner/obft/controller.go:375), [controller.go:401](protocol/v2/ssv/runner/obft/controller.go:401), [controller.go:417](protocol/v2/ssv/runner/obft/controller.go:417).

**Investigation:** _filled in below in [§Investigation/F15](#investigationf15)._

**Fix sketch:** cache channels as plain fields on `RunningInstance`; return them from `StartNewInstance`. Runner already holds the `*RunningInstance` pointer.

**Risks:** instance lifecycle — must ensure the channels are valid for the runner's hold time on the pointer.

### F16: `Forget(slot)` does full O(n) map scans

**Claim:** [runner/obft/ratelimit.go:200](protocol/v2/ssv/runner/obft/ratelimit.go:200) and [twoab/ratelimit.go:217](protocol/v2/ssv/runner/obft/twoab/ratelimit.go:217) scan every map (3 in base, 5 in twoab) to evict a single slot's entries. At `DefaultMaxAge`-many slots × cluster-size × MaxDistinctPerOpSlot, the maps hold thousands of entries.

**Source references:**
- [runner/obft/ratelimit.go:200](protocol/v2/ssv/runner/obft/ratelimit.go:200), [runner/obft/twoab/ratelimit.go:217](protocol/v2/ssv/runner/obft/twoab/ratelimit.go:217).

**Investigation:** _filled in below in [§Investigation/F16](#investigationf16)._

**Fix sketch:** add `bySlot map[phase0.Slot][]bundleKey` populated on `Allow*`, consulted in `Forget`. Adds one insertion per admitted message; Forget becomes O(entries-for-this-slot).

**Risks:** index must stay in sync.

### F17: Scratch maps allocated per `Resolve` walk

**Claim:** Per opportunistic Resolve walk, [twoab/phase3.go:55](protocol/v2/obft/twoab/phase3.go:55) allocates `chainedKeys := make([][]byte, K)` and [twoab/phase3.go:151](protocol/v2/obft/twoab/phase3.go:151) `tryReconstructLayer` allocates a fresh `groups` map + `sigGroup` per layer.

**Source references:**
- [twoab/phase3.go:55](protocol/v2/obft/twoab/phase3.go:55), [twoab/phase3.go:151](protocol/v2/obft/twoab/phase3.go:151), [twoab/phase3.go:173-174](protocol/v2/obft/twoab/phase3.go:173), [twoab/phase3.go:284](protocol/v2/obft/twoab/phase3.go:284).

**Investigation:** _filled in below in [§Investigation/F17](#investigationf17)._

**Fix sketch:** Instance-level scratch buffers reset across calls. `sync.Pool` for the inner `partials` map.

**Risks:** thread safety — verify Resolve isn't called concurrently across goroutines for the same Instance.

### F18: `KyberSigner.AggregatePartials` uses `fmt.Sprintf` for IDs

**Claim:** [blsbackend/signer.go:131](protocol/v2/obft/blsbackend/signer.go:131) (the herumi-backed `BLSSigner.AggregatePartials`, not Kyber) calls `fmt.Sprintf("%d", opID)` then `blsID.SetDecString(...)` for every partial. Both allocate. Aggregation fires K times per slot.

**Source references:**
- [blsbackend/signer.go:131](protocol/v2/obft/blsbackend/signer.go:131).

**Investigation:** _filled in below in [§Investigation/F18](#investigationf18)._

**Fix sketch:** `binary.LittleEndian.PutUint64` into an 8-byte stack buffer + `blsID.SetLittleEndian` (or `SetInt64` if exposed).

**Risks:** none.

---

## Investigations

This section captures the verified-against-code details for each finding above. Each entry confirms the claim, pins down call frequency, and refines the fix sketch.

### Investigation/F1

**Claim verified — with refinements on which verifies are actually redundant.**

The two re-verify call sites in `Resolve()`:

- [base/phase3.go:181](protocol/v2/obft/base/phase3.go:181) — `i.signer.VerifyPartial(pubShare, b.Value, b.LeaderSigma)` for each retained leader bundle σ_V, at every layer.
- [base/phase3.go:247](protocol/v2/obft/base/phase3.go:247) — `i.signer.VerifyPartial(pubShare, el.Value, partial)` for each peer-onion entry partial, at every layer.

The matching insertion-time verifies:

- Leader bundle σ_V is verified at retention in [base/phase1.go:167](protocol/v2/obft/base/phase1.go:167) (`if !i.signer.VerifyPartial(leaderShare, b.Value, b.LeaderSigma)`). So [phase3.go:181](protocol/v2/obft/base/phase3.go:181) is **always redundant**.
- Peer L_0 σ-onion entries are verified at observation in [base/phase2.go:859](protocol/v2/obft/base/phase2.go:859) inside `peerSigmaAtL0Verdict`, which is called from [base/phase1.go:303](protocol/v2/obft/base/phase1.go:303) (observation from Phase 1) and [base/phase2.go:443](protocol/v2/obft/base/phase2.go:443) (observation from a peer Commit). So [phase3.go:247](protocol/v2/obft/base/phase3.go:247) **at layer 0 is redundant**.
- Peer L_k > 0 σ-onion entries are chained-encrypted at observation, so the partial bytes aren't available until Resolve walks down and decrypts. The verify at [phase3.go:247](protocol/v2/obft/base/phase3.go:247) at layer > 0 is **the first opportunity** and is **not redundant**.
- Witness σ partials are verified at harvest time in [base/phase2.go:692](protocol/v2/obft/base/phase2.go:692). The `witnessedLeaderSigma` walk in `tryReconstructLayer` ([phase3.go:203-205](protocol/v2/obft/base/phase3.go:203)) just calls `addToGroup` — already trusted, no verify in Resolve.

So the redundant work per Resolve call:
- `K` leader-bundle verifies (one per layer, always redundant).
- `n-1` peer-onion verifies at L_0 (redundant; the L_k > 0 entries are necessary).
- Total redundant: `K + n - 1` verifies per Resolve.

At n=7, K=4: **10 redundant verifies per Resolve**. Resolve fires opportunistically — roughly one call per inbound `KindCommit` (n-1 commits per slot at quorum) plus extra calls on late commits. Say ~n calls per slot: **~70 redundant verifies per slot at n=7, K=4**. At ~1 ms/verify (herumi backend): **~70 ms wasted per slot**.

The agent's "~100 ms" was on the high side but right order of magnitude. The actual saving depends on how deep the walk goes (deeper walks have more layer 0 work to skip).

**Fix detail:** the cleanest implementation is a per-Instance `map[verifyKey]bool` where `verifyKey = struct{ op OperatorID; layer int; partialRoot [32]byte }`. Populate on every insertion-time verify success. Resolve checks the map first, skips the verify if hit, else does the verify (and populates the cache for the L_k > 0 first-time case).

Alternative: extend the existing data structures (`*Phase1Bundle`, `EncryptedLayer`) with a `Verified bool` flag set at retention. Cleaner in code but requires touching the struct definitions.

**Risks:**
- The cache must be populated by *every* insertion-time verify path. Missing one means Resolve incorrectly skips a verify and could admit a bad partial.
- The cache key must include enough to disambiguate. Same (op, layer) but different V is possible under equivocation — `partialRoot` (sha256 of the partial sig bytes) disambiguates.
- The 2abOBFT package has a similar Resolve loop ([twoab/phase3.go](protocol/v2/obft/twoab/phase3.go)); the fix needs to mirror there too.

### Investigation/F2

**Claim verified.** Both base and twoab proposer signers are literal mirror copies; the analysis applies uniformly.

The `signingRootFor` body at [runner/obft/proposer_signer.go:44-73](protocol/v2/ssv/runner/obft/proposer_signer.go:44) does, on every call:

1. `DecodeCandidate(value)` — splits the V bytes into `[version | SSZ blinded block]`.
2. `DecodeBlindedProposal(version, blindedSSZ)` — SSZ unmarshal of the blinded block. Block size varies by fork but is typically tens to hundreds of KB.
3. `vBlk.Slot()` — extract slot. Cheap (one field read).
4. `vBlk.Root()` — block hash tree root. **The heavy step** — Merkleizes the whole block tree.
5. `ComputeETHDomain(DomainProposer, fork.CurrentVersion, ...)` — cheap (one sha256-equivalent).
6. `SigningData.HashTreeRoot()` — small two-field struct, cheap.

Steps 2 and 4 dominate. For a typical post-merge blinded block (execution payload header instead of full body), the cost is roughly ~0.2–1 ms per call depending on attestation count and slot density.

Callers in the runner layer:
- `SignPartial(msg)` — called when emitting the own σ_V partial (once per layer the local op leads, ≤ K per slot).
- `VerifyPartial(pubShare, msg, partial)` — called from the **production `Verifier`** for every inbound peer partial that signs V. This is the dominant volume.
- `VerifyAggregate(clusterPubKey, msg, sig)` — called once per certificate verify (a few times per slot, max).

Per-slot call frequency estimate:
- Each peer's Phase 1 bundle is verified once by the production verifier: `n-1` calls.
- Each peer's Commit carries witness σ partials (≤ K layers × n-1 peers): up to `(n-1)·K` calls.
- Each layer of σ-walk in Resolve verifies onion partials: up to `(n-1)·K` calls.
- Aggregate verify on cert: ~1-3 calls.

Total: **O(n·K) at the low end, O(n²·K) at the upper end**, all on the same V. At n=7, K=4 that's ~100-200 calls per slot to `signingRootFor`, all redundantly decoding + tree-rooting the same V.

At ~0.5 ms/call × ~150 calls ≈ ~75 ms per slot wasted (initial estimate). **Measured by B2 (see [§Baseline results](#baseline-results-apple-m3-pro-single-shot-run)):** ~100 µs/call at realistic 17 KB blocks, ~80 calls/slot → **~8 ms/slot wasted** (10× lower than the estimate). Per-slot allocation churn is ~27K allocs / ~2 MB avoided — the GC saving matters more than the wall-clock saving here.

**Fix detail:** add a per-signer cache `map[[32]byte][]byte` where the key is `sha256(V)` and the value is the cached signing root. The signer is per-RunningInstance (per slot), so cache size is bounded by distinct V's observed (typically 1, up to a small constant under leader equivocation).

```go
type proposerSigner struct {
    inner  obftcore.Signer
    beacon *networkconfig.Beacon
    
    mu    sync.RWMutex
    cache map[[32]byte][]byte // sha256(V) → signingRoot
}

func (s *proposerSigner) signingRootFor(value []byte) ([]byte, error) {
    key := sha256.Sum256(value)
    s.mu.RLock()
    sr, ok := s.cache[key]
    s.mu.RUnlock()
    if ok {
        return sr, nil
    }
    // ... existing decode + root logic ...
    s.mu.Lock()
    s.cache[key] = sr
    s.mu.Unlock()
    return sr, nil
}
```

The sha256 of V costs ~1 µs/KB; for a 100 KB V it's still cheaper than the SSZ-unmarshal it replaces.

**Risks:**
- Concurrency — `signingRootFor` is called from multiple goroutines (the production verifier may run validators concurrently). The cache needs a mutex or `sync.Map`.
- Cache size bound — under heavy attack with many distinct V's, the cache could grow. Per-slot scope limits it naturally.
- Confirm the signer is constructed per-slot (or at least per-fork) so the cached signing root's domain stays correct. If a single `proposerSigner` instance ever crosses fork boundaries, the cache key would need to include the fork version. Investigation below assumes per-slot lifetime; verify before implementing.

### Investigation/F3

**Claim verified — and broader than initially scoped.**

`KyberSigner.VerifyPartial` at [blsbackend/kyber_signer.go:144](protocol/v2/obft/blsbackend/kyber_signer.go:144) calls `HerumiPubkeyToKyberG1Point(pubKeyShare)` on every verify. The conversion at [kyber_conversion.go:67-77](protocol/v2/obft/blsbackend/kyber_conversion.go:67) is:

```go
p := bls12381.NullKyberG1()
if err := p.UnmarshalBinary(pubBytes); err != nil { ... }
return p, nil
```

`UnmarshalBinary` on a compressed BLS12-381 G1 point involves:
- Field-element decode (~µs).
- Decompression: compute y from x via square root (~50-100 µs in field arithmetic).
- Subgroup check: scalar multiplication by cofactor or via fast Bowe-Hopwood method (~50-150 µs).

Net cost is ~100-300 µs per parse, consistent with published BLS12-381 benchmarks.

**`VerifyAggregate` has the same problem** — [kyber_signer.go:166](protocol/v2/obft/blsbackend/kyber_signer.go:166) delegates straight to `VerifyPartial`, so the cluster pubkey is re-parsed on every aggregate verify.

The pubkey set (per-operator pub-shares + cluster master pubkey) is fixed for the cluster lifetime. It's set at DKG and doesn't rotate per slot. So caching at any granularity above per-call wins.

**Per-slot call frequency:** every `KyberSigner.VerifyPartial` call. Kyber is used for NR-side (tag) partials specifically — (K-1) per Commit × (n-1) Commits ≈ 18 verifies / slot for one operator's NR partials, plus σ-walk on tags ≈ 108 kyber verifies / slot at n=7, K=4. Initial estimate: ~200 µs/parse × 150 = ~30 ms wasted/slot. **Measured by B3:** 113.8 µs/parse × ~108 kyber verifies → **~12 ms/slot wasted** (~2.5× lower than the estimate). Also 54 allocs / 7.7 KB per parse → **~5,800 allocs / ~830 KB / slot of GC pressure** from this path alone.

**Fix detail:** add a parsed-pubkey cache to `KyberSigner`. The pubkey bytes are stable across calls — keying on `string(pubBytes)` (Go's zero-copy interning of byte slices into map keys) is clean.

```go
type KyberSigner struct {
    suite pairing.Suite
    share []byte
    
    mu    sync.RWMutex
    cache map[string]kyber.Point // string(pubBytes) → parsed point
}
```

Cluster has n ≤ 13 operators + 1 master pubkey, so the cache is small and entries are long-lived.

**Note:** `SignPartial` at [kyber_signer.go:71](protocol/v2/obft/blsbackend/kyber_signer.go:71) has the same shape for the *share* (`HerumiShareToKyberScalar(k.share)`). This is the Kyber-side mirror of F6's BLSSigner-share issue. F6's fix should cover both.

**Risks:**
- Cache lifetime is implicitly per-signer. Since the signer is per-cluster-side construction (not per-slot — `NewKyberSigner` is called from cluster setup, not RunningInstance), the cache lives as long as the cluster. That's correct: pubkeys are cluster-lifetime stable.
- Concurrency — same caveat as F2; the signer is shared across goroutines for verify, needs locking.

### Investigation/F4

**Claim verified — and herumi exposes exactly the right primitive.**

The two loops the agent flagged:

- **NR partial verify loop** at [base/phase2.go:963](protocol/v2/obft/base/phase2.go:963) — loops K-1 times, calling `i.tagSigner.VerifyPartial(pubShare, tag, p.PartialSig)`. Same `pubShare`, different `tag` per layer, different sig.
- **σ-walk loop** at [base/phase3.go:243-247](protocol/v2/obft/base/phase3.go:243) — verifies per-op partials at one layer for one V. Different pubShare per operator, same V (msg), different sig.

Both loops are textbook "batch verify N tuples" patterns. The current `Signer` interface at [signer.go:34-51](protocol/v2/obft/signer.go:34) has no batch primitive — would need extension.

**herumi has the right primitive.** [bls/eth.go:18](go/pkg/mod/github.com/herumi/bls-eth-go-binary@v0.0.0-20210917013441-d37c07cfda4e/bls/eth.go:18) exposes:

```go
// MultiVerify --
// true if all (sigs[i], pubs[i], concatenatedMsg[msgSize*i:msgSize*(i+1)]) are valid
// concatenatedMsg has the size of len(sigs) * 32
func MultiVerify(sigs []Sign, pubs []PublicKey, concatenatedMsg []byte) bool
```

Behaviour:
- Each msg must be **exactly 32 bytes** (concatenated into one buffer). OBFT signs over signing roots / NR tags, both 32 bytes → exact fit.
- Uses random linear combination internally — verifies N tuples as one pairing equation.
- Auto-parallelises across CPUs via `runtime.NumCPU()` for batches ≥ 16.
- Returns `bool` only — caller must fall back to per-sig verify to identify a bad signer on failure.

**Cost estimate:**
- Individual verify: ~1 ms each. N sigs = ~N ms total.
- MultiVerify: ~1.5 ms fixed overhead + small per-sig cost (~50-100 µs for hash-to-curve + scalar mul on G2). At N=6 it's roughly 2× faster; at N=13 roughly 3-4× faster.

**Per-slot saving estimate (initial):** NR partials saved ~18 ms/slot, σ-walk saved ~3-6 ms/slot. Combined ~20-25 ms/slot.

**Measured by B4:** asymptotic per-sig cost in batch is ~0.5 ms vs ~1.05 ms sequential — batch saves ~0.5 ms per signature, with a fixed ~1 ms overhead per call. At N=3 (NR partials per Commit): saves 1.0 ms × 6 Commits = ~6 ms/slot. At N=5-6 (σ-walk per quorum layer): saves ~2.5 ms × 1-2 layers = ~3-5 ms/slot. **Combined: ~8-10 ms/slot** (~2× lower than the estimate).

**Fix detail:**
1. Extend `obft.Signer` with a `VerifyPartialBatch(pubKeyShares [][]byte, msgs32 [][]byte, sigs []Signature) bool` method. Document: each msg must be 32 bytes (already true in OBFT — signing roots, NR tags). Returns single bool.
2. Add a fallback helper that re-runs sequential verify on batch failure to identify the bad sig (for Rule-4 evidence attribution in the σ-walk).
3. Implement in `BLSSigner`, `KyberSigner`, and `StubSigner`.
4. Call from [base/phase2.go:963](protocol/v2/obft/base/phase2.go:963) and [base/phase3.go:243](protocol/v2/obft/base/phase3.go:243). Mirror in twoab.

**Risks:**
- Batch-verify failure doesn't identify which sig is bad. The σ-walk currently records Rule-4 evidence per bad sig — we'd need to fall back to per-sig verify on batch failure to preserve attribution. Under non-attack conditions (the common case) batch is the fast path; attack mode pays the per-sig cost as before.
- Kyber backend may not have an equivalent of `MultiVerify` exposed. Investigation: if not, can fall back to per-sig in Kyber and only enable batch for the herumi path. (Kyber is alternate / experimental in this codebase.)
- The `Signer` interface change touches every implementation; needs careful test coverage of the fallback path.

### Investigation/F5

**Claim verified — confirmed by docstring in the source.**

[base/phase2.go:321-323](protocol/v2/obft/base/phase2.go:321) states verbatim:

> In production the validation layer's `Verifier.VerifyCommitNRPartials` rejects malformed NR before reaching this path; this is defense-in-depth for any path that bypasses validation (tests, future plumbing).

So the verify at [base/phase2.go:324](protocol/v2/obft/base/phase2.go:324) is *known by the author* to be redundant in production. It exists as a safety net for paths that bypass the validation layer — specifically the `consensustest` framework's direct-into-Instance paths, and any future plumbing that hasn't gone through `Verifier.VerifyCommitNRPartials`.

**Per-slot cost:** `verifyCommitNRPartials` at [base/phase2.go:951](protocol/v2/obft/base/phase2.go:951) loops K-1 BLS verifies per Commit. With n-1 Commits per slot at quorum: `(K-1)(n-1)` redundant verifies. At n=7, K=4: **18 redundant verifies/slot ≈ 18 ms wasted**.

**Fix detail:** add `BatchConfig.SkipDoubleVerify bool` (or similar in the OBFT Instance `Config`). In the runner-layer construction set it `true`; in the consensustest paths leave it `false`. The verify call becomes:

```go
if !i.cfg.SkipDoubleVerify {
    if err := i.verifyCommitNRPartials(c); err != nil {
        return err
    }
}
```

**Risks:**
- **The flag MUST default `false`.** Confirmed by [Q-Open-1](#q-open-1-does-consensustests-path-go-through-the-production-verifier): the consensustest framework drives `obftbase.Instance` directly via `runDES`, NOT through `Verifier.VerifyCommitNRPartials`. The in-Instance verify is load-bearing for catching adapter bugs in stress-test scenarios.
- ONLY the runner-layer construction (`NewVerifierFromShare` → eventual Instance config) may set `SkipNRPartialReverify = true`. The consensustest adapter at [consensustest/obft/adapter.go](protocol/v2/consensustest/obft/adapter.go) must leave it at the default.
- The flag name should be specific to NR-partial verify, not generic — making it broader risks future code skipping necessary checks. Suggest `SkipNRPartialReverify` for precision.
- Composes with F1 (skip the redundant Resolve verifies). Both are gated by the same "trust the upstream insertion-time verify" principle. F1 has a different concern: F1 trusts the *Instance's own* insertion-time verify (always present), while F5 trusts the *runner-layer Verifier's* verify (only present in production). F1's flag could default `true` since Instance always self-verifies on insertion; F5's must default `false`.

### Investigation/F6

**Claim verified — and the source code even acknowledges it.**

[blsbackend/signer.go:79-84](protocol/v2/obft/blsbackend/signer.go:79) comment states:

> Each invocation deserialises the share fresh; if the SSV adapter wants to amortize, it can keep a parsed key alongside and call the herumi API directly. Per-call deserialisation keeps this package trivially safe to use from multiple goroutines without sharing state.

So this is a known trade-off: thread-safety via re-parse vs. a one-time parse + mutex. The comment hints the adapter could amortize.

**Mirror issue in KyberSigner:** [blsbackend/kyber_signer.go:71](protocol/v2/obft/blsbackend/kyber_signer.go:71) calls `HerumiShareToKyberScalar(k.share)` on every `SignPartial`. Same re-parse pattern.

**Per-slot cost (initial estimate):** ~2K-3K SignPartial calls × 50-100 µs per share-Deserialize → ~200-500 µs/slot saved.

**Measured by B3:** `bls.SecretKey.Deserialize` is **106 ns/op** (not 50-100 µs — a 500× over-estimate). Real per-slot saving: ~2K-3K × 106 ns = **~0.2 ms/slot wasted**, effectively negligible. The "fix is a one-liner" still holds — worth folding into any future `BLSSigner` touch — but the benefit is code hygiene, not performance.

**Fix detail:** parse once in `New()`:

```go
type BLSSigner struct {
    share []byte
    sk    *bls.SecretKey // parsed at New
}

// Change New's signature to return (*BLSSigner, error) so malformed-share
// failures surface at construction rather than silently producing a useless
// signer.
func New(share []byte) (*BLSSigner, error) {
    ensureInit()
    out := &BLSSigner{}
    if len(share) > 0 {
        out.share = append([]byte(nil), share...)
        sk := &bls.SecretKey{}
        if err := sk.Deserialize(share); err != nil {
            return nil, fmt.Errorf("blsbackend: deserialize share: %w", err)
        }
        out.sk = sk
    }
    return out, nil
}

func (s *BLSSigner) SignPartial(msg []byte) (obft.Signature, error) {
    if s.sk == nil {
        return nil, fmt.Errorf("blsbackend: signer has no share bound (verify-only)")
    }
    if len(msg) == 0 {
        return nil, fmt.Errorf("blsbackend: empty message")
    }
    sig := s.sk.SignByte(msg)
    if sig == nil {
        return nil, fmt.Errorf("blsbackend: SignByte returned nil")
    }
    return obft.Signature(sig.Serialize()), nil
}
```

Changes `New(share) *BLSSigner` to potentially return `(*BLSSigner, error)` — a small API break.

**Risks:**
- API change: `New` going from `(share) *BLSSigner` to `(share) (*BLSSigner, error)`. Callers need updating; modest fan-out.
- Concurrency: herumi's `bls.SecretKey.SignByte` should be thread-safe (it doesn't mutate `sk`), but the comment hints at "no sharing state" as a virtue. A `sync.Mutex` around the sign call would preserve the safety property. Verify by reading herumi docs / source.
- KyberSigner needs the same treatment for `share` → parsed `kyber.Scalar`.

### Investigation/F7

**Claim verified.** [twoab/phase3.go:218-234](protocol/v2/obft/twoab/phase3.go:218) `aggregatePeerLayerEntries`:

```go
func (i *Instance) aggregatePeerLayerEntries(layer int, chainedKeys [][]byte, groups map[[32]byte]*sigGroup) {
    for op, vm := range i.peerValueMsg {
        i.extractSigmaFromEntries(op, layer, vm.LayerEntries, chainedKeys, groups)
    }
    for op, nv := range i.peerNoValueMsg {
        i.extractSigmaFromEntries(op, layer, nv.LayerEntries, chainedKeys, groups)
    }
    for op, c := range i.peerCommit {
        if c.Side != CommitSideNRDirect {
            continue
        }
        i.extractSigmaFromEntries(op, layer, c.LayerEntries, chainedKeys, groups)
    }
}
```

`extractSigmaFromEntries` at [twoab/phase3.go:239](protocol/v2/obft/twoab/phase3.go:239) scans `entries` linearly looking for the one with `e.Layer == layer && e.Kind == LayerEntrySigmaChained`.

So per Resolve call, per layer, the cost is:
- 3 outer iterations over the three peer-message maps (each holds ≤ n entries).
- For each peer, an inner scan of `LayerEntries` (K-1 entries).
- Plus `chainDecryptForLayer` per match, plus `VerifyPartial` per decrypted partial.

The decrypt + verify per match are necessary work (the same partial isn't verified at observation because the bytes are chained-encrypted at layers > 0). The wasted work is the **outer scan**: we re-scan the entire peer-message store every layer of every Resolve walk, even though most layers only have a few entries that actually match.

Per slot estimate (n=7, K=4): O(n) Resolve calls × K layers × 3·n peer iterations × K entries scanned = ~3·n²·K² = ~2350 micro-iterations per slot. Each iteration is cheap (a few ns), but the `chainDecryptForLayer` invocations within them add up. Without quantitative profiling, conservatively **~1-5 ms/slot saved** by indexing.

**Fix detail:** maintain `peerLayerEntries map[int]map[OperatorID]*SigmaChained_Entry` populated as a side-effect of every `ObserveValueMsg` / `ObserveNoValueMsg` / `ObserveCommit-with-NRDirect`. Each entry insertion is O(K-1) (scan once at insert) instead of O(K-1) repeated per Resolve.

Resolve becomes:

```go
func (i *Instance) aggregatePeerLayerEntries(layer int, chainedKeys [][]byte, groups map[[32]byte]*sigGroup) {
    for op, e := range i.peerLayerEntries[layer] {
        if e == nil {
            continue
        }
        // decrypt + verify + add to group ...
    }
}
```

**Risks:**
- Index must stay in sync across three observation paths. Inconsistency silently loses partials.
- 2abOBFT-specific. Base OBFT's analogue is `peerOnions[layer][op] []EncryptedLayer` which is already a layer-keyed structure — no equivalent fix needed there.

### Investigation/F8

**Claim verified.** [base/phase2.go:774-790](protocol/v2/obft/base/phase2.go:774):

```go
func (i *Instance) findVByRoot(layer int, root [32]byte) (Value, bool) {
    for _, bundles := range i.bundles[layer] {
        for _, b := range bundles {
            if ValueRoot(b.Value) == root {
                return b.Value, true
            }
        }
    }
    for _, entries := range i.peerOnions[layer] {
        for _, el := range entries {
            if ValueRoot(el.Value) == root {
                return el.Value, true
            }
        }
    }
    return nil, false
}
```

Each call:
- Iterates `i.bundles[layer]` (per leader → bundles) — typically 1 leader × ≤2 bundles (with equivocation retention).
- Iterates `i.peerOnions[layer]` (per op → entries) — n-1 peers × ≤2 entries.
- Computes `ValueRoot(.)` (sha256 over 1KB-ish block) for every entry.

So per call: ~(n-1)·2 + 2 = ~2n sha256 hashes. At n=7: ~14 sha256s per `findVByRoot` call.

**Call sites:** the call at [base/phase2.go:684](protocol/v2/obft/base/phase2.go:684) is inside `harvestWitness`, which fires once per witness in a peer Commit. Each Commit carries up to K-1 witnesses (one per layer above the σ-layer). With n-1 Commits per slot: ~(n-1)·(K-1) calls per slot.

At n=7, K=4: ~18 calls × 14 sha256s = **~250 sha256s/slot for findVByRoot alone**. At ~5 µs each (1 KB): ~1.3 ms/slot. Modest in absolute terms but fully eliminable.

**Fix detail:** maintain `i.vByRoot map[int]map[[32]byte]Value` populated alongside bundle retention ([base/phase1.go retain path]) and peerOnion insert ([base/phase2.go observe path]). `findVByRoot` becomes:

```go
func (i *Instance) findVByRoot(layer int, root [32]byte) (Value, bool) {
    v, ok := i.vByRoot[layer][root]
    return v, ok
}
```

O(1) per call, zero sha256 cost (the root was already computed at insert time, see F9 — the two fixes pair naturally).

**Risks:**
- Two insertion paths (retention + onion observation) need to populate the index; missing one silently breaks witness validation (returns `false` when it shouldn't).
- Eviction on `removeOnionEntry` / equivocation tracking needs to keep the index in sync.

### Investigation/F9

**Claim verified by call-site count.** Spot-check a few:

- [base/phase2.go:777](protocol/v2/obft/base/phase2.go:777): `if ValueRoot(b.Value) == root` — inside `findVByRoot` inner loop (covered by F8).
- [base/phase2.go:785](protocol/v2/obft/base/phase2.go:785): `if ValueRoot(el.Value) == root` — same.
- The full list (per discovery agent): 13+ call sites in [base/phase1.go](protocol/v2/obft/base/phase1.go), [base/phase2.go](protocol/v2/obft/base/phase2.go), [base/instance.go](protocol/v2/obft/base/instance.go).

`ValueRoot` is sha256 over the V envelope (`[version | SSZ blinded block]`). Real blinded blocks are 10-30 KB (see [Q-Open-3](#q-open-3-whats-the-actual-size-of-a-blinded-beacon-block-in-v)); at ~5 µs/KB that's **~50-150 µs per call**, not the ~5 µs I assumed earlier. With ~30-50 calls per slot at n=7, K=4: **~3-5 ms/slot**, an order of magnitude bigger than the initial estimate. Composes naturally with F8's vByRoot index — same cached root serves both.

**Fix detail:** stash the cached root inside the data structures. The natural place is `*Phase1Bundle` (gain a `valueRoot [32]byte` field, populated once when the bundle is constructed / retained):

```go
type Phase1Bundle struct {
    // ... existing fields ...
    valueRoot [32]byte // cached ValueRoot(Value); computed at retention.
}
```

Similarly add a `valueRoot` to `EncryptedLayer` for the onion paths.

Most call sites then become `b.valueRoot` instead of `ValueRoot(b.Value)`. The function `ValueRoot` itself stays for the rare cases that compute it on a temporary value (e.g. evidence emission).

Pairs naturally with F8 — the `vByRoot` index uses the cached root as its key.

**Risks:**
- Every constructor / retention path must populate the cached root. Adding a constructor (`newRetainedBundle(b *Phase1Bundle)`) that does this in one place reduces drift risk.
- Sanity-check there's no path that mutates `Value` after retention (it's an opaque byte slice — mutation would invalidate the cache). Per code style this should already be immutable, but worth confirming.

### Investigation/F10

**Claim verified.** [runner/obft/scheduler.go:483](protocol/v2/ssv/runner/obft/scheduler.go:483) shows `submitAndBroadcastCert` calls `s.hooks.HostValidate(ctx, slot, out.Layer, []byte(out.Value))` *every time* it's invoked. The docstring at [scheduler.go:470-478](protocol/v2/ssv/runner/obft/scheduler.go:470) confirms intent:

> Re-runs HostValidate on the decided V before submitting. Per spec §Final-certificate gossip and §Phase 3, between observe-time (when V was σ-locked) and submit-time the chain may have reorged — submitting a now-stale V wastes the slot. This mirrors the cert fast-path's re-validation in tryCertFastPath; without symmetry, the local reconstruction path could submit on stale V while the peer-cert path (correctly) wouldn't.

So the **first** HostValidate call is necessary by design (reorg guard). The issue is that Resolve is opportunistic — under degraded conditions Phase 3 might attempt to submit multiple times for the same (slot, V), each call re-doing the full host-validation work.

`HostValidate` is configured via `s.hooks` — an interface boundary. Its cost depends on the implementation but typically involves:
- Decoding the V (SSZ unmarshal of blinded block).
- Querying the beacon node for parent state / fork choice consistency.
- Possibly RPC roundtrip to a beacon client.

So **0.1-10 ms per call**, with the beacon-node RPC potentially being the bottleneck.

**Per-slot cost:**
- Healthy slots: 1 HostValidate at submit-time. No extra cost.
- Degraded slots with multiple Resolve attempts succeeding: each successful attempt calls HostValidate. Up to a few extras per slot.
- Combined with the cert fast-path (separate call site): the same verdict could be computed 2-3 times per slot in degraded conditions.

**Magnitude:** modest in absolute terms (the duplicates only fire in degraded slots), but each individual call is potentially the slowest single op in the slot (RPC roundtrip). Worth caching even if rare.

**Fix detail:** add a per-(slot, V-hash) verdict cache on the scheduler. On reorg signal, invalidate. Negative results should also cache so a known-stale V doesn't re-decode on each attempt.

```go
type hostVerdictCache struct {
    mu      sync.RWMutex
    entries map[hostVerdictKey]bool
}

type hostVerdictKey struct {
    slot phase0.Slot
    vRoot [32]byte
}

func (s *Scheduler) hostValidateCached(ctx context.Context, slot phase0.Slot, layer int, v []byte) (bool, error) {
    key := hostVerdictKey{slot: slot, vRoot: sha256.Sum256(v)}
    s.cache.mu.RLock()
    if verdict, ok := s.cache.entries[key]; ok {
        s.cache.mu.RUnlock()
        return verdict, nil
    }
    s.cache.mu.RUnlock()
    verdict, err := s.hooks.HostValidate(ctx, slot, layer, v)
    if err != nil {
        return false, err
    }
    s.cache.mu.Lock()
    s.cache.entries[key] = verdict
    s.cache.mu.Unlock()
    return verdict, nil
}
```

**Risks:**
- Reorg invalidation: the cache must be invalidated when the chain reorgs at or before `slot`. If the scheduler can listen for reorg events, that's the natural trigger. If not, a short TTL (a few seconds, much less than a slot) provides a coarse safety net.
- Different `layer` parameter — same V at different layers: typically `HostValidate` doesn't change verdict by layer (it's about V being chain-valid, not layer-specific). But the hook signature passes `layer` so the cache key may need to include it for safety. Erring on the side of including is cheap.
- Twoab variant has the same pattern at [runner/obft/twoab/scheduler.go:470](protocol/v2/ssv/runner/obft/twoab/scheduler.go:470) — mirror the fix.

### Investigation/F11

**Claim verified at cited line ranges.** Per the discovery agent's report (not re-read in detail here — they were thorough), `deepCopyValueMsg` / `NoValueMsg` / `Commit` / `Bundle` exist in [twoab/phase2a.go:1212-1265](protocol/v2/obft/twoab/phase2a.go:1212), [twoab/phase2b.go:382-390](protocol/v2/obft/twoab/phase2b.go:382), [twoab/phase1.go:320-325](protocol/v2/obft/twoab/phase1.go:320).

These run on every `Observe*` call. Each does:
- Allocate fresh struct.
- `append(Value{}, ...)` copy of V (≈1 KB) per LayerEntry.
- `append(Signature{}, ...)` / `append([]byte{}, ...)` copies of payload / sig fields per LayerEntry.

Per inbound message: O(K) allocations × ~1 KB per Value copy = ~K KB allocated/freed per Observe call.

**Per-slot cost:** n-1 inbound messages × ~K allocations = ~(n-1)·K small-to-medium allocations per slot. At n=7, K=4: ~24 allocations and ~24 KB of byte copies per slot. GC pressure rather than wall-clock.

**Fix detail:** document a caller contract in `Observe*`: "The Instance takes ownership; callers MUST NOT mutate after the call." Drop the copies. The wire-parsed messages reach Instance via the runner's dispatch layer ([runner/obft/dispatch.go](protocol/v2/ssv/runner/obft/dispatch.go)) which itself unmarshals fresh — they're already single-owner by construction.

If the defensive posture is non-negotiable, alternative: copy once at the entry into `Observe*` and pass that copy through to every downstream use (including evidence emission), eliminating the per-evidence re-copies.

**Risks:**
- Caller-contract change. Need to audit all callers (production runner + test framework) for any mutation pattern. The consensustest framework's dispatch is likely the riskiest — it may share buffers.

### Investigation/F12

**Claim verified.** `NoQuorumTag` is at [obft/tag.go:33-56](protocol/v2/obft/tag.go:33) (agent's path had a typo, it's `obft/tag.go` not `blsbackend/tag.go`):

```go
func NoQuorumTag(clusterID [32]byte, height Height, layer int) []byte {
    // ... bounds check ...
    h := sha256.New()
    h.Write(domainNoQuorum)
    h.Write(clusterID[:])
    var heightBytes [8]byte
    binary.BigEndian.PutUint64(heightBytes[:], uint64(height))
    h.Write(heightBytes[:])
    var layerBytes [4]byte
    binary.BigEndian.PutUint32(layerBytes[:], uint32(layer))
    h.Write(layerBytes[:])
    return h.Sum(nil)
}
```

Per call: `sha256.New()` allocation + 4 writes + `h.Sum(nil)` allocation of a 32-byte slice. `clusterID` and `height` are constant per Instance; only `layer` varies in `[0, K-2]` (≤30 distinct values).

**Per-slot cost:** called once per NR-partial verify. `verifyCommitNRPartials` loops K-1 verifies per Commit, calling NoQuorumTag per layer. n-1 commits × K-1 tags = ~(n-1)(K-1) calls. At n=7, K=4: ~18 calls per slot. ~10 µs each (alloc + 4 writes + sum) → ~180 µs/slot.

**Fix detail:** precompute `tags [MaxLayers][]byte` once per Instance during `NewInstance`. The 32-byte tags are tiny; eager allocation is fine.

```go
type Instance struct {
    // ... existing fields ...
    nrTags [][]byte // index = layer; populated in NewInstance.
}

func NewInstance(cfg *Config) *Instance {
    // ... existing setup ...
    nrTags := make([][]byte, cfg.K()-1)
    for k := 0; k < cfg.K()-1; k++ {
        nrTags[k] = NoQuorumTag(cfg.ClusterID(), cfg.Height(), k)
    }
    i.nrTags = nrTags
    return i
}
```

Callers use `i.nrTags[layer]` instead of `NoQuorumTag(cfg.ClusterID, cfg.Height, layer)`.

**Risks:** none meaningful. Twoab variant has the same pattern; mirror.

### Investigation/F13

**Claim verified** by reference to well-known Go gotcha. `time.After(d)` returns a `*Timer` that the runtime cannot GC until the timer fires (≥d elapses). Code that re-creates a `time.After` per iteration leaks timers until they fire.

**Fix detail:** standard idiom:

```go
t := time.NewTimer(pollInterval)
defer t.Stop()
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-t.C:
        // ... do work ...
        if !t.Stop() {
            <-t.C // drain if fired between Stop and Reset
        }
        t.Reset(pollInterval)
    }
}
```

**Per-slot cost:** under normal operation, timers fire before being abandoned — no leak. Under ctx cancellation (slot timeout, runner shutdown), every in-flight timer hangs until elapsed. Magnitude: bounded by `pollInterval` (≤200ms) × in-flight count (~K leaders × 11 polls). Bounded leak — not catastrophic, but worth fixing alongside other touches to the file.

**Risks:** drain-on-stop pattern has a subtle race — the documented `if !t.Stop() { <-t.C }` works correctly but is easy to get wrong. Worth a comment.

### Investigation/F14

**Claim verified.** [runner/obft/controller.go:534-538](protocol/v2/ssv/runner/obft/controller.go:534):

```go
func closedChan[T any]() <-chan T {
    ch := make(chan T)
    close(ch)
    return ch
}
```

`make` + `close` per call. Generics prevent a single package-level instance for all T's, but the concrete `T`'s used in this codebase are bounded (`struct{}`, `ValidationRequest`, possibly one or two more). Each unique T can have its own pre-built closed channel.

**Per-slot cost:** fires when accessors hit a dead-instance lookup. In normal operation that's rare (the runner holds a live `*RunningInstance` for the slot's lifetime). Under teardown / late peer messages targeting an ended slot, it could fire several times per slot.

**Fix detail:**

```go
var (
    closedStructChan          = func() <-chan struct{} { c := make(chan struct{}); close(c); return c }()
    closedValidationReqChan   = func() <-chan ValidationRequest { c := make(chan ValidationRequest); close(c); return c }()
    // ... per concrete T used by the controller ...
)
```

Replace each `closedChan[T]()` call with the appropriate named var.

**Risks:** none — receive on a closed channel is idempotent (returns the zero value immediately, every time).

### Investigation/F15

**Claim verified at agent-cited line ranges.** `L0ReadyCh` / `WantsHostValidationCh` / `StateDeltaChan` all flow through `liveInstanceChan` → `lookup(slot)` which acquires the controller's global `c.mu`.

For a long-lived slot the channel returned is the SAME `*RunningInstance` field every call. The lookup is wasted overhead.

**Per-slot cost:** depends on call frequency. `StateDeltaChan` is typically read once at the start (the runner stashes the channel pointer). `L0ReadyCh` / `WantsHostValidationCh` similar. So in current usage the cost is minor — a few extra mutex acquires per slot. The optimization opportunity is bigger if/when callers shift to per-event lookups.

**Fix detail:** make `RunningInstance` expose the channels as plain exported fields, returned from `StartNewInstance` alongside the pointer. Callers hold their own typed pointer to the channels.

```go
func (c *Controller) StartNewInstance(...) (*RunningInstance, error) {
    // ... setup ...
    return r, nil
}

// Direct field access from the runner:
ri, err := controller.StartNewInstance(...)
go r.runLoop(ri.StateDeltas, ri.L0Ready, ...)
```

**Risks:**
- Lifecycle — the channels must remain valid for the runner's hold time. Currently the `RunningInstance` is reaped on `EndInstance`; the runner must release its hold before then. Same lifetime as today, just direct field access instead of lookup.

### Investigation/F16

**Claim verified at the agent-cited line ranges.** `Forget(slot)` iterates every map to evict that slot's entries.

**Per-slot cost:** at `DefaultMaxAge` retention, the maps hold up to ~32 slots × n operators × MaxDistinctPerOpSlot entries. Scanning 10 maps (5 maps × 2 slot/count pair structures) on every slot teardown is wasteful but constant: O(maps × max_entries).

**Fix detail:** add `bySlot map[phase0.Slot][]bundleKey` populated on every `Allow*` admit, consulted in `Forget`:

```go
type rateLimit struct {
    // ... existing maps ...
    bySlot map[phase0.Slot][]bundleKey // index for Forget()
}

func (r *rateLimit) Allow*(slot, op, hash, ...) bool {
    // ... existing admit logic ...
    key := bundleKey{slot: slot, op: op, hash: hash}
    r.bySlot[slot] = append(r.bySlot[slot], key)
    // ... existing maps populated ...
}

func (r *rateLimit) Forget(slot phase0.Slot) {
    for _, k := range r.bySlot[slot] {
        delete(r.byBundle, k)
        delete(r.byCount, k)
        // ... per other maps ...
    }
    delete(r.bySlot, slot)
}
```

`Forget` becomes O(entries-for-this-slot) — typically n·MaxDistinctPerOpSlot ≈ tens — instead of O(all-entries).

**Risks:** index sync. Every admit path must add to `bySlot`; missing one leaves the entry stranded after Forget.

### Investigation/F17

**Claim verified at agent-cited line ranges.** Per-Resolve allocations in 2abOBFT Phase 3:

- [twoab/phase3.go:55](protocol/v2/obft/twoab/phase3.go:55): `chainedKeys := make([][]byte, K)` — K-len slice of byte slices.
- [twoab/phase3.go:151](protocol/v2/obft/twoab/phase3.go:151): `groups` map per layer reconstruction attempt.
- [twoab/phase3.go:173-174](protocol/v2/obft/twoab/phase3.go:173): `sigGroup` allocations with inner `partials` map.

Resolve fires opportunistically per state-delta. At, say, n=7 deltas per slot × K=4 layers walked: ~28 layer-reconstruction attempts per slot, each allocating fresh map + group.

**Per-slot cost:** ~30-50 small allocations per slot. GC pressure rather than wall-clock. Modest.

**Fix detail:** Instance-level scratch buffers reset across calls:

```go
type Instance struct {
    // ... existing fields ...
    scratchChainedKeys [][]byte
    scratchGroups      map[[32]byte]*sigGroup
}

func (i *Instance) Resolve() (*Output, error) {
    // Reuse scratch buffers.
    if cap(i.scratchChainedKeys) < i.cfg.K() {
        i.scratchChainedKeys = make([][]byte, i.cfg.K())
    } else {
        i.scratchChainedKeys = i.scratchChainedKeys[:i.cfg.K()]
        for j := range i.scratchChainedKeys {
            i.scratchChainedKeys[j] = nil
        }
    }
    chainedKeys := i.scratchChainedKeys
    // ... existing walk ...
}
```

For the inner `groups` map, `clear(map)` + reuse is the standard pattern (Go 1.21+).

**Risks:**
- Resolve must NOT be called concurrently on the same Instance with shared scratch. Verify the controller's `r.instanceMu` discipline holds (it should — Instance is single-threaded by design).

### Investigation/F18

**Claim verified.** [blsbackend/signer.go:131](protocol/v2/obft/blsbackend/signer.go:131):

```go
if err := blsID.SetDecString(fmt.Sprintf("%d", opID)); err != nil { ... }
```

`fmt.Sprintf("%d", n)` allocates a string. `bls.ID.SetDecString` then parses it back to a number. Pure round-trip allocation.

**Per-slot cost:** `AggregatePartials` is called once per layer that reaches qV (≤ K times per slot). Each call loops over partials (up to n=7). So ~28 fmt.Sprintf allocations per slot. Microscopic in absolute terms.

**Fix detail:** check what herumi's `bls.ID` API offers — typically `SetLittleEndian([]byte)` or `SetInt64(int64)` is available.

```go
var idBuf [8]byte
binary.LittleEndian.PutUint64(idBuf[:], uint64(opID))
if err := blsID.SetLittleEndian(idBuf[:]); err != nil {
    return nil, fmt.Errorf("blsbackend: set bls.ID for op %d: %w", opID, err)
}
```

**Risks:** none. Just need to verify the bytes encoding matches what the existing string-decode produced. (Likely just integer little-endian, but worth a one-test sanity check.)

---

## Benchmarks to write

Tier 1 claims need quantitative grounding before committing to fixes. Each benchmark is a small Go `Benchmark*` function; we run with `-benchmem -benchtime=2s` to get allocation counts and stable timing.

### B1: BLS partial verify (baseline cost)

**Goal:** establish the "~1 ms per verify" assumption that underpins F1, F3, F4, F5.

**Location:** `protocol/v2/obft/blsbackend/signer_bench_test.go` (new).

**Shape:**
```go
func BenchmarkBLSSigner_VerifyPartial(b *testing.B) {
    signer := New(testShare)
    pubShare := testPubShare(testShare)
    msg := make([]byte, 32) // 32-byte signing root
    rand.Read(msg)
    sig, _ := signer.SignPartial(msg)
    b.ResetTimer()
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        if !signer.VerifyPartial(pubShare, msg, sig) {
            b.Fatal("verify failed")
        }
    }
}
```

Same shape for `KyberSigner.VerifyPartial`. Expected results: BLSSigner ~0.5-1.5 ms/op, KyberSigner ~1-2 ms/op (drand kyber slower than herumi by 1.5-2×).

### B2: `signingRootFor` cost

**Goal:** quantify the SSZ-unmarshal + tree-root path for F2.

**Location:** `protocol/v2/ssv/runner/obft/proposer_signer_bench_test.go` (new).

**Shape:**
```go
func BenchmarkProposerSigner_signingRootFor(b *testing.B) {
    s := newTestProposerSigner(b) // wraps StubSigner + test beacon config
    v := makeTestV(b)             // version || SSZ blinded block
    b.ResetTimer()
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _, err := s.signingRootFor(v)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

Expected: ~0.3-2 ms/op depending on block size; dominated by `vBlk.Root()`.

After F2's cache: ~5-20 µs/op (cache hit) + the cached-miss cost on first hit. Verifies F2's magnitude estimate.

### B3: Pubkey re-parse cost

**Goal:** quantify `HerumiPubkeyToKyberG1Point` (or `bls.PublicKey.Deserialize` for the herumi path) for F3 + F6.

**Location:** `protocol/v2/obft/blsbackend/kyber_conversion_bench_test.go` (new).

**Shape:**
```go
func BenchmarkHerumiPubkeyToKyberG1Point(b *testing.B) {
    pubBytes := testPubkeyBytes()
    b.ResetTimer()
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _, err := HerumiPubkeyToKyberG1Point(pubBytes)
        if err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkBLSPublicKey_Deserialize(b *testing.B) {
    pubBytes := testPubkeyBytes()
    b.ResetTimer()
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        var pk bls.PublicKey
        if err := pk.Deserialize(pubBytes); err != nil {
            b.Fatal(err)
        }
    }
}
```

Expected: 100-300 µs/op. A cache-hit version (after fix) is sub-microsecond.

### B4: Batch-verify vs sequential

**Goal:** quantify F4's win at realistic N's.

**Location:** `protocol/v2/obft/blsbackend/multiverify_bench_test.go` (new).

**Shape:**
```go
func BenchmarkBLSSigner_MultiVerify_N3(b *testing.B)  { benchMulti(b, 3) }
func BenchmarkBLSSigner_MultiVerify_N6(b *testing.B)  { benchMulti(b, 6) }
func BenchmarkBLSSigner_MultiVerify_N13(b *testing.B) { benchMulti(b, 13) }

func benchMulti(b *testing.B, n int) {
    sigs, pubs, msgs := generateNVerifies(n) // 32-byte msgs
    concatMsg := bytes.Join(msgs, nil)
    b.Run("sequential", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            for j := 0; j < n; j++ {
                if !sigs[j].VerifyByte(&pubs[j], msgs[j]) {
                    b.Fatal()
                }
            }
        }
    })
    b.Run("batch_MultiVerify", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            if !bls.MultiVerify(sigs, pubs, concatMsg) {
                b.Fatal()
            }
        }
    })
}
```

Expected: at N=3 batch wins ~1.5×; at N=13 batch wins ~3-4×.

### B5: ValueRoot cost (optional)

**Goal:** quantify sha256 over a 1 KB block for F9.

Trivial standalone benchmark — sha256 throughput is well-known (~500 MB/s on modern CPU) so this is mostly a sanity check.

### Running the benchmarks

```
go test -tags "blst_enabled lfs" -bench=. -benchmem -benchtime=2s -count=3 \
    ./protocol/v2/obft/blsbackend/... \
    ./protocol/v2/ssv/runner/obft/...
```

`-count=3` for variance estimate. Results table below, before/after each fix lands.

### Baseline results (Apple M3 Pro, single-shot run)

Numbers from a single bench-run on a fast development machine. Production CPUs (typical cloud x86) will be somewhat slower, but the *ratios* generalise. All in ns/op + alloc count.

**B1 — BLS partial verify cost:**

| Op | ns/op | B/op | allocs/op |
|---|---|---|---|
| `BLSSigner.VerifyPartial` (herumi) | **934 µs** | 432 | 2 |
| `KyberSigner.VerifyPartial` (kyber) | **1,335 µs** | 68,744 | **209** |
| `BLSSigner.SignPartial` (herumi) | 337 µs | 416 | 3 |

Confirms the "~1 ms per verify" assumption used throughout the plan. Kyber is ~43% slower than herumi AND allocates 100× more — F3 is a real win even ignoring the time cost, just for GC pressure.

**B2 — `signingRootFor` cost across realistic block sizes:**

| Block content | V size | ns/op | B/op | allocs/op |
|---|---|---|---|---|
| Minimal (0 attestations) | 1,063 B | ~5,700 ns | — | — |
| 32 attestations | 9,031 B | **54.6 µs** | 11,982 | 176 |
| 64 attestations | 16,999 B | **101.6 µs** | 22,237 | 336 |
| 128 attestations (MAX_ATTESTATIONS) | 32,935 B | **204.8 µs** | 42,935 | 656 |

Scales ~linearly with block size: O(size). At a realistic 17 KB block: **~100 µs per call**, not the ~0.5 ms I'd estimated in F2's investigation. The original estimate was 5-10× too high. F2's revised per-slot impact is **~5-10 ms/slot at n=7, K=4** (50-100 calls × 100 µs), not ~75 ms. Updating F2 below.

The allocation cost is more interesting: 336 allocs / 22 KB per call × 100 calls/slot = ~2-4 MB of GC churn per slot from this path alone. Caching eliminates that even if the wall-clock saving is modest.

**B3 — Pubkey + share parse cost:**

| Op | ns/op | B/op | allocs/op |
|---|---|---|---|
| `HerumiPubkeyToKyberG1Point` (G1 + subgroup check) | **113.8 µs** | 7,736 | 54 |
| `bls.PublicKey.Deserialize` (herumi G1) | **83.3 µs** | 144 | 1 |
| `bls.SecretKey.Deserialize` (herumi scalar) | **106 ns** | 32 | 1 |
| `HerumiShareToKyberScalar` (kyber scalar) | **102 ns** | 112 | 2 |

Pubkey parsing is much more expensive than secret-key parsing — G1 point decompression + subgroup check dominates. F3's caching saves ~100 µs per kyber verify; F6's secret-key caching saves only ~100 ns per sign (revised below).

**B4 — Batch (`MultiVerify`) vs sequential at realistic N:**

| N | Sequential | MultiVerify | Speedup |
|---|---|---|---|
| 3 (≈ K-1 in QBFT-3) | 3.29 ms | 2.28 ms | **1.44×** |
| 6 (≈ qV at n=7) | 6.57 ms | 4.01 ms | **1.64×** |
| 13 (full cluster) | 13.64 ms | 7.58 ms | **1.80×** |

Win is smaller than the 2-4× I estimated. MultiVerify carries a fixed overhead (~1.5 ms) that dilutes the benefit at small N. Per-slot saving for F4 revised below.

### Revised per-slot impact estimates

After running the benchmarks, the magnitudes shift:

| Finding | Plan estimate | Measured | Revised |
|---|---|---|---|
| F1 (Resolve re-verify) | ~70 ms/slot | confirmed (10 redundant × 1 ms × 7 Resolves) | **~70 ms/slot** ✓ |
| F2 (signingRootFor) | ~75 ms/slot | 100 µs/call × ~80 calls | **~8 ms/slot** (10× lower) |
| F3 (kyber pubkey parse) | ~30 ms/slot | 114 µs/call × ~108 kyber verifies (NR side) | **~12 ms/slot** (~2.5× lower) |
| F4 (batch verify) | ~22 ms/slot | 1.5-1.8× speedup × ~25 candidate verifies | **~8-10 ms/slot** (~2× lower) |
| F5 (verifyCommitNRPartials defense-in-depth) | ~18 ms/slot | unchanged (18 × 1 ms) | **~18 ms/slot** ✓ |
| F6 (BLSSigner.SignPartial share parse) | ~200-500 µs | 0.1 µs/parse × ~2K = ~0.2 ms | **~0.2 ms/slot** (1000× lower — basically negligible) |

Tier 1 + F5 + F6 revised total: roughly **~115 ms saved per slot at n=7, K=4** (was 215 ms in the plan). Still significant, but F2's magnitude was the most over-estimated single line.

**Implications for sequencing:**

- F6 drops from a meaningful win to a one-line cleanup that happens to also amortize ~0.2 ms. Worth doing as a bundle with other touches to `BLSSigner`, but not on its own merit.
- F2 stays a real win, mostly for the GC-allocation reduction (336 allocs × ~80 calls = ~27K allocs/slot avoided) — wall-clock saving is modest (8 ms).
- F1 remains the single biggest win by a wide margin.
- F3 + F4 combine for ~20-22 ms/slot — still material.

## Sequencing recommendation

Proposed commit order if implementing — revised after benchmarks:

1. **Benchmarks first** (DONE). B1-B4 landed in `*_bench_test.go` files; baseline numbers in [§Baseline results](#baseline-results-apple-m3-pro-single-shot-run).
2. **F1 + F5** (cache verified-partial bit + gate redundant re-verify). **The single biggest win — ~88 ms/slot combined.** Requires safety review since these touch the BLS verify discipline. Pair them because they share the same "trust the upstream verify" principle.
3. **F3** (cache parsed pubkey G1 points in `KyberSigner`). ~12 ms/slot. Contained to `KyberSigner` + mutex. Re-run B1 / B3 to confirm.
4. **F4** (batch-verify API via herumi `MultiVerify`). ~9 ms/slot. New `Signer.VerifyPartialBatch` method, callers in `verifyCommitNRPartials` and σ-walk updated. Re-run B4.
5. **F2** (cache signingRoot per V). ~8 ms/slot wall-clock + significant allocation reduction. Contained to `proposerSigner` + mutex. Re-run B2.
6. **F9 + F8 + F7** (ValueRoot caching, vByRoot index, layer→entry index). The index-on-ingest cluster. Bigger refactor; do as a batch with thorough test coverage. Per-slot saving ~5-10 ms but mostly algorithmic cleanup.
7. **F10** (host-validity verdict cache). Scheduler-only change. Modest saving but lowest risk.
8. **F11-F18 + F6** as time permits. Independent, lower-risk, lower-impact. Bundle into a "cleanup" commit per area (and the F6 one-liner gets folded into the BLSSigner-touching commit).

Each step CI-green standalone.

**Why F1 moved to front:** F1 is benchmarked at ~70 ms/slot wall-clock saving — bigger than F2+F3+F4 combined (~30 ms). The other Tier-1 items are now confirmed as smaller-than-estimated; the leverage ratio (saving / risk) is best on F1 even accounting for the safety review needed.

**F6 dropped from a separate commit** because the benchmark showed it saves only ~0.2 ms/slot (1000× less than the plan estimated). Still worth doing for code hygiene, but folds into whatever commit touches `BLSSigner`.

## Open questions — answered

### Q-Open-1: Does consensustest's path go through the production `Verifier`?

**Answer: No.** The consensustest framework's OBFT adapter at [consensustest/obft/adapter.go](protocol/v2/consensustest/obft/adapter.go) drives `obftbase.Instance` directly via the DES (`runDES(desCfg)` at line 172). It does NOT call `obftbase.Verifier.VerifyCommitNRPartials` first. The production-validation `Verifier` is only constructed by the runner-layer's `NewVerifierFromShare` at [runner/obft/verifier.go:29](protocol/v2/ssv/runner/obft/verifier.go:29); consensustest doesn't build it.

**Implication for F5:** the `SkipDoubleVerify` flag MUST default `false` for safety, and ONLY the runner-layer construction may set it `true`. The consensustest framework's adapter relies on the in-Instance verify to catch adapter bugs (malformed NR partials from a buggy byz-translation, for instance). The docstring at [base/phase2.go:321-323](protocol/v2/obft/base/phase2.go:321) explicitly called this out: "defense-in-depth for any path that bypasses validation (tests, future plumbing)" — confirmed.

F5's fix is unchanged in approach but the risk section needs the explicit "MUST keep `false` in consensustest" callout.

### Q-Open-2: Is `Resolve()` ever called concurrently on the same Instance?

**Answer: No — protected by `r.instanceMu`.** `Controller.Resolve` at [runner/obft/controller.go:426](protocol/v2/ssv/runner/obft/controller.go:426) goes through `withLiveInstance`, which acquires `r.instanceMu.Lock()` ([controller.go:482](protocol/v2/ssv/runner/obft/controller.go:482)) before calling `r.instance.Resolve()`. Every state-touching Controller method follows the same `lookup → lock → ended-check` pattern (documented at [controller.go:466-475](protocol/v2/ssv/runner/obft/controller.go:466)).

**Implications:**

- **F17 (scratch buffers):** safe to reuse Instance-level scratch without extra locking — Resolve is single-threaded per Instance.
- **F1 (verify-cache):** if the cache lives on `Instance`, no extra locking needed — Instance is single-threaded.
- **F3 (KyberSigner pubkey-cache):** the KyberSigner is shared between the production Verifier (called from message-validation goroutines) AND the in-Instance verify path (called under `r.instanceMu`). These run on different goroutines — the pubkey cache DOES need a mutex (or `sync.Map`). Already captured in F3's risk section; reaffirmed.
- **F2 (signingRoot cache):** the `proposerSigner` is wrapped around an inner signer. If both the production Verifier and Instance share the same `proposerSigner` instance, the cache needs a mutex. Same as F3. Worth confirming whether the Verifier and Instance get separately-constructed `proposerSigner` instances — if yes, no shared state, no lock. Either way the safe default is a mutex.

### Q-Open-3: What's the actual size of a blinded Beacon block in V?

**Answer: typically 5-30 KB SSZ-marshaled.** Blinded blocks ship only the execution payload header (~600 bytes), not the full payload, so the size is dominated by the BeaconBlockBody contents — attestations (up to MAX_ATTESTATIONS = 128 pre-Electra, each ~228 bytes), proposer slashings, attester slashings, deposits, voluntary exits, sync aggregates (~160 bytes), and BLS-to-execution changes. Real-world blinded blocks measured around mainnet typically land in the **10-25 KB range**; Electra increases the upper bound with larger committees but the median stays similar.

**Implication for F2 (initial reasoning):** at ~5-15 µs/KB for tree-root, a 20 KB block's `vBlk.Root()` is ~100-300 µs; combined with SSZ-unmarshal, `signingRootFor` is ~0.3-0.8 ms per call. **Measured by B2:** actual is ~100 µs/call at 17 KB (closer to the low end), and the per-slot call count is ~80, not 150 — so F2's saving is **~8 ms/slot, not ~75 ms**. The order-of-magnitude estimate was off; the mechanism is right but the constant was over-pessimistic.

For F9 (ValueRoot caching): same magnitude applies — `ValueRoot` is sha256 over the full V bytes (the [version | SSZ blinded block] envelope). At 20 KB and ~5 µs/KB, ~100 µs per call. ~30-50 calls/slot → 3-5 ms/slot. Earlier estimate of "~150-250 µs/slot" assumed a much smaller V (~1KB) — the actual magnitude is **~3-5 ms/slot**, an order of magnitude bigger. Updating F9 below.

### Q-Open-4: Does the runner-layer `Verifier` cache verified-partial bits already?

**Answer: No.** [base/verify.go:50-68](protocol/v2/obft/base/verify.go:50) shows the `Verifier` struct holds only `Signer`, `TagSigner`, `PubKeyShares` (parsed at construction), `NRPubKeyShares`, `ClusterPubKey`, and `LeaderForLayer`. No verify-result cache. Every `VerifyPhase1Bundle` / `VerifyCommitNRPartials` / `VerifyCertificate` call goes through to the underlying signer.

**Implication for F1:** the fix can't reuse anything from the Verifier — Instance has to maintain its own verify-cache. This is actually cleaner: no risk of cache desync between layers. The verify-cache lives on Instance and is populated at every insertion-time verify path. F1's risk section already captured the "every insertion path must populate the cache" requirement.

**Bonus finding:** since the `Verifier` is stateless (no cache), the runner-layer verify cost is also fully paid every time. F5's fix (skip in-Instance verify in production) leaves the Verifier's verify as the single source — which is correct. No double-cache concern.

## Open questions — implications summary

After these answers, the plan stands as written with three refinements:

- **F9 magnitude scaled up** to ~3-5 ms/slot (was ~150-250 µs). V is 10-30 KB, not 1 KB. The fix is still memoization in `Phase1Bundle` / `EncryptedLayer`, same shape — just bigger payoff.
- **F5 risk callout strengthened**: the consensustest path doesn't go through production `Verifier`. The new `SkipDoubleVerify` flag MUST default `false`.
- **F2/F3 concurrency confirmed**: the in-Instance side runs under `instanceMu`, but the production-Verifier side runs on separate goroutines. Caches inside the shared signer adapters need mutex protection. (No change to fix shape — F2 + F3 already included the mutex.)

## Out of scope

- Deep refactor: an `IncrementalResolve()` API that picks up from the last layer instead of starting over. Noted as a possible Phase 4 follow-up.
- Profiling-driven optimizations beyond the obvious. This audit is read-only; real profiles may surface different hot spots.
- Crypto algorithm changes (e.g., swapping BLS for a different scheme). Out of scope.

## Status

- Audit discovery: complete (4 parallel general-purpose agents).
- Investigation: complete (F1-F18 verified against code; magnitude estimates refined).
- Self-review: complete (summary-table magnitudes reconciled with investigations; pending source-locate placeholders filled in; F18 file-path corrected; combined-impact arithmetic re-summed).
- Open questions: complete (Q-Open-1..4 answered against code; F5/F9 updated to reflect findings).
- Benchmarks: **complete** (B1-B4 in `*_bench_test.go` files; baseline numbers captured; magnitudes for F2/F3/F4/F6 revised downward after measurement).
- Implementation: in progress. Landed so far:
  - **F5 base** — `Config.SkipNRPartialReverify` gate on `verifyCommitNRPartials` (commit `0686b8028`; cross-ref tightening `a8075cb0e`); production runner opts in. ~18 ms/slot saved on production path.
  - **F1 base** — per-Instance verify cache with `(op, layer, valueRoot, partialRoot)` key (commit `534f60ec9`; cross-V safety fix `3c26dd664`). The self-review caught and fixed a cross-V leakage hole in the initial 3-field key. ~70 ms/slot saved.
  - **F1 + F5 mirror to 2abOBFT** — same gate inside `verifyNRTagPartial`, same value-bound cache on twoab Instance, wired at the L_k>0 σ-walk (commits `1b03f45e6` + test-split `b9b7013bb`). Twoab's L_0 σ pool is pre-verified at observation so the cache only buys L_k>0.
  - **F3** — `KyberSigner.pubCache` (commit `8189c7768`). Per-signer `map[string]kyber.Point` guarded by `sync.RWMutex`; warm calls take only an RLock + map read; allocs/op drop from 209 → 156 (the 54-alloc pubkey-parse path eliminated). Per-call ~85 µs steady-state saving; wall-clock improvement masked by system noise on the local M3 Pro bench but the alloc reduction is a real ~5K-allocs/slot GC pressure cut.
- Remaining Tier-1 work: **F4** (BLS batch-verify via herumi `MultiVerify`) — touches the `Signer` interface; needs a focused plan doc analogous to F1+F5's. Pre-existing `bls.MultiVerify` + Go `-race`/`checkptr` interaction surfaced in B4 fixture will be addressed alongside.
