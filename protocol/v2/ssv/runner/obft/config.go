// Package obft is the SSV-side adapter that bridges the spec-independent
// OBFT consensus core (protocol/v2/obft) with SSV's runtime — concrete
// types from ssv-spec, the network layer, the runner lifecycle.
//
// This package is the only place where SSV's OBFT integration depends on
// github.com/ssvlabs/ssv-spec. The OBFT core itself remains spec-independent.
package obft

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Default protocol parameters per docs/OBFT.md §Timing budget — Config A.
//
// Spec Config A operating point (BTT = 200ms, RelayCutoff = 4000ms,
// HeaderSubmitHeadroom = 100ms):
//   - Δ_2 = 2 BTT = 400ms (recommended; KindCommit propagation + jitter)
//   - Δ_3 = ε_3 ≈ 100ms (absolute; doesn't scale with BTT)
//   - T_commit = RelayCutoff − HeaderSubmitHeadroom − Δ_3 − Δ_2 = 3400ms
//
// Adjusting BTT (deployment's P99+δ) re-derives the post-T_commit budget
// while keeping Δ_3 and HeaderSubmitHeadroom absolute. Override these via
// ConfigOverrides for non-default deployments.
const (
	DefaultBTT                  = 200 * time.Millisecond
	DefaultHeaderSubmitHeadroom = 100 * time.Millisecond
	DefaultDelta3               = 100 * time.Millisecond // ε_3, absolute

	// Δ_2 = 2 BTT recommended; defaultDelta2 derives from DefaultBTT.
	DefaultDelta2 = 2 * DefaultBTT

	// T_commit = RelayCutoff − HeaderSubmitHeadroom − Δ_3 − Δ_2.
	// At Config A: 4000 − 100 − 100 − 400 = 3400ms.
	DefaultRelayCutoff = 4000 * time.Millisecond
	DefaultTCommit     = DefaultRelayCutoff - DefaultHeaderSubmitHeadroom - DefaultDelta3 - DefaultDelta2

	// DefaultK is the recommended layer count for SSV proposer duty:
	// K = n = 4 (every cluster member leads exactly one layer; max
	// fall-through depth at f = 1).
	DefaultK = 4

	// MinKFloor is the minimum K floor at f=1 (late-leader resilience: K ≥ f+2).
	// At higher f the K-vs-f bound is computed in ConfigForCluster as
	// max(MinKFloor, f+2); Config.Validate enforces the same bound.
	MinKFloor = 3
)

// Per-layer defaults for the asymmetric staggered schedule (spec §Setting,
// §Application / Timing budget at Config A):
//
//   - fetchAt is when the leader's fetch goroutine wakes and starts the
//     iterative-fetch poll loop (Scheduler.FetchAndBroadcastBundle). Spec
//     §Application: leaders poll relay throughout [RANDAO_done,
//     T_broadcast_max[k]] and broadcast the freshest at deadline. Default
//     fetchAt values are clustered just past RANDAO_done (~150ms) with
//     small per-layer staggering to satisfy the strict-decreasing
//     validation while preserving the spec's "deeper layer fetches from
//     deeper-confirmed parent" intuition (the choice of parent is a hook
//     concern; fetchAt only sets when polling starts).
//   - budget is the T_commit-anchored absorption window B_k+slack; strictly
//     increasing in k (deeper layers tolerate wider propagation tails).
//
// Constraints: fetchAt[k] ≤ TCommit − budget[k] for every k (each leader's
// poll window must fit before its T_broadcast_max — enforced by Validate);
// budget[K-1] ≥ 2·BTT BFT-min.
//
// At Config A (BTT = 200ms, TCommit = 3400ms) the K=4 schedule is:
//   - budget  = [1, 1.5, 2.5, 5.5] BTT = [200, 300, 500, 1100]ms — spec
//     §Setting line 45 recommended.
//   - fetchAt = [180, 170, 160, 150]ms — all clustered at RANDAO_done, with
//     1ms-per-layer staggering for the validation constraint.
//   - V_0 MEV-fetch budget at iterative-fetch ≈ T_broadcast_max[0] −
//     fetchAt[0] − buildBuffer = 3200 − 180 − 50 = 2970ms (vs spec's 3050ms
//     target — ~80ms shy due to fetchAt staggering and buildBuffer; both
//     small).
//
// For K>4 (n=7, n=10, n=13 deployments) both schedules interpolate linearly
// between L_0 and the deepest-layer endpoints.
var defaultLayerSchedules = map[int]struct {
	fetchAt []time.Duration
	budget  []time.Duration
}{
	3: {
		fetchAt: []time.Duration{170 * time.Millisecond, 160 * time.Millisecond, 150 * time.Millisecond},
		budget:  []time.Duration{200 * time.Millisecond, 500 * time.Millisecond, 1100 * time.Millisecond},
	},
	4: {
		fetchAt: []time.Duration{180 * time.Millisecond, 170 * time.Millisecond, 160 * time.Millisecond, 150 * time.Millisecond},
		budget:  []time.Duration{200 * time.Millisecond, 300 * time.Millisecond, 500 * time.Millisecond, 1100 * time.Millisecond},
	},
}

// Endpoint defaults used for K>4 linear interpolation. Match the K=4 L_0
// and deepest entries in defaultLayerSchedules.
const (
	primaryFetchDefault  = 180 * time.Millisecond
	deepestFetchDefault  = 150 * time.Millisecond
	primaryBudgetDefault = 200 * time.Millisecond
	deepestBudgetDefault = 1100 * time.Millisecond
)

// ConfigOverrides allows callers to override the default protocol timings
// and layer count. Zero values fall back to package defaults — and where
// the spec defines a derivation (e.g. T_commit = RelayCutoff − headroom −
// Δ_3 − Δ_2; Δ_2 = 2·BTT), zero-valued fields derive from the supplied
// BTT / RelayCutoff / HeaderSubmitHeadroom rather than from the package
// defaults. Per spec §Application: callers can configure (BTT,
// HeaderSubmitHeadroom, RelayCutoff) and the post-T_commit timing falls
// out automatically.
type ConfigOverrides struct {
	K                    int
	BTT                  time.Duration // P99 + δ; spec §Setting unit propagation+skew budget
	RelayCutoff          time.Duration // application hard deadline (e.g. 4s for proposer duty)
	HeaderSubmitHeadroom time.Duration // reserved for cert broadcast + relay submit (absolute)

	// TCommit, Delta2, Delta3 — when zero, derive from the above per spec.
	// Set explicitly only to deviate from the spec derivation.
	TCommit time.Duration
	Delta2  time.Duration
	Delta3  time.Duration

	// FetchAt overrides the default per-layer fetch offsets. If nil,
	// defaults are used (Config A K=4: 3000/2900/2700/2100ms). Length
	// must match K (or zero/nil to use defaults).
	FetchAt []time.Duration

	// BroadcastBudget overrides the default per-layer absorption windows
	// (T_commit-anchored, per spec §Setting). When nil, all layers fall
	// back to obft.Config's single uniform cap 2*BTT — equivalent to
	// the historical pre-staggered behavior. When set, length must match K
	// and values must be strictly increasing in layer index.
	BroadcastBudget []time.Duration
}

func (o *ConfigOverrides) k() int {
	if o == nil || o.K == 0 {
		return DefaultK
	}
	return o.K
}

func (o *ConfigOverrides) btt() time.Duration {
	if o == nil || o.BTT == 0 {
		return DefaultBTT
	}
	return o.BTT
}

func (o *ConfigOverrides) relayCutoff() time.Duration {
	if o == nil || o.RelayCutoff == 0 {
		return DefaultRelayCutoff
	}
	return o.RelayCutoff
}

func (o *ConfigOverrides) headerSubmitHeadroom() time.Duration {
	if o == nil || o.HeaderSubmitHeadroom == 0 {
		return DefaultHeaderSubmitHeadroom
	}
	return o.HeaderSubmitHeadroom
}

// delta3 derives from absolute ε_3 (doesn't scale with BTT per spec).
func (o *ConfigOverrides) delta3() time.Duration {
	if o == nil || o.Delta3 == 0 {
		return DefaultDelta3
	}
	return o.Delta3
}

// delta2 derives as 2 BTT per spec §Phase 2 recommendation (KindCommit
// propagation + jitter cushion). Override only to deviate.
func (o *ConfigOverrides) delta2() time.Duration {
	if o != nil && o.Delta2 != 0 {
		return o.Delta2
	}
	return 2 * o.btt()
}

// tCommit derives as RelayCutoff − HeaderSubmitHeadroom − Δ_3 − Δ_2 per
// spec §Application / Timing budget.
func (o *ConfigOverrides) tCommit() time.Duration {
	if o != nil && o.TCommit != 0 {
		return o.TCommit
	}
	return o.relayCutoff() - o.headerSubmitHeadroom() - o.delta3() - o.delta2()
}

// interpolatedSchedule returns a length-K slice running linearly between
// the L_0 endpoint (k=0) and the deepest endpoint (k=K-1). Direction
// depends on the values: FetchAt is monotonically decreasing (l0 > deepest);
// BroadcastBudget is monotonically increasing (l0 < deepest). Used as the
// K>4 fallback for both schedules.
func interpolatedSchedule(K int, l0, deepest time.Duration) []time.Duration {
	out := make([]time.Duration, K)
	step := (l0 - deepest) / time.Duration(K-1)
	for k := 0; k < K; k++ {
		out[k] = l0 - time.Duration(k)*step
	}
	return out
}

// defaultFetchSchedule returns the K-tier per-layer FetchAt schedule —
// strictly decreasing in layer index k. K=2 isn't supported (MinKFloor=3).
//
// For K=3 and K=4, returns the tabulated defaults. For K>4 (n=7, n=10,
// n=13 deployments), interpolates linearly from primary to deepest.
func defaultFetchSchedule(K int) []time.Duration {
	if s, ok := defaultLayerSchedules[K]; ok {
		return append([]time.Duration{}, s.fetchAt...)
	}
	return interpolatedSchedule(K, primaryFetchDefault, deepestFetchDefault)
}

// DefaultBroadcastBudgetSchedule returns the per-layer T_commit-anchored
// absorption windows paired with defaultFetchSchedule. Mirrors the asymmetric
// staggered design from spec §Setting at the production timing.
//
// For K=3 and K=4, returns the tabulated defaults. For K>4, interpolates
// linearly from L_0 (smallest budget, max MEV freshness) to the deepest
// layer (largest budget, max absorption tolerance).
func DefaultBroadcastBudgetSchedule(K int) []time.Duration {
	if s, ok := defaultLayerSchedules[K]; ok {
		return append([]time.Duration{}, s.budget...)
	}
	return interpolatedSchedule(K, primaryBudgetDefault, deepestBudgetDefault)
}

// ConfigForCluster builds an *obft.Config for the given cluster + slot.
//
// `clusterID` is a stable per-cluster identifier (used in NR-tag construction
// to prevent cross-cluster replay). For SSV proposer-duty, it's typically
// derived from the validator pubkey (`SSVShare.CommitteeID()`).
//
// Leader rotation: layer k → committee[(slot + k) mod n], mirroring SSV's
// QBFT RoundRobinProposer convention. At K = n, every operator leads
// exactly one layer per slot.
func ConfigForCluster(
	slot phase0.Slot,
	committee []spectypes.OperatorID,
	clusterID [32]byte,
	overrides *ConfigOverrides,
) (*obftcore.Config, error) {
	if len(committee) == 0 {
		return nil, errors.New("obft adapter: empty committee")
	}
	n := len(committee)
	if (n-1)%3 != 0 {
		return nil, fmt.Errorf("obft adapter: cluster size %d is not 3f+1", n)
	}
	f := (n - 1) / 3

	K := overrides.k()
	// Per spec §Setting: enforce late-leader-resilience minimum K ≥ f+2,
	// floored at MinKFloor so the f=1 case stays at K ≥ 3 (which is f+2 at f=1).
	// At f≥2 the f+2 bound dominates and prevents BFT-liveness violations.
	minK := f + 2
	if minK < MinKFloor {
		minK = MinKFloor
	}
	if K < minK {
		return nil, fmt.Errorf("obft adapter: K=%d below late-leader-resilience minimum %d (= max(%d, f+2) at f=%d)",
			K, minK, MinKFloor, f)
	}
	if K > n {
		return nil, fmt.Errorf("obft adapter: K=%d exceeds cluster size %d", K, n)
	}

	sorted := make([]spectypes.OperatorID, len(committee))
	copy(sorted, committee)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	fetchAt := overrides.FetchAt
	if fetchAt == nil {
		fetchAt = defaultFetchSchedule(K)
	}
	if len(fetchAt) != K {
		return nil, fmt.Errorf("obft adapter: FetchAt has %d entries, expected K=%d", len(fetchAt), K)
	}

	var broadcastBudget []time.Duration
	if overrides != nil && overrides.BroadcastBudget != nil {
		if len(overrides.BroadcastBudget) != K {
			return nil, fmt.Errorf("obft adapter: BroadcastBudget has %d entries, expected K=%d",
				len(overrides.BroadcastBudget), K)
		}
		broadcastBudget = overrides.BroadcastBudget
	}

	layers := make([]obftcore.LayerSpec, K)
	for k := 0; k < K; k++ {
		idx := (uint64(slot) + uint64(k)) % uint64(n) //nolint:gosec // small positive ints
		layers[k] = obftcore.LayerSpec{
			Leader:  obftcore.OperatorID(sorted[idx]),
			FetchAt: fetchAt[k],
		}
		if broadcastBudget != nil {
			layers[k].BroadcastBudget = broadcastBudget[k]
		}
	}

	operators := make([]obftcore.OperatorID, n)
	for i, op := range sorted {
		operators[i] = obftcore.OperatorID(op)
	}

	cfg := &obftcore.Config{
		Height:    obftcore.Height(slot),
		ClusterID: clusterID,
		Operators: operators,
		F:         f,
		Layers:    layers,
		TCommit:   overrides.tCommit(),
		Delta2:    overrides.delta2(),
		Delta3:    overrides.delta3(),
		BTT:       overrides.btt(),
	}
	return cfg, nil
}
