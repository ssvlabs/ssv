package storage

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestShareOptionsToShare(t *testing.T) {
	threshold.Init()

	origShare, sk := generateRandomValidatorShare()

	shareOpts := ShareOptions{
		ShareKey:  sk.SerializeToHexStr(),
		PublicKey: sk.GetPublicKey().SerializeToHexStr(),
		NodeID:    1,
		Committee: map[string]int{},
	}

	t.Run("valid ShareOptions", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			shareOpts.Committee[string(fixtures.RefSplitSharesPubKeys[i])] = i + 1
		}
		share, err := shareOpts.ToShare()
		require.NoError(t, err)
		require.NotNil(t, share)
		require.Equal(t, len(share.Committee), 4)
		require.Equal(t, share.PublicKey.GetHexString(), origShare.PublicKey.GetHexString())
	})

	t.Run("empty ShareOptions", func(t *testing.T) {
		emptyShareOpts := ShareOptions{}
		share, err := emptyShareOpts.ToShare()
		require.EqualError(t, err, "empty share")
		require.Nil(t, share)
	})

	t.Run("ShareOptions w/o committee", func(t *testing.T) {
		emptyShareOpts := ShareOptions{
			ShareKey:  sk.SerializeToHexStr(),
			PublicKey: sk.GetPublicKey().SerializeToHexStr(),
			NodeID:    1,
		}
		share, err := emptyShareOpts.ToShare()
		require.EqualError(t, err, "empty share")
		require.Nil(t, share)
	})
}
