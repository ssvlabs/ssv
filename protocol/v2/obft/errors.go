package obft

import "errors"

// ErrNoQuorum is returned by Resolve when neither σ-quorum at any layer nor
// NR-quorum sufficient to advance the walk was reached. The slot is missed.
var ErrNoQuorum = errors.New("obft: no quorum reached at any layer")

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
