package twoab

import "errors"

// Protocol-specific errors. Additional errors will accrete as Phases E-L
// land — phase-1-late-bundle, verdict-already-broadcast, etc.

// ErrNilConfig is returned by NewInstance when called with a nil Config.
var ErrNilConfig = errors.New("twoab: nil config")

// ErrNotImplemented is returned by Phase-B stub methods that haven't
// been filled in yet. Will be removed as Phases E-I implement each
// phase's methods.
var ErrNotImplemented = errors.New("twoab: not implemented (Phase B skeleton)")
