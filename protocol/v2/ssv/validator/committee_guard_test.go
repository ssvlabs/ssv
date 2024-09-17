package validator

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestCommitteeDutyGuard(t *testing.T) {
	guard := NewCommitteeDutyGuard()

	for _, role := range []spectypes.BeaconRole{spectypes.BNRoleAttester, spectypes.BNRoleSyncCommittee} {
		err := guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 1)
		require.EqualError(t, err, "duty not found")

		// Start duty at slot 2:
		{
			err = guard.StartDuty(role, spectypes.ValidatorPK{0x1}, 2)
			require.NoError(t, err)

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 2)
			require.NoError(t, err)

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 3)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 2")

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 1)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 2")

			err = guard.StartDuty(role, spectypes.ValidatorPK{0x1}, 2)
			require.EqualError(t, err, "duty already running at slot 2")
		}

		// Start duty at slot 3:
		{
			err = guard.StartDuty(role, spectypes.ValidatorPK{0x1}, 3)
			require.NoError(t, err)

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 1)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 3")

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 2)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 3")

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 3)
			require.NoError(t, err)
		}

		// Try new validator 0x2:
		{
			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x2}, 4)
			require.EqualError(t, err, "duty not found")

			err = guard.StartDuty(role, spectypes.ValidatorPK{0x2}, 4)
			require.NoError(t, err)

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x2}, 4)
			require.NoError(t, err)

			// Check validator 0x1 is unchanged:
			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 2)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 3")

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 3)
			require.NoError(t, err)
		}

		// Stop validator 0x1:
		{
			guard.StopValidator(spectypes.ValidatorPK{0x1})

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x1}, 3)
			require.EqualError(t, err, "duty not found")

			// Check validator 0x2 is unchanged:
			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x2}, 4)
			require.NoError(t, err)

			err = guard.ValidDuty(role, spectypes.ValidatorPK{0x2}, 3)
			require.ErrorContains(t, err, "slot mismatch: duty is running at slot 4")
		}
	}

	// Stop validator 0x2 to verify both duties are stopped:
	{
		guard.StopValidator(spectypes.ValidatorPK{0x2})

		err := guard.ValidDuty(spectypes.BNRoleAttester, spectypes.ValidatorPK{0x2}, 4)
		require.EqualError(t, err, "duty not found")

		err = guard.ValidDuty(spectypes.BNRoleSyncCommittee, spectypes.ValidatorPK{0x2}, 4)
		require.EqualError(t, err, "duty not found")
	}
}
