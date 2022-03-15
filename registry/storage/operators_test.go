package storage

import (
	"fmt"

	"github.com/bloxapp/ssv/fixtures"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"strings"
	"testing"
)

func TestStorage_SaveAndGetOperatorInformation(t *testing.T) {
	storage, done := newStorageForTest()
	require.NotNil(t, storage)
	defer done()

	operatorInfo := OperatorInformation{
		PublicKey:    string(fixtures.RefPk[:]),
		Name:         "my_operator",
		OwnerAddress: common.Address{},
	}

	t.Run("get non-existing operator", func(t *testing.T) {
		nonExistingOperator, found, _ := storage.GetOperatorInformation("dummyPK")
		require.Nil(t, nonExistingOperator)
		require.False(t, found)
	})

	t.Run("create and get operator", func(t *testing.T) {
		err := storage.SaveOperatorInformation(&operatorInfo)
		require.NoError(t, err)
		operatorInfoFromDB, _, err := storage.GetOperatorInformation(operatorInfo.PublicKey)
		require.NoError(t, err)
		require.Equal(t, "my_operator", operatorInfoFromDB.Name)
		require.Equal(t, int64(0), operatorInfoFromDB.Index)
		require.True(t, strings.EqualFold(operatorInfoFromDB.PublicKey, operatorInfo.PublicKey))
	})

	t.Run("create existing operator", func(t *testing.T) {
		oi := OperatorInformation{
			PublicKey:    "010101010101",
			Name:         "my_operator1",
			OwnerAddress: common.Address{},
		}
		err := storage.SaveOperatorInformation(&oi)
		require.NoError(t, err)
		oiDup := OperatorInformation{
			PublicKey:    "010101010101",
			Name:         "my_operator2",
			OwnerAddress: common.Address{},
		}
		err = storage.SaveOperatorInformation(&oiDup)
		require.NoError(t, err)
		require.Equal(t, oiDup.Index, oi.Index)
	})

	t.Run("create and get multiple operators", func(t *testing.T) {
		i, err := storage.(*operatorsStorage).nextIndex(operatorsPrefix)
		require.NoError(t, err)

		ois := []OperatorInformation{
			{
				PublicKey:    "01010101",
				Name:         "my_operator1",
				OwnerAddress: common.Address{},
			}, {
				PublicKey:    "02020202",
				Name:         "my_operator2",
				OwnerAddress: common.Address{},
			}, {
				PublicKey:    "03030303",
				Name:         "my_operator3",
				OwnerAddress: common.Address{},
			},
		}
		for _, oi := range ois {
			err = storage.SaveOperatorInformation(&oi)
			require.NoError(t, err)
		}

		for _, oi := range ois {
			operatorInfoFromDB, _, err := storage.GetOperatorInformation(oi.PublicKey)
			require.NoError(t, err)
			require.Equal(t, oi.Name, operatorInfoFromDB.Name)
			require.Equal(t, i, operatorInfoFromDB.Index)
			require.Equal(t, operatorInfoFromDB.PublicKey, oi.PublicKey)
			i++
		}
	})
}

func TestStorage_ListOperators(t *testing.T) {
	storage, done := newStorageForTest()
	require.NotNil(t, storage)
	defer done()

	n := 5
	for i := 0; i < n; i++ {
		pk, _, err := rsaencryption.GenerateKeys()
		require.NoError(t, err)
		operator := OperatorInformation{
			PublicKey: string(pk),
			Name:      fmt.Sprintf("operator-%d", i+1),
		}
		err = storage.SaveOperatorInformation(&operator)
		require.NoError(t, err)
	}

	operators, err := storage.ListOperators(0, 0)
	require.NoError(t, err)
	require.Equal(t, 1, len(operators))
	for _, operator := range operators {
		require.True(t, strings.Contains(operator.Name, "operator-"))
	}
}

func newStorageForTest() (OperatorsCollection, func()) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := NewOperatorsStorage(db, logger, []byte("test"))
	return s, func() {
		db.Close()
	}
}
