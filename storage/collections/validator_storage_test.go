package collections

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/storage/kv"
)

func TestValidatorSerializer(t *testing.T) {
	validatorShare := generateRandomValidatorShare()
	b, err := validatorShare.Serialize()
	require.NoError(t, err)

	obj := storage.Obj{
		Key:   validatorShare.ValidatorPK.Serialize(),
		Value: b,
	}
	v, err := validatorShare.Deserialize(obj)
	require.NoError(t, err)
	require.NotNil(t, v.ValidatorPK)
	require.Equal(t, v.ValidatorPK.SerializeToHexStr(), validatorShare.ValidatorPK.SerializeToHexStr())
	require.NotNil(t, v.ShareKey)
	require.Equal(t, v.ShareKey.SerializeToHexStr(), validatorShare.ShareKey.SerializeToHexStr())
	require.NotNil(t, v.Committee)
	require.NotNil(t, v.NodeID)
}

func TestSaveAndGetValidatorStorage(t *testing.T) {
	db, err := kv.New("./data/db", *zap.L(), &kv.Options{InMemory: true})
	require.NoError(t, err)
	defer db.Close()

	validatorStorage := ValidatorStorage{
		prefix: []byte("validator-"),
		db:     db,
		logger: nil,
	}

	validatorShare := generateRandomValidatorShare()
	require.NoError(t, validatorStorage.SaveValidatorShare(&validatorShare))

	validatorShare2 := generateRandomValidatorShare()
	require.NoError(t, validatorStorage.SaveValidatorShare(&validatorShare2))

	validatorShareByKey, err := validatorStorage.GetValidatorsShare(validatorShare.ValidatorPK.Serialize())
	require.NoError(t, err)
	require.EqualValues(t, validatorShareByKey.ValidatorPK.SerializeToHexStr(), validatorShare.ValidatorPK.SerializeToHexStr())

	validators, err := validatorStorage.GetAllValidatorShares()
	require.NoError(t, err)
	require.EqualValues(t, len(validators), 2)
}

func generateRandomValidatorShare() ValidatorShare {
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

	return ValidatorShare{
		NodeID:      1,
		ValidatorPK: sk.GetPublicKey(),
		ShareKey:    &sk,
		Committee:   ibftCommittee,
	}
}
