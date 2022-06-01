package validator

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/threshold"
)

func TestValidatorSerializer(t *testing.T) {
	threshold.Init()

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	const keysCount = 4

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	validatorShare, _ := generateRandomValidatorShare(splitKeys)
	b, err := validatorShare.Serialize()
	require.NoError(t, err)

	obj := basedb.Obj{
		Key:   validatorShare.PublicKey.Serialize(),
		Value: b,
	}
	v, err := validatorShare.Deserialize(obj.Key, obj.Value)
	require.NoError(t, err)
	require.NotNil(t, v.PublicKey)
	require.Equal(t, v.PublicKey.SerializeToHexStr(), validatorShare.PublicKey.SerializeToHexStr())
	require.NotNil(t, v.Committee)
	require.NotNil(t, v.NodeID)
}

func TestSaveAndGetValidatorStorage(t *testing.T) {
	options := basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	}

	db, err := storage.GetStorageFactory(options)
	require.NoError(t, err)
	defer db.Close()

	collection := NewCollection(CollectionOptions{
		DB:     db,
		Logger: options.Logger,
	})

	threshold.Init()
	const keysCount = 4

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	validatorShare, _ := generateRandomValidatorShare(splitKeys)
	require.NoError(t, collection.SaveValidatorShare(validatorShare))

	validatorShare2, _ := generateRandomValidatorShare(splitKeys)
	require.NoError(t, collection.SaveValidatorShare(validatorShare2))

	validatorShareByKey, found, err := collection.GetValidatorShare(validatorShare.PublicKey.Serialize())
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, validatorShareByKey.PublicKey.SerializeToHexStr(), validatorShare.PublicKey.SerializeToHexStr())

	validators, err := collection.GetAllValidatorShares()
	require.NoError(t, err)
	require.EqualValues(t, 2, len(validators))
}

func generateRandomValidatorShare(splitKeys map[uint64]*bls.SecretKey) (*beacon.Share, *bls.SecretKey) {
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	ibftCommittee := map[message.OperatorID]*beacon.Node{
		1: {
			IbftID: 1,
			Pk:     splitKeys[1].Serialize(),
		},
		2: {
			IbftID: 2,
			Pk:     splitKeys[2].Serialize(),
		},
		3: {
			IbftID: 3,
			Pk:     splitKeys[3].Serialize(),
		},
		4: {
			IbftID: 4,
			Pk:     splitKeys[4].Serialize(),
		},
	}

	return &beacon.Share{
		NodeID:       1,
		PublicKey:    sk.GetPublicKey(),
		Committee:    ibftCommittee,
		OwnerAddress: "0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e",
	}, &sk
}
