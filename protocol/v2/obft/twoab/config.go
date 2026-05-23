// Package twoab implements the 2abOBFT protocol — a single-round agreement
// protocol for SSV clusters with a split Phase 2 (Phase 2a coordination
// broadcast + Phase 2b dynamic σ-or-NR commit).
//
// The protocol is described in docs/2abOBFT.md. 2abOBFT extends bare OBFT
// (docs/OBFT.md) by inserting a Phase-2a broadcast between Phase 1 and
// Phase 2b. Each operator broadcasts a `KindValue` (has V_0 + host valid)
// or `KindNoValue` (otherwise) at the Phase-2a fire-instant
// `T_phase_2a = T_0_broadcast + 1·BTT`. `KindValue` is the σ-side terminal
// emission — it carries the emitter's σ partial inline — while `KindNoValue`
// is a non-binding NR-side coordination signal; so Phase 2b only fires for
// the NR side: each operator emits at most one `KindCommit` (NR / NR-direct)
// when the NR-eligibility trigger fires locally on the observed pool
// (NR-direct being a Phase-2a-time emission when the op observes leader
// equivocation at L_0). There is NO protocol-level Phase-2b deadline — the
// slot's relay-submission cutoff is the only hard wall (runner-level).
// relay-submission cutoff is the only hard wall (runner-level).
//
// Shared cryptography primitives (Signer, ThresholdIBE, NoQuorumTag, bare
// type aliases) live in the parent obft package; this package re-exports
// the type aliases for callers' convenience and owns all 2abOBFT-specific
// data structures, state machine, wire format, evidence types, and EKM
// coordinator. The bare-OBFT implementation lives in the parallel
// sub-package protocol/v2/obft/base. Intentional API/pattern divergences
// between the two — Phase 2a Value/NoValue split, peer-reflood-V witness
// harvest, Rule 6a, no T_commit, etc. — and the convergence work that
// aligned the rest are catalogued in
// [docs/OBFT-TWOAB-CONVERGENCE-PLAN.md].
//
// This package is intentionally independent of github.com/ssvlabs/ssv-spec.
// SSV-specific integration lives in a future runner adapter (analog of
// protocol/v2/ssv/runner/obft).
package twoab

import (
	"errors"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Type aliases for the super-generic primitives owned by the parent obft
// package, re-exported here so callers using twoab/ types don't need to
// import both packages. These are not new types — twoab.OperatorID and
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

	// Shared wire types owned by the parent obft package.
	Certificate  = obft.Certificate
	Output       = obft.Output
	Phase1Bundle = obft.Phase1Bundle
	// Shared cluster/layer-topology type.
	LayerSpec = obft.LayerSpec
	// Shared host-validation request type.
	ValidationRequest = obft.ValidationRequest
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

// ValueRoot returns the wire identifier (sha256) of V. See obft.ValueRoot.
func ValueRoot(v Value) [32]byte {
	return obft.ValueRoot(v)
}

// Config parameterizes one 2abOBFT consensus instance. Timing fields are
// absolute offsets relative to slot start; see docs/2abOBFT.md §Setting and
// §Timing budget for the constraints relating them.
//
// **Note on operator-facing API surface:** per the principle "operators
// supply BTT only; protocol timings derive deterministically from BTT",
// the protocol-timing fields (`TPhase2a`, `SafetyBuffer`) are NOT
// operator-tunable in production. 2abOBFT does not yet have an SSV-runner
// adapter; when it's built, the adapter should expose `BTT` plus
// deployment-environment values (`RelayCutoff`, `HeaderSubmitHeadroom`,
// `RefloodDelay` — used as the default for `SafetyBuffer`) and derive
// protocol timings internally. Until the runner-adapter is built, callers
// (currently consensustest only) construct this Config directly with
// explicit protocol timings.
type Config struct {
	// Height identifies this consensus instance (the slot, in SSV usage).
	Height Height

	// ClusterID uniquely identifies the cluster running this instance.
	// Used in NR-tag construction to prevent cross-cluster replay.
	ClusterID [32]byte

	// Operators is the full set of operators in the cluster.
	Operators []OperatorID

	// F is the byzantine bound. Cluster size must be ≥ 3F+1.
	F int

	// Layers is the priority-ordered list of K layers; K = len(Layers).
	// Layer 0 is the primary; deeper layers are progressively-fallback
	// backups. Each layer's leader must be a distinct cluster member.
	Layers []LayerSpec

	// TPhase2a is the Phase-2a fire-instant: the slot-relative offset at
	// which every operator emits exactly one of {KindValue, KindNoValue,
	// KindCommit-NRDirect} per their local state.
	//
	// Per spec §Setting: `TPhase2a = T_0_broadcast + 1·BTT`, where
	// T_0_broadcast is the primary leader's broadcast time. The 1·BTT
	// gap gives V_0 one propagation cycle to reach honest peers before
	// Phase 2a fires.
	TPhase2a time.Duration

	// SafetyBuffer is the protocol-level mesh-tail tolerance configurable.
	// SafetyBuffer widens the post-TPhase2a σ-pool fill window — the
	// wall-clock between TPhase2a and the scheduled Resolve sweep, during
	// which peer KindValues propagate and σ-pool[V_0] reaches qV. The
	// σ-side critical path is 1 hop (KindValue carries the σ partial
	// directly); the NR fall-through path is 2 hops (KindNoValue →
	// KindCommit-NR → aggregate). A slot resolves σ-ward XOR NR-ward
	// (mutually exclusive), so the window is the MAX of the two paths,
	// not their sum:
	//
	//	resolveWindow = max(1·BTT + SafetyBuffer, 2·BTT) + ε_3
	//	              = 1·BTT + max(SafetyBuffer, 1·BTT) + ε_3
	//
	// The runner / adapter shifts TPhase2a earlier by SafetyBuffer
	// (relative to the runner-level RelayCutoff) so the cluster has
	// SafetyBuffer extra wall-clock to absorb slow / jittery peer hops
	// (e.g., σ-pool fill via gossipsub IHAVE/IWANT recovery when initial
	// KindValue eager-push is incomplete). Each fetchAt[k] correspondingly
	// shifts earlier by SafetyBuffer (since t0Broadcast = TPhase2a − BTT
	// and fetchAt[k] = t0Broadcast − B_k), preserving the leader's
	// structural broadcast budget at the minimum `B_k = (k+2)·BTT` while
	// keeping the wall-clock pre-broadcast headroom unchanged at default
	// SafetyBuffer.
	//
	// Default sizing: `SafetyBuffer = RefloodDelay` (the cluster's
	// gossipsub HeartbeatInterval — typically 700ms in SSV deployments).
	// At this default, 2abOBFT and bare OBFT have the same total
	// post-broadcast structural budget and the same MEV-fetch headroom.
	//
	// Lower SafetyBuffer (e.g. 300ms / 500ms) reclaims MEV-fetch headroom
	// at the cost of σ-pool-fill tolerance: the cluster commits to
	// slot-miss rather than wait for late peer KindValue arrivals when
	// the network's per-hop latency tail exceeds 1·BTT. Higher
	// SafetyBuffer (e.g. 1·BTT + RefloodDelay) widens the tolerance at
	// the cost of MEV-fetch headroom. SafetyBuffer is decoupled from the
	// network's HeartbeatInterval (the gossipsub constant); SafetyBuffer
	// is a protocol-level configurable, not a network parameter.
	//
	// NOT consumed by Instance internally — only by Validate() for the
	// sign-check (SafetyBuffer >= 0). The active consumer is the
	// runner/adapter layer (see `protocol/v2/consensustest/twoab/des.go`
	// for the test-adapter consumer; the planned production runner
	// adapter will read SafetyBuffer the same way) which sizes the
	// cascade window externally. The field lives on twoab.Config to
	// keep the protocol-level configurable in one place and to
	// forward-compat with adapter codepaths that wire timing fields
	// through `twoab.Config` rather than a parallel runner-config.
	SafetyBuffer time.Duration

	// BTT is Block-Trip-Time, the unit propagation+skew budget. Per spec
	// §Setting: BTT = P99 + δ. Used as the unit for time-budget formulas.
	// Concrete sizing at Config A: P99 = 150ms, δ = 50ms, BTT = 200ms.
	BTT time.Duration

	// BFTStart is BFT_start — the slot-relative offset at which the
	// protocol's primary broadcast pipeline begins. Pre-fetch and
	// pre-consensus sit in `[slot_start, BFTStart]`. Default 0. When
	// BFTStart > T_0_broadcast − B_k for some layer k, the spec's
	// runtime clamp `T_broadcast_max_k = max(BFTStart, T_0_broadcast − B_k)`
	// floors that layer's broadcast deadline at BFTStart and the
	// schedule degrades but stays valid.
	BFTStart time.Duration
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

// T0Broadcast returns the primary leader's broadcast time anchor: the
// offset by which V_0 must enter the gossipsub mesh so it has 1·BTT to
// propagate before Phase 2a fires. `T0Broadcast = TPhase2a − BTT`.
func (c *Config) T0Broadcast() time.Duration {
	return c.TPhase2a - c.BTT
}

// BroadcastMaxOffsetForLayer returns `T_broadcast_max_k =
// max(BFTStart, T_0_broadcast − B_k)` for layer k — the leader's target
// Phase-1 broadcast time per spec §Setting.
//
// B_k is a target, not a hard cap; the BFTStart floor (default 0) handles
// the degraded case where the layer's design-time budget overshoots
// T_0_broadcast. In that case the leader broadcasts at BFTStart
// best-effort, and the layer's effective absorption window contracts
// accordingly.
func (c *Config) BroadcastMaxOffsetForLayer(k int) time.Duration {
	if d := c.T0Broadcast() - c.Layers[k].BroadcastBudget; d > c.BFTStart {
		return d
	}
	return c.BFTStart
}

// DefaultBroadcastBudget returns a spec-conforming staggered B_k schedule
// for K layers at the given BTT and T0Broadcast. Per spec §Setting,
// B_k is sized at the structural minimum for one Phase-1 propagation
// cycle per layer:
//
//	B_k_shallow = (k+2)·BTT     for k ∈ [0, K-2]
//	B_{K-1}     = T0Broadcast   (deepest broadcasts at BFT_start)
//
// Mesh-tail / IHAVE-IWANT-recovery slack lives in the SafetyBuffer
// configurable (on Config.SafetyBuffer), which structurally shifts
// `TPhase2a` earlier to widen the post-Phase-2a cascade window —
// `B_k` itself stays at the structural minimum. See Config.SafetyBuffer
// for the cascade-window-vs-B_0 rationale on why the spec post-tighten
// puts SafetyBuffer in the cascade rather than in B_k.
//
// At K=4 returns [2·BTT, 3·BTT, 4·BTT, T0Broadcast]. At K=3 returns
// [2·BTT, 3·BTT, T0Broadcast]. At K=2 returns [2·BTT, T0Broadcast].
// For K>4 the first three layers stay at 2 / 3 / 4 BTT and the
// intermediate layers (k = 3, ..., K-2) interpolate linearly in
// duration space from 4·BTT (at L_2) to T0Broadcast (at L_{K-1}).
//
// At extreme degraded operating points where T0Broadcast shrinks below
// the canonical shallow multiples (e.g. T0Broadcast ≤ 4·BTT at K≥4),
// the helper still returns a schedule — the shallow B_k values can
// exceed T0Broadcast. The protocol's runtime `T_broadcast_max_k =
// max(BFT_start, T0Broadcast − B_k)` clamps those layers' targets at
// BFT_start, so the configuration remains valid (the fall-through
// depth shrinks but the cluster still operates). Callers that want
// the canonical staggered shape preserved can widen T0Broadcast
// (loosen the post-Phase-2a budget / header headroom) or supply their
// own per-layer schedule.
func DefaultBroadcastBudget(K int, btt, t0Broadcast time.Duration) ([]time.Duration, error) {
	if K < 1 {
		return nil, fmt.Errorf("twoab: DefaultBroadcastBudget K=%d must be ≥ 1", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("twoab: DefaultBroadcastBudget BTT=%v must be > 0", btt)
	}
	// shallow returns (k+2)*BTT for shallow layer k.
	shallow := func(k int) time.Duration {
		return time.Duration(k+2) * btt
	}
	out := make([]time.Duration, K)
	switch K {
	case 1:
		out[0] = t0Broadcast
	case 2:
		out[0] = shallow(0)
		out[1] = t0Broadcast
	case 3:
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = t0Broadcast
	case 4:
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = shallow(2)
		out[3] = t0Broadcast
	default:
		// First three layers at 2·BTT / 3·BTT / 4·BTT; intermediate layers
		// interpolate linearly in duration space from 4·BTT (at L_2) to
		// T0Broadcast (at L_{K-1}). Per Config.SafetyBuffer's contract,
		// SafetyBuffer is NOT folded into B_k — it widens the post-TPhase2a
		// cascade window via the resolveWindow formula, not the per-layer
		// Phase-1 broadcast budgets.
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = shallow(2)
		out[K-1] = t0Broadcast
		span := t0Broadcast - out[2]
		steps := K - 3
		for k := 3; k < K-1; k++ {
			out[k] = out[2] + span*time.Duration(k-2)/time.Duration(steps)
		}
	}
	// Cap each B_k at T0Broadcast so the schedule stays non-decreasing
	// even at degraded operating points where the canonical staggered
	// shallow multiples overshoot.
	// Capped layers share `T_broadcast_max_k = max(BFT_start,
	// T0Broadcast − B_k) = BFT_start` — multiple layers may collide at
	// BFT_start without safety impact. The deepest layer is already
	// T0Broadcast by construction; the cap turns degraded shallow layers
	// into "broadcast at BFT_start" peers of the deepest.
	for k := 0; k < K; k++ {
		if out[k] > t0Broadcast {
			out[k] = t0Broadcast
		}
	}
	return out, nil
}

// Validate checks the config for internal consistency. Bad configs are
// programmer errors; callers run this once at instance construction.
func (c *Config) Validate() error {
	if err := obft.ValidateClusterTopology(c.Operators, c.F, c.Layers, c.BTT); err != nil {
		return err
	}
	if c.TPhase2a <= 0 {
		return errors.New("twoab: TPhase2a must be positive")
	}
	if c.SafetyBuffer < 0 {
		return errors.New("twoab: SafetyBuffer must be >= 0")
	}
	// TPhase2a must accommodate T_0_broadcast = TPhase2a − BTT being positive.
	if c.TPhase2a <= c.BTT {
		return errors.New("twoab: TPhase2a must be > BTT so T0Broadcast is positive")
	}
	// Anchor-specific: FetchAt must land within each layer's broadcast
	// deadline max(BFTStart, T0Broadcast − B_k).
	for k, layer := range c.Layers {
		if layer.FetchAt > c.BroadcastMaxOffsetForLayer(k) {
			return fmt.Errorf("twoab: layer %d FetchAt %v exceeds broadcast deadline %v (max(BFTStart=%v, T0Broadcast−B_k=%v))",
				k, layer.FetchAt, c.BroadcastMaxOffsetForLayer(k),
				c.BFTStart, c.T0Broadcast()-c.Layers[k].BroadcastBudget)
		}
	}
	return nil
}
