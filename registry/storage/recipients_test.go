package storage

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func TestStorage_SaveAndGetRecipientData(t *testing.T) {
	logger := logex.TestLogger(t)
	storage, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storage)
	defer done()

	recipientData := RecipientData{
		Owner: common.BytesToAddress([]byte("0x1")),
	}
	copy(recipientData.FeeRecipient[:], "0x2")

	t.Run("get non-existing recipient", func(t *testing.T) {
		nonExistingRecipient, found, err := storage.GetRecipientData(recipientData.Owner)
		require.NoError(t, err)
		require.Nil(t, nonExistingRecipient)
		require.False(t, found)
	})

	t.Run("create and get recipient", func(t *testing.T) {
		rd, err := storage.SaveRecipientData(&recipientData)
		require.NoError(t, err)
		recipientDataFromDB, found, err := storage.GetRecipientData(recipientData.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, recipientData.Owner, recipientDataFromDB.Owner)
		require.Equal(t, recipientData.FeeRecipient, recipientDataFromDB.FeeRecipient)
		require.Equal(t, recipientData.Owner, rd.Owner)
		require.Equal(t, recipientData.FeeRecipient, rd.FeeRecipient)
	})

	t.Run("create existing recipient", func(t *testing.T) {
		rdToSave := &RecipientData{
			Owner: common.BytesToAddress([]byte("0x2")),
		}
		copy(rdToSave.FeeRecipient[:], "0x2")
		rd, err := storage.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)
		rdDup, err := storage.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.Nil(t, rdDup)
		rdFromDB, found, err := storage.GetRecipientData(rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, rdFromDB)
	})

	t.Run("update existing recipient", func(t *testing.T) {
		rdToSave := &RecipientData{
			Owner: common.BytesToAddress([]byte("0x3")),
		}
		copy(rdToSave.FeeRecipient[:], "0x2")
		rd, err := storage.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)

		copy(rdToSave.FeeRecipient[:], "0x3")
		rdNew, err := storage.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rdNew)
		rdFromDB, found, err := storage.GetRecipientData(rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, rdNew.Owner, rdFromDB.Owner)
		require.Equal(t, rdNew.FeeRecipient, rdFromDB.FeeRecipient)
	})

	t.Run("delete recipient", func(t *testing.T) {
		rdToSave := &RecipientData{
			Owner: common.BytesToAddress([]byte("0x4")),
		}
		copy(rdToSave.FeeRecipient[:], "0x2")
		rd, err := storage.SaveRecipientData(rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)

		err = storage.DeleteRecipientData(rd.Owner)
		require.NoError(t, err)
		rdFromDB, found, err := storage.GetRecipientData(rd.Owner)
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, rdFromDB)
	})
}

func newRecipientStorageForTest(logger *zap.Logger) (RecipientsCollection, func()) {
	db, err := ssvstorage.GetStorageFactory(logger, basedb.Options{
		Type: "badger-memory",
		Path: "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := NewRecipientsStorage(db, []byte("test"))
	return s, func() {
		db.Close(logger)
	}
}
