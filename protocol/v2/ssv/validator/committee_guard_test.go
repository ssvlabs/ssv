package validator

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestCommitteeDutyGuard(t *testing.T) {
	var (
		pk1 = spectypes.ValidatorPK{1}
		pk2 = spectypes.ValidatorPK{2}
		pk3 = spectypes.ValidatorPK{3}

		attester = spectypes.BNRoleAttester
		sync     = spectypes.BNRoleSyncCommittee
	)

	guard := NewCommitteeDutyGuard()

	// Unsupported role:
	err := guard.StartDuty(spectypes.BNRoleProposer, pk1, 1)
	require.EqualError(t, err, "unsupported role 2")
	err = guard.ValidDuty(spectypes.BNRoleProposer, pk1, 1)
	require.EqualError(t, err, "unsupported role 2")

	// Comprehensive test for both roles:
	for _, role := range []spectypes.BeaconRole{attester, sync} {
		err := guard.ValidDuty(role, pk1, 1)
		require.EqualError(t, err, "duty not found")

		// Start duty at slot 2:
		{
			err = guard.StartDuty(role, pk1, 2)
			require.NoError(t, err)

			err = guard.ValidDuty(role, pk1, 2)
			require.NoError(t, err)

			err = guard.ValidDuty(role, pk1, 3)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 2")

			err = guard.ValidDuty(role, pk1, 1)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 2")

			err = guard.StartDuty(role, pk1, 2)
			require.EqualError(t, err, "duty already running at slot 2")
		}

		// Start duty at slot 3:
		{
			err = guard.StartDuty(role, pk1, 3)
			require.NoError(t, err)

			err = guard.ValidDuty(role, pk1, 1)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 3")

			err = guard.ValidDuty(role, pk1, 2)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 3")

			err = guard.ValidDuty(role, pk1, 3)
			require.NoError(t, err)
		}

		// Try new validator 0x2:
		{
			err = guard.ValidDuty(role, pk2, 4)
			require.EqualError(t, err, "duty not found")

			err = guard.StartDuty(role, pk2, 4)
			require.NoError(t, err)

			err = guard.ValidDuty(role, pk2, 4)
			require.NoError(t, err)

			// Check validator 0x1 is unchanged:
			err = guard.ValidDuty(role, pk1, 2)
			require.EqualError(t, err, "slot mismatch: duty is running at slot 3")

			err = guard.ValidDuty(role, pk1, 3)
			require.NoError(t, err)
		}

		// Stop validator 0x1:
		{
			guard.StopValidator(pk1)

			err = guard.ValidDuty(role, pk1, 3)
			require.EqualError(t, err, "duty not found")

			// Check validator 0x2 is unchanged:
			err = guard.ValidDuty(role, pk2, 4)
			require.NoError(t, err)

			err = guard.ValidDuty(role, pk2, 3)
			require.ErrorContains(t, err, "slot mismatch: duty is running at slot 4")
		}
	}

	// Stop non-existing validator:
	{
		guard.StopValidator(pk3)

		// Pre-check that validator 0x2 is unchanged:
		err := guard.ValidDuty(attester, pk2, 4)
		require.NoError(t, err)

		err = guard.ValidDuty(sync, pk2, 3)
		require.EqualError(t, err, "slot mismatch: duty is running at slot 4")
	}

	// Stop validator 0x2 to verify both duties are stopped:
	{
		guard.StopValidator(pk2)

		err = guard.ValidDuty(attester, pk2, 4)
		require.EqualError(t, err, "duty not found")

		err = guard.ValidDuty(sync, pk2, 4)
		require.EqualError(t, err, "duty not found")
	}
}
