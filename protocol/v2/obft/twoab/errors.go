package twoab

import "errors"

// Protocol-specific errors. Additional errors will accrete as later
// phases land.

// ErrNilConfig is returned by NewInstance when called with a nil Config.
var ErrNilConfig = errors.New("twoab: nil config")

// ErrLatePhase1Bundle is returned by ObservePhase1Bundle when the
// bundle's first-observation offset is past the Phase-2a end (T_commit).
// Per spec §Phase 1, bundles observed past T_accept_max but before
// T_commit go into auth-only retention; past T_commit, retention is
// pointless — Phase 2a is over and the bundle can no longer be used.
var ErrLatePhase1Bundle = errors.New("twoab: phase-1 bundle observed past T_commit")

// ErrNotLeader is returned by BuildPhase1Bundle when called on an
// operator who is not the layer's designated leader.
var ErrNotLeader = errors.New("twoab: not leader at this layer")

// ErrEmptyValue is returned by BuildPhase1Bundle when called with an
// empty Value.
var ErrEmptyValue = errors.New("twoab: empty value")

// ErrLayerOutOfRange is returned when a layer index is outside
// [0, K).
var ErrLayerOutOfRange = errors.New("twoab: layer out of range")

// ErrSigmaLocked is returned when the EKM detects an attempt to σ-commit
// on a different V than the one already locked at this (slot, layer), or
// to σ-commit at a layer where NR is already locked. Single-σ-V + σ-XOR-NR
// invariants per spec §EKM coordination.
var ErrSigmaLocked = errors.New("twoab: σ already locked at this layer (single-σ-V or σ-XOR-NR violation)")

// ErrNRLocked is returned when the EKM detects an attempt to σ-commit at
// a layer where NR is already locked (σ-XOR-NR invariant).
var ErrNRLocked = errors.New("twoab: NR already locked at this layer (σ-XOR-NR violation)")

// ErrOnion2bAlreadyEmitted is returned by BuildOwnOnion2b on a second
// call within the same slot. Per spec §Phase 2b emission, each operator
// emits exactly one Onion2b per (slot, operator).
var ErrOnion2bAlreadyEmitted = errors.New("twoab: Onion2b already emitted for this slot")
