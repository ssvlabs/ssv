// Package base implements the bare OBFT (Onion BFT) protocol — a single-round
// agreement protocol for SSV clusters that produces one collective threshold-
// signed value per "slot" against a hard deadline.
//
// The protocol is described in docs/OBFT.md. OBFT is the simpler-spec cousin
// of OBFTR (multi-round retry) and 2abOBFT (Phase 2a/2b split); this package
// implements the bare single-round (R=1) form.
//
// Shared cryptography primitives (Signer, ThresholdIBE, NoQuorumTag, bare
// type aliases) live in the parent obft package; this package re-exports
// the type aliases for callers' convenience and owns all bare-OBFT-specific
// data structures, state machine, wire format, evidence types, and EKM
// coordinator.
//
// This package is intentionally independent of github.com/ssvlabs/ssv-spec.
// SSV-specific integration lives in protocol/v2/ssv/runner/obft.
package base

import (
	"errors"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Type aliases for the super-generic primitives owned by the parent obft
// package, re-exported here so callers using base/ types don't need to
// import both packages. These are not new types — base.OperatorID and
// obft.OperatorID are the same underlying uint64.
type (
	OperatorID   = obft.OperatorID
	Height       = obft.Height
	Value        = obft.Value
	Signature    = obft.Signature
	Signer       = obft.Signer
	ThresholdIBE = obft.ThresholdIBE
	StubSigner   = obft.StubSigner
	StubIBE      = obft.StubIBE
)

// Re-exported constructors/functions from the parent obft package.
// Wrapper functions (not var aliases) so the symbols are immutable and
// can't be rebound at runtime.

// NewStubSigner returns a deterministic test signer. See obft.NewStubSigner.
func NewStubSigner(quorum int, share []byte) *obft.StubSigner {
	return obft.NewStubSigner(quorum, share)
}

// NewStubIBE returns a deterministic test IBE. See obft.NewStubIBE.
func NewStubIBE(quorum int) *obft.StubIBE {
	return obft.NewStubIBE(quorum)
}

// NoQuorumTag derives the IBE tag for layer k. See obft.NoQuorumTag.
func NoQuorumTag(clusterID [32]byte, height obft.Height, layer int) []byte {
	return obft.NoQuorumTag(clusterID, height, layer)
}

// LayerSpec describes one layer of the K-layer onion structure: which
// operator is the layer's leader, when (relative to slot start) they should
// fetch their candidate value, and how much absorption budget the layer's
// receivers are given.
//
// Per spec §Setting, fetch times are non-increasing in layer index:
// Layers[0] is the primary L_0 (latest fetch — picks up MEV-late values),
// Layers[K-1] is the deepest backup L_{K-1} (earliest fetch — fetches from
// a deeper-confirmed parent). The asymmetric schedule is what gives backups
// re-org resistance while primary leaders capture late MEV.
type LayerSpec struct {
	Leader  OperatorID
	FetchAt time.Duration

	// BroadcastBudget is the layer's T_commit-anchored absorption *target*
	// `B_k` per OBFT.md §Setting: the leader aims to broadcast their
	// Phase-1 bundle by `T_broadcast_max_k = max(0, T_commit − B_k)` so
	// the bundle's first-observation at any honest receiver lands by
	// `T_commit` under partial-synchrony assumptions for that layer's
	// propagation budget. Per spec `B_0 < B_1 < ... < B_{K-1}` — deeper
	// layers get larger budgets (wider absorption); the primary gets the
	// smallest (max MEV-fetch headroom, willing to fall through to L_1+
	// if propagation slips).
	//
	// B_k is a *target*, not a hard runtime cap. The only runtime
	// acceptance gate is `T_commit` (peers admit bundles first-observed
	// in `[slot_start, T_commit]` regardless of which layer they came
	// from). A leader that cannot meet `T_broadcast_max_k` broadcasts
	// best-effort (broadcast as soon as the bundle is ready). When
	// `B_k ≥ T_commit`, `T_broadcast_max_k` clamps at 0 — the leader's
	// target broadcast time is slot start. The recommended deepest
	// `B_{K-1} = T_commit` deliberately hits this clamp ("earliest
	// possible" deepest broadcast).
	//
	// Spec K=4 Config A (BTT=200ms, T_commit=3600ms, RefloodDelay=700ms):
	// B_k values are 2·BTT+RD, 3·BTT+RD, 4·BTT+RD, T_commit
	// (= 1100/1300/1500/3600 ms). The deepest is "earliest possible" —
	// leader's target broadcast clamps to slot start. Each shallow B_k
	// decomposes as (k+2)·BTT (1·BTT P99 propagation + 1·BTT IWANT
	// round-trip + (k)·BTT per-deeper-layer jitter cushion) + RefloodDelay
	// (one full IHAVE/IWANT cycle), per spec §Setting. The decomposition
	// is informative; this field is the single T_commit-anchored target.
	//
	// Required: must be > 0 on every layer. Config.Validate rejects
	// zero/negative values and decreasing-in-k schedules (equal adjacent
	// budgets are accepted — multiple layers may share the BFT_start
	// clamp at degraded operating points; see spec §Setting). Use
	// DefaultBroadcastBudget(K, BTT, T_commit) for the spec-recommended
	// staggered schedule when constructing a Config manually.
	BroadcastBudget time.Duration
}

// Config parameterizes one consensus instance. Timing fields are absolute
// offsets relative to slot start; see docs/OBFT.md §Timing budget for the
// constraints relating them.
type Config struct {
	// Height identifies this consensus instance (the slot, in SSV usage).
	Height Height

	// ClusterID uniquely identifies the cluster running this instance.
	// Used in NR-tag construction to prevent cross-cluster replay.
	ClusterID [32]byte

	// Operators is the full set of operators in the cluster.
	Operators []OperatorID

	// F is the byzantine bound. Cluster size must be >= 3F+1.
	F int

	// Layers is the priority-ordered list of K layers; K = len(Layers).
	// Layer 0 is the primary; deeper layers are progressively-fallback
	// backups. Each layer's leader must be a distinct cluster member.
	Layers []LayerSpec

	// TCommit is T_commit — the view-fix point. Each operator commits its
	// stance based on what it observed by this offset.
	TCommit time.Duration

	// Delta2 is Δ_2 — the Phase 2 window length. Per spec §Phase 2,
	// `Delta2 = 1 BTT` is the recommended sizing: one propagation cycle
	// for KindCommit messages emitted by T_commit (the synchronous
	// fallback) to reach all honest peers before Phase 3. Reflood
	// absorption is structurally provided by per-layer `B_k` via the
	// reflood-aware schedule (`(k+2)·BTT + RefloodDelay`), so Delta2 no
	// longer carries a reflood cushion. Sub-1·BTT sizings are sub-BFT
	// (Phase-2 propagation can't complete within the budget).
	Delta2 time.Duration

	// Eps3 is ε_3 — the Phase 3 window length. Per spec, Eps3 covers
	// local reconstruction processing (BLS aggregation, IBE decryption walk,
	// certificate construction). KindCommit propagation is already covered
	// by Delta2. Absolute (does not scale with BTT); per OBFT.md §Phase 3 /
	// §Timing budget, ε_3 ≈ 50ms at Config A for single-layer
	// reconstruction (σ-quorum at L_0). Under multi-layer fall-through,
	// the IBE-decryption walk runs sequentially through each NR-quorum-
	// unlocked layer, so end-to-end Phase 3 cost grows roughly linearly
	// with the number of fall-throughs (~ε_3 × K at K layers walked).
	Eps3 time.Duration

	// BTT is Block-Trip-Time, the unit propagation+skew budget. Per spec
	// §Setting: BTT = P99 + δ, where P99 is the cluster gossipsub propagation
	// at the deployment's chosen tail percentile and δ is the clock-skew
	// bound. Used as the unit for time-budget formulas (e.g. Delta2 = 2 BTT
	// recommended; B_k staggered as multiples of BTT). Concrete sizing at
	// Config A: P99 = 150ms, δ = 50ms, BTT = 200ms.
	BTT time.Duration
}

// K returns the number of layers (= len(Layers)).
func (c *Config) K() int {
	return len(c.Layers)
}

// QV returns the σ-quorum threshold (positive partial-sig quorum). Per spec
// §Setting, qV = 2f+1.
func (c *Config) QV() int {
	return 2*c.F + 1
}

// QEnc returns the IBE-unlock threshold (NR-attestation quorum). Per spec
// §Setting, qEnc = qV = 2f+1 — the unified threshold is what Pigeonhole 1's
// algebra requires.
func (c *Config) QEnc() int {
	return 2*c.F + 1
}

// Quorum is an alias for QV; kept for code-readability where the σ-side
// threshold is the natural reading.
func (c *Config) Quorum() int {
	return c.QV()
}

// BroadcastMaxOffsetForLayer returns `T_broadcast_max_k = max(0, T_commit − B_k)`
// for layer k — the leader's target broadcast time per spec §Setting.
//
// `B_k` is a target, not a hard cap; the clamp at 0 handles the degraded case
// where the layer's design-time budget overshoots T_commit. In that case the
// leader broadcasts best-effort from slot start and the layer's effective
// absorption window contracts to T_commit (= `min(B_k, T_commit)`). The only
// runtime acceptance gate remains T_commit at receivers.
func (c *Config) BroadcastMaxOffsetForLayer(k int) time.Duration {
	if d := c.TCommit - c.Layers[k].BroadcastBudget; d > 0 {
		return d
	}
	return 0
}

// DefaultBroadcastBudget returns a spec-conforming staggered B_k schedule
// for K layers at the given BTT, RefloodDelay, and T_commit. Per spec
// §Setting, B_k is sized to accommodate one gossipsub IHAVE/IWANT reflood
// cycle when initial eager-push fails to reach all honest peers:
//
//	B_k_shallow = (k+2)·BTT + RefloodDelay  for k ∈ [0, K-2]
//	B_{K-1}     = T_commit                  (deepest broadcasts at BFT_start)
//
// `RefloodDelay` is the worst-case gossipsub-lazy-push latency before a
// retransmission cycle completes — bounded by the cluster's HeartbeatInterval.
// At RefloodDelay = 0 the schedule collapses to {2, 3, 4}·BTT (the "fully-
// meshed cluster, eager push reliable" assumption); production SSV
// deployments use RefloodDelay = 700ms (SSV's gossipsub HeartbeatInterval).
//
// At K=4 returns [2·BTT+RD, 3·BTT+RD, 4·BTT+RD, T_commit]. At K=3 returns
// [2·BTT+RD, 3·BTT+RD, T_commit]. At K=2 returns [2·BTT+RD, T_commit]. For
// K>4 the first three layers stay at 2 / 3 / 4 BTT + RD and the intermediate
// layers (k = 3, ..., K-2) interpolate linearly in duration space from
// 4·BTT + RD (at L_2) to T_commit (at L_{K-1}).
//
// At extreme degraded operating points where T_commit shrinks below the
// canonical shallow multiples (e.g. T_commit ≤ 4·BTT + RefloodDelay at K≥4),
// the helper still returns a schedule — the shallow B_k values can exceed
// T_commit. The protocol's runtime `T_broadcast_max_k = max(BFT_start,
// T_commit − B_k)` clamps those layers' targets at BFT_start, so the
// configuration remains valid (fall-through depth shrinks but the
// cluster still operates). Callers that want the canonical staggered
// shape preserved can either widen T_commit (loosen Δ_2 / ε_3 / header
// headroom), lower RefloodDelay for denser meshes, or supply their own
// per-layer schedule.
//
// Callers that need deployment-specific tunings (e.g. the SSV adapter's
// FetchAt-paired schedule) should provide their own per-layer values; this
// helper is for tests and minimal callers that want an obviously-correct
// default. Required by Validate: every LayerSpec.BroadcastBudget entry
// must be > 0 and the slice must be non-decreasing.
func DefaultBroadcastBudget(K int, btt, refloodDelay, tCommit time.Duration) ([]time.Duration, error) {
	if K < 1 {
		return nil, fmt.Errorf("obft: DefaultBroadcastBudget K=%d must be ≥ 1", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("obft: DefaultBroadcastBudget BTT=%v must be > 0", btt)
	}
	if refloodDelay < 0 {
		return nil, fmt.Errorf("obft: DefaultBroadcastBudget RefloodDelay=%v must be >= 0", refloodDelay)
	}
	// shallow returns (k+2)*BTT + RefloodDelay for shallow layer k.
	shallow := func(k int) time.Duration {
		return time.Duration(k+2)*btt + refloodDelay
	}
	out := make([]time.Duration, K)
	switch K {
	case 1:
		out[0] = tCommit
	case 2:
		out[0] = shallow(0)
		out[1] = tCommit
	case 3:
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = tCommit
	case 4:
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = shallow(2)
		out[3] = tCommit
	default:
		// First three layers at 2 / 3 / 4 BTT + RD; intermediate layers
		// interpolate linearly in duration space from 4·BTT + RD (at L_2)
		// to T_commit (at L_{K-1}).
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = shallow(2)
		out[K-1] = tCommit
		span := tCommit - out[2]
		steps := K - 3
		for k := 3; k < K-1; k++ {
			out[k] = out[2] + span*time.Duration(k-2)/time.Duration(steps)
		}
	}
	// Cap each B_k at T_commit so the schedule stays non-decreasing even
	// at degraded operating points where the canonical staggered shallow
	// multiples (or RefloodDelay-inflated values) overshoot. Capped layers
	// share `T_broadcast_max_k = max(BFT_start, T_commit − B_k) = BFT_start`
	// — multiple layers may collide at BFT_start without safety impact.
	// The deepest layer is already T_commit by construction; the cap turns
	// degraded shallow layers into "broadcast at BFT_start" peers of the
	// deepest.
	for k := 0; k < K; k++ {
		if out[k] > tCommit {
			out[k] = tCommit
		}
	}
	return out, nil
}

// PhaseTwoStartOffset returns the start of Phase 2 = T_commit relative to
// slot_start. Bundles first-observed past this point at any honest receiver
// are not counted by that receiver toward σ-quorum; the cluster relies on
// K-layer fall-through for partition recovery (no Defer state, no late
// σ-emit window).
func (c *Config) PhaseTwoStartOffset() time.Duration {
	return c.TCommit
}

// PhaseTwoEndOffset returns the end of the Phase-2 propagation budget —
// `T_commit + Delta2`. By this offset, every honest operator's KindCommit
// should be observable cluster-wide under nominal partial synchrony.
//
// This is the SOFT target for "Phase-2 inputs are complete enough for
// σ-quorum to form", not a hard gate on Phase-3 Resolve. Resolve is
// idempotent (re-running on incomplete state returns ErrNoQuorum without
// mutation), so the canonical implementation is observer-mode: Resolve is
// invoked opportunistically from T_commit onward on every KindCommit /
// KindCertificate arrival, and the average healthy slot decides well
// before this offset.
func (c *Config) PhaseTwoEndOffset() time.Duration {
	return c.TCommit + c.Delta2
}

// RoundEndOffset returns T_commit + Delta2 + Eps3 — the SOFT per-operator
// target by which the local Phase-3 reconstruction walk is expected to
// complete under nominal partial synchrony. Per spec §Phase 3, this is
// NOT a hard deadline:
//
//   - Phase 3 may be attempted opportunistically from T_commit onward
//     (Resolve is idempotent and returns ErrNoQuorum cleanly on incomplete
//     state). PhaseTwoEndOffset is the SOFT propagation target, not a
//     Resolve-gating wall. The canonical implementation observes inbound
//     KindCommit / KindCertificate arrivals and calls Resolve on each.
//   - Reconstruction overrunning Eps3 can spill into the submission
//     slack; a faster peer's KindCertificate gossip can let an operator
//     that hasn't completed local reconstruction submit (V, S) directly.
//   - Late KindCommit arrivals past T_commit + Delta2 can be incorporated
//     by re-running the reconstruction walk; Pigeonhole semantics still
//     hold (at most one V can reconstruct cluster-wide regardless of
//     timing).
//
// The hard wall for the slot is the relay-submission deadline (typically
// T_relay_cutoff − T_submit), enforced at the runner level via context
// cancellation, not here.
func (c *Config) RoundEndOffset() time.Duration {
	return c.TCommit + c.Delta2 + c.Eps3
}

// Validate checks the config for internal consistency. Bad configs are
// programmer errors; callers run this once at instance construction.
func (c *Config) Validate() error {
	if c.F < 1 {
		return errors.New("obft: byzantine bound F must be >= 1")
	}
	if len(c.Operators) < 3*c.F+1 {
		return errors.New("obft: cluster size must be at least 3F+1")
	}
	// Per spec §Setting: K ≥ f+1 is the BFT-liveness minimum (pigeonhole
	// over the f-byz bound guarantees ≥ 1 honest leader; at K < f+1 all
	// leaders could be byzantine and no σ-quorum reaches at any layer).
	// K ≥ f+2 additionally provides late-leader-resilience (≥ 2 honest
	// leaders, so a single late-broadcasting honest leader doesn't
	// foreclose the slot via the deepest-layer NR-lock pathology) — that
	// choice is left to the operator/deployment per spec §Setting and is
	// not enforced here.
	minK := c.F + 1
	if len(c.Layers) < minK {
		return fmt.Errorf("obft: K=%d below BFT-liveness minimum %d (= f+1 at f=%d)",
			len(c.Layers), minK, c.F)
	}
	if len(c.Layers) > len(c.Operators) {
		return errors.New("obft: K cannot exceed cluster size")
	}
	if c.BTT <= 0 {
		return errors.New("obft: BTT must be positive")
	}
	if c.TCommit <= 0 {
		return errors.New("obft: TCommit must be positive")
	}
	if c.Delta2 <= 0 {
		return errors.New("obft: Delta2 must be positive")
	}
	if c.Eps3 <= 0 {
		return errors.New("obft: Eps3 must be positive")
	}
	// Delta2 < 1 BTT and TCommit < 2 BTT are the BFT-liveness minimums for
	// Phase 2 propagation and the broadcast deadline (spec §Setting). Below
	// these thresholds the cluster systematically misses (KindCommit
	// messages don't propagate before Phase 3 starts; leader broadcasts
	// don't fit before T_commit). Validate does not enforce these floors
	// — that's a deployment / operator choice. The simulator and
	// production stack still run; the resulting 0% success-rate is
	// informative data, not a setup error.

	members := make(map[OperatorID]bool, len(c.Operators))
	for _, op := range c.Operators {
		if members[op] {
			return errors.New("obft: duplicate operator ID in cluster")
		}
		members[op] = true
	}

	// Per-layer BroadcastBudget — required on every layer per spec §Setting.
	// Callers can use DefaultBroadcastBudget(K, BTT, T_commit) for the spec-recommended
	// staggered schedule, or supply their own per-layer values.
	for k, l := range c.Layers {
		if l.BroadcastBudget <= 0 {
			return fmt.Errorf("obft: layer %d BroadcastBudget must be > 0 (use DefaultBroadcastBudget for a spec-conforming staggered schedule)", k)
		}
	}
	// B_0 ≤ B_1 ≤ ... ≤ B_{K-1}: deeper layers get ≥ their predecessor's
	// absorption / chain-decryption headroom (spec §Setting "B_k ≥
	// B_{k-1}" verbatim). Equal adjacent budgets are tolerated: at
	// degraded operating points multiple layers' broadcast targets clamp
	// to BFT_start (the runtime `max(BFT_start, T_commit − B_k)` floor),
	// and at extreme operating points the canonical staggered shallow
	// budgets even exceed T_commit. Strict-increasing was historically
	// enforced but rejected these degenerate-but-still-valid configs;
	// non-decreasing keeps the staggering intent without blocking them.
	for k := 1; k < len(c.Layers); k++ {
		if c.Layers[k].BroadcastBudget < c.Layers[k-1].BroadcastBudget {
			return errors.New("obft: BroadcastBudget must be non-decreasing in layer index (B_0 ≤ B_1 ≤ ...)")
		}
	}
	// The deepest layer's budget is the cluster's worst-case liveness
	// guarantee. Spec §Setting recommends `B_{K-1} ≥ 2·BTT` for the
	// cluster to have a liveness guarantee at any layer; below that the
	// deepest leader's bundle can't both propagate and reach Phase-2
	// quorum before T_commit, so the cluster systematically misses. The
	// floor is *informational* — Validate does not enforce it. Operators
	// who want to study (or knowingly run) at extreme operating points
	// where no layer has a liveness guarantee can; the simulator and
	// production stack still execute.

	seenLeaders := make(map[OperatorID]bool, len(c.Layers))
	for k, layer := range c.Layers {
		if !members[layer.Leader] {
			return errors.New("obft: layer leader is not a cluster member")
		}
		if seenLeaders[layer.Leader] {
			return errors.New("obft: duplicate leader across layers")
		}
		seenLeaders[layer.Leader] = true
		if layer.FetchAt < 0 {
			return errors.New("obft: layer FetchAt must be non-negative")
		}
		if layer.FetchAt > c.BroadcastMaxOffsetForLayer(k) {
			// When B_k ≥ T_commit the deadline clamps to 0 (best-effort
			// broadcast at slot start, per spec §Setting); FetchAt must
			// then be 0 too. Surface the underlying T_commit−B_k value
			// so over-budget configs are obvious in the error.
			return fmt.Errorf("obft: layer %d FetchAt %v exceeds broadcast deadline %v (T_commit−B_k = %v)",
				k, layer.FetchAt, c.BroadcastMaxOffsetForLayer(k), c.TCommit-c.Layers[k].BroadcastBudget)
		}
		// Per spec §Setting: T_{K-1} ≤ ... ≤ T_1 ≤ T_0. Deeper layers
		// fetch ≤ their predecessor's offset (re-org resistance, MEV-
		// fetch asymmetry between primary and backups). Strict-
		// decreasing was historically enforced; the non-increasing
		// relaxation lets multiple layers' targets collide at BFT_start
		// when the operating point pushes shallow targets past T_commit
		// (matches the BroadcastBudget non-decreasing relaxation above).
		if k > 0 && layer.FetchAt > c.Layers[k-1].FetchAt {
			return errors.New("obft: layer fetch times must be non-increasing in k")
		}
	}
	return nil
}
