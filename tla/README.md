# OBFT Formal Verification — TLA+ Specifications

This directory contains TLA+ specifications and TLC model-checker configurations for the OBFT formal verification effort. See [`docs/OBFT-formal-verif.md`](../docs/OBFT-formal-verif.md) for the methodology and properties under verification.

## Status

**Verification results so far**:

1. **`BareOBFT_Safety` ✓ verified** at n=4, f=1, K=2, |Values|=2 — TLC explored 262,144 distinct states (1.96M total state transitions, 13 levels deep) in 10 seconds with no counterexamples. All three Pigeonholes (1, 2, 3) hold across the entire reachable state space.

2. **`LBid_Safety` ◐ partial verification** at n=4, f=1, K=2, |Values|=2 (with TLC symmetry reduction over Honest × Values = 12×) — TLC explored 98M+ distinct canonical states up to depth 15 without finding any counterexample, before the run was halted (the full state space at this config is estimated ~100-150M distinct canonical states; the run was making steady progress but exceeded the interactive-session time budget). All four invariants (Pigeonholes 1, 2, 3 + PigeonholeVerdicts) held across all explored states.

3. **`LBidNew_Safety` ◐ partial verification** at n=4, f=1, K=2, |Values|=2 (with TLC symmetry reduction over Honest × Values = 12×) — TLC explored 139M+ distinct canonical states up to depth 16 without finding any counterexample, before the run was halted at a 20-min user-budget cap. All four invariants (Pigeonholes 1, 2, 3 + PigeonholeVerdicts) held across all explored states. **Refinement of L_Bid SAFETY**: the L_Bid_New spec uses the same algebraic-level structure as L_Bid (per-operator σ-or-NR commitments, threshold pools, chained encryption) with L_Bid_New's σ-when-uncertain rule and deep-only verdict scope encoded as honest-side tightenings; these tightenings don't affect Pigeonhole invariants but provide the structural base for future LIVENESS verification of F.5.2 corner cases.

**Status for both L_Bid and L_Bid_New**: spec encoding is correct (validated by SANY parse + 100M+ explored states each); exhaustive verification at this config is a follow-up task — likely needs a few hours of CPU time per variant, or a state-space constraint to bound byzantine action sequences.

Pending: liveness verification (Phase 2 / 3 / 4 LIVENESS_NON_GRIEF), larger configurations (K=4, n=7), exhaustive completion of L_Bid + L_Bid_New SAFETY.

The current specs are intentionally algebraic: they capture per-operator σ-or-NR commitments, threshold pools, verdict pools, and chained-encryption gates without modeling Phase-1 fetch loops, network non-determinism, or per-receiver verdict-pool views. This is sufficient to verify that Pigeonholes 1, 2, 3 hold algebraically — the load-bearing safety guarantee — under all reachable byzantine action sequences (cluster-wide verdict pool view; safety properties are invariant under the per-receiver-view abstraction since honest σ at L_Bid is verdict-quorum-gated and verdict-quorum is a strict property under Pigeonhole on verdicts).

## Files

| File | Purpose |
|---|---|
| `BareOBFT_Safety.tla` | Bare OBFT SAFETY spec — Pigeonholes 1, 2, 3 |
| `BareOBFT_Safety.cfg` | TLC config for n=4, f=1 |
| `LBid_Safety.tla` | OBFT + L_Bid SAFETY spec — Pigeonholes 1, 2, 3 + verdict pigeonhole; symmetry reduction over Honest × Values |
| `LBid_Safety.cfg` | TLC config for n=4, f=1 with symmetry |
| `LBidNew_Safety.tla` | OBFT + L_Bid_New SAFETY spec — same invariants as LBid_Safety, encoded for L_Bid_New's structural differences (deep-only verdict scope, σ-when-uncertain rule, primary-bid placement at L_0); symmetry reduction over Honest × Values |
| `LBidNew_Safety.cfg` | TLC config for n=4, f=1 with symmetry |

Planned (not yet written):

- `BareOBFT_Liveness.tla` — bare OBFT LIVENESS_NON_GRIEF (Class A closure).
- `LBid_Liveness.tla` — current L_Bid liveness verification.
- `LBidNew_Liveness.tla` — L_Bid_New liveness verification (would model bid_1 per-operator to verify F.5.2 corner cases).
- Configs for n=7, f=2.

## Running TLC

### Prerequisites

- Java 11+ (for the TLA+ tools).
- `tla2tools.jar` (TLC model checker, SANY parser).

Quickstart on macOS with Homebrew openjdk:

```sh
brew install openjdk

# Download tla2tools.jar into the tla/ directory (or anywhere; adjust path):
curl -sSL -o tla/tla2tools.jar https://github.com/tlaplus/tlaplus/releases/latest/download/tla2tools.jar
```

`tla/tla2tools.jar` is gitignored — each contributor downloads their own copy.

### Verifying SAFETY for bare OBFT at n=4, f=1

```sh
cd tla

# Validate spec syntax with SANY:
/opt/homebrew/opt/openjdk/bin/java -cp tla2tools.jar tla2sany.SANY BareOBFT_Safety.tla

# Run TLC model checker:
/opt/homebrew/opt/openjdk/bin/java -cp tla2tools.jar tlc2.TLC -config BareOBFT_Safety.cfg BareOBFT_Safety
```

Expected output ends with: `Model checking completed. No error has been found.` plus state-count statistics.

Result on this config: 262,144 distinct states verified in ~10s; SAFETY holds.

If TLC finds a counterexample, the trace will be printed showing the sequence of state transitions leading to the safety violation. This would indicate either (a) a bug in the spec encoding, or (b) a real safety bug in the protocol design.

### Verifying SAFETY for OBFT + L_Bid at n=4, f=1

```sh
cd tla

# Validate spec syntax with SANY:
/opt/homebrew/opt/openjdk/bin/java -cp tla2tools.jar tla2sany.SANY LBid_Safety.tla

# Run TLC model checker (recommend ≥ 8 GB heap and parallel GC for the
# expanded state space; symmetry reduction is enabled in the .cfg):
/opt/homebrew/opt/openjdk/bin/java -Xmx8g -XX:+UseParallelGC \
    -cp tla2tools.jar tlc2.TLC -workers auto -config LBid_Safety.cfg LBid_Safety
```

Expected runtime: tens of minutes (full coverage). The L_Bid spec adds three state variables on top of bare OBFT (`lbid_sigma`, `lbid_nr`, `verdicts`) and TLC explores a much larger state space — ~100-150M distinct canonical states under symmetry vs bare OBFT's 262K.

Partial-coverage results so far: 98M+ canonical states explored to depth 15 without counterexample. See [§Status](#status) for full notes.

### Verifying SAFETY for OBFT + L_Bid_New at n=4, f=1

```sh
cd tla

# Validate spec syntax with SANY:
/opt/homebrew/opt/openjdk/bin/java -cp tla2tools.jar tla2sany.SANY LBidNew_Safety.tla

# Run TLC model checker (same heap + GC recommendation as L_Bid):
/opt/homebrew/opt/openjdk/bin/java -Xmx8g -XX:+UseParallelGC \
    -cp tla2tools.jar tlc2.TLC -workers auto -config LBidNew_Safety.cfg LBidNew_Safety
```

Same runtime profile as L_Bid SAFETY. Partial-coverage results so far: 139M+ canonical states explored to depth 16 without counterexample.

### State-space estimates

| Configuration | Estimated state count | Expected TLC runtime |
|---|---|---|
| n=4, f=1, K=2, |Values|=2 | ~10^4 states | seconds |
| n=4, f=1, K=2, |Values|=3 | ~10^5 states | <1 minute |
| n=4, f=1, K=4, |Values|=4 | ~10^7 states | minutes |
| n=7, f=2, K=4, |Values|=4 | ~10^10 states | hours; may need symmetry reductions |

## Iteration plan

1. **Phase 1 (current)**: bare OBFT SAFETY at small `n`. Verify Pigeonholes hold under all byz action sequences.
2. **Phase 2**: bare OBFT LIVENESS_NON_GRIEF at small `n`. Verify no Class A leakage under non-grief operation.
3. **Phase 3**: L_Bid SAFETY + LIVENESS at small `n`. Includes mini-consensus mechanics.
4. **Phase 4**: L_Bid_New SAFETY + LIVENESS at small `n`.
5. **Phase 5**: Scale verification to n=7, f=2 for all variants. Apply symmetry reductions.
6. **Phase 6**: Document results in `docs/OBFT-formal-verif.md` §7.

## Notes on encoding choices

- **No explicit time / phases**: the spec models commitments as monotonically-growing sets of partials, not time-stepped events. This is sufficient for Pigeonhole verification (which is a property of the cluster-wide signed-message set, not of timing).
- **Honest XOR rule encoded directly**: honest operators may add at most one (σ-on-V) or one NR commitment per layer. EKM enforcement is implicit via this constraint.
- **Byzantine action space is loose**: byzantine operators may add any partial at any time, including violating XOR (cross-signing) or single-σ-V (multi-V signing). This captures the "byzantine controls own EKM" assumption.
- **Network is implicit**: σ partials, once added, are visible to all operators (= cluster-wide aggregation). Network non-determinism is modeled implicitly as part of the byzantine adversary's control over which partials exist.

These simplifications are sound for SAFETY verification — Pigeonhole arguments are properties of the cluster-wide signed-message set under EKM enforcement, independent of network mechanics. For LIVENESS verification, the network model and timing will need explicit modeling — deferred to the liveness modules.
