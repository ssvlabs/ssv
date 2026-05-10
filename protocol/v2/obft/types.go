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

	// BroadcastBudget is the layer's T_commit-anchored absorption window:
	// the leader must broadcast their Phase-1 bundle no later than
	// T_commit - BroadcastBudget. Per spec §Setting this corresponds to
	// `B_k + slack` (the receiver-side absorption ceiling — bundles
	// first-observed past T_commit are not counted, so the absorption
	// must include the `slack` decision-deferral window). Per spec
	// B_0 + slack < B_1 + slack < ... < B_{K-1} + slack — deeper layers
	// get larger budgets (max absorption tolerance); the primary gets the
	// smallest (max MEV-fetch headroom, willing to fall through to L_1+
	// if propagation slips).
	//
	// Spec K=4 Config A (BTT=200ms, slack=0.5 BTT): B_k+slack values are
	// 1, 1.5, 2.5, 5.5 BTT (= 200/300/500/1100 ms). Equivalently the
	// leader-side budgets B_k cited in the spec are 0.5/1/2/5 BTT
	// (Ls_arrival-anchored); this field carries the T_commit-anchored
	// form for cleaner per-layer math.
	//
	// When zero across all layers, falls back to the single uniform cap
	// 2*BTT for every layer (preserves backwards-compat with configs
	// that don't opt into the staggered schedule).
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

// BroadcastMaxOffset returns the single-cap T_broadcast_max relative to
// slot_start: T_commit - 2*BTT. Used as the per-layer fallback when
// LayerSpec.BroadcastBudget is unset; for explicit per-layer caps see
// BroadcastMaxOffsetForLayer.
func (c *Config) BroadcastMaxOffset() time.Duration {
	return c.TCommit - 2*c.BTT
}

// BroadcastMaxOffsetForLayer returns T_commit - B_k for layer k, falling back
// to the single cap when LayerSpec.BroadcastBudget is unset. Per spec §Setting,
// the leader of layer k must broadcast their Phase-1 bundle by this offset.
func (c *Config) BroadcastMaxOffsetForLayer(k int) time.Duration {
	b := c.Layers[k].BroadcastBudget
	if b <= 0 {
		return c.BroadcastMaxOffset()
	}
	return c.TCommit - b
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
	if c.BroadcastMaxOffset() < 0 {
		return errors.New("obft: TCommit too small for broadcast deadline (need TCommit > 2*BTT)")
	}

	members := make(map[OperatorID]bool, len(c.Operators))
	for _, op := range c.Operators {
		if members[op] {
			return errors.New("obft: duplicate operator ID in cluster")
		}
		members[op] = true
	}

	// Two BroadcastBudget modes: all unset (single-cap fallback) or all set
	// (per-layer staggered schedule with strict monotonicity). Negative
	// values are always invalid.
	for k, l := range c.Layers {
		if l.BroadcastBudget < 0 {
			return fmt.Errorf("obft: layer %d BroadcastBudget %v is negative", k, l.BroadcastBudget)
		}
	}
	allBudgetsSet := true
	allBudgetsZero := true
	for _, l := range c.Layers {
		if l.BroadcastBudget > 0 {
			allBudgetsZero = false
		} else {
			allBudgetsSet = false
		}
	}
	if !allBudgetsSet && !allBudgetsZero {
		return errors.New("obft: LayerSpec.BroadcastBudget must be set on every layer or none (no mixed configs)")
	}
	if allBudgetsSet {
		// B_0 < B_1 < ... < B_{K-1}: deeper layers get strictly larger
		// absorption / chain-decryption headroom.
		//
		// Spec §Setting states "B_k ≥ B_{k-1}" (non-strict). We enforce strict
		// "<" because all the spec's recommended operating-point schedules
		// (Config A: 0.5 / 1 / 2 / 5 BTT) are strictly increasing, and equal
		// adjacent budgets would mean the deeper layer offers no additional
		// absorption — defeating the purpose of staggering. The strict bound
		// catches misconfigurations that would silently degrade fall-through
		// recovery at no protocol benefit.
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
			return fmt.Errorf("obft: layer %d FetchAt %v exceeds broadcast deadline %v",
				k, layer.FetchAt, c.BroadcastMaxOffsetForLayer(k))
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
