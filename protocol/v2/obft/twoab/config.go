// Package twoab implements the 2abOBFT protocol — a single-round agreement
// protocol for SSV clusters with a two-window Phase-2 split (Phase 2a verdict
// broadcast + Phase 2b σ-or-NR commit).
//
// The protocol is described in docs/2abOBFT.md. 2abOBFT extends bare OBFT
// (docs/OBFT.md) by inserting a verdict-broadcast phase between Phase 1
// and Phase 2, enabling cluster-wide convergence on σ-eligibility before
// any operator cryptographically commits at Phase 2b.
//
// Shared cryptography primitives (Signer, ThresholdIBE, NoQuorumTag, bare
// type aliases) live in the parent obft package; this package re-exports
// the type aliases for callers' convenience and owns all 2ab-specific
// data structures, state machine, wire format, evidence types, and EKM
// coordinator. The bare-OBFT implementation lives in the parallel
// sub-package protocol/v2/obft/base.
//
// This package is intentionally independent of github.com/ssvlabs/ssv-spec.
// SSV-specific integration lives in protocol/v2/ssv/runner/obft/twoab (once
// Phase L of the impl plan lands).
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

// LayerSpec describes one layer of the K-layer onion structure. Per spec
// §Setting, fetch times are non-increasing in layer index (deeper layers
// fetch earlier from deeper-confirmed parents), and per-layer broadcast
// budgets are staggered so deeper layers absorb more propagation tail.
//
// In 2abOBFT the broadcast deadline is anchored on T_verdict_start (the
// Phase-1 cutoff / Phase-2a start), not T_commit — under the spec's
// aligned T_commit semantics (= σ-or-NR commit point), T_commit is Δ_2a
// later than the Phase-1 cutoff.
type LayerSpec struct {
	Leader  OperatorID
	FetchAt time.Duration

	// BroadcastBudget is the layer's T_verdict_start-anchored absorption
	// target `B_k` per spec §Setting: the leader aims to broadcast their
	// Phase-1 bundle by `T_broadcast_max_k = max(0, T_verdict_start − B_k)`.
	// Per spec, `B_0 < B_1 < ... < B_{K-1}` — deeper layers get larger
	// budgets (wider absorption); the primary gets the smallest (max
	// MEV-fetch headroom, willing to fall through to L_1+ if propagation
	// slips).
	//
	// B_k is a target, not a hard runtime cap. The only runtime acceptance
	// gate is `T_accept_max = T_commit − 1 BTT` at receivers; bundles
	// first-observed past that are auth-only-retained.
	//
	// Required: must be > 0 on every layer. Config.Validate rejects
	// zero/negative values and non-strict-increasing schedules. Use
	// DefaultBroadcastBudget(K, BTT, T_verdict_start) for the spec-
	// recommended staggered schedule when constructing a Config manually.
	BroadcastBudget time.Duration
}

// Config parameterizes one 2abOBFT consensus instance. Timing fields are
// absolute offsets relative to slot start; see docs/2abOBFT.md §Setting and
// §Timing budget for the constraints relating them.
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

	// TCommit is T_commit — the σ-or-NR commit point (start of Phase 2b).
	// Aligned semantically with bare OBFT and QBFT T_commit: this is the
	// "point of no return" where each operator cryptographically binds
	// their per-layer choice. Phase 2a runs in [TCommit−Delta2a, TCommit].
	TCommit time.Duration

	// Delta2a is Δ_2a — the Phase 2a (verdict broadcast) window length.
	// Per spec §Setting, Δ_2a ≥ 2 BTT is the minimum coherent sizing
	// (Δ_2a = 1 BTT is broken-by-construction with the late-broadcast
	// schedule). Recommended for production: Δ_2a = 2 BTT.
	Delta2a time.Duration

	// Delta2b is Δ_2b — the Phase 2b (σ-or-NR commit) window length.
	// Per spec, Δ_2b ≥ 1 BTT is the propagation budget for Phase-2b σ/NR
	// partials to reach all honest peers before Phase 3. Recommended: 2 BTT.
	Delta2b time.Duration

	// Delta3 is Δ_3 — the Phase 3 reconstruction window length.
	// Per spec, Δ_3 ≥ 1 BTT + ε_3 covers (a) end-of-Phase-2b partial
	// propagation and (b) local reconstruction processing (BLS aggregation,
	// IBE decryption walk, certificate construction). ε_3 ≈ 50ms at Config A.
	Delta3 time.Duration

	// BTT is Block-Trip-Time, the unit propagation+skew budget. Per spec
	// §Setting: BTT = P99 + δ. Used as the unit for time-budget formulas.
	// Concrete sizing at Config A: P99 = 150ms, δ = 50ms, BTT = 200ms.
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

// TVerdictStart returns the start of Phase 2a (= end of Phase 1, also
// known as the Phase-1 broadcast cutoff) — `TCommit − Delta2a`.
func (c *Config) TVerdictStart() time.Duration {
	return c.TCommit - c.Delta2a
}

// TAcceptMax returns the receiver acceptance horizon — `TCommit − 1 BTT`
// per spec §Setting. Phase-1 bundles first-observed in
// `[slot_start, TAcceptMax]` are verdict-eligible; later ones are
// auth-only-retained.
func (c *Config) TAcceptMax() time.Duration {
	return c.TCommit - c.BTT
}

// TVerdictMax returns the verdict broadcast horizon — coincident with
// TAcceptMax by construction (`TCommit − 1 BTT`). Operators must emit
// their Phase-2a verdict envelope by this time so it propagates to all
// honest peers before Phase-2a end.
func (c *Config) TVerdictMax() time.Duration {
	return c.TCommit - c.BTT
}

// BroadcastMaxOffsetForLayer returns `T_broadcast_max_k =
// max(0, TVerdictStart − B_k)` for layer k — the leader's target Phase-1
// broadcast time per spec §Setting.
//
// B_k is a target, not a hard cap; the clamp at 0 handles the degraded
// case where the layer's design-time budget overshoots TVerdictStart. In
// that case the leader broadcasts best-effort from slot start.
func (c *Config) BroadcastMaxOffsetForLayer(k int) time.Duration {
	if d := c.TVerdictStart() - c.Layers[k].BroadcastBudget; d > 0 {
		return d
	}
	return 0
}

// Phase2aStartOffset returns the start of Phase 2a = TVerdictStart.
// Bundles first-observed past TAcceptMax at any honest receiver enter
// auth-only retention.
func (c *Config) Phase2aStartOffset() time.Duration {
	return c.TVerdictStart()
}

// Phase2aEndOffset returns the end of Phase 2a (= TCommit). At this
// moment each operator computes its convergence decision per layer
// from the observed Phase-2a verdict pool.
func (c *Config) Phase2aEndOffset() time.Duration {
	return c.TCommit
}

// Phase2bStartOffset returns the start of Phase 2b (= TCommit). Each
// operator emits its σ-or-NR partials per layer.
func (c *Config) Phase2bStartOffset() time.Duration {
	return c.TCommit
}

// Phase2bEndOffset returns the end of Phase 2b — `TCommit + Delta2b`.
// After this, Phase 3 reconstruction begins.
func (c *Config) Phase2bEndOffset() time.Duration {
	return c.TCommit + c.Delta2b
}

// Phase3StartOffset returns the start of Phase 3 (= Phase2bEndOffset).
func (c *Config) Phase3StartOffset() time.Duration {
	return c.TCommit + c.Delta2b
}

// RoundEndOffset returns TCommit + Delta2b + Delta3 — the SOFT per-operator
// target by which the local Phase-3 reconstruction walk is expected to
// complete under nominal partial synchrony. Per spec §Phase 3, this is
// NOT a hard deadline:
//
//   - Phase 3 starts at TCommit + Delta2b (= Phase3StartOffset) and runs
//     until σ-quorum reaches OR the slot's relay-submission deadline
//     forces termination.
//   - Reconstruction overrunning Delta3 can spill into the submission
//     slack; a faster peer's KindCertificate gossip can let an operator
//     that hasn't completed local reconstruction submit (V, S) directly.
//   - Late KindOnion2b arrivals past Phase2bEndOffset can be incorporated
//     by re-running the reconstruction walk; Pigeonhole semantics still
//     hold (at most one V can reconstruct cluster-wide regardless of
//     timing).
//
// The hard wall for the slot is the relay-submission deadline (typically
// T_relay_cutoff − T_submit), enforced at the runner level via context
// cancellation, not here.
func (c *Config) RoundEndOffset() time.Duration {
	return c.TCommit + c.Delta2b + c.Delta3
}

// ErrInsufficientVerdictStart is returned by DefaultBroadcastBudget when
// TVerdictStart is too small for the spec's staggered default schedule to
// fit (TVerdictStart ≤ 2.5·BTT for K≥3, with smaller bounds at K<3). At
// this operating point the schedule's strict-increasing invariant cannot
// be satisfied: the shallow layer B_{K-2} = 2.5·BTT already exceeds the
// deepest's "earliest possible" anchor.
//
// Callers that hit this can either supply a custom per-layer schedule via
// LayerSpec.BroadcastBudget, or treat the operating point as out of
// envelope for 2abOBFT (the consensustest adapter wraps as
// consensustest.ErrNotApplicable so cells render cleanly as "n/a"
// rather than logging a verbose error per scenario).
var ErrInsufficientVerdictStart = errors.New("twoab: TVerdictStart too small for default staggered schedule")

// DefaultBroadcastBudget returns a spec-conforming staggered B_k schedule
// for K layers at the given BTT and TVerdictStart. Matches the spec's
// recommendation: B_0 = 1 BTT (max MEV freshness at primary), B_{K-1} =
// TVerdictStart (deepest broadcasts at slot start, last-resort absorption
// ≈ entire pre-Phase-2a budget); intermediate shallow layers at 1.5 /
// 2.5 BTT.
//
// At K=4 (the 2abOBFT proposer-duty default) returns [1·BTT, 1.5·BTT,
// 2.5·BTT, TVerdictStart]. At K=3 returns [1·BTT, 2.5·BTT, TVerdictStart].
// For K>4 the first three layers stay at 1 / 1.5 / 2.5 BTT and the
// intermediate layers (k = 3, ..., K-2) interpolate linearly from 2.5·BTT
// to TVerdictStart in duration space.
//
// Returns an error wrapping ErrInsufficientVerdictStart when
// TVerdictStart ≤ 2.5·BTT (the default deepest would no longer be strictly
// greater than B_{K-2} = 2.5·BTT). Callers operating at extreme degraded
// BTT can provide a custom per-layer schedule.
func DefaultBroadcastBudget(K int, btt, tVerdictStart time.Duration) ([]time.Duration, error) {
	if K < 1 {
		return nil, fmt.Errorf("twoab: DefaultBroadcastBudget K=%d must be ≥ 1", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("twoab: DefaultBroadcastBudget BTT=%v must be > 0", btt)
	}
	var minDeepest time.Duration
	switch {
	case K == 1:
		minDeepest = 2 * btt
	case K == 2:
		minDeepest = btt
	default:
		minDeepest = btt * 250 / 100
	}
	if tVerdictStart <= minDeepest {
		return nil, fmt.Errorf("%w: TVerdictStart=%v must be > %v (B_{K-2} for K=%d at BTT=%v); supply a custom per-layer schedule for this operating point",
			ErrInsufficientVerdictStart, tVerdictStart, minDeepest, K, btt)
	}
	out := make([]time.Duration, K)
	switch K {
	case 1:
		out[0] = tVerdictStart
	case 2:
		out[0] = btt
		out[1] = tVerdictStart
	case 3:
		out[0] = btt
		out[1] = btt * 250 / 100
		out[2] = tVerdictStart
	case 4:
		out[0] = btt
		out[1] = btt * 150 / 100
		out[2] = btt * 250 / 100
		out[3] = tVerdictStart
	default:
		out[0] = btt
		out[1] = btt * 150 / 100
		out[2] = btt * 250 / 100
		out[K-1] = tVerdictStart
		span := tVerdictStart - out[2]
		steps := K - 3
		for k := 3; k < K-1; k++ {
			out[k] = out[2] + span*time.Duration(k-2)/time.Duration(steps)
		}
	}
	return out, nil
}

// Validate checks the config for internal consistency. Bad configs are
// programmer errors; callers run this once at instance construction.
func (c *Config) Validate() error {
	if c.F < 1 {
		return errors.New("twoab: byzantine bound F must be >= 1")
	}
	if len(c.Operators) < 3*c.F+1 {
		return errors.New("twoab: cluster size must be at least 3F+1")
	}
	// Per spec §Setting: K ≥ f+1 is the BFT-liveness minimum (pigeonhole
	// over the f-byz bound guarantees ≥ 1 honest leader). K ≥ f+2
	// additionally provides late-leader-resilience (≥ 2 honest leaders);
	// that choice is left to the operator/deployment per spec §Setting
	// and is not enforced here.
	minK := c.F + 1
	if len(c.Layers) < minK {
		return fmt.Errorf("twoab: K=%d below BFT-liveness minimum %d (= f+1 at f=%d)",
			len(c.Layers), minK, c.F)
	}
	if len(c.Layers) > len(c.Operators) {
		return errors.New("twoab: K cannot exceed cluster size")
	}
	if c.BTT <= 0 {
		return errors.New("twoab: BTT must be positive")
	}
	if c.TCommit <= 0 {
		return errors.New("twoab: TCommit must be positive")
	}
	// Per spec §Setting: Δ_2a ≥ 2 BTT is the minimum coherent sizing
	// (Δ_2a = 1 BTT is broken-by-construction with the late-broadcast
	// schedule — verdict broadcast at TVerdictMax − ε_proc would fall
	// before Phase 2a begins).
	if c.Delta2a < 2*c.BTT {
		return errors.New("twoab: Delta2a must be >= 2 BTT (minimum coherent sizing per spec §Setting)")
	}
	if c.Delta2b < c.BTT {
		return errors.New("twoab: Delta2b must be >= 1 BTT (BFT minimum per spec §Setting)")
	}
	if c.Delta3 <= 0 {
		return errors.New("twoab: Delta3 must be positive")
	}
	// TCommit must accommodate the Phase-1 broadcast budget (TVerdictStart
	// = TCommit − Delta2a > 0) plus the deepest-layer broadcast cushion.
	if c.TCommit <= c.Delta2a {
		return errors.New("twoab: TCommit must be > Delta2a so TVerdictStart is positive")
	}

	members := make(map[OperatorID]bool, len(c.Operators))
	for _, op := range c.Operators {
		if members[op] {
			return errors.New("twoab: duplicate operator ID in cluster")
		}
		members[op] = true
	}

	// Per-layer BroadcastBudget — required on every layer per spec §Setting.
	for k, l := range c.Layers {
		if l.BroadcastBudget <= 0 {
			return fmt.Errorf("twoab: layer %d BroadcastBudget must be > 0 (use DefaultBroadcastBudget for a spec-conforming staggered schedule)", k)
		}
	}
	// B_0 < B_1 < ... < B_{K-1}: deeper layers get strictly larger
	// absorption / chain-decryption headroom (spec §Setting).
	for k := 1; k < len(c.Layers); k++ {
		if c.Layers[k].BroadcastBudget <= c.Layers[k-1].BroadcastBudget {
			return errors.New("twoab: BroadcastBudget must be strictly increasing in layer index (B_0 < B_1 < ...)")
		}
	}
	// The deepest layer's budget is the cluster's worst-case liveness
	// guarantee — must satisfy the 2*BTT BFT-min bound.
	bftMin := 2 * c.BTT
	K := len(c.Layers)
	if c.Layers[K-1].BroadcastBudget < bftMin {
		return fmt.Errorf("twoab: deepest layer L_%d BroadcastBudget %v below BFT-min %v: cluster has no liveness guarantee",
			K-1, c.Layers[K-1].BroadcastBudget, bftMin)
	}

	seenLeaders := make(map[OperatorID]bool, len(c.Layers))
	for k, layer := range c.Layers {
		if !members[layer.Leader] {
			return errors.New("twoab: layer leader is not a cluster member")
		}
		if seenLeaders[layer.Leader] {
			return errors.New("twoab: duplicate leader across layers")
		}
		seenLeaders[layer.Leader] = true
		if layer.FetchAt < 0 {
			return errors.New("twoab: layer FetchAt must be non-negative")
		}
		if layer.FetchAt > c.BroadcastMaxOffsetForLayer(k) {
			return fmt.Errorf("twoab: layer %d FetchAt %v exceeds broadcast deadline %v (TVerdictStart−B_k = %v)",
				k, layer.FetchAt, c.BroadcastMaxOffsetForLayer(k), c.TVerdictStart()-c.Layers[k].BroadcastBudget)
		}
		// Per spec §Setting: T_{K-1} < ... < T_1 < T_0. Layer index k
		// increases as we go deeper; FetchAt must strictly decrease
		// with k so backups fetch from progressively-deeper-confirmed
		// parents.
		if k > 0 && layer.FetchAt >= c.Layers[k-1].FetchAt {
			return errors.New("twoab: layer fetch times must be strictly decreasing in k")
		}
	}
	return nil
}
