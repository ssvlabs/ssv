package beacon

import (
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/stretchr/testify/require"
)

func TestValidatorMetadata_Status(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: eth2apiv1.ValidatorStateActiveOngoing,
		}
		require.True(t, meta.Activated())
		require.False(t, meta.Exited())
		require.False(t, meta.Slashed())
		require.False(t, meta.Pending())
	})

	t.Run("exited", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: eth2apiv1.ValidatorStateWithdrawalPossible,
		}
		require.True(t, meta.Exited())
		require.True(t, meta.Activated())
		require.False(t, meta.Slashed())
		require.False(t, meta.Pending())
	})

	t.Run("exitedSlashed", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: eth2apiv1.ValidatorStateExitedSlashed,
		}
		require.True(t, meta.Slashed())
		require.True(t, meta.Exited())
		require.True(t, meta.Activated())
		require.False(t, meta.Pending())
	})

	t.Run("pending", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: eth2apiv1.ValidatorStatePendingQueued,
		}
		require.True(t, meta.Pending())
		require.False(t, meta.Slashed())
		require.False(t, meta.Exited())
		require.False(t, meta.Activated())
	})
}

func TestValidatorMetadata_Equals(t *testing.T) {
	meta1 := &ValidatorMetadata{Status: eth2apiv1.ValidatorStateActiveOngoing, Index: 1, ActivationEpoch: 2}
	meta2 := &ValidatorMetadata{Status: eth2apiv1.ValidatorStateActiveOngoing, Index: 1, ActivationEpoch: 2}
	meta3 := &ValidatorMetadata{Status: eth2apiv1.ValidatorStateExitedUnslashed, Index: 1, ActivationEpoch: 2}

	require.True(t, meta1.Equals(meta2))
	require.False(t, meta1.Equals(meta3))
	require.False(t, meta1.Equals(nil))
}
