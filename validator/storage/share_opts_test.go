package storage

import (
	"testing"

	"github.com/bloxapp/ssv/utils/blskeygen"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/stretchr/testify/require"
)

func TestShareOptionsToShare(t *testing.T) {
	threshold.Init()

	const keysCount = 4
	refSK, _ := blskeygen.GenBLSKeyPair()
	splitKeys, err := threshold.Create(refSK.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	origShare, sk := generateRandomValidatorShare(splitKeys)

	shareOpts := ShareOptions{
		ShareKey:     sk.SerializeToHexStr(),
		PublicKey:    sk.GetPublicKey().SerializeToHexStr(),
		NodeID:       1,
		Committee:    map[string]int{},
		OwnerAddress: "0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e",
	}

	t.Run("valid ShareOptions", func(t *testing.T) {
		for i := 1; i <= keysCount; i++ {
			key := splitKeys[uint64(i)]
			shareOpts.Committee[key.GetHexString()] = i
		}
		share, err := shareOpts.ToShare()
		require.NoError(t, err)
		require.NotNil(t, share)
		require.Equal(t, len(share.Committee), keysCount)
		require.Equal(t, share.PublicKey.GetHexString(), origShare.PublicKey.GetHexString())
		require.Equal(t, share.OwnerAddress, origShare.OwnerAddress)
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
