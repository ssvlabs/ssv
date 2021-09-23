package beacon

import (
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidatorMetadata_Status(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: v1.ValidatorStateActiveOngoing,
		}
		require.True(t, meta.Deposited())
		require.False(t, meta.Exiting())
		require.False(t, meta.Slashed())
	})

	t.Run("exited", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: v1.ValidatorStateWithdrawalPossible,
		}
		require.True(t, meta.Exiting())
		require.True(t, meta.Deposited())
		require.False(t, meta.Slashed())
	})

	t.Run("exitedSlashed", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: v1.ValidatorStateExitedSlashed,
		}
		require.True(t, meta.Slashed())
		require.True(t, meta.Exiting())
		require.True(t, meta.Deposited())
	})
}
