package roundtimer

import "time"

// Default (no build tag) stubs — production-safe no-ops

func rcTestForceRC() bool { return false }

func rcTestTimeout() time.Duration { return 0 }

func rcTestLogClamp(_ bool, _ time.Duration) {}

