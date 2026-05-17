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
//   - Δ_2 = 1 BTT = 200ms (recommended; KindCommit propagation cycle).
//     Reflood absorption lives in per-layer B_k via the reflood-aware
//     schedule, so Δ_2 no longer carries a reflood cushion.
//   - ε_3 ≈ 50ms (absolute; local CPU reconstruction)
//   - JitterBuffer ≈ 50ms (absolute; residual slack between Phase-3
//     completion and cert-broadcast / relay-submit start)
//   - T_commit = RelayCutoff − HeaderSubmitHeadroom − JitterBuffer − ε_3 − Δ_2 = 3600ms
//
// Adjusting BTT (deployment's P99+δ) re-derives the post-T_commit budget
// while keeping ε_3, JitterBuffer, and HeaderSubmitHeadroom absolute.
// Override these via ConfigOverrides for non-default deployments.
const (
	DefaultBTT                  = 200 * time.Millisecond
	DefaultHeaderSubmitHeadroom = 100 * time.Millisecond
	DefaultEps3                 = 50 * time.Millisecond // ε_3, absolute (local CPU reconstruction)
	DefaultJitterBuffer         = 50 * time.Millisecond // residual jitter between Phase-3-complete and cert/submit

	// Δ_2 = 1 BTT recommended; defaultDelta2 derives from DefaultBTT.
	// Reflood lives in B_k via RefloodDelay — Δ_2 only needs to cover
	// the synchronous-fallback KindCommit propagation cycle.
	DefaultDelta2 = 1 * DefaultBTT

	// T_commit = RelayCutoff − HeaderSubmitHeadroom − JitterBuffer − ε_3 − Δ_2.
	// At Config A: 4000 − 100 − 50 − 50 − 200 = 3600ms.
	DefaultRelayCutoff = 4000 * time.Millisecond
	DefaultTCommit     = DefaultRelayCutoff - DefaultHeaderSubmitHeadroom - DefaultJitterBuffer - DefaultEps3 - DefaultDelta2

	// DefaultK is the recommended layer count for SSV proposer duty:
	// K = n = 4 (every cluster member leads exactly one layer; max
	// fall-through depth at f = 1).
	DefaultK = 4

	// DefaultRefloodDelay is the worst-case gossipsub-lazy-push latency
	// before a retransmission cycle completes, defaulted to SSV's gossipsub
	// HeartbeatInterval (network/topics/params/gossipsub.go). Used to size
	// per-layer broadcast budgets B_k so that one full IHAVE/IWANT reflood
	// cycle fits within a layer's absorption window for mesh-flaky receivers
	// — without it, a missed eager-push at L_0 forecloses L_0 σ-quorum even
	// under partial-synchrony assumptions. Deployments running on dense,
	// fully-meshed clusters where eager-push reliably reaches all peers
	// (typically n=4 fully connected) MAY override to a lower value (down
	// to 0) to recover MEV-fetch headroom.
	DefaultRefloodDelay = 700 * time.Millisecond
)

// Per-layer schedule defaults (spec §Setting, §Application / Timing budget
// at Config A):
//
//   - L_0 (primary) is MEV-fresh: fetches from RANDAO_done onward (~150ms
//     past slot start) and broadcasts by `T_broadcast_max_0 = T_commit −
//     B_0` where `B_0 = 2·BTT + RefloodDelay` covers one IWANT round-trip
//     plus one IHAVE/IWANT reflood cycle for mesh-flaky receivers.
//   - L_1..L_{K-1} (backups) are last-resort safety nets: each fetches at
//     slot start from a deepest-confirmed parent (FetchAt = 0) and
//     broadcasts at BFT_start (B_k = T_commit clamps T_broadcast_max_k to
//     BFT_start). They trade MEV-fresh fetch for the maximally-wide
//     propagation absorption window — the entire commit budget.
//
// At Config A (BTT = 200ms, TCommit = 3600ms, RefloodDelay = 700ms) the
// K=4 schedule resolves to:
//   - budget  = [B_0=1100ms, T_commit=3600ms, T_commit, T_commit].
//   - fetchAt = [153ms, 0, 0, 0]ms — L_0 fetches just past RANDAO_done;
//     backups fetch at slot start.
//   - V_0 MEV-fetch budget at iterative-fetch = T_broadcast_max[0] −
//     fetchAt[0] − buildBuffer = (3600 − 1100) − 153 − 10 = 2337ms.
//   - V_1..V_3 MEV-fetch ~ 0ms (deepest-confirmed-parent vanilla payloads).
//
// FetchAt is RANDAO-anchored (absolute), not BTT-scaled. The primary's
// 153ms is a small staggering past RANDAO_done; backups fetch at 0 so
// the fetch + broadcast both fit in [slot_start, BFT_start].
//
// Constraints: fetchAt[k] ≤ TCommit − budget[k] for every k (each leader's
// poll window must fit before its T_broadcast_max — enforced by Validate);
// budget[K-1] ≥ 2·BTT BFT-min (trivially satisfied since backups = T_commit
// and T_commit ≥ 2·BTT is independently enforced).
//
// Generalizes to K>4 (n=7, n=10, n=13) without per-K tables: L_0 uses the
// primary defaults; all backups use FetchAt=0 + B_k=T_commit.
//
// K=2 is the BFT-liveness minimum at f=1; per spec §Setting it is accepted
// (the operator/deployment decides whether to use it instead of K ≥ f+2).
const (
	primaryFetchDefault        = 153 * time.Millisecond
	deepestFetchDefault        = 0
	primaryBudgetDefaultBTT100 = 200 // 2.0 BTT — paired with +RefloodDelay added at compute time
)

// ConfigOverrides allows callers to override the default protocol timings
// and layer count. Zero values fall back to package defaults — and where
// the spec defines a derivation (e.g. T_commit = RelayCutoff − headroom −
// JitterBuffer − ε_3 − Δ_2; Δ_2 = 1·BTT), zero-valued fields derive from
// the supplied BTT / RelayCutoff / HeaderSubmitHeadroom rather than from
// the package defaults. Per spec §Application: callers can configure
// (BTT, HeaderSubmitHeadroom, RelayCutoff) and the post-T_commit timing
// falls out automatically.
type ConfigOverrides struct {
	K                    int
	BTT                  time.Duration // P99 + δ; spec §Setting unit propagation+skew budget
	RelayCutoff          time.Duration // application hard deadline (e.g. 4s for proposer duty)
	HeaderSubmitHeadroom time.Duration // reserved for cert broadcast + relay submit (absolute)

	// RefloodDelay is the worst-case gossipsub-lazy-push latency before a
	// retransmission cycle completes — bounded by the cluster's
	// HeartbeatInterval. When zero, defaults to DefaultRefloodDelay (700ms,
	// matching SSV's configured HeartbeatInterval). The per-layer B_k
	// schedule adds RefloodDelay on top of the {2, 3, 4}·BTT shallow base so
	// one full IHAVE/IWANT cycle fits in each shallow layer's absorption
	// window. Deployments on fully-meshed clusters where eager-push reaches
	// all peers reliably may use a tiny positive value (e.g. 1·time.Nanosecond)
	// to opt out — Go's zero-means-default convention prevents passing 0
	// explicitly.
	RefloodDelay time.Duration

	// TCommit, Delta2, Eps3, JitterBuffer — when zero, derive from the
	// above per spec. Set explicitly only to deviate from the spec
	// derivation. JitterBuffer is the residual slack between Phase-3
	// completion and cert-broadcast / relay-submit start (see spec
	// §Application / Timing budget).
	TCommit      time.Duration
	Delta2       time.Duration
	Eps3         time.Duration
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

func (o *ConfigOverrides) refloodDelay() time.Duration {
	if o == nil || o.RefloodDelay == 0 {
		return DefaultRefloodDelay
	}
	return o.RefloodDelay
}

// eps3 derives from absolute ε_3 (doesn't scale with BTT per spec).
func (o *ConfigOverrides) eps3() time.Duration {
	if o == nil || o.Eps3 == 0 {
		return DefaultEps3
	}
	return o.Eps3
}

// jitterBuffer derives from absolute residual jitter (doesn't scale with
// BTT per spec §Application / Timing budget).
func (o *ConfigOverrides) jitterBuffer() time.Duration {
	if o == nil || o.JitterBuffer == 0 {
		return DefaultJitterBuffer
	}
	return o.JitterBuffer
}

// delta2 derives as 1 BTT per spec §Phase 2 recommendation (KindCommit
// synchronous-fallback propagation cycle). Reflood lives in B_k via
// RefloodDelay. Override only to deviate.
func (o *ConfigOverrides) delta2() time.Duration {
	if o != nil && o.Delta2 != 0 {
		return o.Delta2
	}
	return 1 * o.btt()
}

// tCommit derives as RelayCutoff − HeaderSubmitHeadroom − JitterBuffer −
// ε_3 − Δ_2 per spec §Application / Timing budget.
func (o *ConfigOverrides) tCommit() time.Duration {
	if o != nil && o.TCommit != 0 {
		return o.TCommit
	}
	return o.relayCutoff() - o.headerSubmitHeadroom() - o.jitterBuffer() - o.eps3() - o.delta2()
}

// defaultFetchSchedule returns the K-tier per-layer FetchAt schedule per
// spec §Setting / §Application: L_0 (primary) fetches just past
// RANDAO_done; L_1..L_{K-1} (backups) all fetch at slot start from the
// deepest-confirmed parent. FetchAt is RANDAO-anchored (absolute, not
// BTT-scaled).
//
// For K=1 returns [primaryFetchDefault] (degenerate single-layer case).
// For K≥2 returns [primaryFetchDefault, 0, ..., 0].
func defaultFetchSchedule(K int) []time.Duration {
	out := make([]time.Duration, K)
	out[0] = primaryFetchDefault
	// Backups all fetch at slot start (FetchAt = 0). No-op since make()
	// already zero-initializes.
	return out
}

// DefaultBroadcastBudgetSchedule returns the per-layer T_commit-anchored
// absorption windows paired with defaultFetchSchedule. Implements the
// post-tighten spec §Setting design:
//
//	B_0           = 2·BTT + RefloodDelay  (primary, MEV-fresh)
//	B_1..B_{K-1}  = T_commit              (backups broadcast at BFT_start)
//
// where `RefloodDelay` is the worst-case gossipsub IHAVE/IWANT reflood
// latency (bounded by HeartbeatInterval; defaults to 700ms for SSV
// deployments). The primary's 2·BTT base absorbs propagation (1·BTT P99
// + 1·BTT IWANT round-trip); the additive RefloodDelay accommodates one
// full reflood cycle when initial eager-push fails to reach all honest
// peers. Backups trade MEV-fresh fetch for the maximally-wide propagation
// absorption window — the entire commit budget.
//
// For K=1 returns [T_commit] (degenerate single-layer case — primary IS
// deepest). For K≥2 returns [2·BTT+RefloodDelay, T_commit, ..., T_commit].
//
// At extreme degraded operating points where T_commit shrinks below
// 2·BTT + RefloodDelay, B_0 is capped at T_commit so the schedule stays
// non-decreasing. The primary's target then clamps at BFT_start (same as
// the backups), and the primary becomes a redundant peer of the backups
// — cluster still operates. Operators who want the primary's MEV-fetch
// headroom preserved can widen T_commit (loosen Δ_2 / ε_3 / header
// headroom), lower RefloodDelay for denser meshes, or supply a custom
// schedule.
//
// Spec example values at BTT=200ms, RefloodDelay=700ms, T_commit=3600ms
// (Config A): K=4 → [1100, 3600, 3600, 3600]ms. At BTT=200ms,
// RefloodDelay=0 (fully-meshed cluster): K=4 → [400, 3600, 3600, 3600]ms.
func DefaultBroadcastBudgetSchedule(K int, btt, refloodDelay, tCommit time.Duration) ([]time.Duration, error) {
	if K < 1 {
		return nil, fmt.Errorf("obft adapter: DefaultBroadcastBudgetSchedule K=%d must be ≥ 1", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("obft adapter: DefaultBroadcastBudgetSchedule BTT=%v must be > 0", btt)
	}
	if refloodDelay < 0 {
		return nil, fmt.Errorf("obft adapter: DefaultBroadcastBudgetSchedule RefloodDelay=%v must be >= 0", refloodDelay)
	}
	out := make([]time.Duration, K)
	if K == 1 {
		out[0] = tCommit
		return out, nil
	}
	// L_0: primary with reflood-aware budget. Cap at T_commit (degraded case).
	b0 := btt*time.Duration(primaryBudgetDefaultBTT100)/100 + refloodDelay
	if b0 > tCommit {
		b0 = tCommit
	}
	out[0] = b0
	// L_1..L_{K-1}: all backups broadcast at BFT_start (B_k = T_commit).
	for k := 1; k < K; k++ {
		out[k] = tCommit
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
		broadcastBudget, err = DefaultBroadcastBudgetSchedule(K, overrides.btt(), overrides.refloodDelay(), overrides.tCommit())
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
		Eps3:      overrides.eps3(),
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
