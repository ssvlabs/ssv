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
// operator is the layer's leader and when (relative to slot start) they
// should fetch their candidate value.
//
// Per spec §Setting, fetch times are non-increasing in layer index:
// Layers[0] is the primary L_0 (latest fetch — picks up MEV-late values),
// Layers[K-1] is the deepest backup L_{K-1} (earliest fetch — fetches from
// a deeper-confirmed parent). The asymmetric schedule is what gives backups
// re-org resistance while primary leaders capture late MEV.
type LayerSpec struct {
	Leader  OperatorID
	FetchAt time.Duration
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

	// Delta2 is Δ_2 — the Phase 2 window length. Per spec, Delta2 >=
	// (D + Delta) is the BFT minimum; Delta2 >= 2*(D + Delta) is recommended
	// (gives within-Phase-2 partition recovery via late re-flood absorption).
	Delta2 time.Duration

	// Delta3 is Δ_3 — the Phase 3 window length. Per spec, Delta3 >=
	// (D + Delta) + ε_3, where ε_3 covers local reconstruction processing.
	// Sizing Delta3 as just propagation-independent local work is a bug
	// because NR partials emitted at end-of-Phase-2 must propagate before
	// Phase 3 can use them.
	Delta3 time.Duration

	// D is the cluster gossipsub propagation P99/P999 budget.
	D time.Duration

	// Delta is δ — the clock skew bound across operators.
	Delta time.Duration
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

// BroadcastMaxOffset returns T_broadcast_max relative to slot_start —
// the latest moment a leader's Phase-1 bundle can be broadcast such that
// all honest first-observe within partial synchrony before T_commit.
//
// Per spec §Setting: T_broadcast_max = T_commit - 2*(D + delta).
func (c *Config) BroadcastMaxOffset() time.Duration {
	return c.TCommit - 2*(c.D+c.Delta)
}

// AcceptMaxOffset returns T_accept_max relative to slot_start — the latest
// first-observation time at which a Phase-1 bundle is accepted. Bundles
// first-observed after this point are rejected entirely; a downstream σ-emit
// on such a bundle would not propagate before peers' NR-decision, so
// accepting is operationally useless.
//
// Per spec §Setting: T_accept_max = T_commit + Delta2 - (D + delta).
func (c *Config) AcceptMaxOffset() time.Duration {
	return c.TCommit + c.Delta2 - (c.D + c.Delta)
}

// PhaseTwoEndOffset returns the end of Phase 2 — when Defer-state operators
// force-commit (σ if Defer-due-to-partition resolved, else NR) and emit their
// KindNR.
func (c *Config) PhaseTwoEndOffset() time.Duration {
	return c.TCommit + c.Delta2
}

// RoundEndOffset returns T_round_end — the cutoff for Phase-3 reconstruction.
// Per spec §Slot structure: T_round_end = T_commit + Delta2 + Delta3.
func (c *Config) RoundEndOffset() time.Duration {
	return c.TCommit + c.Delta2 + c.Delta3
}

// Validate checks the config for internal consistency. Bad configs are
// programmer errors; callers run this once at instance construction.
func (c *Config) Validate() error {
	if len(c.Layers) < 3 {
		return errors.New("obft: K must be >= 3 (late-leader resilience)")
	}
	if len(c.Layers) > len(c.Operators) {
		return errors.New("obft: K cannot exceed cluster size")
	}
	if c.F < 1 {
		return errors.New("obft: byzantine bound F must be >= 1")
	}
	if len(c.Operators) < 3*c.F+1 {
		return errors.New("obft: cluster size must be at least 3F+1")
	}
	if c.D <= 0 || c.Delta <= 0 {
		return errors.New("obft: D and Delta must be positive")
	}
	if c.TCommit <= 0 {
		return errors.New("obft: TCommit must be positive")
	}
	if c.Delta2 < c.D+c.Delta {
		return errors.New("obft: Delta2 must be >= D + Delta (BFT minimum)")
	}
	if c.Delta3 < c.D+c.Delta {
		return errors.New("obft: Delta3 must be >= D + Delta (NR-partial propagation)")
	}
	if c.BroadcastMaxOffset() < 0 {
		return errors.New("obft: TCommit too small for broadcast deadline (need TCommit > 2*(D+Delta))")
	}

	members := make(map[OperatorID]bool, len(c.Operators))
	for _, op := range c.Operators {
		if members[op] {
			return errors.New("obft: duplicate operator ID in cluster")
		}
		members[op] = true
	}

	seenLeaders := make(map[OperatorID]bool, len(c.Layers))
	broadcastMax := c.BroadcastMaxOffset()
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
		if layer.FetchAt > broadcastMax {
			return errors.New("obft: layer FetchAt exceeds broadcast deadline T_broadcast_max")
		}
		// Per spec §Setting: T_{K-1} <= ... <= T_1 <= T_0. Layer index k
		// increases as we go deeper; FetchAt must therefore not increase
		// with k.
		if k > 0 && layer.FetchAt > c.Layers[k-1].FetchAt {
			return errors.New("obft: layer fetch times must be non-increasing in k")
		}
	}
	return nil
}
