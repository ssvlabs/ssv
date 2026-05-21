package twoab

import (
	"errors"
	"fmt"
)

// Protocol-specific errors.

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

// ErrPhase2aAlreadyFired is returned by MaybeFirePhase2a on a second
// call within the same slot. Per spec §Phase 2a, each operator fires
// Phase 2a exactly once at T_phase_2a; subsequent calls are silent no-ops
// at the API surface (the cached emission is returned).
var ErrPhase2aAlreadyFired = errors.New("twoab: Phase 2a already fired for this slot")

// ErrCommitAlreadyEmitted is returned by MaybeBuildAndBroadcastCommit on
// a second call after an emission already fired at this layer. Per spec
// §Phase 2b, each operator emits exactly one Commit per (slot, layer).
var ErrCommitAlreadyEmitted = errors.New("twoab: Commit already emitted at this layer")

// ErrUpgradeNotAvailable is returned by MaybeBuildAndBroadcastUpgrade when
// the upgrade preconditions are not met (op is not on KindNoValue path, or
// has no V_0, or host re-validates as NV, or op has already emitted
// KindCommit at L_0 — the post-commit upgrade is not authorized per A1).
var ErrUpgradeNotAvailable = errors.New("twoab: upgrade KindValue not available")

// ErrUnauthorizedEmissionSequence is returned when an internal call site
// attempts to construct an emission whose resulting sequence is not in
// the authorized A1-A8 set (defense-in-depth — the builders enforce the
// invariant). Receiver-side detection of unauthorized sequences from peers
// goes through evidence.go (Rule 6a), not this error.
var ErrUnauthorizedEmissionSequence = errors.New("twoab: emission would produce unauthorized Phase-2 sequence")

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
