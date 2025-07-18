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

func BeaconRoleToRunnerRole(runnerRole spectypes.BeaconRole) spectypes.RunnerRole {
	switch runnerRole {
	case spectypes.BNRoleAttester:
		return spectypes.RoleCommittee
	case spectypes.BNRoleAggregator:
		return spectypes.RoleAggregator
	case spectypes.BNRoleProposer:
		return spectypes.RoleProposer
	case spectypes.BNRoleSyncCommittee:
		return spectypes.RoleCommittee
	case spectypes.BNRoleSyncCommitteeContribution:
		return spectypes.RoleSyncCommitteeContribution
	case spectypes.BNRoleValidatorRegistration:
		return spectypes.RoleValidatorRegistration
	case spectypes.BNRoleVoluntaryExit:
		return spectypes.RoleVoluntaryExit
	default:
		return spectypes.RoleUnknown
	}
}

// DurationToUint64 returns error if duration is negative and converts time.Duration to uint64 safe otherwise
func DurationToUint64(t time.Duration) (uint64, error) {
	if t < 0 {
		return 0, ErrNegativeTime
	}
	return uint64(t), nil // #nosec G115
}
