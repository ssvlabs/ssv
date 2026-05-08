package casts

import (
	"math"
	"time"
)

// DurationFromUint64 converts uint64 to time.Duration
func DurationFromUint64(t uint64) time.Duration {
	if t > math.MaxInt64 {
		return time.Duration(math.MaxInt64) // todo: error handling refactor
	}
	return time.Duration(t) // #nosec G115
}
