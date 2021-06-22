package exporter

import (
	"bytes"
	"fmt"
	"github.com/bloxapp/ssv/fixtures"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"
	"strings"
	"testing"
)

func TestStorage_SaveAndGetOperatorInformation(t *testing.T) {
	storage, done := newStorageForTest()
	require.NotNil(t, storage)
	defer done()

	operatorInfo := OperatorInformation{
		PublicKey:    fixtures.RefPk[:],
		Name:         "my_operator",
		OwnerAddress: common.Address{},
	}

	t.Run("get non-existing operator", func(t *testing.T) {
		nonExistingOperator, err := storage.GetOperatorInformation([]byte("dummyPK"))
		require.Nil(t, nonExistingOperator)
		require.EqualError(t, err, kv.EntryNotFoundError)
	})

	t.Run("create and get operator", func(t *testing.T) {
		err := storage.SaveOperatorInformation(&operatorInfo)
		require.NoError(t, err)
		operatorInfoFromDB, err := storage.GetOperatorInformation(fixtures.RefPk[:])
		require.NoError(t, err)
		require.Equal(t, "my_operator", operatorInfoFromDB.Name)
		require.Equal(t, 0, operatorInfoFromDB.Index)
		require.True(t, bytes.Equal(operatorInfoFromDB.PublicKey, fixtures.RefPk[:]))
	})

	t.Run("create existing operator", func(t *testing.T) {
		err := storage.SaveOperatorInformation(&OperatorInformation{
			PublicKey:    []byte{1, 1, 1, 1, 1, 1},
			Name:         "my_operator1",
			OwnerAddress: common.Address{},
		})
		require.NoError(t, err)
		err = storage.SaveOperatorInformation(&OperatorInformation{
			PublicKey:    []byte{1, 1, 1, 1, 1, 1},
			Name:         "my_operator2",
			OwnerAddress: common.Address{},
		})
		require.Errorf(t, err, "operator [%x] already exist", []byte{1, 1, 1, 1})
	})

	t.Run("create and get multiple operators", func(t *testing.T) {
		i, err := storage.(*exporterStorage).nextOperatorIndex()
		require.NoError(t, err)

		ois := []OperatorInformation{
			{
				PublicKey:    []byte{1, 1, 1, 1},
				Name:         "my_operator1",
				OwnerAddress: common.Address{},
			}, {
				PublicKey:    []byte{2, 2, 2, 2},
				Name:         "my_operator2",
				OwnerAddress: common.Address{},
			}, {
				PublicKey:    []byte{3, 3, 3, 3},
				Name:         "my_operator3",
				OwnerAddress: common.Address{},
			},
		}
		for _, oi := range ois {
			err = storage.SaveOperatorInformation(&oi)
			require.NoError(t, err)
		}

		for _, oi := range ois {
			operatorInfoFromDB, err := storage.GetOperatorInformation(oi.PublicKey)
			require.NoError(t, err)
			require.Equal(t, oi.Name, operatorInfoFromDB.Name)
			require.Equal(t, i, operatorInfoFromDB.Index)
			require.True(t, bytes.Equal(operatorInfoFromDB.PublicKey, oi.PublicKey))
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
			PublicKey: pk,
			Name:      fmt.Sprintf("operator-%d", i+1),
		}
		err = storage.SaveOperatorInformation(&operator)
		require.NoError(t, err)
	}

	operators, err := storage.ListOperators(0)
	require.NoError(t, err)
	require.Equal(t, 5, len(operators))
	for _, operator := range operators {
		require.True(t, strings.Contains(operator.Name, "operator-"))
	}
}

func TestExporterStorage_SaveAndGetSyncOffset(t *testing.T) {
	s, done := newStorageForTest()
	require.NotNil(t, s)
	defer done()

	offset := new(big.Int)
	offset.SetString("49e08f", 16)
	err := s.SaveSyncOffset(offset)
	require.NoError(t, err)

	o, err := s.GetSyncOffset()
	require.NoError(t, err)
	require.Zero(t, offset.Cmp(o))
}

func newStorageForTest() (Storage, func()) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := NewExporterStorage(db, logger)
	return s, func() {
		db.Close()
	}
}
