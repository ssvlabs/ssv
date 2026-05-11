package consensustest

import (
	"fmt"
	"time"
)

// DefaultBkSchedule returns the per-layer T_commit-anchored broadcast-budget
// schedule for K layers at the given BTT and T_commit. Shallow layers follow
// OBFT.md §Setting recommended multipliers (1·BTT, 1.5·BTT, 2.5·BTT at K=4);
// the deepest layer is always T_commit ("earliest possible" — deepest leader
// broadcasts at slot start):
//
//   - K=3: [1·BTT, 2.5·BTT, T_commit] — matches the production SSV-adapter K=3
//     schedule so framework simulations and production validation operate on
//     the same envelope.
//   - K=4: [1·BTT, 1.5·BTT, 2.5·BTT, T_commit] — spec example (Config A).
//   - K≥5: [1·BTT, 1.5·BTT, 2.5·BTT, ...interpolated..., T_commit] — keep the
//     spec's first 3 shallow layers, then linearly interpolate from 2.5·BTT
//     to T_commit (in duration space) across the remaining K-4 intermediate
//     layers.
//
// Returns an error wrapping ErrNotApplicable when T_commit ≤ B_{K-2} —
// the operating point is too tight for the default staggered schedule's
// strict-increasing invariant. Callers can either supply a custom schedule
// via SimConfig.BroadcastBudget or treat the cluster as out-of-envelope
// for this BTT (the framework's batch driver renders such cells as "n/a"
// silently, no per-scenario log spam). Wrapping with ErrNotApplicable
// makes this an "operating point not applicable to default schedule"
// signal rather than a hard config error.
//
// K<3 / BTT≤0 return plain errors (programmer errors — SimConfig.Validate
// enforces K ≥ max(3, f+2) and BTT > 0 upstream, so these shouldn't trip
// in practice).
func DefaultBkSchedule(K int, btt, tCommit time.Duration) ([]time.Duration, error) {
	if K < 3 {
		return nil, fmt.Errorf("consensustest: DefaultBkSchedule K=%d below minimum 3", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("consensustest: DefaultBkSchedule BTT=%v must be > 0", btt)
	}
	minDeepest := btt * 250 / 100 // 2.5·BTT (B_{K-2} for K≥3)
	if tCommit <= minDeepest {
		return nil, fmt.Errorf("%w: consensustest: DefaultBkSchedule T_commit=%v must be > %v (B_{K-2} = 2.5·BTT at BTT=%v); supply a custom per-layer schedule",
			ErrConfigOutOfEnvelope, tCommit, minDeepest, btt)
	}
	mul := func(x float64) time.Duration { return time.Duration(x * float64(btt)) }
	if K == 3 {
		return []time.Duration{mul(1.0), mul(2.5), tCommit}, nil
	}
	out := make([]time.Duration, K)
	out[0] = mul(1.0)
	out[1] = mul(1.5)
	out[2] = mul(2.5)
	out[K-1] = tCommit
	if K == 4 {
		return out, nil
	}
	// K ≥ 5: linear interpolation from L_2 (2.5·BTT) to L_{K-1} (T_commit)
	// in duration space, filling indices 3..K-2.
	span := tCommit - out[2]
	steps := K - 3
	for k := 3; k < K-1; k++ {
		out[k] = out[2] + span*time.Duration(k-2)/time.Duration(steps)
	}
	return out, nil
}

// DefaultFetchSchedule returns the per-layer leader fetch-offset schedule for
// K layers, anchored at tCommit and BTT. Each layer's FetchAt sits a small
// buffer (default 0.25 BTT) ahead of the layer's T_broadcast_max so each
// leader has non-negative headroom to fetch+sign before its broadcast
// deadline. Strictly decreasing in k (deeper layers fetch progressively
// earlier).
//
// perLayerOffset overrides the default 0.25 BTT buffer per-layer: missing key
// → default; explicit zero → leader broadcasts exactly at T_broadcast_max_k
// (the spec's max-MEV operating point per OBFT.md §Timing budget). Pass nil
// for the default everywhere.
//
// Bk is sourced from DefaultBkSchedule(K, btt, tCommit) so the schedules stay
// consistent. Returns an error wrapping ErrNotApplicable when the derived
// schedule would violate the strict-decreasing-in-k invariant — typically
// because two or more shallow layers' computed FetchAt clamp to 0 at
// degraded BTT (e.g. at BTT≈800ms with the default buffer, T_{K-2} ≤ 0 just
// as T_{K-1} = 0 from the "earliest possible" deepest, so T_{K-1} ≮ T_{K-2}).
// Wrapping with ErrNotApplicable makes the operating-point-too-tight case a
// quiet "n/a" in batch runs rather than a per-cell log line.
func DefaultFetchSchedule(K int, btt, tCommit time.Duration, perLayerOffset map[int]time.Duration) ([]time.Duration, error) {
	bk, err := DefaultBkSchedule(K, btt, tCommit)
	if err != nil {
		return nil, err
	}
	defaultBuffer := btt / 4

	out := make([]time.Duration, K)
	for k := 0; k < K; k++ {
		buf := defaultBuffer
		if perLayerOffset != nil {
			if v, ok := perLayerOffset[k]; ok {
				buf = v
			}
		}
		out[k] = tCommit - bk[k] - buf
		if out[k] < 0 {
			out[k] = 0
		}
	}
	for k := 1; k < K; k++ {
		if out[k] >= out[k-1] {
			return nil, fmt.Errorf("%w: consensustest: DefaultFetchSchedule T_%d=%v >= T_%d=%v at BTT=%v T_commit=%v (operating point too tight for default schedule)",
				ErrConfigOutOfEnvelope, k, out[k], k-1, out[k-1], btt, tCommit)
		}
	}
	return out, nil
}
