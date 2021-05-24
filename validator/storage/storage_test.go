package storage

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/threshold"

	"github.com/herumi/bls-eth-go-binary/bls"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestValidatorSerializer(t *testing.T) {
	validatorShare := generateRandomValidatorShare()
	b, err := validatorShare.Serialize()
	require.NoError(t, err)

	obj := basedb.Obj{
		Key:   validatorShare.PublicKey.Serialize(),
		Value: b,
	}
	v, err := validatorShare.Deserialize(obj)
	require.NoError(t, err)
	require.NotNil(t, v.PublicKey)
	require.Equal(t, v.PublicKey.SerializeToHexStr(), validatorShare.PublicKey.SerializeToHexStr())
	require.NotNil(t, v.ShareKey)
	require.Equal(t, v.ShareKey.SerializeToHexStr(), validatorShare.ShareKey.SerializeToHexStr())
	require.NotNil(t, v.Committee)
	require.NotNil(t, v.NodeID)
}

func TestSaveAndGetValidatorStorage(t *testing.T) {
	options:= basedb.Options{
		Type: "badger-memory",
		Logger: zap.L(),
		Path: "",
	}

	db, err := storage.GetStorageFactory(options)
	require.NoError(t, err)

	collection := NewCollection(CollectionOptions{
		DB:     &db,
		Logger: options.Logger,
	})

	validatorShare := generateRandomValidatorShare()
	require.NoError(t, collection.SaveValidatorShare(&validatorShare))

	validatorShare2 := generateRandomValidatorShare()
	require.NoError(t, collection.SaveValidatorShare(&validatorShare2))

	validatorShareByKey, err := collection.GetValidatorsShare(validatorShare.PublicKey.Serialize())
	require.NoError(t, err)
	require.EqualValues(t, validatorShareByKey.PublicKey.SerializeToHexStr(), validatorShare.PublicKey.SerializeToHexStr())

	validators, err := collection.GetAllValidatorsShare()
	require.NoError(t, err)
	require.EqualValues(t, len(validators), 2)
}

func TestShareOptionsToShare(t *testing.T) {
	threshold.Init()

	origShare := generateRandomValidatorShare()

	shareOpts := ShareOptions{
		ShareKey: origShare.ShareKey.SerializeToHexStr(),
		PublicKey: origShare.ShareKey.GetPublicKey().SerializeToHexStr(),
		NodeID: 1,
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
		require.True(t, len(share.Committee[1].Sk) > 0)
		require.True(t, len(share.Committee[2].Sk) == 0)
		require.Equal(t, share.PublicKey.GetHexString(), origShare.PublicKey.GetHexString())
		require.Equal(t, share.ShareKey.GetHexString(), origShare.ShareKey.GetHexString())
	})

	t.Run("empty ShareOptions", func(t *testing.T) {
		emptyShareOpts := ShareOptions{}
		share, err := emptyShareOpts.ToShare()
		require.EqualError(t, err, "empty share")
		require.Nil(t, share)
	})

	t.Run("ShareOptions w/o committee", func(t *testing.T) {
		emptyShareOpts := ShareOptions{
			ShareKey: origShare.ShareKey.SerializeToHexStr(),
			PublicKey: origShare.ShareKey.GetPublicKey().SerializeToHexStr(),
			NodeID: 1,
		}
		share, err := emptyShareOpts.ToShare()
		require.EqualError(t, err, "empty share")
		require.Nil(t, share)
	})
}

func generateRandomValidatorShare() Share {
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	ibftCommittee := map[uint64]*proto.Node{
		1: {
			IbftId: 1,
			Pk:     fixtures.RefSplitSharesPubKeys[0],
			Sk:     sk.Serialize(),
		},
		2: {
			IbftId: 2,
			Pk:     fixtures.RefSplitSharesPubKeys[1],
		},
		3: {
			IbftId: 3,
			Pk:     fixtures.RefSplitSharesPubKeys[2],
		},
		4: {
			IbftId: 4,
			Pk:     fixtures.RefSplitSharesPubKeys[3],
		},
	}

	return Share{
		NodeID:      1,
		PublicKey: sk.GetPublicKey(),
		ShareKey:    &sk,
		Committee:   ibftCommittee,
	}
}
