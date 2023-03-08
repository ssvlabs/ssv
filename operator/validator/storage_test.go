package validator

import (
	"encoding/hex"
	"testing"

	"github.com/bloxapp/ssv/logging"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
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

	obj := basedb.Obj{
		Key:   validatorShare.ValidatorPubKey,
		Value: b,
	}
	v1 := &types.SSVShare{}
	require.NoError(t, v1.Decode(obj.Value))
	require.NotNil(t, v1.ValidatorPubKey)
	require.Equal(t, hex.EncodeToString(v1.ValidatorPubKey), hex.EncodeToString(validatorShare.ValidatorPubKey))
	require.NotNil(t, v1.Committee)
	require.NotNil(t, v1.OperatorID)
	require.Equal(t, v1.BeaconMetadata, validatorShare.BeaconMetadata)
	require.Equal(t, v1.OwnerAddress, validatorShare.OwnerAddress)
	require.Equal(t, v1.Liquidated, validatorShare.Liquidated)
}

func TestSaveAndGetValidatorStorage(t *testing.T) {
	logger := logging.TestLogger(t)
	options := basedb.Options{
		Type: "badger-memory",
		Path: "",
	}

	db, err := storage.GetStorageFactory(logger, options)
	require.NoError(t, err)
	defer db.Close(logger)

	collection := NewCollection(CollectionOptions{
		DB: db,
	})

	threshold.Init()
	const keysCount = 4

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	validatorShare, _ := generateRandomValidatorShare(splitKeys)
	require.NoError(t, collection.SaveValidatorShare(logger, validatorShare))

	validatorShare2, _ := generateRandomValidatorShare(splitKeys)
	require.NoError(t, collection.SaveValidatorShare(logger, validatorShare2))

	validatorShareByKey, found, err := collection.GetValidatorShare(validatorShare.ValidatorPubKey)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, hex.EncodeToString(validatorShareByKey.ValidatorPubKey), hex.EncodeToString(validatorShare.ValidatorPubKey))

	validators, err := collection.GetAllValidatorShares(logger)
	require.NoError(t, err)
	require.EqualValues(t, 2, len(validators))

	require.NoError(t, collection.DeleteValidatorShare(validatorShare.ValidatorPubKey))
	_, found, err = collection.GetValidatorShare(validatorShare.ValidatorPubKey)
	require.NoError(t, err)
	require.False(t, found)
}

func generateRandomValidatorShare(splitKeys map[uint64]*bls.SecretKey) (*types.SSVShare, *bls.SecretKey) {
	threshold.Init()

	sk1 := bls.SecretKey{}
	sk1.SetByCSPRNG()

	sk2 := bls.SecretKey{}
	sk2.SetByCSPRNG()

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
			ValidatorPubKey: sk1.GetPublicKey().Serialize(),
			SharePubKey:     sk2.GetPublicKey().Serialize(),
			Committee:       ibftCommittee,
			Quorum:          3,
			PartialQuorum:   2,
			DomainType:      types.GetDefaultDomain(),
			Graffiti:        nil,
		},
		Metadata: types.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Balance: 1,
				Status:  2,
				Index:   3,
			},
			OwnerAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
			Liquidated:   true,
		},
	}, &sk1
}
