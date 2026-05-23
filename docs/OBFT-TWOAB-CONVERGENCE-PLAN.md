# OBFT ↔ 2abOBFT Convergence Plan

Status: applied, 2026-05-23 (phases 1–4 landed across the convergence commits; the deferred architectural questions E1–E4 remain future work).
Author: derived from systematic audit of `protocol/v2/obft/base` vs `protocol/v2/obft/twoab` after three review-rounds surfacing 22 findings.

## Executive summary

The two consensus implementations share an outer `obft` package (Signer, ThresholdIBE, NoQuorumTag primitives) and a similar lifecycle but have drifted apart in coding patterns: ~60 distinct asymmetries across 15 categories. About 25 are forced by protocol semantics (twoab's Phase 2a split, Rule 6a, L0Witness vs SigmaV, trigger-driven Phase 2b vs T_commit deadline) and must stay. About 35 are stylistic choices that can — and should — converge.

The convergence target is **not** "twoab matches base" or "base matches twoab" mechanically. Each pattern is analyzed for the more-robust / cleaner direction. The result is:
- ~15 changes flow from twoab to base (twoab is DRY-er on pool helpers, has Stats(), has nil-guard discipline, etc.).
- ~10 changes flow from base to twoab (base has stronger lifecycle guards, sentinel-error discipline, clusterPubKey-emptiness check, etc.).
- ~5 bidirectional cleanups (extract shared helpers, harmonize doc conventions).
- 1 deferred architectural question (snapshot-at-Finalize) called out as future work.

Rollout: 4 phases, each landing as an independent set of commits.

---

## 1. Methodology

Audit pass: systematically compared every non-test source file across both packages, categorized into 15 themes (lifecycle guards, error handling, defensive copy, pool aggregation, cascade processing, channel-based host integration, evidence accumulation, validation, constructor, wire format, stats/introspection, EKM, naming, docs, other). For each divergence: recorded file:line, brief description, and a YES/NO/PARTIAL on whether protocol semantics force the divergence.

Decision rule per finding: prefer the pattern with stronger defenses **only when** the defense maps to a real failure mode that has bitten or is likely to bite. Avoid blanket defensiveness ("guard everywhere because base does") and avoid blanket simplification ("remove guards because twoab doesn't"). Pick the direction that converges on cleanest-and-most-robust per finding.

---

## 2. Categories

### Category A — Protocol-forced divergences (KEEP)

These are direct consequences of the protocols' different mechanics. No work to do.

- twoab has Phase 2a Value/NoValue split → distinct message kinds, valuePool/noValuePool, KindValue wire format, validateLayerEntries helper, A1 upgrade path, MaybeFire* naming.
- twoab has Phase 2b trigger-driven emission (no T_commit deadline) → afterStateDelta cascade, cascadeErrors accumulator, MaybeBuild* methods, no observedTimeOK gate, NR-eligibility cannot-σ gate.
- twoab has Op3 L0Witness in Phase-1 bundle + Op11 peer-harvest → verifyL0WitnessCached, RetentionSource enum, maybeHarvestPhase1BundleFromValueMsg, retainedBundle wrapper.
- twoab has Op5 σ-partial-in-KindValue → no Commit-Signed wire kind, L0Partial field, verifyAndPoolL0Partial.
- twoab has Rule 6a (Phase-2 equivocation) → Phase2EquivocationEvidence type, rule6aFired slot-wide bucket.
- base has T_commit hard wall → observedTimeOK, ErrLatePhase1Bundle, CommitState enum (with CommitNRSilent post-T_commit semantics), L0ReadyCh signal, witness section in Commit.

This category accounts for ~25 of the ~60 findings. They are documented for completeness; no action.

---

### Category B — Convergence flow: twoab patterns adopted by base

These twoab patterns are cleaner / more robust and should be backported.

#### B1. `i == nil` receiver guards on public mutators

**twoab** has them on every public method (e.g. `phase1.go:37`, `phase2a.go:50`, `phase2b.go:127`). **base** has none.

**Rationale to adopt in base**: cheap defense against nil-receiver panics; matches Go-stdlib idioms (e.g., `(*url.URL).String()` checks `u == nil`). Zero protocol cost. Returns `fmt.Errorf("obft: nil instance")` to match twoab's pattern. ~10 lines added per package.

**Action**: add `if i == nil` to every public method in base. Estimated: 10 method-level additions across base/phase1.go, base/phase2.go, base/phase3.go, base/instance.go.

#### B2. ~~Pool-init helper extraction~~ (DROPPED on execution)

**twoab** centralizes pool operations: `addToValuePool`, `addToNoValuePool`, `removeFromNoValuePool`, `addToSigmaPool`, `addToNrTagPool` etc. (`twoab/instance.go:821-888`). **base** inlines `if i.peerOnions[k] == nil { i.peerOnions[k] = make(...) }` at the call sites.

**Self-review on execution**: only 4 call sites in base actually have the lazy-init pattern (2 each for peerOnions, peerNR; total ~12 LOC of init code). Twoab's helpers encapsulate both the layer-map init AND the inner entry set with uniform semantics (bool-valued claim pools). In base, the inner entry semantics differ per call site — peerOnions appends with own-vs-peer dedup, peerNR conditionally overwrites with first-write-wins, witnessedLeaderSigma stores a struct with verify metadata. Helper extraction would either need multiple wrapper-variants (negating the DRY win) or fold all the dedup logic into one helper (which obscures the per-site semantic differences). The inline form is the more honest representation.

**Action**: none. Inline pattern stays.

#### B3. `requestHostValidation` `i.ended` guard

**twoab/instance.go:441-444** has the guard with explicit "send-on-closed-channel" rationale. **base/instance.go:484** lacks it.

**Self-review correction**: in base today, `requestHostValidation` is structurally unreachable post-Finalize — its sole caller is `maybeRequestL0PeerVValidation` (base/phase2.go:580), called only from `ObserveCommit` (base/phase2.go:27, which has the `i.ended` guard). So adding the guard is NOT a panic fix — it's defense-in-depth against a future refactor that adds a new caller from an unguarded path.

In twoab the situation is symmetric once C1 lands: all callers of `requestHostValidation` will be guarded, so the inner guard becomes belt-and-suspenders.

**Action**: add `if i.ended { return }` at top of `base/instance.go requestHostValidation`. One line. Pure defense-in-depth; no behavior change today.

#### B4. `recordRulePerLayer` shared template

**twoab/instance.go:770** has a shared `recordRulePerLayer(bucket, op, layer) bool` template used by `recordRule1`, `recordRule3`, `recordRule4`, `recordRule5`. **base/instance.go:887-963** has 5 explicit `recordRuleN` implementations with identical-but-duplicated lazy-init + first-fire logic.

**Rationale to adopt in base**: DRY. Each base helper is ~12 lines of boilerplate; the template form is ~4 lines per call site. Net ~40 LOC reduction, easier to add Rule N in the future.

**Action**: extract `recordRulePerLayer` template in base; rewrite 4 of the 5 helpers using it. Keep `recordRule2` as-is (slot-wide shape).

#### B5. Stats() introspection method

**twoab/instance.go:509-560** has `Stats() InstanceStats`. **base** does not.

**Rationale to adopt in base**: provides public-API introspection parity with twoab; future external test/telemetry consumers can use it uniformly across protocols.

**Action**: add `Stats() InstanceStats` to base, with fields matching twoab's shape where applicable: `PendingValidationCount`, `WitnessedLeaderSigmaCount` (base-specific), `EvidenceCount`, `Ended`. ~~Refactor existing tests that reach into unexported state.~~

**Self-review on execution**: the test-refactor sub-task was reconsidered. The base tests flagged by the audit (e.g. `base/instance_test.go:1345-1346` reading `receiver.peerOnions[0]` / `receiver.peerNR[0]`) are package-internal white-box tests — direct unexported-state access is the correct idiom for them. Retrofitting them to use Stats() would require bloating Stats with `PeerOnionsCount[layer]` / `PeerNRPartialsCount[layer]` / etc. (counters useful only to those tests) to compensate. The Stats() API stays focused on the small set of counters with broad value; future external/black-box tests can use it, but the existing white-box tests stay as they are.

#### B6. `MessageKind.String()` method

**twoab/wire/envelope.go:49-64** has it. **base/wire/envelope.go** does not.

**Rationale to adopt in base**: trivial diagnostic helper. Logging / error messages benefit. Five-line addition.

**Action**: add `String()` method to `base/wire/envelope.go`.

#### B7. `selectWinningGroup` helper extraction

**twoab/phase3.go:342-357** extracts the lex-tiebreak σ-pool group selection into a standalone helper. **base/phase3.go:212-226** inlines it.

**Rationale to adopt in base**: testability. The lex-tiebreak logic is non-trivial and currently has no isolated test. Extracted form makes it independently testable. ~10 lines of refactor.

**Action**: extract `selectWinningGroup` in base; add unit test.

#### B8. Sentinel-error wrapping discipline

**twoab/errors.go** defines `ErrNotLeader`, `ErrEmptyValue`, `ErrLayerOutOfRange` and wraps them: `fmt.Errorf("twoab: %w: ...", ErrLayerOutOfRange, ...)`. **base** uses ad-hoc `errors.New("obft: ...")` / `fmt.Errorf("obft: ...")` with no sentinels for these same error classes.

**Rationale to adopt in base**: callers can `errors.Is(err, ErrNotLeader)` to programmatically distinguish. Today base callers must string-match. Cost is small (~5 sentinels added to base/errors.go).

**Action**: define `ErrNotLeader`, `ErrEmptyValue`, `ErrLayerOutOfRange` in base/errors.go. Refactor existing ad-hoc errors at the ~10 sites in base/phase1.go and base/phase2.go to use the new sentinels.

#### B9. `clusterPubKey` emptiness check

**twoab** lacks this; **base/instance.go:397-399** has it. Wait — this is base→twoab. Listed under C2.

---

### Category C — Convergence flow: base patterns adopted by twoab

These base patterns are stronger defenses and should be added to twoab.

#### C1. `i.ended` guards on Observe*/Build*/Resolve methods

**This is the big one.** Discussed at length in the prior review-round.

**base** has the guard on every state-mutating public method (returning `ErrInstanceEnded`). **twoab** has it only on `ApplyHostValidity` and `requestHostValidation`.

**Decision**: adopt the base pattern in twoab. Add `if i.ended { return ErrInstanceEnded }` to: `BuildPhase1Bundle`, `ObservePhase1Bundle`, `MaybeFirePhase2a`, `MaybeBuildAndBroadcastUpgrade`, `MaybeBuildAndBroadcastCommit`, `ObserveValueMsg`, `ObserveNoValueMsg`, `ObserveCommit`, `Resolve`, `ObserveCertificate`, `BuildCertificate`. ~10 method-level additions.

**Rationale**: belt-and-suspenders defense against runner-dispatch race. The earlier analysis suggested the runner can "drain Observe* before Finalize" as the discipline, but:
1. The runner adapter for twoab is not written yet (Phase L). Designing the dispatch discipline in advance is brittle.
2. The cost is one line per method.
3. Slashing-evidence late mutation IS a real concern: a post-Finalize `ObserveValueMsg` can fire Rule 3 / Rule 6a evidence, mutating `i.evidence`. If the runner has already snapshotted via `Evidence()`, the snapshot diverges from internal state.
4. Once base ships with the guards, the cross-package mental model is uniform.

**Counter-argument considered**: the principled fix is snapshot-at-Finalize (make `Evidence()`/`RetainedBundles()`/`Stats()` deep-copy at call time and freeze the runner's view at Finalize-time). This is correct but is a bigger refactor with more surface area. Adding the guard now does not preclude landing the snapshot pattern later; it is the cheaper interim defense.

**Action**: add the guard. Sentinel = `ErrInstanceEnded` (re-use, define in twoab/errors.go).

**Open in this category** (deferred to future): the snapshot-at-Finalize design. Documented in Category E as a placeholder for the Phase L runner design.

#### C2. `clusterPubKey` emptiness check in NewInstance

**base/instance.go:397-399** rejects empty `clusterPubKey`. **twoab/instance.go** does not.

**Rationale to adopt in twoab**: catches a real configuration bug at construction rather than at certificate-verification time. The error message ("IBE trust anchor required") guides operators to the right config field. Three-line addition.

**Action**: add the check in `twoab.NewInstance`.

#### C3. `MaxRetainedPerOpLayer` named constant

**base/instance.go:43** defines `MaxRetainedPerOpLayer = 2`. **twoab/phase1.go:223** inlines `if len(retained) >= 2`.

**Rationale to adopt in twoab**: named-constant discipline. The cap is a spec-level invariant (per [2abOBFT.md §Phase 1](2abOBFT.md#phase-1--candidate-broadcast) Retention bounds), and inlining it scatters the policy. A const declaration centralizes the invariant.

**Action**: add `MaxRetainedPerOpLayer = 2` in `twoab/instance.go`. Reference it at the one inline site in `twoab/phase1.go`.

**Semantic-mismatch note (L5)**: the constants share name and value across packages but apply to slightly different surfaces — base caps both Phase-1 bundle retention AND σ-onion entry retention with the same constant; twoab caps only Phase-1 bundle retention (twoab's σ partial moved into `KindValue.L0Partial`, leaving no separate σ-onion message to cap). Documented in both constants' docstrings.

#### C4. `ApplyHostValidity` verdict-flip handling

**base/phase1.go:432-437** rejects verdict-flips with an error. **twoab/phase2a.go:65-68** silently overwrites.

**Decision**: **diverge intentionally**. The two protocols have different host-revalidation models — base treats host verdict as a one-time commit-time decision; twoab treats it as a flowing signal that can flip mid-slot (the docstring at `twoab/phase2a.go:25-28` documents this). Forcing convergence would change protocol semantics in one direction.

**Action**: document the divergence at both sites with a forward reference to the other (landed in Phase 4 commit `71275fd97`).

#### C5. `i.ended` guard on `ObserveCertificate` / `Resolve`

Subset of C1 above; covered by the same fix.

---

### Category D — Bidirectional cleanups

These should be addressed in both packages simultaneously.

#### D0. Promote `ErrInstanceEnded` to parent `obft` package

After C1 lands in twoab, both packages will have `ErrInstanceEnded`. Today base/errors.go:111 defines it, twoab/errors.go does not.

**Rationale**: structural lifecycle errors (instance finalized, nil instance, etc.) describe an Instance-API contract that exists at the same conceptual level in both protocols. Putting them in the parent `obft` package matches `obft.Signer`, `obft.ThresholdIBE`, `obft.NoQuorumTag` — primitives that span both protocols.

Protocol-specific sentinels (`ErrUpgradeNotAvailable`, `ErrLatePhase1Bundle`) stay per-package.

**Action**: define `obft.ErrInstanceEnded` in a new `obft/errors.go` (or extend `obft/types.go`). Re-export from base and twoab as type aliases for backward compatibility:
```go
// in base/errors.go
var ErrInstanceEnded = obft.ErrInstanceEnded
```
**Risk**: existing callers using `base.ErrInstanceEnded` continue to work via the re-export. Cross-package `errors.Is(err, base.ErrInstanceEnded)` and `errors.Is(err, obft.ErrInstanceEnded)` both succeed.

#### D1. Extract `operatorInCluster` to shared `obft/`

**base/validation.go:165-172** and **twoab/validation.go:299-306** are identical helpers.

**Action**: move to `obft/util.go` or extend `obft/types.go`. Both packages import.

#### D2. Harmonize `deepCopyBundle` style

**base/phase1.go:349-358** uses imperative full-field copy. **twoab/phase1.go:307-312** uses struct-literal + slice-field overrides.

**Decision**: twoab's style is shorter and harder to forget a field on. Adopt in base.

**Action**: refactor `base/phase1.go deepCopyBundle` to struct-literal style.

#### D3. ~~Harmonize `valueRootKey` helper~~ (DROPPED on self-review)

**base/instance.go:736-739** has the helper. **twoab** uses inline `string(ValueRoot(v)[:])`.

**Self-review decision**: SKIP. Both forms compile to identical code; the helper is just locally-named in base. Extracting to `obft.ValueRootKey` would force twoab to either adopt the helper (one extra function-call layer for no semantic gain) or keep the inline form (and the "shared helper" is then only used by base — same duplication situation, just relocated). The asymmetry here is a style preference with no robustness consequence. Document and move on.

**Action**: none. Both forms remain valid; consider documenting at one or both sites if a future reader is confused.

#### D4. Harmonize evidence file organization

**base** declares `EvidenceObserver` in `instance.go`; **twoab** declares it in `evidence.go`. **base** declares `evidenceObservedKey` in `instance.go`; **twoab** declares it in `evidence.go`.

**Decision**: twoab's organization is cleaner (all evidence-related types in evidence.go). Adopt in base.

**Action**: move `EvidenceObserver` + `evidenceObservedKey` to `base/evidence.go`.

#### D5. Harmonize content-hash helper style

**base/messages.go:203-246** has one `commitContentHash`. **twoab/messages.go:369-414** has three content-hash funcs plus explicit `writeUint32`/`writeUint64` helpers that avoid `binary.Write`'s silently-discarded errors.

**Decision**: twoab's explicit-error pattern is stronger (catches errors `binary.Write` would discard). Adopt in base.

**Action**: refactor `base/messages.go commitContentHash` to use explicit `writeUint32`/`writeUint64` helpers (or import twoab's). Reduces silent-discard footprint.

#### D6. Wire-version migration documentation

**twoab/wire/wire.go:13-25** documents the cluster-cutover lockstep policy. **base/wire/wire.go** has no equivalent docstring.

**Action**: backport the cluster-cutover policy docstring to `base/wire/wire.go`.

---

### Category E — Open architectural questions (deferred)

These require deeper design work and are out of scope for the convergence pass.

#### E1. Snapshot-at-Finalize design

**Question**: should `Evidence()`, `RetainedBundles()`, `Stats()` etc. deep-snapshot at Finalize-time, so the runner's view is frozen regardless of subsequent internal state mutations?

**Why deferred**: this is a runner-Instance API redesign. The current C1 fix (add `i.ended` guards) is the cheaper interim defense; the snapshot pattern is the cleaner long-term fix. Defer to Phase L runner design (`docs/2abOBFT-PHASE-L-PLAN.md`).

**Owner**: whoever designs the Phase L runner adapter.

#### E2. Pool-removal API surface

**twoab** has `removeFromNoValuePool` (load-bearing for A1 upgrade). **base** has `removeOnionEntry` (load-bearing for Rule 5 cryptoFake drop). These are different shapes for different reasons. Worth considering: should the underlying pool abstraction grow a uniform remove-by-(op, kind, value) API?

**Why deferred**: requires designing the unified pool abstraction. Out of scope for an audit pass.

#### E3. Cascade error model

**twoab** stores `cascadeErrors []error` with a 100-entry cap. **base** has no cascade and thus no analog. The user feedback from a prior review pass was: "should errors be stored at all, or channeled out via observer-style emit?". This is a real design question for both packages once they grow more cascade points.

**Why deferred**: requires picking a unified async-error model (channel? observer callback? both?). Out of scope.

#### E4. Verifier abstraction for SSV runner boundary

**base/verify.go** exists as a 204-line Verifier abstraction for the SSV-runner-side message-verify boundary. **twoab** has no equivalent.

**Why deferred**: the twoab runner adapter is Phase L work. The Verifier abstraction should land alongside that adapter, mirroring base/verify.go's shape.

---

## 3. Phased rollout

### Phase 1: bidirectional cleanups (low risk, high signal)

Land first because they touch the smallest surface and provide immediate value.

- D0. Promote `ErrInstanceEnded` to parent `obft` package (forward-prep for C1).
- D1. Extract `operatorInCluster` to shared.
- D2. Harmonize `deepCopyBundle` style.
- ~~D3.~~ (dropped on self-review — style-only, no robustness consequence.)
- D4. Move `EvidenceObserver` + `evidenceObservedKey` to evidence.go in base.
- D5. Adopt `writeUint32`/`writeUint64` helpers in base.
- D6. Backport wire-version migration docs to base.

**Estimated**: 1-2 commits (split if D5 refactor balloons), ~200 LOC net (extraction + refactor + new shared file).

### Phase 2: base adopts twoab's hygiene patterns (medium risk)

- B1. `i == nil` receiver guards on base public methods.
- B2. Pool-init helper extraction in base.
- B3. `requestHostValidation` `i.ended` guard in base.
- B4. `recordRulePerLayer` template in base.
- B5. `Stats()` method in base.
- B6. `MessageKind.String()` in base.
- B7. `selectWinningGroup` helper extraction in base.
- B8. Sentinel-error wrapping discipline in base.

**Estimated**: 2-3 commits separated by concern (guards/helpers/stats), ~300 LOC net.

### Phase 3: twoab adopts base's lifecycle guards (medium risk)

- C1. `i.ended` guards on Observe*/Build*/Resolve in twoab.
- C2. `clusterPubKey` emptiness check in twoab.
- C3. `MaxRetainedPerOpLayer` named constant in twoab.

**Estimated**: 1 commit, ~60 LOC net.

### Phase 4: documentation pass

- Cross-reference plan (`docs/OBFT-TWOAB-CONVERGENCE-PLAN.md`) at top of each instance.go preamble.
- Update each diverged-by-design site with a forward-pointer to the sibling.
- E1-E4 logged as TODOs in the Phase L plan doc.

**Estimated**: 1 commit, doc-only.

---

## 4. Non-goals

To prevent scope creep:

- **No protocol change.** Convergence is a coding-pattern alignment, not a protocol redesign. Anywhere semantics differ, document the divergence; do not change behavior.
- **No public API breakage.** Sentinel-error additions are backward-compatible (callers using ad-hoc string-match continue to work); helper extractions don't change exported signatures. Stats() and similar additions are pure expansions.
- **No new test infrastructure.** Use existing test helpers in each package. Stats() addition unlocks test cleanups but those are out of scope.
- **No Phase L runner work.** The runner adapter for twoab is its own design and its own scope. This plan touches the Instance layer only.

---

## 5. Acceptance criteria

- Phase 1 lands cleanly: tests green in both packages.
- Phase 2 lands cleanly: tests green; base callers (existing tests + any internal users of base's public API) work without modification (the guards are additive — they only error on a path that was previously panic-prone or silently mutating).
- Phase 3 lands cleanly: twoab tests green; the runner consumer of twoab (consensustest/twoab adapter) updated to handle `ErrInstanceEnded` returns from Observe*.
- Phase 4: this document moved from `draft` → `applied` once all phases land.

---

## 6. Risks

- **R1**: base callers may have callers we don't know about, internal or external. The sentinel-error refactor (B8) is backward-compatible at the string level but changes the type of returned errors. Mitigation: keep the existing error strings verbatim; only add `%w` wrapping with sentinels.
- **R2**: `i.ended` guards on twoab Observe* methods will change the error-return-value contract. The twoab consensustest adapter currently doesn't expect `ErrInstanceEnded` from Observe* calls. Mitigation: audit the test-adapter dispatch loop and add the error-handling branch; should be a 5-10 line change in `consensustest/twoab/events.go`.
- **R3**: The convergence work surfaces additional asymmetries during refactor. Mitigation: file them as follow-up findings rather than expanding scope of the active commit.

---

## 7. Open questions for the author/reviewer

- Should sentinel-error renames cross the package boundary (e.g. `obft.ErrInstanceEnded` instead of `base.ErrInstanceEnded` + `twoab.ErrInstanceEnded`)? Cross-package sentinel reuse would tighten the cross-protocol error contract but adds an import edge.
- For C1: the runner adapter for twoab needs to handle `ErrInstanceEnded` from Observe* methods. Does the existing `consensustest/twoab/events.go` adapter need updating before C1 lands, or as part of C1?
- For Stats() in base (B5): what should the shape of `WitnessedLeaderSigmaCount` look like — slot-wide count, per-layer count, or per-(layer, V_root) count? Twoab uses `VerifiedWitnessesCount` summed across layers; base may want finer-grained.

---

## Appendix B — Documented intentional divergences (kept on purpose)

These asymmetries were considered for convergence and explicitly kept:

- **C4. `ApplyHostValidity` verdict-flip semantics**: base rejects, twoab tolerates. Reflects different host-revalidation models. Cross-referenced at both sites.
- **L2. `selectWinningGroup` input-shape**: base takes `[]*sigGroup` (slice-driven group collection); twoab takes `map[[32]byte]*sigGroup` (V_root-keyed map). Function bodies are line-for-line identical, but each package's internal sigGroup-collection strategy chose the natural container. Unifying would force slice-conversion churn at the twoab call site for cosmetic gain. Documented at both sites.
- **L5. `MaxRetainedPerOpLayer` semantic surface**: base caps both Phase-1 bundle retention AND σ-onion entry retention; twoab caps only Phase-1 bundle retention (σ partial moved into KindValue.L0Partial). Same constant name + value, slightly different policy surface. Documented at both constants' docstrings.

## Appendix A — Catalog summary

Full audit categorized 60 asymmetries:

| Category | Forced | Convergeable | Resolution path |
|---|---:|---:|---|
| A. Lifecycle guards | 0 | 6 | C1 |
| B. Error handling | 1 | 7 | B8 |
| C. Defensive copy | 3 | 4 | D2 |
| D. Pool aggregation | 4 | 1 | B2 |
| E. Cascade processing | 3 | 0 | — (forced) |
| F. Channel host integration | 0 | 1 | B3 |
| G. Evidence accumulation | 2 | 3 | B4, D4 |
| H. Validation | 2 | 1 | D1 |
| I. Constructor | 0 | 1 | C2 |
| J. Wire encapsulation | 1 | 2 | B6, D6 |
| K. Stats / introspection | 4 | 1 | B5 |
| L. EKM | 3 | 0 | — (forced) |
| M. Naming | 4 | 0 | — (forced or minor) |
| N. Documentation | 0 | 4 | Phase 4 |
| O. Other | 8 | 6 | mixed |

Net: ~25 forced, ~35 convergeable; addressed by ~15 distinct refactor items across 4 phases.

The detailed catalog with file:line references is in the audit transcript — too long to inline here; preserved in `docs/.audit-2026-05-22.md` (separate artifact, can be created on demand) for reference.
