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
	// zero/negative values and decreasing-in-k schedules (equal adjacent
	// budgets are accepted — multiple layers may share the BFT_start
	// clamp at degraded operating points; see spec §Setting). Use
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
	// Per spec, `Delta2b ≥ 1 BTT + ε_proc` is the propagation budget for
	// Phase-2b σ/NR partials (emitted after ε_proc convergence computation)
	// to reach all honest peers before Phase 3. **Recommended sizing:
	// `Delta2b = 1·BTT + ε_proc` (= 250ms at Config A with ε_proc ≈ 50ms)**
	// — the minimum coherent value. Reflood absorption is structurally
	// provided by per-layer `B_k` via the reflood-aware schedule, so
	// Delta2b no longer carries a reflood cushion.
	Delta2b time.Duration

	// Eps3 is ε_3 — the Phase 3 reconstruction window length.
	// Per spec, Eps3 covers local reconstruction processing (BLS
	// aggregation, IBE decryption walk, certificate construction).
	// Phase-2b emission propagation is already covered by Delta2b, so
	// Eps3 is purely local-CPU. Absolute (does not scale with BTT);
	// ε_3 ≈ 50ms at Config A.
	Eps3 time.Duration

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

// Phase2bEndOffset returns the end of the Phase-2b propagation budget —
// `TCommit + Delta2b`. By this offset, all honest peers' σ/NR partials are
// expected to be observable cluster-wide under nominal partial synchrony.
//
// This is the SOFT target for "Phase-2b inputs are complete enough for
// σ-quorum to form", not a hard gate on Phase-3 Resolve. Resolve is
// idempotent (re-running on incomplete state returns ErrNoQuorum without
// mutation), so the canonical implementation is observer-mode: Resolve is
// invoked opportunistically from TCommit onward on every KindOnion2b /
// KindCertificate arrival, and the average healthy slot decides well
// before this offset.
func (c *Config) Phase2bEndOffset() time.Duration {
	return c.TCommit + c.Delta2b
}

// Phase3StartOffset returns the SOFT target by which Phase-3 reconstruction
// is expected to *complete the propagation-budget portion* — coincident
// with Phase2bEndOffset. Operators MAY (and the production runner DOES)
// invoke Resolve opportunistically from TCommit onward; this offset marks
// the moment by which σ-quorum should form under partial synchrony, not a
// gate on attempting reconstruction.
func (c *Config) Phase3StartOffset() time.Duration {
	return c.TCommit + c.Delta2b
}

// RoundEndOffset returns TCommit + Delta2b + Eps3 — the SOFT per-operator
// target by which the local Phase-3 reconstruction walk is expected to
// complete under nominal partial synchrony. Per spec §Phase 3, this is
// NOT a hard deadline:
//
//   - Phase 3 may be attempted opportunistically from TCommit onward
//     (Resolve is idempotent and returns ErrNoQuorum cleanly on incomplete
//     state). Phase2bEndOffset is the SOFT propagation target, not a
//     Resolve-gating wall. The canonical implementation observes inbound
//     KindOnion2b / KindCertificate arrivals and calls Resolve on each.
//   - Reconstruction overrunning Eps3 can spill into the submission
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
	return c.TCommit + c.Delta2b + c.Eps3
}

// DefaultBroadcastBudget returns a spec-conforming staggered B_k schedule
// for K layers at the given BTT, RefloodDelay, and TVerdictStart. Per spec
// §Setting, B_k is sized to accommodate one gossipsub IHAVE/IWANT reflood
// cycle when initial eager-push fails to reach all honest peers:
//
//	B_k_shallow = (k+2)·BTT + RefloodDelay  for k ∈ [0, K-2]
//	B_{K-1}     = TVerdictStart             (deepest broadcasts at BFT_start)
//
// `RefloodDelay` is the worst-case gossipsub-lazy-push latency before a
// retransmission cycle completes — bounded by the cluster's HeartbeatInterval.
// At RefloodDelay = 0 the schedule collapses to {2, 3, 4}·BTT (the "fully-
// meshed cluster, eager push reliable" assumption); production SSV
// deployments use RefloodDelay = 700ms (SSV's gossipsub HeartbeatInterval).
//
// At K=4 returns [2·BTT+RefloodDelay, 3·BTT+RefloodDelay, 4·BTT+RefloodDelay, TVerdictStart]. At K=3
// returns [2·BTT+RefloodDelay, 3·BTT+RefloodDelay, TVerdictStart]. At K=2 returns
// [2·BTT+RefloodDelay, TVerdictStart]. For K>4 the first three layers stay at
// 2 / 3 / 4 BTT + RefloodDelay and the intermediate layers (k = 3, ..., K-2)
// interpolate linearly in duration space from 4·BTT + RefloodDelay (at L_2) to
// TVerdictStart (at L_{K-1}).
//
// At extreme degraded operating points where TVerdictStart shrinks below
// the canonical shallow multiples (e.g. TVerdictStart ≤ 4·BTT + RefloodDelay
// at K≥4), the helper still returns a schedule — the shallow B_k values
// can exceed TVerdictStart. The protocol's runtime
// `T_broadcast_max_k = max(BFT_start, TVerdictStart − B_k)` clamps those
// layers' targets at BFT_start, so the configuration remains valid (the
// fall-through depth shrinks but the cluster still operates). Callers
// that want the canonical staggered shape preserved can either widen
// TVerdictStart (loosen Δ_2a / Δ_2b / ε_3 / header headroom), lower
// RefloodDelay for denser meshes, or supply their own per-layer schedule.
func DefaultBroadcastBudget(K int, btt, refloodDelay, tVerdictStart time.Duration) ([]time.Duration, error) {
	if K < 1 {
		return nil, fmt.Errorf("twoab: DefaultBroadcastBudget K=%d must be ≥ 1", K)
	}
	if btt <= 0 {
		return nil, fmt.Errorf("twoab: DefaultBroadcastBudget BTT=%v must be > 0", btt)
	}
	if refloodDelay < 0 {
		return nil, fmt.Errorf("twoab: DefaultBroadcastBudget RefloodDelay=%v must be >= 0", refloodDelay)
	}
	// shallow returns (k+2)*BTT + RefloodDelay for shallow layer k.
	shallow := func(k int) time.Duration {
		return time.Duration(k+2)*btt + refloodDelay
	}
	out := make([]time.Duration, K)
	switch K {
	case 1:
		out[0] = tVerdictStart
	case 2:
		out[0] = shallow(0)
		out[1] = tVerdictStart
	case 3:
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = tVerdictStart
	case 4:
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = shallow(2)
		out[3] = tVerdictStart
	default:
		// First three layers at 2 / 3 / 4 BTT + RefloodDelay; intermediate layers
		// interpolate linearly in duration space from 4·BTT + RefloodDelay (at L_2)
		// to TVerdictStart (at L_{K-1}).
		out[0] = shallow(0)
		out[1] = shallow(1)
		out[2] = shallow(2)
		out[K-1] = tVerdictStart
		span := tVerdictStart - out[2]
		steps := K - 3
		for k := 3; k < K-1; k++ {
			out[k] = out[2] + span*time.Duration(k-2)/time.Duration(steps)
		}
	}
	// Cap each B_k at TVerdictStart so the schedule stays non-decreasing
	// even at degraded operating points where the canonical staggered
	// shallow multiples (or RefloodDelay-inflated values) overshoot.
	// Capped layers share `T_broadcast_max_k = max(BFT_start,
	// TVerdictStart − B_k) = BFT_start` — multiple layers may collide at
	// BFT_start without safety impact. The deepest layer is already
	// TVerdictStart by construction; the cap turns degraded shallow layers
	// into "broadcast at BFT_start" peers of the deepest.
	for k := 0; k < K; k++ {
		if out[k] > tVerdictStart {
			out[k] = tVerdictStart
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
	// before Phase 2a begins). This is a structural coherency floor,
	// not a BFT-liveness floor — keep enforced.
	if c.Delta2a < 2*c.BTT {
		return errors.New("twoab: Delta2a must be >= 2 BTT (minimum coherent sizing per spec §Setting)")
	}
	if c.Delta2b <= 0 {
		return errors.New("twoab: Delta2b must be positive")
	}
	if c.Eps3 <= 0 {
		return errors.New("twoab: Eps3 must be positive")
	}
	// Delta2b < 1 BTT is the BFT-liveness minimum for Phase-2b propagation
	// (spec §Setting recommends Δ_2b ≥ 1 BTT so σ/NR partials reach all
	// honest before Phase 3 starts). Below that the cluster systematically
	// misses; Validate does not enforce — operator choice.
	//
	// TCommit must accommodate the Phase-1 broadcast budget (TVerdictStart
	// = TCommit − Delta2a > 0) plus the deepest-layer broadcast cushion.
	// This is a basic-feasibility floor (the protocol can't run with
	// non-positive TVerdictStart) — keep enforced.
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
	// B_0 ≤ B_1 ≤ ... ≤ B_{K-1}: deeper layers get ≥ their predecessor's
	// absorption / chain-decryption headroom (spec §Setting "B_k ≥
	// B_{k-1}" verbatim). Equal adjacent budgets are tolerated: at
	// degraded operating points multiple layers' broadcast targets clamp
	// to BFT_start (the runtime `max(BFT_start, TVerdictStart − B_k)`
	// floor), and at extreme operating points the canonical staggered
	// shallow budgets even exceed TVerdictStart. Strict-increasing was
	// historically enforced but rejected these degenerate-but-still-valid
	// configs; non-decreasing keeps the staggering intent without
	// blocking them.
	for k := 1; k < len(c.Layers); k++ {
		if c.Layers[k].BroadcastBudget < c.Layers[k-1].BroadcastBudget {
			return errors.New("twoab: BroadcastBudget must be non-decreasing in layer index (B_0 ≤ B_1 ≤ ...)")
		}
	}
	// The deepest layer's budget is the cluster's worst-case liveness
	// guarantee. Spec §Setting recommends `B_{K-1} ≥ 2·BTT` for the
	// cluster to have a liveness guarantee at any layer; below that the
	// deepest leader's bundle can't both propagate and reach Phase-2b
	// quorum before commit, so the cluster systematically misses. The
	// floor is *informational* — Validate does not enforce it. Operators
	// who want to study (or knowingly run) at extreme operating points
	// where no layer has a liveness guarantee can; the simulator and
	// production stack still execute.

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
		// Per spec §Setting: T_{K-1} ≤ ... ≤ T_1 ≤ T_0. Deeper layers
		// fetch ≤ their predecessor's offset (re-org resistance, MEV-
		// fetch asymmetry). Strict-decreasing was historically enforced;
		// the non-increasing relaxation lets multiple layers' targets
		// collide at BFT_start when the operating point pushes shallow
		// targets past TVerdictStart (matches the BroadcastBudget
		// non-decreasing relaxation above).
		if k > 0 && layer.FetchAt > c.Layers[k-1].FetchAt {
			return errors.New("twoab: layer fetch times must be non-increasing in k")
		}
	}
	return nil
}
