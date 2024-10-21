package casts

import (
	"fmt"
	"math"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var (
	ErrNegativeTime        = fmt.Errorf("time can't be negative")
	ErrMaxDurationOverflow = fmt.Errorf("duration can't exceed max int64")
)

// DurationFromUint64 converts uint64 to time.Duration
func DurationFromUint64(t uint64) time.Duration {
	if t > math.MaxInt64 {
		return time.Duration(math.MaxInt64) // todo: error handling refactor
	}
	return time.Duration(t) // #nosec G115
}

func BeaconRoleToConvertRole(beaconRole spectypes.BeaconRole) spectypes.RunnerRole {
	return spectypes.RunnerRole(beaconRole) // #nosec G115
}

func BeaconRoleToRunnerRole(beaconRole spectypes.BeaconRole) spectypes.RunnerRole {
	return spectypes.RunnerRole(beaconRole) // #nosec G115
}

func RunnerRoleToBeaconRole(role spectypes.RunnerRole) spectypes.BeaconRole {
	return spectypes.BeaconRole(role) // #nosec G115
}

// DurationToUint64 returns error if duration is negative and converts time.Duration to uint64 safe otherwise
func DurationToUint64(t time.Duration) (uint64, error) {
	if t < 0 {
		return 0, ErrNegativeTime
	}
	return uint64(t), nil // #nosec G115
}
