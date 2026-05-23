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

// LayerSpec describes one layer of the K-layer onion structure: which
// operator is the layer's leader, when (relative to slot start) they should
// fetch their candidate value, and how much absorption budget the layer's
// receivers are given.
//
// Per spec §Setting: Layers[0] is the primary L_0 (latest fetch — picks up
// MEV-late values), Layers[1..K-1] are backups that all fetch from a
// deepest-confirmed parent at slot start and broadcast at BFT_start. Only
// L_0 carries MEV-fresh fetch; backups are last-resort safety nets that
// trade MEV freshness for maximally-wide propagation absorption.
type LayerSpec struct {
	Leader  OperatorID
	FetchAt time.Duration

	// BroadcastBudget is the layer's T_commit-anchored absorption *target*
	// `B_k` per OBFT.md §Setting: the leader aims to broadcast their
	// Phase-1 bundle by `T_broadcast_max_k = max(0, T_commit − B_k)` so
	// the bundle's first-observation at any honest receiver lands by
	// `T_commit` under partial-synchrony assumptions for that layer's
	// propagation budget.
	//
	// Spec recommends `B_0 = 2·BTT + RefloodDelay` (primary, MEV-fresh)
	// and `B_1..B_{K-1} = T_commit` (backups broadcast at BFT_start).
	// `B_0 ≤ B_1 = ... = B_{K-1}` — backups all share the maximally-wide
	// absorption budget; the primary has a tighter budget for MEV-fetch
	// headroom and falls through to a backup if propagation slips.
	//
	// B_k is a *target*, not a hard runtime cap. The only runtime
	// acceptance gate is `T_commit` (peers admit bundles first-observed
	// in `[slot_start, T_commit]` regardless of which layer they came
	// from). A leader that cannot meet `T_broadcast_max_k` broadcasts
	// best-effort (broadcast as soon as the bundle is ready). When
	// `B_k ≥ T_commit`, `T_broadcast_max_k` clamps at 0 — the leader's
	// target broadcast time is slot start. `B_k = T_commit` for backups
	// deliberately hits this clamp ("earliest possible" backup broadcast).
	//
	// Spec K=4 Config A (BTT=200ms, T_commit=3600ms, RefloodDelay=700ms):
	// B_k values are [1100, 3600, 3600, 3600]ms. The primary L_0 absorbs
	// real propagation up to 2·BTT + RefloodDelay (one IWANT round-trip
	// plus one IHAVE/IWANT reflood cycle for mesh-flaky receivers); the
	// backups broadcast at slot start and absorb up to the entire commit
	// budget.
	//
	// Required: must be > 0 on every layer. Config.Validate rejects
	// zero/negative values and decreasing-in-k schedules (equal adjacent
	// budgets are accepted — backups all tie at T_commit; see spec
	// §Setting). Use DefaultBroadcastBudget(K, BTT, RefloodDelay,
	// T_commit) for the spec-recommended schedule when constructing a
	// Config manually.
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

	// BFTStart is BFT_start — the slot-relative offset at which the
	// protocol's primary broadcast pipeline begins. Pre-fetch and
	// pre-consensus sit in `[slot_start, BFTStart]`. Default 0 (BFT
	// starts at slot start). When BFTStart > T_commit − B_k for some
	// layer k, the spec's runtime clamp
	// `T_broadcast_max_k = max(BFTStart, T_commit − B_k)` floors that
	// layer's broadcast deadline at BFTStart — the schedule degrades
	// (effective B_k = T_commit − BFTStart) but stays valid; the
	// validator below admits these configurations.
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

// BroadcastMaxOffsetForLayer returns `T_broadcast_max_k = max(BFTStart, T_commit − B_k)`
// for layer k — the leader's target broadcast time per spec §Setting.
//
// `B_k` is a target, not a hard cap; the BFTStart floor (default 0) handles
// the degraded case where the layer's design-time budget overshoots
// `T_commit − BFTStart`. In that case the leader broadcasts at BFTStart
// best-effort, and the layer's effective absorption window contracts to
// `T_commit − BFTStart` (= `min(B_k, T_commit − BFTStart)`). The only
// runtime acceptance gate remains T_commit at receivers.
func (c *Config) BroadcastMaxOffsetForLayer(k int) time.Duration {
	if d := c.TCommit - c.Layers[k].BroadcastBudget; d > c.BFTStart {
		return d
	}
	return c.BFTStart
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
// `T_broadcast_max_k = max(BFT_start, T_commit − B_k)` clamps the primary's
// target at BFT_start, so the configuration remains valid (the primary
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
	// Callers can use DefaultBroadcastBudget(K, BTT, RefloodDelay, T_commit)
	// for the spec-recommended primary-vs-backup schedule (B_0 = 2·BTT +
	// RefloodDelay; B_1..B_{K-1} = T_commit), or supply their own per-layer
	// values.
	for k, l := range c.Layers {
		if l.BroadcastBudget <= 0 {
			return fmt.Errorf("obft: layer %d BroadcastBudget must be > 0 (use DefaultBroadcastBudget for the spec-conforming primary-vs-backup schedule)", k)
		}
	}
	// B_0 ≤ B_1 ≤ ... ≤ B_{K-1}: deeper layers get ≥ their predecessor's
	// absorption / chain-decryption headroom (spec §Setting "B_k ≥
	// B_{k-1}" verbatim). Equal adjacent budgets are tolerated: at
	// the spec-recommended primary-vs-backup schedule all backups share
	// B_k = T_commit (multiple layers' broadcast targets clamp to BFT_start
	// via the runtime `max(BFT_start, T_commit − B_k)` floor). Strict-
	// increasing was historically enforced (when the schedule was staggered)
	// but rejected the now-default primary-vs-backup configs; non-decreasing
	// is the current convention.
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
			// Deadline = max(BFTStart, T_commit−B_k). When B_k ≥ T_commit
			// the T_commit−B_k component collapses to ≤ 0, so the deadline
			// is BFTStart (best-effort broadcast at BFT_start, per spec
			// §Setting); FetchAt must then be ≤ BFTStart. Surface the
			// underlying T_commit−B_k value so over-budget configs are
			// obvious in the error.
			return fmt.Errorf("obft: layer %d FetchAt %v exceeds broadcast deadline %v (max(BFTStart=%v, T_commit−B_k=%v))",
				k, layer.FetchAt, c.BroadcastMaxOffsetForLayer(k),
				c.BFTStart, c.TCommit-c.Layers[k].BroadcastBudget)
		}
		// Per spec §Setting: T_{K-1} ≤ ... ≤ T_1 ≤ T_0. Deeper layers
		// fetch ≤ their predecessor's offset (re-org resistance, MEV-
		// fetch asymmetry between primary and backups). Strict-
		// decreasing was historically enforced (when the schedule was
		// staggered); the non-increasing relaxation lets all backups
		// share FetchAt = 0 under the current primary-vs-backup schedule
		// (matches the BroadcastBudget non-decreasing relaxation above).
		if k > 0 && layer.FetchAt > c.Layers[k-1].FetchAt {
			return errors.New("obft: layer fetch times must be non-increasing in k")
		}
	}
	return nil
}
