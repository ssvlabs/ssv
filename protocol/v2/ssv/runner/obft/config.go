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
//   - Δ_3 = ε_3 ≈ 50ms (absolute; local CPU reconstruction)
//   - JitterBuffer ≈ 50ms (absolute; residual slack between Phase-3
//     completion and cert-broadcast / relay-submit start)
//   - T_commit = RelayCutoff − HeaderSubmitHeadroom − JitterBuffer − Δ_3 − Δ_2 = 3400ms
//
// Adjusting BTT (deployment's P99+δ) re-derives the post-T_commit budget
// while keeping Δ_3, JitterBuffer, and HeaderSubmitHeadroom absolute.
// Override these via ConfigOverrides for non-default deployments.
const (
	DefaultBTT                  = 200 * time.Millisecond
	DefaultHeaderSubmitHeadroom = 100 * time.Millisecond
	DefaultDelta3               = 50 * time.Millisecond // ε_3, absolute (local CPU reconstruction)
	DefaultJitterBuffer         = 50 * time.Millisecond // residual jitter between Phase-3-complete and cert/submit

	// Δ_2 = 2 BTT recommended; defaultDelta2 derives from DefaultBTT.
	DefaultDelta2 = 2 * DefaultBTT

	// T_commit = RelayCutoff − HeaderSubmitHeadroom − JitterBuffer − Δ_3 − Δ_2.
	// At Config A: 4000 − 100 − 50 − 50 − 400 = 3400ms.
	DefaultRelayCutoff = 4000 * time.Millisecond
	DefaultTCommit     = DefaultRelayCutoff - DefaultHeaderSubmitHeadroom - DefaultJitterBuffer - DefaultDelta3 - DefaultDelta2

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
//     concern; fetchAt only sets when polling starts). FetchAt is
//     RANDAO-anchored (absolute), not BTT-scaled.
//   - budgetBTT100 is the T_commit-anchored per-layer propagation budget B_k
//     in BTT-hundredths (so 100 = 1 BTT, 150 = 1.5 BTT, etc.); strictly
//     increasing in k. Stored as multipliers so the schedule scales with
//     BTT — at BTT=200ms the K=4 budgets resolve to 200/300/500/1100ms;
//     at BTT=400ms they'd be 400/600/1000/2200ms. Per spec §Setting line
//     45's recommended 1/1.5/2.5/5.5 BTT for K=4 — applies regardless of
//     the deployment's BTT.
//
// Constraints: fetchAt[k] ≤ TCommit − budget[k] for every k (each leader's
// poll window must fit before its T_broadcast_max — enforced by Validate);
// budget[K-1] ≥ 2·BTT BFT-min.
//
// At Config A (BTT = 200ms, TCommit = 3400ms) the K=4 schedule resolves to:
//   - budget  = [1, 1.5, 2.5, 5.5] × 200ms = [200, 300, 500, 1100]ms — spec
//     §Setting line 45 recommended.
//   - fetchAt = [153, 152, 151, 150]ms — all clustered at RANDAO_done, with
//     1ms-per-layer staggering for the strict-decreasing validation
//     constraint (the staggering is symbolic; iterative-fetch uses the full
//     window per layer regardless of fetchAt order).
//   - V_0 MEV-fetch budget at iterative-fetch = T_broadcast_max[0] −
//     fetchAt[0] − buildBuffer = 3200 − 153 − 10 = 3037ms (vs spec's 3050ms
//     target — within 13ms).
//
// For K>4 (n=7, n=10, n=13 deployments) both schedules interpolate linearly
// between L_0 and the deepest-layer endpoints.
//
// K=2 is intentionally absent: MinKFloor=3 enforces K ≥ f+2 floor at f=1,
// so K=2 is rejected before reaching the schedule lookup.
var defaultLayerSchedules = map[int]struct {
	fetchAt      []time.Duration
	budgetBTT100 []int // BTT-hundredths; 100 = 1.0 BTT
}{
	3: {
		fetchAt:      []time.Duration{152 * time.Millisecond, 151 * time.Millisecond, 150 * time.Millisecond},
		budgetBTT100: []int{100, 250, 550}, // 1.0, 2.5, 5.5 BTT
	},
	4: {
		fetchAt:      []time.Duration{153 * time.Millisecond, 152 * time.Millisecond, 151 * time.Millisecond, 150 * time.Millisecond},
		budgetBTT100: []int{100, 150, 250, 550}, // 1.0, 1.5, 2.5, 5.5 BTT
	},
}

// Endpoint defaults used for K>4 linear interpolation. Match the K=4 L_0
// and deepest entries in defaultLayerSchedules. FetchAt endpoints are
// absolute (RANDAO-anchored); BroadcastBudget endpoints are in BTT-hundredths
// so they scale with deployment BTT. (Per-layer FetchAt staggering is
// symbolic for the strict-decreasing constraint; iterative-fetch uses the
// full window regardless.)
//
// Drift from defaultLayerSchedules[4] is guarded by
// TestDefaultBroadcastBudgetSchedule_EndpointConstantsMatchK4 in config_test.go.
const (
	primaryFetchDefault        = 153 * time.Millisecond
	deepestFetchDefault        = 150 * time.Millisecond
	primaryBudgetDefaultBTT100 = 100 // 1.0 BTT
	deepestBudgetDefaultBTT100 = 550 // 5.5 BTT
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

	// TCommit, Delta2, Delta3, JitterBuffer — when zero, derive from the
	// above per spec. Set explicitly only to deviate from the spec
	// derivation. JitterBuffer is the residual slack between Phase-3
	// completion and cert-broadcast / relay-submit start (see spec
	// §Application / Timing budget).
	TCommit      time.Duration
	Delta2       time.Duration
	Delta3       time.Duration
	JitterBuffer time.Duration

	// FetchAt overrides the default per-layer fetch offsets. If nil,
	// defaults are used (tabulated in defaultLayerSchedules — see the
	// per-K schedule for production values at the current operating
	// point). Length must match K (or zero/nil to use defaults).
	FetchAt []time.Duration

	// BroadcastBudget overrides the default per-layer absorption windows
	// `B_k` (T_commit-anchored, per spec §Setting). When nil,
	// ConfigForCluster substitutes DefaultBroadcastBudgetSchedule(K, BTT)
	// which produces a strictly-increasing schedule conforming to spec
	// (K=4 Config A: [1, 1.5, 2.5, 5.5]·BTT). obft.Config.Validate now
	// requires every layer's BroadcastBudget > 0 — no all-zero fallback.
	// When set, length must match K and values must be strictly
	// increasing in layer index.
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

// jitterBuffer derives from absolute residual jitter (doesn't scale with
// BTT per spec §Application / Timing budget).
func (o *ConfigOverrides) jitterBuffer() time.Duration {
	if o == nil || o.JitterBuffer == 0 {
		return DefaultJitterBuffer
	}
	return o.JitterBuffer
}

// delta2 derives as 2 BTT per spec §Phase 2 recommendation (KindCommit
// propagation + jitter cushion). Override only to deviate.
func (o *ConfigOverrides) delta2() time.Duration {
	if o != nil && o.Delta2 != 0 {
		return o.Delta2
	}
	return 2 * o.btt()
}

// tCommit derives as RelayCutoff − HeaderSubmitHeadroom − JitterBuffer −
// Δ_3 − Δ_2 per spec §Application / Timing budget.
func (o *ConfigOverrides) tCommit() time.Duration {
	if o != nil && o.TCommit != 0 {
		return o.TCommit
	}
	return o.relayCutoff() - o.headerSubmitHeadroom() - o.jitterBuffer() - o.delta3() - o.delta2()
}

// interpolatedDurationSchedule returns a length-K slice of durations running
// linearly between the L_0 endpoint (k=0) and the deepest endpoint (k=K-1).
// Direction depends on the values: FetchAt is monotonically decreasing
// (l0 > deepest). Used as the K>4 fallback for FetchAt.
func interpolatedDurationSchedule(K int, l0, deepest time.Duration) []time.Duration {
	out := make([]time.Duration, K)
	step := (l0 - deepest) / time.Duration(K-1)
	for k := 0; k < K; k++ {
		out[k] = l0 - time.Duration(k)*step
	}
	return out
}

// interpolatedBudgetSchedule returns a length-K slice running linearly
// between the L_0 endpoint and deepest endpoint, both in BTT-hundredths,
// then scaled to the supplied BTT. Output is monotonically increasing
// (l0 < deepest, so deeper layers have larger budgets). Used as the K>4
// fallback for BroadcastBudget.
func interpolatedBudgetSchedule(K int, l0BTT100, deepestBTT100 int, btt time.Duration) []time.Duration {
	out := make([]time.Duration, K)
	step := (deepestBTT100 - l0BTT100) / (K - 1)
	for k := 0; k < K; k++ {
		mult := l0BTT100 + k*step
		out[k] = btt * time.Duration(mult) / 100
	}
	return out
}

// defaultFetchSchedule returns the K-tier per-layer FetchAt schedule —
// strictly decreasing in layer index k. K=2 isn't supported (MinKFloor=3).
//
// For K=3 and K=4, returns the tabulated defaults. For K>4 (n=7, n=10,
// n=13 deployments), interpolates linearly from primary to deepest.
//
// FetchAt is RANDAO-anchored (absolute, not BTT-scaled).
func defaultFetchSchedule(K int) []time.Duration {
	if s, ok := defaultLayerSchedules[K]; ok {
		return append([]time.Duration{}, s.fetchAt...)
	}
	return interpolatedDurationSchedule(K, primaryFetchDefault, deepestFetchDefault)
}

// DefaultBroadcastBudgetSchedule returns the per-layer T_commit-anchored
// absorption windows paired with defaultFetchSchedule, scaled to the supplied
// BTT. Mirrors the asymmetric staggered design from spec §Setting line 45's
// recommended 1/1.5/2.5/5.5 BTT multipliers (at K=4) regardless of the
// deployment's BTT.
//
// For K=3 and K=4, returns the tabulated multipliers × btt. For K>4,
// interpolates linearly from L_0 (1.0 BTT, smallest budget, max MEV
// freshness) to the deepest layer (5.5 BTT, largest budget, max absorption
// tolerance).
//
// Spec example values at BTT=200ms (Config A): K=4 → [200, 300, 500, 1100]ms.
// At BTT=400ms (degraded mesh): K=4 → [400, 600, 1000, 2200]ms.
func DefaultBroadcastBudgetSchedule(K int, btt time.Duration) []time.Duration {
	if s, ok := defaultLayerSchedules[K]; ok {
		out := make([]time.Duration, K)
		for k, m := range s.budgetBTT100 {
			out[k] = btt * time.Duration(m) / 100
		}
		return out
	}
	return interpolatedBudgetSchedule(K, primaryBudgetDefaultBTT100, deepestBudgetDefaultBTT100, btt)
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
	// Normalize nil overrides to the zero-value struct so subsequent field
	// reads (FetchAt, BroadcastBudget — not nil-safe accessors) don't
	// panic. The k()/btt()/etc. methods are already nil-safe via the
	// receiver-nil check; this normalization gives the field-read sites
	// the same behavior.
	if overrides == nil {
		overrides = &ConfigOverrides{}
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

	broadcastBudget := overrides.BroadcastBudget
	if broadcastBudget == nil {
		broadcastBudget = DefaultBroadcastBudgetSchedule(K, overrides.btt())
	}
	if len(broadcastBudget) != K {
		return nil, fmt.Errorf("obft adapter: BroadcastBudget has %d entries, expected K=%d",
			len(broadcastBudget), K)
	}

	layers := make([]obftcore.LayerSpec, K)
	for k := 0; k < K; k++ {
		idx := (uint64(slot) + uint64(k)) % uint64(n) //nolint:gosec // small positive ints
		layers[k] = obftcore.LayerSpec{
			Leader:          obftcore.OperatorID(sorted[idx]),
			FetchAt:         fetchAt[k],
			BroadcastBudget: broadcastBudget[k],
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
