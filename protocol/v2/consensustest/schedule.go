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
//   - K=2: [1·BTT, T_commit] — BFT-liveness minimum at f=1.
//   - K=3: [1·BTT, 2.5·BTT, T_commit] — matches the production SSV-adapter K=3
//     schedule so framework simulations and production validation operate on
//     the same envelope.
//   - K=4: [1·BTT, 1.5·BTT, 2.5·BTT, T_commit] — spec example (Config A).
//   - K≥5: [1·BTT, 1.5·BTT, 2.5·BTT, ...interpolated..., T_commit] — keep the
//     spec's first 3 shallow layers, then linearly interpolate from 2.5·BTT
//     to T_commit (in duration space) across the remaining K-4 intermediate
//     layers.
//
// At extreme operating points where the canonical staggered shallow
// budgets exceed T_commit (e.g. BTT=1000ms collapses T_commit below
// 2.5·BTT = B_{K-2}), the schedule is emitted as-is: the shallow B_k
// values can be larger than T_commit, and the protocol's runtime
// `T_broadcast_max_k = max(BFT_start, T_commit − B_k)` clamps those
// layers' targets at BFT_start (= slot_start in the simplified test
// timing). Multiple layers may share the BFT_start broadcast target
// without violating the schedule — fall-through depth shrinks at the
// degraded operating point but safety holds. The caller is responsible
// for deciding whether the resulting schedule is meaningful for their
// experiment.
//
// K<2 / BTT≤0 return plain errors (programmer errors — SimConfig.Validate
// enforces K ≥ f+1 and BTT > 0 upstream, so these shouldn't trip
// in practice).
func DefaultBkSchedule(K int, btt, tCommit time.Duration) ([]time.Duration, error) {
	if K < 2 {
		return nil, fmt.Errorf("consensustest: DefaultBkSchedule K=%d below minimum 2", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("consensustest: DefaultBkSchedule BTT=%v must be > 0", btt)
	}
	mul := func(x float64) time.Duration { return time.Duration(x * float64(btt)) }
	out := make([]time.Duration, K)
	switch K {
	case 2:
		out[0] = mul(1.0)
		out[1] = tCommit
	case 3:
		out[0] = mul(1.0)
		out[1] = mul(2.5)
		out[2] = tCommit
	default:
		out[0] = mul(1.0)
		out[1] = mul(1.5)
		out[2] = mul(2.5)
		out[K-1] = tCommit
		// K ≥ 5: linear interpolation from L_2 (2.5·BTT) to L_{K-1}
		// (T_commit) in duration space, filling indices 3..K-2.
		span := tCommit - out[2]
		steps := K - 3
		for k := 3; k < K-1; k++ {
			out[k] = out[2] + span*time.Duration(k-2)/time.Duration(steps)
		}
	}
	// Cap shallow B_k at T_commit so the schedule stays non-decreasing
	// even at degraded operating points where the canonical staggered
	// multiples (1·BTT, 1.5·BTT, 2.5·BTT, ...) overshoot. Capped layers
	// share `T_broadcast_max_k = max(BFT_start, T_commit − B_k) =
	// BFT_start` — multiple layers may collide at BFT_start without
	// safety impact. The deepest layer is already T_commit by
	// construction; the cap turns degraded shallow layers into
	// "broadcast at BFT_start" peers of the deepest, which is the
	// natural degradation mode at tight T_commit.
	for k := 0; k < K; k++ {
		if out[k] > tCommit {
			out[k] = tCommit
		}
	}
	return out, nil
}

// DefaultFetchSchedule returns the per-layer leader fetch-offset schedule for
// K layers, anchored at tCommit and BTT. Each layer's FetchAt sits a small
// buffer (default 0.25 BTT) ahead of the layer's T_broadcast_max so each
// leader has non-negative headroom to fetch+sign before its broadcast
// deadline. Non-increasing in k (deeper layers fetch progressively earlier
// at typical operating points; multiple layers may tie at BFT_start when
// the operating point is degraded enough that their computed offsets all
// clamp to BFT_start).
//
// perLayerOffset overrides the default 0.25 BTT buffer per-layer: missing key
// → default; explicit zero → leader broadcasts exactly at T_broadcast_max_k
// (the spec's max-MEV operating point per OBFT.md §Timing budget). Pass nil
// for the default everywhere.
//
// Bk is sourced from DefaultBkSchedule(K, btt, tCommit) so the schedules stay
// consistent. At degraded operating points (BTT large enough that the
// computed offsets push past BFT_start for multiple layers) the schedule is
// returned as-is — multiple FetchAt entries can collide at BFT_start.
// Functionally this means those layers' leaders fetch and broadcast at
// BFT_start; fall-through depth shrinks but the protocol still operates.
// The caller is responsible for deciding whether the resulting schedule is
// meaningful for their experiment.
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
