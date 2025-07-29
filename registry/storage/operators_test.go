package storage_test

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"testing"

	"github.com/ssvlabs/ssv/registry/storage"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/logging"

	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestStorage_SaveAndGetOperatorData(t *testing.T) {
	logger := logging.TestLogger(t)
	storageCollection, done := newOperatorStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	operatorData := storage.OperatorData{
		PublicKey:    base64.StdEncoding.EncodeToString([]byte("samplePublicKey")),
		OwnerAddress: common.Address{},
		ID:           1,
	}

	t.Run("get non-existing operator", func(t *testing.T) {
		nonExistingOperator, found, err := storageCollection.GetOperatorData(nil, 1)
		require.NoError(t, err)
		require.Nil(t, nonExistingOperator)
		require.False(t, found)
	})

	t.Run("get non-existing operator by public key", func(t *testing.T) {
		nonExistingOperator, found, err := storageCollection.GetOperatorDataByPubKey(nil, "dummyPK")
		require.NoError(t, err)
		require.Nil(t, nonExistingOperator)
		require.False(t, found)
	})

	t.Run("create and get operator", func(t *testing.T) {
		_, err := storageCollection.SaveOperatorData(nil, &operatorData)
		require.NoError(t, err)
		operatorDataFromDB, found, err := storageCollection.GetOperatorData(nil, operatorData.ID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, operatorData.ID, operatorDataFromDB.ID)
		require.Equal(t, operatorData.PublicKey, operatorDataFromDB.PublicKey)
		operatorDataFromDBCmp, found, err := storageCollection.GetOperatorDataByPubKey(nil, operatorData.PublicKey)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, operatorDataFromDB.ID, operatorDataFromDBCmp.ID)
		require.Equal(t, operatorDataFromDB.PublicKey, operatorDataFromDBCmp.PublicKey)
	})

	t.Run("create existing operator", func(t *testing.T) {
		od := storage.OperatorData{
			PublicKey:    base64.StdEncoding.EncodeToString([]byte("010101010101")),
			OwnerAddress: common.Address{},
			ID:           1,
		}
		_, err := storageCollection.SaveOperatorData(nil, &od)
		require.NoError(t, err)
		odDup := storage.OperatorData{
			PublicKey:    base64.StdEncoding.EncodeToString([]byte("010101010101")),
			OwnerAddress: common.Address{},
			ID:           1,
		}
		_, err = storageCollection.SaveOperatorData(nil, &odDup)
		require.NoError(t, err)
		_, found, err := storageCollection.GetOperatorData(nil, od.ID)
		require.NoError(t, err)
		require.True(t, found)
	})

	t.Run("check operator exists", func(t *testing.T) {
		found, err := storageCollection.OperatorsExist(nil, []spectypes.OperatorID{operatorData.ID})
		require.NoError(t, err)
		require.True(t, found)
	})

	t.Run("create and get multiple operators", func(t *testing.T) {
		ods := []storage.OperatorData{
			{
				PublicKey:    base64.StdEncoding.EncodeToString([]byte("01010101")),
				OwnerAddress: common.Address{},
				ID:           10,
			}, {
				PublicKey:    base64.StdEncoding.EncodeToString([]byte("02020202")),
				OwnerAddress: common.Address{},
				ID:           11,
			}, {
				PublicKey:    base64.StdEncoding.EncodeToString([]byte("03030303")),
				OwnerAddress: common.Address{},
				ID:           12,
			},
		}
		for _, od := range ods {
			odCopy := od
			_, err := storageCollection.SaveOperatorData(nil, &odCopy)
			require.NoError(t, err)
		}

		for _, od := range ods {
			operatorDataFromDB, found, err := storageCollection.GetOperatorData(nil, od.ID)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, od.ID, operatorDataFromDB.ID)
			require.Equal(t, od.PublicKey, operatorDataFromDB.PublicKey)
		}
	})
}

func TestStorage_ListOperators(t *testing.T) {
	logger := logging.TestLogger(t)
	storageCollection, done := newOperatorStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	n := 5
	for i := 0; i < n; i++ {
		randNum := rand.Int()
		operator := storage.OperatorData{
			PublicKey: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("samplePublicKey%d", randNum))),
			ID:        spectypes.OperatorID(i),
		}
		_, err := storageCollection.SaveOperatorData(nil, &operator)
		require.NoError(t, err)
	}

	t.Run("successfully list operators", func(t *testing.T) {
		operators, err := storageCollection.ListOperators(nil, 0, 0)
		require.NoError(t, err)
		require.Equal(t, n, len(operators))
	})

	t.Run("successfully list operators in range", func(t *testing.T) {
		operators, err := storageCollection.ListOperators(nil, 1, 2)
		require.NoError(t, err)
		require.Equal(t, 2, len(operators))
	})
}

func TestStorage_DeleteOperatorAndDropOperators(t *testing.T) {
	logger := logging.TestLogger(t)
	storageCollection, done := newOperatorStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	// prepare storage test fixture
	n := 5
	for i := 0; i < n; i++ {
		randNum := rand.Int()
		operator := storage.OperatorData{
			PublicKey: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("samplePublicKey%d", randNum))),
			ID:        spectypes.OperatorID(i),
		}
		_, err := storageCollection.SaveOperatorData(nil, &operator)
		require.NoError(t, err)
	}

	t.Run("DeleteOperator_OperatorNotExists", func(t *testing.T) {
		err := storageCollection.DeleteOperatorData(nil, spectypes.OperatorID(12345))
		require.NoError(t, err)
	})

	t.Run("DeleteOperator_OperatorExists", func(t *testing.T) {
		err := storageCollection.DeleteOperatorData(nil, spectypes.OperatorID(1))
		require.NoError(t, err)

		operators, err := storageCollection.ListOperators(nil, 0, 0)
		require.NoError(t, err)
		require.Equal(t, n-1, len(operators))
	})

	t.Run("DropRecipients", func(t *testing.T) {
		err := storageCollection.DropOperators()
		require.NoError(t, err)

		operators, err := storageCollection.ListOperators(nil, 0, 0)
		require.NoError(t, err)
		require.Equal(t, 0, len(operators))
	})

}

func newOperatorStorageForTest(logger *zap.Logger) (storage.Operators, func()) {
	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, func() {}
	}
	s := storage.NewOperatorsStorage(logger, db, []byte("test"))
	return s, func() {
		db.Close()
	}
}
