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

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
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
)

// Per-layer defaults for the asymmetric staggered schedule (spec §Setting,
// §Application / Timing budget at Config A):
//
//   - fetchAt is when the leader's fetch goroutine wakes and starts the
//     iterative-fetch poll loop (Scheduler.FetchAndBroadcastBundle). Spec
//     §Application: leaders poll relay throughout [RANDAO_done,
//     T_broadcast_max[k]] and broadcast the freshest at deadline. Default
//     fetchAt values are clustered just past RANDAO_done (~150ms) with
//     small per-layer staggering preserving the spec's "deeper layer
//     fetches from deeper-confirmed parent" intuition (the choice of
//     parent is a hook concern; fetchAt only sets when polling starts).
//     The 1ms-per-layer staggering keeps the schedule strict-decreasing
//     at typical operating points even though obft.Config.Validate only
//     requires non-increasing — useful as a self-documenting hint that
//     L_k+1 fetches earlier than L_k. FetchAt is RANDAO-anchored
//     (absolute), not BTT-scaled.
//   - budgetBTT100 holds the per-layer propagation budgets B_k for the
//     *shallow* layers (k ∈ [0, K-2]) in BTT-hundredths (so 100 = 1 BTT,
//     150 = 1.5 BTT, etc.); strictly increasing in k under typical
//     operating points, capped at T_commit at degraded operating points.
//     The deepest layer is *not* stored here — its budget is always
//     T_commit ("earliest possible": deepest leader broadcasts at
//     BFT_start). Stored as multipliers so the schedule scales with BTT
//     — at BTT=200ms the K=4 shallow budgets resolve to 200/300/500ms;
//     at BTT=400ms they'd be 400/600/1000ms. Per spec §Setting line 45's
//     recommended 1/1.5/2.5 BTT for the shallow K-1 layers; deepest is
//     `T_commit`.
//
// Constraints: fetchAt[k] ≤ TCommit − budget[k] for every k (each leader's
// poll window must fit before its T_broadcast_max — enforced by Validate);
// budget[K-1] ≥ 2·BTT BFT-min (trivially satisfied since deepest = T_commit
// and T_commit ≥ 2·BTT is independently enforced).
//
// At Config A (BTT = 200ms, TCommit = 3400ms) the K=4 schedule resolves to:
//   - budget  = [1·BTT, 1.5·BTT, 2.5·BTT, T_commit] = [200, 300, 500, 3400]ms.
//   - fetchAt = [153, 152, 151, 0]ms — shallow layers clustered at RANDAO_done
//     with 1ms-per-layer staggering (a self-documenting hint that L_{k+1}
//     fetches before L_k; obft.Config.Validate only requires non-increasing,
//     so the tight stagger is convention not requirement). The deepest layer
//     at 0 because B_{K-1} = T_commit clamps T_broadcast_max_{K-1} to
//     BFT_start (iterative-fetch uses the full window per layer regardless
//     of fetchAt order).
//   - V_0 MEV-fetch budget at iterative-fetch = T_broadcast_max[0] −
//     fetchAt[0] − buildBuffer = 3200 − 153 − 10 = 3037ms (vs spec's 3050ms
//     target — within 13ms).
//
// For K>4 (n=7, n=10, n=13 deployments) the first three shallow layers stay
// at 1 / 1.5 / 2.5 BTT and intermediate layers interpolate linearly from
// 2.5·BTT (at L_2) to T_commit (at L_{K-1}).
//
// K=2 is the BFT-liveness minimum at f=1; per spec §Setting it is accepted
// (the operator/deployment decides whether to use it instead of K ≥ f+2).
var defaultLayerSchedules = map[int]struct {
	fetchAt             []time.Duration
	shallowBudgetBTT100 []int // BTT-hundredths for L_0 .. L_{K-2}; deepest is always T_commit
}{
	2: {
		fetchAt:             []time.Duration{151 * time.Millisecond, 0},
		shallowBudgetBTT100: []int{100}, // 1.0 BTT (L_0); deepest L_1 = T_commit
	},
	3: {
		fetchAt:             []time.Duration{152 * time.Millisecond, 151 * time.Millisecond, 0},
		shallowBudgetBTT100: []int{100, 250}, // 1.0, 2.5 BTT (L_0, L_1); deepest L_2 = T_commit
	},
	4: {
		fetchAt:             []time.Duration{153 * time.Millisecond, 152 * time.Millisecond, 151 * time.Millisecond, 0},
		shallowBudgetBTT100: []int{100, 150, 250}, // 1.0, 1.5, 2.5 BTT (L_0..L_2); deepest L_3 = T_commit
	},
}

// Endpoint defaults used for K>4 linear interpolation. Match the K=4 L_0
// and deepest entries in defaultLayerSchedules. The deepest FetchAt is 0
// because B_{K-1} = T_commit clamps T_broadcast_max_{K-1} to BFT_start;
// shallow FetchAt endpoints are absolute (RANDAO-anchored). BroadcastBudget
// L_0 endpoint is in BTT-hundredths so it scales with deployment BTT; the
// deepest budget endpoint is always T_commit (not represented as a BTT
// multiplier).
//
// Drift from defaultLayerSchedules[4] is guarded by
// TestDefaultBroadcastBudgetSchedule_EndpointConstantMatchK4 in config_test.go.
const (
	primaryFetchDefault        = 153 * time.Millisecond
	deepestFetchDefault        = 0
	primaryBudgetDefaultBTT100 = 100 // 1.0 BTT
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
	// ConfigForCluster substitutes DefaultBroadcastBudgetSchedule(K, BTT,
	// T_commit) which produces a non-decreasing schedule conforming to
	// spec (K=4 Config A: [1·BTT, 1.5·BTT, 2.5·BTT, T_commit]; capped
	// at T_commit when shallow multiples overshoot at degraded BTT).
	// obft.Config.Validate requires every layer's BroadcastBudget > 0
	// — no all-zero fallback. When set, length must match K and values
	// must be non-decreasing in layer index.
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

// interpolatedBudgetSchedule returns a length-K slice with the L_0 endpoint
// expressed in BTT-hundredths (scaled to BTT internally) and the deepest
// endpoint as an absolute duration (T_commit). Output is monotonically
// increasing. Used as the K>4 fallback for BroadcastBudget.
//
// The first three shallow layers stay at spec values (1·BTT, 1.5·BTT,
// 2.5·BTT); intermediate layers (k = 3, ..., K-2) interpolate linearly in
// duration space from 2.5·BTT (at L_2) to deepest (at L_{K-1}).
func interpolatedBudgetSchedule(K int, l0BTT100 int, deepest, btt time.Duration) []time.Duration {
	out := make([]time.Duration, K)
	// Shallow spec values for k ∈ {0, 1, 2}.
	out[0] = btt * time.Duration(l0BTT100) / 100
	if K >= 2 {
		out[K-1] = deepest
	}
	if K >= 3 {
		out[1] = btt * 150 / 100 // 1.5 BTT
		out[2] = btt * 250 / 100 // 2.5 BTT
	}
	// Intermediate layers between L_2 and L_{K-1} interpolate in duration
	// space (the deepest is absolute T_commit, not a BTT multiple).
	if K > 4 {
		span := deepest - out[2]
		steps := K - 3
		for k := 3; k < K-1; k++ {
			out[k] = out[2] + span*time.Duration(k-2)/time.Duration(steps)
		}
	}
	return out
}

// defaultFetchSchedule returns the K-tier per-layer FetchAt schedule.
// Strict-decreasing at typical operating points (1ms-per-layer staggering
// as a self-documenting hint); the underlying obft.Config.Validate only
// requires non-increasing.
//
// For K=2 (BFT-liveness minimum at f=1), K=3 and K=4, returns the tabulated
// defaults. For K>4 (n=7, n=10, n=13 deployments), interpolates linearly
// from primary to deepest.
//
// FetchAt is RANDAO-anchored (absolute, not BTT-scaled).
func defaultFetchSchedule(K int) []time.Duration {
	if s, ok := defaultLayerSchedules[K]; ok {
		return append([]time.Duration{}, s.fetchAt...)
	}
	return interpolatedDurationSchedule(K, primaryFetchDefault, deepestFetchDefault)
}

// DefaultBroadcastBudgetSchedule returns the per-layer T_commit-anchored
// absorption windows paired with defaultFetchSchedule. Mirrors the asymmetric
// staggered design from spec §Setting line 45's recommended 1/1.5/2.5 BTT
// shallow multipliers at K=4, with the deepest layer fixed at T_commit
// ("earliest possible": deepest leader broadcasts at BFT_start).
//
// For K=2 returns [1·BTT, T_commit] (BFT-liveness minimum at f=1). For K=3
// returns [1·BTT, 2.5·BTT, T_commit]. For K=4 returns [1·BTT, 1.5·BTT,
// 2.5·BTT, T_commit]. For K>4 the first three shallow layers stay at 1 /
// 1.5 / 2.5 BTT and intermediate layers interpolate linearly in duration
// space from 2.5·BTT to T_commit at L_{K-1}.
//
// At degraded operating points where the canonical staggered shallow
// multiples overshoot T_commit (e.g. T_commit ≤ 2.5·BTT at K≥3), shallow
// B_k entries are capped at T_commit so the schedule stays non-decreasing.
// Capped layers share `T_broadcast_max_k = max(BFT_start, T_commit − B_k)
// = BFT_start` — multiple layers may collide at BFT_start without safety
// impact. Operators who want the canonical staggered shape preserved can
// either widen T_commit (loosen Δ_2 / Δ_3 / header headroom) or supply
// a custom schedule.
//
// Spec example values at BTT=200ms, T_commit=3400ms (Config A): K=4 →
// [200, 300, 500, 3400]ms. At BTT=600ms, T_commit=2600ms (degraded mesh):
// K=4 → [600, 900, 1500, 2600]ms.
func DefaultBroadcastBudgetSchedule(K int, btt, tCommit time.Duration) ([]time.Duration, error) {
	if K < 1 {
		return nil, fmt.Errorf("obft adapter: DefaultBroadcastBudgetSchedule K=%d must be ≥ 1", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("obft adapter: DefaultBroadcastBudgetSchedule BTT=%v must be > 0", btt)
	}
	var out []time.Duration
	if s, ok := defaultLayerSchedules[K]; ok {
		out = make([]time.Duration, K)
		for k, m := range s.shallowBudgetBTT100 {
			out[k] = btt * time.Duration(m) / 100
		}
		out[K-1] = tCommit
	} else {
		out = interpolatedBudgetSchedule(K, primaryBudgetDefaultBTT100, tCommit, btt)
	}
	// Cap each B_k at T_commit so the schedule stays non-decreasing even
	// at degraded operating points where the shallow multiples overshoot.
	// Capped layers all clamp to BFT_start at runtime — see func doc.
	for k := 0; k < K; k++ {
		if out[k] > tCommit {
			out[k] = tCommit
		}
	}
	return out, nil
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
	// Per spec §Setting: K ≥ f+1 is the BFT-liveness minimum (pigeonhole
	// guarantees ≥ 1 honest leader). K ≥ f+2 additionally provides
	// late-leader-resilience and is not enforced here — the deployment
	// chooses.
	minK := f + 1
	if K < minK {
		return nil, fmt.Errorf("obft adapter: K=%d below BFT-liveness minimum %d (= f+1 at f=%d)",
			K, minK, f)
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
		var err error
		broadcastBudget, err = DefaultBroadcastBudgetSchedule(K, overrides.btt(), overrides.tCommit())
		if err != nil {
			return nil, fmt.Errorf("obft adapter: %w", err)
		}
	}
	if len(broadcastBudget) != K {
		return nil, fmt.Errorf("obft adapter: BroadcastBudget has %d entries, expected K=%d",
			len(broadcastBudget), K)
	}

	layers := make([]obftcore.LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = obftcore.LayerSpec{
			Leader:          leaderForLayer(sorted, obftcore.Height(slot), k),
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

// leaderForLayer applies the cluster leader-rotation rule to map (height,
// layer) → expected leader. `sorted` is the cluster's operator IDs in
// ascending order (the rotation's stable index). Public to the package so
// other adapter code (validator-time witness checks) can derive expected
// leaders without rebuilding a full *Config.
func leaderForLayer(sorted []spectypes.OperatorID, height obftcore.Height, layer int) obftcore.OperatorID {
	n := uint64(len(sorted))
	if n == 0 {
		return 0
	}
	idx := (uint64(height) + uint64(layer)) % n //nolint:gosec // small positive ints
	return obftcore.OperatorID(sorted[idx])
}

// LeaderForLayerFunc returns a closure that maps (height, layer) → the
// expected leader under the cluster's per-slot leader rotation. Used by
// the validator-time Verifier (Verifier.LeaderForLayer) to reject witness
// sections claiming a wrong-layer leader. `committee` is the cluster's
// operator IDs (any order — sorted internally).
func LeaderForLayerFunc(committee []spectypes.OperatorID) func(obftcore.Height, int) obftcore.OperatorID {
	sorted := make([]spectypes.OperatorID, len(committee))
	copy(sorted, committee)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return func(height obftcore.Height, layer int) obftcore.OperatorID {
		return leaderForLayer(sorted, height, layer)
	}
}
