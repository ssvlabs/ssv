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
//     Reflood absorption lives in the primary's B_0 = 2·BTT + SafetyBuffer
//     (backups L_1..L_{K-1} have B_k = T_commit), so Δ_2 no longer
//     carries a reflood cushion.
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
	// Reflood lives in B_k via SafetyBuffer — Δ_2 only needs to cover
	// the synchronous-fallback KindCommit propagation cycle.
	DefaultDelta2 = 1 * DefaultBTT

	// T_commit = RelayCutoff − HeaderSubmitHeadroom − JitterBuffer − ε_3 − Δ_2.
	// At Config A: 4000 − 100 − 50 − 50 − 200 = 3600ms.
	DefaultRelayCutoff = 4000 * time.Millisecond
	DefaultTCommit     = DefaultRelayCutoff - DefaultHeaderSubmitHeadroom - DefaultJitterBuffer - DefaultEps3 - DefaultDelta2

	// DefaultK is the recommended layer count for SSV proposer duty:
	// K = f+1 = 2 at n=4 (the BFT-liveness minimum). Motivated by spec
	// docs/OBFT.md §K-tuning: production testing showed K > f+1 doesn't
	// materially improve outcomes once peer-reflood-V closes the h_V=1
	// case at L_0. Trade-off: K=2 has only one fall-through layer (L_0
	// → L_1), so the late-deepest-layer-leader-broadcast Class A failure
	// mode at L_1 has no further backstop. Deployments preferring deeper
	// fall-through can set K ∈ [f+2, n] (up to K=n=4 at n=4) via
	// ConfigOverrides.K — see docs/OBFT.md §Setting for the K-bounds
	// discussion.
	DefaultK = 2

	// DefaultSafetyBuffer is the worst-case gossipsub-lazy-push latency
	// before a retransmission cycle completes, defaulted to SSV's gossipsub
	// HeartbeatInterval (network/topics/params/gossipsub.go). Used to size
	// the primary layer's broadcast budget B_0 = 2·BTT + SafetyBuffer so
	// that one full IHAVE/IWANT reflood cycle fits within the primary's
	// absorption window for mesh-flaky receivers — without it, a missed
	// eager-push at L_0 forecloses L_0 σ-quorum even under partial-
	// synchrony assumptions. Backups L_1..L_{K-1} use B_k = T_commit and
	// broadcast at BFT_start (SafetyBuffer does not affect them). Deployments
	// running on dense, fully-meshed clusters where eager-push reliably
	// reaches all peers (typically n=4 fully connected) MAY override to a
	// lower value (down to 0) to recover MEV-fetch headroom at L_0.
	DefaultSafetyBuffer = 700 * time.Millisecond
)

// Per-layer schedule defaults (spec §Setting, §Application / Timing budget
// at Config A):
//
//   - L_0 (primary) is MEV-fresh: fetches from RANDAO_done onward (~150ms
//     past slot start) and broadcasts by its recovery-floored target
//     `BroadcastTargetForLayer(0) = max(0, T_commit − max(B_0, 3·BTT))`,
//     where `B_0 = 2·BTT + SafetyBuffer` covers one IWANT round-trip plus
//     one IHAVE/IWANT reflood cycle for mesh-flaky receivers. The 3·BTT
//     floor is dormant at SafetyBuffer ≥ 1·BTT (a no-op at the Config A
//     default below, where it equals `T_commit − B_0`); at the
//     SafetyBuffer=0 opt-out it pulls the broadcast 1·BTT earlier so the
//     h_V=1 peer-reflood-V σ-upgrade still lands before T_commit — see
//     obft/base BroadcastTargetOffset; mirrors 2abOBFT's
//     max(SafetyBuffer, 1·BTT).
//   - L_1..L_{K-1} (backups) are last-resort safety nets: each fetches at
//     slot start from a deepest-confirmed parent (FetchAt = 0) and
//     broadcasts at BFT_start (B_k = T_commit clamps T_broadcast_max_k to
//     BFT_start). They trade MEV-fresh fetch for the maximally-wide
//     propagation absorption window — the entire commit budget.
//
// At Config A (BTT = 200ms, TCommit = 3600ms, SafetyBuffer = 700ms) the
// K=2 default schedule resolves to:
//   - budget  = [B_0=1100ms, T_commit=3600ms].
//   - fetchAt = [153ms, 0]ms — L_0 fetches just past RANDAO_done;
//     backup L_1 fetches at slot start.
//   - V_0 MEV-fetch budget at iterative-fetch = T_broadcast_max[0] −
//     fetchAt[0] − buildBuffer = (3600 − 1100) − 153 − 10 = 2337ms.
//   - V_1 MEV-fetch ~ 0ms (deepest-confirmed-parent vanilla payload).
//
// K=4 up-tier extends the backup pattern: budget = [1100, 3600, 3600,
// 3600]ms, fetchAt = [153, 0, 0, 0]ms — all backups (V_1, V_2, V_3) fetch
// at slot start with deepest-confirmed-parent.
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
// Generalizes to higher n (n=7 → K=3 default; n=10 → K=4 default;
// n=13 → K=5 default) per K = f+1 BFT-min rule. L_0 uses the primary
// defaults; all backups use FetchAt=0 + B_k=T_commit.
const (
	primaryFetchDefault        = 153 * time.Millisecond
	primaryBudgetDefaultBTT100 = 200 // 2.0 BTT — paired with +SafetyBuffer added at compute time
)

// ConfigOverrides allows callers to override the default deployment-environment
// parameters and per-duty layer config. Zero values fall back to package defaults.
//
// **Operator-facing surface (Q-I2 / spec docs/OBFT.md §Application):** operators
// supply `BTT` (the deployment's P99 + δ unit) plus deployment-environment values
// (`RelayCutoff`, `HeaderSubmitHeadroom`, `SafetyBuffer`). All protocol timings —
// `T_commit`, `Δ_2`, `ε_3`, JitterBuffer — derive deterministically from `BTT`
// and the deployment-environment values via spec-recommended formulas, so all
// operators in a cluster compute identical values. This prevents per-operator
// drift from breaking consensus.
//
// The `*Override` fields below (`tCommitOverride`, `delta2Override`, etc.) are
// unexported — they exist as test-only knobs for sim-stability fixtures that
// need over-budgeted timings, and are not part of the operator-facing API.
type ConfigOverrides struct {
	K                    int
	BTT                  time.Duration // P99 + δ; spec §Setting unit propagation+skew budget
	RelayCutoff          time.Duration // application hard deadline (e.g. 4s for proposer duty)
	HeaderSubmitHeadroom time.Duration // reserved for cert broadcast + relay submit (absolute)

	// SafetyBuffer is the worst-case gossipsub-lazy-push latency before a
	// retransmission cycle completes — bounded by the cluster's
	// HeartbeatInterval. When zero, defaults to DefaultSafetyBuffer (700ms,
	// matching SSV's configured HeartbeatInterval). The primary L_0's
	// B_0 = 2·BTT + SafetyBuffer so one full IHAVE/IWANT cycle fits in the
	// primary's absorption window for mesh-flaky receivers; backups
	// L_1..L_{K-1} have B_k = T_commit and broadcast at BFT_start with
	// deepest-confirmed-parent fetch (no MEV-fetch budget, maximally-wide
	// absorption). Deployments on fully-meshed clusters where eager-push
	// reaches all peers reliably may use a tiny positive value (e.g.
	// 1·time.Nanosecond) to opt out — Go's zero-means-default convention
	// prevents passing 0 explicitly.
	SafetyBuffer time.Duration

	// Test-only protocol-timing overrides. Production callers MUST NOT set
	// these — operator-supplied `BTT` is the only protocol-timing input, and
	// all other timings derive deterministically per spec (see type docstring
	// for the principle). Unexported so external callers cannot construct
	// these; settable from package-internal test fixtures only.
	tCommitOverride      time.Duration
	delta2Override       time.Duration
	eps3Override         time.Duration
	jitterBufferOverride time.Duration

	// FetchAt overrides the default per-layer fetch offsets. If nil,
	// defaults are used: L_0 fetches just past RANDAO_done
	// (primaryFetchDefault = 153ms); backups L_1..L_{K-1} all fetch at
	// slot start (FetchAt = 0). Length must match K (or zero/nil to use
	// defaults).
	FetchAt []time.Duration

	// BroadcastBudget overrides the default per-layer absorption windows
	// `B_k` (T_commit-anchored, per spec §Setting). When nil,
	// ConfigForCluster substitutes DefaultBroadcastBudgetSchedule(K, BTT,
	// SafetyBuffer, T_commit) which produces a non-decreasing schedule
	// conforming to spec (K=2 default Config A: B_0 = 2·BTT + SafetyBuffer =
	// 1100ms; B_1 = T_commit = 3600ms. K=4 up-tier: B = [1100, 3600, 3600,
	// 3600]ms). Backups broadcast at BFT_start in all cases. At degraded
	// operating points B_0 caps at T_commit so the schedule stays
	// non-decreasing. obft.Config.Validate requires every layer's
	// BroadcastBudget > 0 — no all-zero fallback. When set, length must
	// match K and values must be non-decreasing in layer index.
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

func (o *ConfigOverrides) safetyBuffer() time.Duration {
	if o == nil || o.SafetyBuffer == 0 {
		return DefaultSafetyBuffer
	}
	return o.SafetyBuffer
}

// eps3 derives from absolute ε_3 (doesn't scale with BTT per spec).
// Test-only override via eps3Override field.
func (o *ConfigOverrides) eps3() time.Duration {
	if o == nil || o.eps3Override == 0 {
		return DefaultEps3
	}
	return o.eps3Override
}

// jitterBuffer derives from absolute residual jitter (doesn't scale with
// BTT per spec §Application / Timing budget). Test-only override via
// jitterBufferOverride field.
func (o *ConfigOverrides) jitterBuffer() time.Duration {
	if o == nil || o.jitterBufferOverride == 0 {
		return DefaultJitterBuffer
	}
	return o.jitterBufferOverride
}

// delta2 derives as 1 BTT per spec §Phase 2 recommendation (KindCommit
// synchronous-fallback propagation cycle). Reflood lives in B_k via
// SafetyBuffer. Test-only override via delta2Override field.
func (o *ConfigOverrides) delta2() time.Duration {
	if o != nil && o.delta2Override != 0 {
		return o.delta2Override
	}
	return 1 * o.btt()
}

// tCommit derives as RelayCutoff − HeaderSubmitHeadroom − JitterBuffer −
// ε_3 − Δ_2 per spec §Application / Timing budget. Test-only override via
// tCommitOverride field.
func (o *ConfigOverrides) tCommit() time.Duration {
	if o != nil && o.tCommitOverride != 0 {
		return o.tCommitOverride
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
//	B_0           = 2·BTT + SafetyBuffer  (primary, MEV-fresh)
//	B_1..B_{K-1}  = T_commit              (backups broadcast at BFT_start)
//
// where `SafetyBuffer` is the worst-case gossipsub IHAVE/IWANT reflood
// latency (bounded by HeartbeatInterval; defaults to 700ms for SSV
// deployments). The primary's 2·BTT base absorbs propagation (1·BTT P99
// + 1·BTT IWANT round-trip); the additive SafetyBuffer accommodates one
// full reflood cycle when initial eager-push fails to reach all honest
// peers. Backups trade MEV-fresh fetch for the maximally-wide propagation
// absorption window — the entire commit budget.
//
// For K=1 returns [T_commit] (degenerate single-layer case — primary IS
// deepest). For K≥2 returns [2·BTT+SafetyBuffer, T_commit, ..., T_commit].
//
// At extreme degraded operating points where T_commit shrinks below
// 2·BTT + SafetyBuffer, B_0 is capped at T_commit so the schedule stays
// non-decreasing. The primary's target then clamps at BFT_start (same as
// the backups), and the primary becomes a redundant peer of the backups
// — cluster still operates. Operators who want the primary's MEV-fetch
// headroom preserved can widen T_commit (loosen Δ_2 / ε_3 / header
// headroom), lower SafetyBuffer for denser meshes, or supply a custom
// schedule.
//
// Spec example values at BTT=200ms, SafetyBuffer=700ms, T_commit=3600ms
// (Config A): K=2 default → [1100, 3600]ms; K=4 up-tier → [1100, 3600,
// 3600, 3600]ms. At BTT=200ms, SafetyBuffer=0 (fully-meshed cluster):
// K=2 → [400, 3600]ms; K=4 → [400, 3600, 3600, 3600]ms.
func DefaultBroadcastBudgetSchedule(K int, btt, safetyBuffer, tCommit time.Duration) ([]time.Duration, error) {
	if K < 1 {
		return nil, fmt.Errorf("obft adapter: DefaultBroadcastBudgetSchedule K=%d must be ≥ 1", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("obft adapter: DefaultBroadcastBudgetSchedule BTT=%v must be > 0", btt)
	}
	if safetyBuffer < 0 {
		return nil, fmt.Errorf("obft adapter: DefaultBroadcastBudgetSchedule SafetyBuffer=%v must be >= 0", safetyBuffer)
	}
	out := make([]time.Duration, K)
	if K == 1 {
		out[0] = tCommit
		return out, nil
	}
	// L_0: primary with reflood-aware budget. Cap at T_commit (degraded case).
	b0 := btt*time.Duration(primaryBudgetDefaultBTT100)/100 + safetyBuffer
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
		broadcastBudget, err = DefaultBroadcastBudgetSchedule(K, overrides.btt(), overrides.safetyBuffer(), overrides.tCommit())
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
