package twoab

import (
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Protocol-specific errors.

// ErrInstanceEnded is re-exported from the parent `obft` package so the
// Finalized-instance contract is uniform across both protocol
// implementations. Callers using either `obft.ErrInstanceEnded` or
// `twoab.ErrInstanceEnded` with `errors.Is` match identically.
// Returned by twoab Instance state-mutating methods after Finalize per
// the C1 convergence (see docs/OBFT-TWOAB-CONVERGENCE-PLAN.md).
var ErrInstanceEnded = obft.ErrInstanceEnded

// ErrNilConfig is returned by NewInstance when called with a nil Config.
var ErrNilConfig = errors.New("twoab: nil config")

// ErrNotLeader is returned by BuildPhase1Bundle when called on an
// operator who is not the layer's designated leader.
var ErrNotLeader = errors.New("twoab: not leader at this layer")

// ErrEmptyValue is returned by BuildPhase1Bundle when called with an
// empty Value.
var ErrEmptyValue = errors.New("twoab: empty value")

// ErrLayerOutOfRange is returned when a layer index is outside [0, K).
var ErrLayerOutOfRange = errors.New("twoab: layer out of range")

// ErrSigmaLocked is returned when the EKM detects an attempt to σ-commit
// on a different V than the one already locked at this (slot, layer), or
// to σ-commit at a layer where NR is already locked. Single-σ-V + σ-XOR-NR
// invariants per spec §EKM coordination.
var ErrSigmaLocked = errors.New("twoab: σ already locked at this layer (single-σ-V or σ-XOR-NR violation)")

// ErrNRLocked is returned when the EKM detects an attempt to σ-commit at
// a layer where NR is already locked (σ-XOR-NR invariant).
var ErrNRLocked = errors.New("twoab: NR already locked at this layer (σ-XOR-NR violation)")

// ErrUpgradeNotAvailable is returned by MaybeBuildAndBroadcastUpgrade when
// the upgrade preconditions are not met (op is not on KindNoValue path, or
// has no V_0, or host re-validates as NV, or op has already emitted
// KindCommit at L_0 — a post-commit upgrade is not authorized).
var ErrUpgradeNotAvailable = errors.New("twoab: upgrade KindValue not available")

// ErrNoQuorum is returned by Resolve when the K-layer walk exhausts
// without σ-quorum reaching at any layer AND without NR-quorum at the
// previous layer to unlock decryption at the next. Per spec §Phase 3,
// the slot misses cleanly in this case (no safety violation). Wrapped
// by ResolveError on the failure paths in phase3.go; callers using
// errors.Is(err, ErrNoQuorum) continue to match either flavor.
var ErrNoQuorum = errors.New("twoab: no quorum (slot missed)")

// ResolveFailureReason discriminates the two ways Resolve can fail
// after walking the K-layer chain — same taxonomy as obft/base, so
// upstream classifiers can apply uniform logic to both protocols. See
// protocol/v2/obft/base/errors.go ResolveFailureReason for the
// rationale.
type ResolveFailureReason int

const (
	ResolveFailureUnknown ResolveFailureReason = iota
	ResolveFailureDeadlock
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
// callers continue to match.
type ResolveError struct {
	StoppedAtLayer int
	Reason         ResolveFailureReason
}

func (e *ResolveError) Error() string {
	switch e.Reason {
	case ResolveFailureDeadlock:
		return fmt.Sprintf("twoab: deadlock at L_%d (σ-pool short, no NR-fallthrough)", e.StoppedAtLayer)
	case ResolveFailureExhaustion:
		return fmt.Sprintf("twoab: exhausted all %d layers without σ-quorum", e.StoppedAtLayer+1)
	default:
		return ErrNoQuorum.Error()
	}
}

// Unwrap makes errors.Is(err, ErrNoQuorum) still match a *ResolveError.
func (e *ResolveError) Unwrap() error {
	return ErrNoQuorum
}
