package conversion

import (
	"time"
)

// LenUint64 - Returns the length of the slice as uint64, due length can't be negative
func LenUint64[T any](slice []T) uint64 {
	return uint64(len(slice))
}

func TimeDurationFromUint64(t uint64) time.Duration {
	return time.Duration(t) // #nosec G115
}

func TimeUnixFromUint64(t uint64) time.Time {
	return time.Unix(int64(t), 0) // #nosec G115
}
