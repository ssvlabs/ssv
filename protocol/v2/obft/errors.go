package obft

import "errors"

// ErrNoQuorum is returned by Resolve when neither σ-quorum at any layer nor
// NR-quorum sufficient to advance the walk was reached. The slot is missed.
var ErrNoQuorum = errors.New("obft: no quorum reached at any layer")

// ErrLatePhase1Bundle is returned by ObservePhase1Bundle when the bundle's
// first-observation time is past T_accept_max. Per spec §Phase 1, such
// bundles are rejected entirely — accepting them is operationally useless
// since a downstream σ-emit on the bundle would not propagate before peers'
// NR-decision at end of Phase 2.
var ErrLatePhase1Bundle = errors.New("obft: phase-1 bundle first-observed past T_accept_max")

// ErrSigmaLocked is returned by EKM-style enforcement when an operation
// would violate cross-phase exclusivity or the single-σ-V-per-(slot, layer)
// invariant. Per spec §Slashing-protection scope, an operator who has
// σ-emitted at layer k may not subsequently emit NR/NV on nr_tag_k, and may
// not σ on a different V' at the same layer.
var ErrSigmaLocked = errors.New("obft: operator is σ-locked at this layer")

// ErrNRLocked is the symmetric case: an operator who has NR-emitted at
// layer k may not subsequently emit σ at the same layer.
var ErrNRLocked = errors.New("obft: operator is NR-locked at this layer")

// ErrEquivocationLocked is returned when σ-emit is requested at a layer where
// the operator has retained ≥ 2 distinct Phase-1 bundles from the leader
// (Defer-due-to-equivocation). Per spec §Phase 1 / Equivocation handling,
// recovery via late re-flood is foreclosed once equivocation is observed.
var ErrEquivocationLocked = errors.New("obft: operator is in Defer-due-to-equivocation at this layer")
