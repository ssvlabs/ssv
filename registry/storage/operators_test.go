package storage

import (
	"fmt"
	"github.com/bloxapp/ssv/logging"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/blskeygen"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

func TestStorage_SaveAndGetOperatorInformation(t *testing.T) {
	logger := logging.TestLogger(t)
	storage, done := newStorageForTest(logger)
	require.NotNil(t, storage)
	defer done()

	_, pk := blskeygen.GenBLSKeyPair()

	operatorData := OperatorData{
		PublicKey:    string(pk.Serialize()),
		Name:         "my_operator",
		OwnerAddress: common.Address{},
		Index:        1,
	}

	t.Run("get non-existing operator", func(t *testing.T) {
		nonExistingOperator, found, err := storage.GetOperatorData(1)
		require.NoError(t, err)
		require.Nil(t, nonExistingOperator)
		require.False(t, found)
	})

	t.Run("get non-existing operator by public key", func(t *testing.T) {
		nonExistingOperator, found, err := storage.GetOperatorDataByPubKey(logger, "dummyPK")
		require.NoError(t, err)
		require.Nil(t, nonExistingOperator)
		require.False(t, found)
	})

	t.Run("create and get operator", func(t *testing.T) {
		err := storage.SaveOperatorData(logger, &operatorData)
		require.NoError(t, err)
		operatorDataFromDB, found, err := storage.GetOperatorData(operatorData.Index)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "my_operator", operatorDataFromDB.Name)
		require.Equal(t, operatorData.Index, operatorDataFromDB.Index)
		require.True(t, strings.EqualFold(operatorData.PublicKey, operatorDataFromDB.PublicKey))
		operatorDataFromDBCmp, found, err := storage.GetOperatorDataByPubKey(logger, operatorData.PublicKey)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, operatorDataFromDB.Name, operatorDataFromDBCmp.Name)
		require.Equal(t, operatorDataFromDB.Index, operatorDataFromDBCmp.Index)
		require.True(t, strings.EqualFold(operatorDataFromDB.PublicKey, operatorDataFromDBCmp.PublicKey))
	})

	t.Run("create existing operator", func(t *testing.T) {
		od := OperatorData{
			PublicKey:    "010101010101",
			Name:         "my_operator1",
			OwnerAddress: common.Address{},
			Index:        1,
		}
		err := storage.SaveOperatorData(logger, &od)
		require.NoError(t, err)
		odDup := OperatorData{
			PublicKey:    "010101010101",
			Name:         "my_operator2",
			OwnerAddress: common.Address{},
			Index:        1,
		}
		err = storage.SaveOperatorData(logger, &odDup)
		require.NoError(t, err)
		odFromDb, found, err := storage.GetOperatorData(od.Index)
		require.NoError(t, err)
		require.True(t, found)
		require.False(t, strings.EqualFold(odDup.Name, odFromDb.Name))
	})

	t.Run("create and get multiple operators", func(t *testing.T) {
		ods := []OperatorData{
			{
				PublicKey:    "01010101",
				Name:         "my_operator1",
				OwnerAddress: common.Address{},
				Index:        10,
			}, {
				PublicKey:    "02020202",
				Name:         "my_operator2",
				OwnerAddress: common.Address{},
				Index:        11,
			}, {
				PublicKey:    "03030303",
				Name:         "my_operator3",
				OwnerAddress: common.Address{},
				Index:        12,
			},
		}
		for _, od := range ods {
			odCopy := od
			require.NoError(t, storage.SaveOperatorData(logger, &odCopy))
		}

		for _, od := range ods {
			operatorDataFromDB, found, err := storage.GetOperatorData(od.Index)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, od.Name, operatorDataFromDB.Name)
			require.Equal(t, od.Index, operatorDataFromDB.Index)
			require.Equal(t, od.PublicKey, operatorDataFromDB.PublicKey)
		}
	})
}

func TestStorage_ListOperators(t *testing.T) {
	logger := logging.TestLogger(t)
	storage, done := newStorageForTest(logger)
	require.NotNil(t, storage)
	defer done()

	n := 5
	for i := 0; i < n; i++ {
		pk, _, err := rsaencryption.GenerateKeys()
		require.NoError(t, err)
		operator := OperatorData{
			PublicKey: string(pk),
			Name:      fmt.Sprintf("operator-%d", i+1),
			Index:     uint64(i),
		}
		err = storage.SaveOperatorData(logger, &operator)
		require.NoError(t, err)
	}

	t.Run("successfully list operators", func(t *testing.T) {
		operators, err := storage.ListOperators(logger, 0, 0)
		require.NoError(t, err)
		require.Equal(t, n, len(operators))
		for _, operator := range operators {
			require.True(t, strings.HasPrefix(operator.Name, "operator-"))
		}
	})

	t.Run("successfully list operators in range", func(t *testing.T) {
		operators, err := storage.ListOperators(logger, 1, 2)
		require.NoError(t, err)
		require.Equal(t, 2, len(operators))
	})
}

func newStorageForTest(logger *zap.Logger) (OperatorsCollection, func()) {
	db, err := ssvstorage.GetStorageFactory(logger, basedb.Options{
		Type: "badger-memory",
		Path: "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := NewOperatorsStorage(db, []byte("test"))
	return s, func() {
		db.Close(logger)
	}
}
