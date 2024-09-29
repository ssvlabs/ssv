package conversion

import (
	"fmt"
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/convert"
)

var (
	ErrNegativeTime = fmt.Errorf("time can't be negative")
)

// LenUint64 - Returns the length of the slice as uint64, due length can't be negative
func LenUint64[T any](slice []T) uint64 {
	return uint64(len(slice))
}

func DurationFromUint64(t uint64) time.Duration {
	return time.Duration(t) // #nosec G115
}

func BeaconRoleToConvertRole(beaconRole spectypes.BeaconRole) convert.RunnerRole {
	return convert.RunnerRole(beaconRole) // #nosec G115
}

func BeaconRoleToRunnerRole(beaconRole spectypes.BeaconRole) spectypes.RunnerRole {
	return spectypes.RunnerRole(beaconRole) // #nosec G115
}

func RunnerRoleToBeaconRole(role spectypes.RunnerRole) spectypes.BeaconRole {
	return spectypes.BeaconRole(role) // #nosec G115
}

// TODO fix the type in spec and remove this func
func CutoffRoundUint64() uint64 {
	return uint64(specqbft.CutoffRound) // #nosec G115
}

// DurationToUint64 returns error if duration is negative and converts time.Duration to uint64 safe otherwise
func DurationToUint64(t time.Duration) (uint64, error) {
	if t < 0 {
		return 0, ErrNegativeTime
	}
	return uint64(t), nil // #nosec G115
}
