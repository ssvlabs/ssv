package consensustest

import "time"

// DefaultBkSchedule returns the per-layer T_commit-anchored broadcast-budget
// schedule for K layers, scaled by BTT. The schedule follows OBFT.md §Setting
// example values for K=4 ([1, 1.5, 2.5, 5.5] BTT) and extends to other K via:
//
//   - K=3: [1, 1.5, 5.5] BTT — fix L_0, L_1 at spec values; deepest at 5.5 BTT
//     so the cluster keeps the wide-tail absorption tolerance the spec
//     establishes for the deepest fall-through layer.
//   - K=4: [1, 1.5, 2.5, 5.5] BTT — spec example (Config A).
//   - K≥5: [1, 1.5, 2.5, ...interpolated..., 5.5] BTT — keep the spec's first
//     3 layers, then linearly interpolate from 2.5 BTT to 5.5 BTT across the
//     remaining K-3 layers.
//
// The result is monotonically increasing in k. Returns nil for K<3 (caller's
// SimConfig.Validate enforces K ≥ max(3, f+2) so this should never trip in
// practice).
func DefaultBkSchedule(K int, btt time.Duration) []time.Duration {
	if K < 3 {
		return nil
	}
	const (
		l0 = 1.0
		l1 = 1.5
		l2 = 2.5
		ld = 5.5
	)
	mul := func(x float64) time.Duration { return time.Duration(x * float64(btt)) }
	if K == 3 {
		return []time.Duration{mul(l0), mul(l1), mul(ld)}
	}
	out := make([]time.Duration, K)
	out[0] = mul(l0)
	out[1] = mul(l1)
	out[2] = mul(l2)
	out[K-1] = mul(ld)
	if K == 4 {
		return out
	}
	// K ≥ 5: linear interpolation from L_2 (2.5 BTT) to L_{K-1} (5.5 BTT)
	// across K-3 steps, filling indices 3..K-2.
	steps := K - 3
	stride := (ld - l2) / float64(steps)
	for k := 3; k < K-1; k++ {
		out[k] = mul(l2 + float64(k-2)*stride)
	}
	return out
}

// DefaultFetchSchedule returns the per-layer leader fetch-offset schedule for
// K layers, anchored at tCommit and BTT. Each layer's FetchAt sits a small
// buffer (0.25 BTT) ahead of the layer's T_broadcast_max so each leader has
// non-negative headroom to fetch+sign before its broadcast deadline. Strictly
// decreasing in k (deeper layers fetch from progressively earlier).
//
// Bk is sourced from DefaultBkSchedule(K, btt) so the schedules stay
// consistent.
func DefaultFetchSchedule(K int, btt, tCommit time.Duration) []time.Duration {
	if K < 3 {
		return nil
	}
	bk := DefaultBkSchedule(K, btt)
	fetchBuffer := btt / 4

	out := make([]time.Duration, K)
	for k := 0; k < K; k++ {
		out[k] = tCommit - bk[k] - fetchBuffer
		if out[k] < 0 {
			out[k] = 0
		}
	}
	return out
}
