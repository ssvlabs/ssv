// Package obft implements the OBFT (Onion BFT) protocol — a single-round
// agreement protocol for SSV clusters that produces one collective threshold-
// signed value per "slot" against a hard deadline.
//
// The protocol is described in docs/OBFT.md. OBFT is the simpler-spec cousin
// of OBFTR (multi-round retry); this package implements the bare single-round
// (R=1) form.
//
// This package is intentionally independent of github.com/ssvlabs/ssv-spec.
// All types here are generic; SSV-specific integration lives in
// protocol/v2/ssv/runner/obft.
package obft

import (
	"errors"
	"fmt"
	"time"
)

// OperatorID identifies a participant in the cluster. Matches the uint64 ID
// space used elsewhere in SSV but is not coupled to spec types.
type OperatorID uint64

// Height is a generic instance identifier — typically a slot number when used
// in SSV but otherwise opaque. The protocol does not interpret it; tag
// construction binds it into the IBE labels to prevent cross-instance replay.
type Height uint64

// Value is the candidate bytes being agreed upon (e.g. a serialized blinded
// block in the SSV proposer-duty case). The protocol treats it as opaque.
type Value []byte

// Signature is a BLS signature, either a partial (operator share) or a
// reconstructed full signature.
type Signature []byte

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
	// Spec K=4 Config A (BTT=200ms, T_commit=3400ms): B_k values are 1·BTT,
	// 1.5·BTT, 2.5·BTT, T_commit (= 200/300/500/3400 ms). The deepest
	// is "earliest possible" — leader's target broadcast clamps to slot
	// start. Internally each shallow B_k decomposes as typical-mesh
	// propagation + convergence buffer (spec §Setting quotes B_0 = 1
	// BTT as ≈0.5 BTT propagation + 0.5 BTT convergence); the
	// decomposition is informative-only — this field is the single
	// T_commit-anchored value, NOT the propagation half alone.
	//
	// Required: must be > 0 on every layer. Config.Validate rejects
	// zero/negative values and non-strict-increasing schedules. Use
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

	// Delta2 is Δ_2 — the Phase 2 window length. Per spec §Phase 2, Delta2 ≥
	// 1 BTT is the propagation budget for KindCommit messages emitted at
	// T_commit to reach all honest peers before Phase 3. Recommended for
	// production: Delta2 = 2 BTT (one full propagation cycle of slack on top
	// of the P99 budget).
	Delta2 time.Duration

	// Delta3 is Δ_3 — the Phase 3 window length. Per spec, Delta3 covers
	// local reconstruction processing (BLS aggregation, IBE decryption walk,
	// certificate construction). KindCommit propagation is already covered
	// by Delta2. Absolute (does not scale with BTT); per OBFT.md §Phase 3 /
	// §Timing budget, ε_3 ≈ 50ms at Config A for single-layer
	// reconstruction (σ-quorum at L_0). Under multi-layer fall-through,
	// the IBE-decryption walk runs sequentially through each NR-quorum-
	// unlocked layer, so end-to-end Phase 3 cost grows roughly linearly
	// with the number of fall-throughs (~ε_3 × K at K layers walked).
	Delta3 time.Duration

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
// for K layers at the given BTT and T_commit — strictly increasing, deepest
// = T_commit (the spec §Setting "earliest possible" deepest: leader's target
// broadcast clamps to slot start). Matches the spec §Setting recommendation:
// B_0 = 1 BTT (max MEV freshness at primary), B_{K-1} = T_commit (deepest
// broadcasts at slot start, last-resort absorption ≈ entire slot);
// intermediate shallow layers at 1.5 / 2.5 BTT.
//
// At K=4 (the OBFT proposer-duty default) returns [1·BTT, 1.5·BTT, 2.5·BTT,
// T_commit]. At K=3 returns [1·BTT, 2.5·BTT, T_commit]. For K>4 (n=7, 10, 13
// deployments) the first three layers stay at 1 / 1.5 / 2.5 BTT and the
// intermediate layers (k = 3, ..., K-2) interpolate linearly from 2.5·BTT
// to T_commit in duration space.
//
// Returns an error when T_commit ≤ 2.5·BTT (the default deepest would no
// longer be strictly greater than B_{K-2} = 2.5·BTT). Callers operating at
// extreme degraded BTT can provide a custom per-layer schedule.
//
// Callers that need deployment-specific tunings (e.g. the SSV adapter's
// FetchAt-paired schedule) should provide their own per-layer values; this
// helper is for tests and minimal callers that want an obviously-correct
// default. Required by Validate: every LayerSpec.BroadcastBudget entry
// must be > 0 and the slice must be strictly increasing.
func DefaultBroadcastBudget(K int, btt, tCommit time.Duration) ([]time.Duration, error) {
	if K < 1 {
		return nil, fmt.Errorf("obft: DefaultBroadcastBudget K=%d must be ≥ 1", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("obft: DefaultBroadcastBudget BTT=%v must be > 0", btt)
	}
	// The shallow staggered values reach 2.5·BTT at L_2 (K≥3) or 2·BTT at
	// L_1 (K=2). The deepest must be > the shallowest-but-deepest entry,
	// and that is 2.5·BTT for K≥3, 1·BTT for K=2, 2·BTT for K=1.
	var minDeepest time.Duration
	switch {
	case K == 1:
		minDeepest = 2 * btt
	case K == 2:
		minDeepest = btt
	default:
		minDeepest = btt * 250 / 100
	}
	if tCommit <= minDeepest {
		return nil, fmt.Errorf("obft: DefaultBroadcastBudget T_commit=%v must be > %v (B_{K-2} for K=%d at BTT=%v); supply a custom per-layer schedule for this operating point",
			tCommit, minDeepest, K, btt)
	}
	out := make([]time.Duration, K)
	switch K {
	case 1:
		out[0] = tCommit
	case 2:
		out[0] = btt
		out[1] = tCommit
	case 3:
		out[0] = btt
		out[1] = btt * 250 / 100
		out[2] = tCommit
	case 4:
		out[0] = btt
		out[1] = btt * 150 / 100
		out[2] = btt * 250 / 100
		out[3] = tCommit
	default:
		// First three layers at 1 / 1.5 / 2.5 BTT (spec values); intermediate
		// layers interpolate linearly in duration space from 2.5·BTT (at L_2)
		// to T_commit (at L_{K-1}).
		out[0] = btt
		out[1] = btt * 150 / 100
		out[2] = btt * 250 / 100
		out[K-1] = tCommit
		span := tCommit - out[2]
		steps := K - 3
		for k := 3; k < K-1; k++ {
			out[k] = out[2] + span*time.Duration(k-2)/time.Duration(steps)
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

// PhaseTwoEndOffset returns the end of Phase 2 — when Phase 3 reconstruction
// begins. Each operator emits exactly one KindCommit at T_commit; the Δ_2
// window is sized for that message to propagate to all honest peers before
// Phase 3.
func (c *Config) PhaseTwoEndOffset() time.Duration {
	return c.TCommit + c.Delta2
}

// RoundEndOffset returns T_commit + Delta2 + Delta3 — the SOFT per-operator
// target by which the local Phase-3 reconstruction walk is expected to
// complete under nominal partial synchrony. Per spec §Phase 3, this is
// NOT a hard deadline:
//
//   - Phase 3 starts at T_commit + Delta2 (= PhaseTwoEndOffset) and runs
//     until σ-quorum reaches OR the slot's relay-submission deadline
//     forces termination.
//   - Reconstruction overrunning Delta3 can spill into the submission
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
	return c.TCommit + c.Delta2 + c.Delta3
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
	// Per spec §Setting: K ≥ max(2, f+1) is BFT-liveness minimum (≥ 1
	// honest leader by pigeonhole); K ≥ f+2 is the late-leader-resilience
	// recommendation (≥ 2 honest leaders). We enforce the stricter f+2
	// bound and additionally floor at 3 — at f=1 the two coincide; at
	// higher f the f+2 bound dominates and prevents BFT-liveness violations
	// (e.g., f=3 with K=3 has all leaders potentially byzantine).
	minK := c.F + 2
	if minK < 3 {
		minK = 3
	}
	if len(c.Layers) < minK {
		return fmt.Errorf("obft: K=%d below late-leader-resilience minimum %d (= max(3, f+2) at f=%d)",
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
	if c.Delta2 < c.BTT {
		return errors.New("obft: Delta2 must be >= 1 BTT (BFT minimum per spec §Phase 2)")
	}
	if c.Delta3 <= 0 {
		return errors.New("obft: Delta3 must be positive")
	}
	if c.TCommit < 2*c.BTT {
		return errors.New("obft: TCommit too small for broadcast deadline (need TCommit >= 2*BTT)")
	}

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
	// B_0 < B_1 < ... < B_{K-1}: deeper layers get strictly larger
	// absorption / chain-decryption headroom.
	//
	// Spec §Setting states "B_k ≥ B_{k-1}" (non-strict). We enforce strict
	// "<" because all the spec's recommended operating-point schedules
	// (Config A at K=4: 1·BTT / 1.5·BTT / 2.5·BTT / T_commit) are strictly
	// increasing, and equal adjacent budgets would mean the deeper layer
	// offers no additional absorption — defeating the purpose of
	// staggering. The strict bound catches misconfigurations that would
	// silently degrade fall-through recovery at no protocol benefit.
	for k := 1; k < len(c.Layers); k++ {
		if c.Layers[k].BroadcastBudget <= c.Layers[k-1].BroadcastBudget {
			return errors.New("obft: BroadcastBudget must be strictly increasing in layer index (B_0 < B_1 < ...)")
		}
	}
	// The deepest layer's budget is the cluster's worst-case liveness
	// guarantee — must satisfy the 2*BTT BFT-min bound.
	bftMin := 2 * c.BTT
	K := len(c.Layers)
	if c.Layers[K-1].BroadcastBudget < bftMin {
		return fmt.Errorf("obft: deepest layer L_%d BroadcastBudget %v below BFT-min %v: cluster has no liveness guarantee",
			K-1, c.Layers[K-1].BroadcastBudget, bftMin)
	}

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
		// Per spec §Setting: T_{K-1} < ... < T_1 < T_0. Layer index k
		// increases as we go deeper; FetchAt must strictly decrease
		// with k so backups fetch from progressively-deeper-confirmed
		// parents (re-org resistance) and the asymmetric MEV-fetch
		// advantage between primary and backups is preserved.
		if k > 0 && layer.FetchAt >= c.Layers[k-1].FetchAt {
			return errors.New("obft: layer fetch times must be strictly decreasing in k")
		}
	}
	return nil
}
