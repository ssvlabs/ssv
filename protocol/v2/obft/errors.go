package obft

import "errors"

// ErrInstanceEnded is returned by any state-mutating public method on a
// Finalized Instance (across both base and twoab protocol implementations).
// Per-package Instance types alias this sentinel via their own errors.go
// for ergonomics (callers can use either `obft.ErrInstanceEnded` or the
// per-package re-export with `errors.Is`).
//
// Shared at this package level because the Finalized-instance contract
// describes an Instance-API lifecycle property that exists identically
// at the same conceptual level in both protocol implementations —
// matching the placement of `Signer`, `ThresholdIBE`, and `NoQuorumTag`
// in this package.
//
// Protocol-specific errors (e.g. base's ErrLatePhase1Bundle,
// ErrAlreadyCommitted; twoab's ErrUpgradeNotAvailable) stay in the
// respective per-package errors.go files.
var ErrInstanceEnded = errors.New("obft: instance has been finalized")
