package validator

import (
	"encoding/hex"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/types"
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
	b, err := validatorShare.Encode()
	require.NoError(t, err)

	obj1 := basedb.Obj{
		Key:   validatorShare.ValidatorPubKey,
		Value: b,
	}
	v1 := &types.SSVShare{}
	require.NoError(t, v1.Decode(obj1.Value))
	require.NotNil(t, v1.ValidatorPubKey)
	require.Equal(t, hex.EncodeToString(v1.ValidatorPubKey), hex.EncodeToString(validatorShare.ValidatorPubKey))
	require.NotNil(t, v1.Committee)
	require.NotNil(t, v1.OperatorID)
	require.Equal(t, v1.Stats, validatorShare.Stats)
	require.Equal(t, v1.OwnerAddress, validatorShare.OwnerAddress)
	require.Equal(t, v1.Operators, validatorShare.Operators)
	require.Equal(t, v1.OperatorIDs, validatorShare.OperatorIDs)
	require.Equal(t, v1.Liquidated, validatorShare.Liquidated)
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

	validatorShareByKey, found, err := collection.GetValidatorShare(validatorShare.ValidatorPubKey)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, hex.EncodeToString(validatorShareByKey.ValidatorPubKey), hex.EncodeToString(validatorShare.ValidatorPubKey))

	validators, err := collection.GetAllValidatorShares()
	require.NoError(t, err)
	require.EqualValues(t, 2, len(validators))

	require.NoError(t, collection.DeleteValidatorShare(validatorShare.ValidatorPubKey))
	_, found, err = collection.GetValidatorShare(validatorShare.ValidatorPubKey)
	require.NoError(t, err)
	require.False(t, found)
}

func generateRandomValidatorShare(splitKeys map[uint64]*bls.SecretKey) (*types.SSVShare, *bls.SecretKey) {
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	ibftCommittee := []*spectypes.Operator{
		{
			OperatorID: 1,
			PubKey:     splitKeys[1].Serialize(),
		},
		{
			OperatorID: 2,
			PubKey:     splitKeys[2].Serialize(),
		},
		{
			OperatorID: 3,
			PubKey:     splitKeys[3].Serialize(),
		},
		{
			OperatorID: 4,
			PubKey:     splitKeys[4].Serialize(),
		},
	}

	return &types.SSVShare{
		Share: spectypes.Share{
			OperatorID:      1,
			ValidatorPubKey: sk.GetPublicKey().Serialize(),
			Committee:       ibftCommittee,
		},
	}, &sk
}
