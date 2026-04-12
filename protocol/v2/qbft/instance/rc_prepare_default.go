package instance

import "go.uber.org/zap"

// Default (no build tag) stubs — production-safe no-ops

func rcShouldDropPrepare() bool { return false }

func rcMaxRounds() int { return 0 }

func rcLogConfigIf(_ *zap.Logger) {}

