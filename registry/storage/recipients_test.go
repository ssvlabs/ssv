package storage

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

func TestStorage_SaveAndGetRecipientData(t *testing.T) {
	storage, done := newRecipientStorageForTest()
	require.NotNil(t, storage)
	defer done()

	recipientData := RecipientData{
		Fee:   common.BytesToAddress([]byte("0x2")),
		Owner: common.BytesToAddress([]byte("0x1")),
	}

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
		require.Equal(t, recipientData.Fee, recipientDataFromDB.Fee)
		require.Equal(t, recipientData.Owner, rd.Owner)
		require.Equal(t, recipientData.Fee, rd.Fee)
	})

	t.Run("create existing recipient", func(t *testing.T) {
		rd, err := storage.SaveRecipientData(&RecipientData{
			Fee:   common.BytesToAddress([]byte("0x2")),
			Owner: common.BytesToAddress([]byte("0x2")),
		})
		require.NoError(t, err)
		require.NotNil(t, rd)
		rdDup, err := storage.SaveRecipientData(&RecipientData{
			Fee:   common.BytesToAddress([]byte("0x2")),
			Owner: common.BytesToAddress([]byte("0x2")),
		})
		require.NoError(t, err)
		require.Nil(t, rdDup)
		rdFromDB, found, err := storage.GetRecipientData(rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, rdFromDB)
	})

	t.Run("update existing recipient", func(t *testing.T) {
		rd, err := storage.SaveRecipientData(&RecipientData{
			Fee:   common.BytesToAddress([]byte("0x2")),
			Owner: common.BytesToAddress([]byte("0x3")),
		})
		require.NoError(t, err)
		require.NotNil(t, rd)
		rdNew, err := storage.SaveRecipientData(&RecipientData{
			Fee:   common.BytesToAddress([]byte("0x3")),
			Owner: common.BytesToAddress([]byte("0x3")),
		})
		require.NoError(t, err)
		require.NotNil(t, rdNew)
		rdFromDB, found, err := storage.GetRecipientData(rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, rdNew.Owner, rdFromDB.Owner)
		require.Equal(t, rdNew.Fee, rdFromDB.Fee)
	})

	t.Run("delete recipient", func(t *testing.T) {
		rd, err := storage.SaveRecipientData(&RecipientData{
			Fee:   common.BytesToAddress([]byte("0x2")),
			Owner: common.BytesToAddress([]byte("0x4")),
		})
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

func newRecipientStorageForTest() (RecipientsCollection, func()) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := NewRecipientsStorage(db, logger, []byte("test"))
	return s, func() {
		db.Close()
	}
}
