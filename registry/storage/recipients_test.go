package storage_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/registry/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

func TestStorage_SaveAndGetRecipientData(t *testing.T) {
	logger := logging.TestLogger(t)
	storageCollection, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	recipientData := storage.RecipientData{
		Owner: common.BytesToAddress([]byte("0x1")),
	}
	copy(recipientData.FeeRecipient[:], "0x2")

	t.Run("get non-existing recipient", func(t *testing.T) {
		nonExistingRecipient, found, err := storageCollection.GetRecipientData(recipientData.Owner)
		require.NoError(t, err)
		require.Nil(t, nonExistingRecipient)
		require.False(t, found)
	})

	t.Run("create and get recipient", func(t *testing.T) {
		rd, err := storageCollection.SaveRecipientData(&recipientData)
		require.NoError(t, err)
		recipientDataFromDB, found, err := storageCollection.GetRecipientData(recipientData.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, recipientData.Owner, recipientDataFromDB.Owner)
		require.Equal(t, recipientData.FeeRecipient, recipientDataFromDB.FeeRecipient)
		require.Equal(t, recipientData.Owner, rd.Owner)
		require.Equal(t, recipientData.FeeRecipient, rd.FeeRecipient)
	})

	t.Run("create existing recipient", func(t *testing.T) {
		rdToSave := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x2")),
		}
		copy(rdToSave.FeeRecipient[:], "0x2")
		rd, err := storageCollection.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)
		rdDup, err := storageCollection.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.Nil(t, rdDup)
		rdFromDB, found, err := storageCollection.GetRecipientData(rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, rdFromDB)
	})

	t.Run("update existing recipient", func(t *testing.T) {
		rdToSave := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x3")),
		}
		copy(rdToSave.FeeRecipient[:], "0x2")
		rd, err := storageCollection.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)

		copy(rdToSave.FeeRecipient[:], "0x3")
		rdNew, err := storageCollection.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rdNew)
		rdFromDB, found, err := storageCollection.GetRecipientData(rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, rdNew.Owner, rdFromDB.Owner)
		require.Equal(t, rdNew.FeeRecipient, rdFromDB.FeeRecipient)
	})

	t.Run("delete recipient", func(t *testing.T) {
		rdToSave := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x4")),
		}
		copy(rdToSave.FeeRecipient[:], "0x2")
		rd, err := storageCollection.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)

		err = storageCollection.DeleteRecipientData(rd.Owner)
		require.NoError(t, err)
		rdFromDB, found, err := storageCollection.GetRecipientData(rd.Owner)
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, rdFromDB)
	})
}

func newRecipientStorageForTest(logger *zap.Logger) (storage.RecipientsCollection, func()) {
	db, err := ssvstorage.GetStorageFactory(logger, basedb.Options{
		Type: "badger-memory",
		Path: "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := storage.NewRecipientsStorage(db, []byte("test"))
	return s, func() {
		db.Close(logger)
	}
}
