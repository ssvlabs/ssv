package exporter

import (
	"bytes"
	"fmt"
	"github.com/bloxapp/ssv/fixtures"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"
	"strings"
	"testing"
)

func TestOperatorStorage_SaveAndGetOperatorInformation(t *testing.T) {
	operatorStorage, done := newOperatorStorageForTest()
	require.NotNil(t, operatorStorage)
	defer done()

	operatorInfo := OperatorInformation{
		PublicKey:    fixtures.RefPk[:],
		Name:         "my_operator",
		OwnerAddress: common.Address{},
	}

	t.Run("non-existing operator", func(t *testing.T) {
		nonExistingOperator, err := operatorStorage.GetOperatorInformation([]byte("dummyPK"))
		require.Nil(t, nonExistingOperator)
		require.EqualError(t, err, "Key not found")
	})

	t.Run("create and get operator", func(t *testing.T) {
		err := operatorStorage.SaveOperatorInformation(&operatorInfo)
		require.NoError(t, err)
		operatorInfoFromDB, err := operatorStorage.GetOperatorInformation(fixtures.RefPk[:])
		require.NoError(t, err)
		require.Equal(t, "my_operator", operatorInfoFromDB.Name)
		require.True(t, bytes.Equal(operatorInfoFromDB.PublicKey, fixtures.RefPk[:]))
	})
}

func TestOperatorStorage_ListOperators(t *testing.T) {
	operatorStorage, done := newOperatorStorageForTest()
	require.NotNil(t, operatorStorage)
	defer done()

	n := 5
	for i := 0; i < n; i++ {
		pk, _, err := rsaencryption.GenerateKeys()
		require.NoError(t, err)
		operator := OperatorInformation{
			PublicKey: pk,
			Name:      fmt.Sprintf("operator-%d", i+1),
		}
		err = operatorStorage.SaveOperatorInformation(&operator)
		require.NoError(t, err)
	}

	operators, err := operatorStorage.ListOperators()
	require.NoError(t, err)
	require.Equal(t, 5, len(operators))
	for _, operator := range operators {
		require.True(t, strings.Contains(operator.Name, "operator-"))
	}
}

func TestExporterStorage_SaveAndGetSyncOffset(t *testing.T) {
	s, done := newOperatorStorageForTest()
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

func newOperatorStorageForTest() (Storage, func()) {
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
