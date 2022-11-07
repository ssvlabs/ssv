package validator

import (
	"encoding/hex"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
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
	v1 := &spectypes.Share{}
	require.NoError(t, v1.Decode(obj1.Value))
	require.NotNil(t, v1.ValidatorPubKey)
	require.Equal(t, hex.EncodeToString(v1.ValidatorPubKey), hex.EncodeToString(validatorShare.ValidatorPubKey))
	require.NotNil(t, v1.Committee)
	require.NotNil(t, v1.OperatorID)

	shareMetadata, _ := generateRandomShareMetadata()
	b, err = shareMetadata.Serialize()
	require.NoError(t, err)

	obj2 := basedb.Obj{
		Key:   shareMetadata.PublicKey.Serialize(),
		Value: b,
	}
	v2, err := shareMetadata.Deserialize(obj2.Key, obj2.Value)
	require.NoError(t, err)
	require.NotNil(t, v2.PublicKey)
	require.Equal(t, v2.PublicKey.SerializeToHexStr(), shareMetadata.PublicKey.SerializeToHexStr())
	require.Equal(t, shareMetadata.Stats, v2.Stats)
	require.Equal(t, shareMetadata.OwnerAddress, v2.OwnerAddress)
	require.Equal(t, shareMetadata.Operators, v2.Operators)
	require.Equal(t, shareMetadata.OperatorIDs, v2.OperatorIDs)
	require.Equal(t, shareMetadata.Liquidated, v2.Liquidated)
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

	shareMetadata, _ := generateRandomShareMetadata()
	require.NoError(t, collection.SaveShareMetadata(shareMetadata))

	shareMetadata2, _ := generateRandomShareMetadata()
	require.NoError(t, collection.SaveShareMetadata(shareMetadata2))

	validatorShareByKey, found, err := collection.GetValidatorShare(validatorShare.ValidatorPubKey)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, hex.EncodeToString(validatorShareByKey.ValidatorPubKey), hex.EncodeToString(validatorShare.ValidatorPubKey))

	shareMetadataByKey, found, err := collection.GetShareMetadata(shareMetadata.PublicKey.Serialize())
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, hex.EncodeToString(shareMetadataByKey.PublicKey.Serialize()), hex.EncodeToString(shareMetadata.PublicKey.Serialize()))

	validators, err := collection.GetAllValidatorShares()
	require.NoError(t, err)
	require.EqualValues(t, 2, len(validators))

	metadataList, err := collection.GetAllShareMetadata()
	require.NoError(t, err)
	require.EqualValues(t, 2, len(metadataList))

	require.NoError(t, collection.DeleteValidatorShare(validatorShare.ValidatorPubKey))
	_, found, err = collection.GetValidatorShare(validatorShare.ValidatorPubKey)
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, collection.DeleteShareMetadata(shareMetadata.PublicKey.Serialize()))
	_, found, err = collection.GetShareMetadata(shareMetadata.PublicKey.Serialize())
	require.NoError(t, err)
	require.False(t, found)
}

func generateRandomValidatorShare(splitKeys map[uint64]*bls.SecretKey) (*spectypes.Share, *bls.SecretKey) {
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

	return &spectypes.Share{
		OperatorID:      1,
		ValidatorPubKey: sk.GetPublicKey().Serialize(),
		Committee:       ibftCommittee,
	}, &sk
}

func generateRandomShareMetadata() (*types.ShareMetadata, *bls.SecretKey) {
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	return &types.ShareMetadata{
		PublicKey: sk.GetPublicKey(),
		Stats: &beacon.ValidatorMetadata{
			Balance: 1,
			Status:  2,
			Index:   3,
		},
		OwnerAddress: "0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e",
		Operators: [][]byte{
			{1, 1, 1, 1},
			{2, 2, 2, 2},
		},
		OperatorIDs: []uint64{1, 2, 3, 4},
		Liquidated:  true,
	}, &sk
}
