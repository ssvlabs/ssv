package base

import (
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// ErrNoQuorum is returned by Resolve when neither σ-quorum at any layer nor
// NR-quorum sufficient to advance the walk was reached. The slot is missed.
// Wrapped by ResolveError on the failure paths in phase3.go; callers using
// errors.Is(err, ErrNoQuorum) continue to match either flavor.
var ErrNoQuorum = errors.New("obft: no quorum reached at any layer")

// ResolveFailureReason discriminates the two ways Resolve can fail to reach
// σ-quorum after walking the layer chain. The distinction matters for
// upstream diagnostics — HV1-style adversarial scenarios produce
// ResolveFailureDeadlock at a specific layer (typically L_0); a
// fully-silent cluster produces ResolveFailureExhaustion at L_{K-1}.
type ResolveFailureReason int

const (
	// ResolveFailureUnknown is the zero value; never returned in practice.
	ResolveFailureUnknown ResolveFailureReason = iota
	// ResolveFailureDeadlock: walk stopped mid-chain at StoppedAtLayer
	// because σ-pool < qV AND NR-pool < qEnc, so no L_{k+1} fall-through
	// existed. Characteristic of OBFT's onion design under HV1-style
	// adversaries that split the σ-pool below qV while suppressing the
	// NR fall-through path. 2abOBFT addresses this via its Phase-2a
	// verdict step (NR-quorum recovers cluster-wide even when σ-pool is
	// split at L_0); a 2abOBFT ResolveFailureDeadlock therefore implies
	// a different / deeper pathology than its OBFT counterpart.
	ResolveFailureDeadlock
	// ResolveFailureExhaustion: walk visited every layer in [0, K-1]
	// without σ-quorum and without NR-fallthrough exhaustion — the
	// cluster simply never assembled a threshold signature for any V.
	ResolveFailureExhaustion
)

// String returns a stable telemetry name for the reason.
func (r ResolveFailureReason) String() string {
	switch r {
	case ResolveFailureDeadlock:
		return "deadlock"
	case ResolveFailureExhaustion:
		return "exhaustion"
	default:
		return "unknown"
	}
}

// ResolveError describes a failed Resolve() walk in structured form.
// Wraps ErrNoQuorum (via Unwrap) so existing errors.Is(err, ErrNoQuorum)
// callers continue to match; callers wanting the per-failure detail use
// errors.As(err, &ResolveError{}).
type ResolveError struct {
	// StoppedAtLayer is the deepest layer the walk attempted before
	// giving up, in [0, K-1].
	StoppedAtLayer int
	// Reason is the high-level classification (Deadlock vs Exhaustion).
	Reason ResolveFailureReason
}

func (e *ResolveError) Error() string {
	switch e.Reason {
	case ResolveFailureDeadlock:
		return fmt.Sprintf("obft: deadlock at L_%d (σ-pool short, no NR-fallthrough)", e.StoppedAtLayer)
	case ResolveFailureExhaustion:
		return fmt.Sprintf("obft: exhausted all %d layers without σ-quorum", e.StoppedAtLayer+1)
	default:
		return ErrNoQuorum.Error()
	}
}

// Unwrap makes errors.Is(err, ErrNoQuorum) still match a *ResolveError —
// the two are semantically the same outcome (no quorum), distinguished
// only by diagnostic detail.
func (e *ResolveError) Unwrap() error {
	return ErrNoQuorum
}

// ErrLatePhase1Bundle is returned by ObservePhase1Bundle when the bundle's
// first-observation time is past T_commit. Per spec §Phase 1, bundles
// first-observed past T_commit at any honest receiver are not counted by
// that receiver toward σ-quorum at this layer; the cluster relies on K-layer
// fall-through for partition recovery.
var ErrLatePhase1Bundle = errors.New("obft: phase-1 bundle first-observed past T_commit")

// ErrSigmaLocked is returned by EKM-style enforcement when an operation
// would violate cross-phase exclusivity or the single-σ-V-per-(slot, layer)
// invariant. Per spec §Slashing-protection scope, an operator who has
// σ-emitted at layer k may not subsequently emit NR/NV on nr_tag_k, and may
// not σ on a different V' at the same layer.
var ErrSigmaLocked = errors.New("obft: operator is σ-locked at this layer")

// ErrNRLocked is the symmetric case: an operator who has NR-emitted at
// layer k may not subsequently emit σ at the same layer.
var ErrNRLocked = errors.New("obft: operator is NR-locked at this layer")

// ErrAlreadyCommitted is returned when BuildOwnCommit is called more than
// once in a slot. Per spec §Phase 2, each operator emits exactly one
// KindCommit per (slot, operator) at T_commit.
var ErrAlreadyCommitted = errors.New("obft: operator already committed for this slot")

// ErrInstanceEnded is returned by state-mutating Instance methods (and
// Resolve, which mutates via Rule-4 evidence recording) when the instance
// has been Finalize'd. Defense-in-depth: the Controller adapter is the
// authoritative serialization layer and rejects mutator dispatches to a
// finalized instance under its instanceMu; this sentinel additionally
// protects any caller path that bypasses the adapter (tests, future
// plumbing) so the ended-flag promise on Instance is honored end-to-end.
//
// Re-exported from the parent `obft` package — the shared sentinel
// lets callers writing cross-package code use either form interchangeably
// with `errors.Is` (e.g. `errors.Is(err, base.ErrInstanceEnded)` and
// `errors.Is(err, obft.ErrInstanceEnded)` both succeed). See
// `protocol/v2/obft/errors.go` for the canonical declaration.
var ErrInstanceEnded = obft.ErrInstanceEnded
