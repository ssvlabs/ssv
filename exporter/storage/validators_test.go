package storage

import (
	"encoding/hex"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStorage_SaveAndGetValidatorInformation(t *testing.T) {
	storage, done := newStorageForTest()
	require.NotNil(t, storage)
	defer done()

	validatorInfo := ValidatorInformation{
		PublicKey: "kds6E6tCimycIOcQRIjLaWGr6rYOVs9LoZnu07X2587WcOywZslwTcL6kxM3kjgc",
		Operators: []OperatorNodeLink{
			{
				ID:        1,
				PublicKey: hex.EncodeToString([]byte{2, 2, 2, 2}),
			},
			{
				ID:        2,
				PublicKey: hex.EncodeToString([]byte{2, 2, 2, 2}),
			},
			{
				ID:        3,
				PublicKey: hex.EncodeToString([]byte{3, 3, 3, 3}),
			},
			{
				ID:        4,
				PublicKey: hex.EncodeToString([]byte{4, 4, 4, 4}),
			},
		},
	}

	t.Run("get non-existing validator", func(t *testing.T) {
		nonExistingOperator, found, _ := storage.GetValidatorInformation("dummyPK")
		require.Nil(t, nonExistingOperator)
		require.False(t, found)
	})

	t.Run("create and get validator", func(t *testing.T) {
		err := storage.SaveValidatorInformation(&validatorInfo)
		require.NoError(t, err)
		validatorInfoFromDB, _, err := storage.GetValidatorInformation(validatorInfo.PublicKey)
		require.NoError(t, err)
		require.Equal(t, "kds6E6tCimycIOcQRIjLaWGr6rYOVs9LoZnu07X2587WcOywZslwTcL6kxM3kjgc",
			validatorInfoFromDB.PublicKey)
		require.Equal(t, int64(0), validatorInfoFromDB.Index)
		require.Equal(t, 4, len(validatorInfoFromDB.Operators))
	})

	t.Run("create existing validator", func(t *testing.T) {
		vi := ValidatorInformation{
			PublicKey: "82e9b36feb8147d3f82c1a03ba246d4a63ac1ce0b1dabbb6991940a06401ab46fb4afbf971a3c145fdad2d4bddd30e12",
			Operators: validatorInfo.Operators[:],
		}
		err := storage.SaveValidatorInformation(&vi)
		require.NoError(t, err)
		viDup := ValidatorInformation{
			PublicKey: vi.PublicKey,
			Operators: validatorInfo.Operators[1:],
		}
		err = storage.SaveValidatorInformation(&viDup)
		require.NoError(t, err)
		require.Equal(t, viDup.Index, vi.Index)
	})

	t.Run("create and get multiple validators", func(t *testing.T) {
		i, err := storage.(*exporterStorage).nextIndex(validatorsPrefix)
		require.NoError(t, err)

		vis := []ValidatorInformation{
			{
				PublicKey: "8111b36feb8147d3f82c1a0",
				Operators: validatorInfo.Operators[:],
			}, {
				PublicKey: "8222b36feb8147d3f82c1a0",
				Operators: validatorInfo.Operators[:],
			}, {
				PublicKey: "8333b36feb8147d3f82c1a0",
				Operators: validatorInfo.Operators[:],
			},
		}
		for _, vi := range vis {
			err = storage.SaveValidatorInformation(&vi)
			require.NoError(t, err)
		}

		for _, vi := range vis {
			validatorInfoFromDB, _, err := storage.GetValidatorInformation(vi.PublicKey)
			require.NoError(t, err)
			require.Equal(t, i, validatorInfoFromDB.Index)
			require.Equal(t, validatorInfoFromDB.PublicKey, vi.PublicKey)
			i++
		}
	})
}

func TestStorage_ListValidators(t *testing.T) {
	storage, done := newStorageForTest()
	require.NotNil(t, storage)
	defer done()

	n := 5
	for i := 0; i < n; i++ {
		pk, _, err := rsaencryption.GenerateKeys()
		require.NoError(t, err)
		validator := ValidatorInformation{
			PublicKey: hex.EncodeToString(pk),
			Operators: []OperatorNodeLink{},
		}
		err = storage.SaveValidatorInformation(&validator)
		require.NoError(t, err)
	}

	validators, err := storage.ListValidators(0, 0)
	require.NoError(t, err)
	require.Equal(t, 5, len(validators))
}

func TestStorage_UpdateValidator(t *testing.T) {
	storage, done := newStorageForTest()
	require.NotNil(t, storage)
	defer done()

	pk, _, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)
	validator := ValidatorInformation{
		PublicKey: hex.EncodeToString(pk),
		Operators: []OperatorNodeLink{},
		Metadata: &beacon.ValidatorMetadata{
			Status:  v1.ValidatorStateUnknown,
			Balance: 10000,
			Index:   spec.ValidatorIndex(12),
		},
	}
	err = storage.SaveValidatorInformation(&validator)
	require.NoError(t, err)

	err = storage.UpdateValidatorMetadata(validator.PublicKey, &beacon.ValidatorMetadata{
		Status:  v1.ValidatorStateExitedSlashed,
		Balance: 1000001,
		Index:   spec.ValidatorIndex(1),
	})
	require.NoError(t, err)

	// get
	gotVal, _, err := storage.GetValidatorInformation(hex.EncodeToString(pk))
	require.NoError(t, err)
	require.Len(t, gotVal.Operators, 0)
	require.EqualValues(t, 7, gotVal.Metadata.Status)
	require.EqualValues(t, 1000001, gotVal.Metadata.Balance)
	require.EqualValues(t, 1, gotVal.Metadata.Index)
}
