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
// Returns an error when T_commit ≤ B_{K-2} (default would fail strict-
// increasing). Callers operating at extreme degraded BTT must supply a custom
// per-layer schedule via SimConfig.BroadcastBudget. K<3 also returns error
// (caller's SimConfig.Validate enforces K ≥ max(3, f+2) so this should never
// trip in practice).
func DefaultBkSchedule(K int, btt, tCommit time.Duration) ([]time.Duration, error) {
	if K < 3 {
		return nil, fmt.Errorf("consensustest: DefaultBkSchedule K=%d below minimum 3", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("consensustest: DefaultBkSchedule BTT=%v must be > 0", btt)
	}
	minDeepest := btt * 250 / 100 // 2.5·BTT (B_{K-2} for K≥3)
	if tCommit <= minDeepest {
		return nil, fmt.Errorf("consensustest: DefaultBkSchedule T_commit=%v must be > %v (B_{K-2} = 2.5·BTT at BTT=%v); supply a custom per-layer schedule",
			tCommit, minDeepest, btt)
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
// consistent. Returns an error in the same conditions as DefaultBkSchedule.
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
	return out, nil
}
