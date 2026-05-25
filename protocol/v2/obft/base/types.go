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
// Sibling package `protocol/v2/obft/twoab` implements 2abOBFT. The two
// packages share lifecycle shape, wire-format conventions, and the
// obft primitives from the parent package. Intentional API/pattern
// divergences (and the convergence work that aligned the rest) are
// catalogued in [docs/OBFT-TWOAB-CONVERGENCE-PLAN.md].
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
	// primary-vs-backup reflood-aware schedule (`B_0 = 2·BTT + RefloodDelay`
	// for the MEV-fresh primary; `B_1..B_{K-1} = T_commit` for all backups
	// broadcasting at BFT_start), so Delta2 no longer carries a reflood
	// cushion. Sub-1·BTT sizings are sub-BFT (Phase-2 propagation can't
	// complete within the budget).
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
	// bound. Used as the unit for time-budget formulas (e.g. Delta2 = 1 BTT
	// recommended at tightened sizing; B_0 = 2·BTT + RefloodDelay primary,
	// B_1..B_{K-1} = T_commit backups). Concrete sizing at Config A:
	// P99 = 150ms, δ = 50ms, BTT = 200ms.
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
// `B_k` is a target, not a hard cap; the slot-start floor (0) handles the
// degraded case where the layer's design-time budget overshoots `T_commit`.
// In that case the leader broadcasts at slot start best-effort, and the
// layer's effective absorption window contracts to `T_commit`. The only
// runtime acceptance gate remains T_commit at receivers.
func (c *Config) BroadcastMaxOffsetForLayer(k int) time.Duration {
	if d := c.TCommit - c.Layers[k].BroadcastBudget; d > 0 {
		return d
	}
	return 0
}

// BroadcastTargetOffset returns the recovery-floored broadcast target for a
// layer: `max(0, T_commit − max(B_k, 3·BTT))`. The `3·BTT` floor (a 2·BTT
// peer-reflood-V recovery cascade + a 1·BTT margin) guarantees the h_V=1
// σ-upgrade lands before the T_commit view-fix even when `B_k < 3·BTT` (i.e.
// RefloodDelay < 1·BTT), mirroring 2abOBFT's `max(SafetyBuffer, 1·BTT)`
// resolve-window floor. The floor is dormant when `B_k ≥ 3·BTT` (the common
// case at default RefloodDelay), where it coincides with the plain
// `T_commit − B_k`.
//
// This is the offset leaders should actually broadcast by; it is the single
// source shared by the production runner's fetch/broadcast deadline and the
// consensustest adapter's FetchAt. `BroadcastMaxOffsetForLayer` is the looser
// `B_k`-budget deadline (the spec's `T_broadcast_max_k`), retained for FetchAt
// validation and diagnostics — the actual broadcast may land earlier by the
// floor.
func BroadcastTargetOffset(tCommit, broadcastBudget, btt time.Duration) time.Duration {
	if floor := 3 * btt; broadcastBudget < floor {
		broadcastBudget = floor
	}
	if d := tCommit - broadcastBudget; d > 0 {
		return d
	}
	return 0
}

// BroadcastTargetForLayer returns BroadcastTargetOffset for layer k.
func (c *Config) BroadcastTargetForLayer(k int) time.Duration {
	return BroadcastTargetOffset(c.TCommit, c.Layers[k].BroadcastBudget, c.BTT)
}

// DefaultBroadcastBudget returns a spec-conforming B_k schedule for K layers
// at the given BTT, RefloodDelay, and T_commit. Per spec §Setting, the
// primary L_0's B_0 is sized to accommodate one gossipsub IHAVE/IWANT reflood
// cycle when initial eager-push fails to reach all honest peers; backups
// L_1..L_{K-1} all broadcast at BFT_start (B_k = T_commit) with the
// deepest-confirmed-parent fetch strategy:
//
//	B_0           = 2·BTT + RefloodDelay  (primary, MEV-fresh)
//	B_1..B_{K-1}  = T_commit              (backups broadcast at BFT_start)
//
// `RefloodDelay` is the worst-case gossipsub-lazy-push latency before a
// retransmission cycle completes — bounded by the cluster's HeartbeatInterval.
// At RefloodDelay = 0 the primary budget collapses to 2·BTT (the "fully-
// meshed cluster, eager push reliable" assumption); production SSV
// deployments use RefloodDelay = 700ms (SSV's gossipsub HeartbeatInterval).
//
// At K=4 returns [2·BTT+RefloodDelay, T_commit, T_commit, T_commit]. At K=3
// returns [2·BTT+RefloodDelay, T_commit, T_commit]. At K=2 returns
// [2·BTT+RefloodDelay, T_commit]. At K=1 returns [T_commit] (degenerate
// single-layer case — L_0 IS the deepest).
//
// At extreme degraded operating points where T_commit shrinks below
// 2·BTT + RefloodDelay, B_0 can exceed T_commit. The protocol's runtime
// `T_broadcast_max_k = max(0, T_commit − B_k)` clamps the primary's
// target at slot start, so the configuration remains valid (the primary
// becomes a redundant peer of the backups; cluster still operates).
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
	out := make([]time.Duration, K)
	if K == 1 {
		out[0] = tCommit
		return out, nil
	}
	// L_0: primary with reflood-aware budget. Cap at T_commit (degraded case).
	b0 := 2*btt + refloodDelay
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
	if err := obft.ValidateClusterTopology(c.Operators, c.F, c.Layers, c.BTT); err != nil {
		return err
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
	// Anchor-specific: FetchAt must land within each layer's broadcast
	// deadline max(0, T_commit − B_k).
	for k, layer := range c.Layers {
		if layer.FetchAt > c.BroadcastMaxOffsetForLayer(k) {
			return fmt.Errorf("obft: layer %d FetchAt %v exceeds broadcast deadline %v (max(0, T_commit−B_k=%v))",
				k, layer.FetchAt, c.BroadcastMaxOffsetForLayer(k),
				c.TCommit-c.Layers[k].BroadcastBudget)
		}
	}
	return nil
}
