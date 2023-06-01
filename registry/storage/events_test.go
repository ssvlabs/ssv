package storage_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/registry/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

func TestStorage_SaveAndGetEventData(t *testing.T) {
	logger := logging.TestLogger(t)
	storageCollection, done := newEventStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	t.Run("get non-existing event", func(t *testing.T) {
		eventData := &storage.EventData{}
		copy(eventData.TxHash[:], "0x1")
		nonExistingEvent, found, err := storageCollection.GetEventData(eventData.TxHash)
		require.NoError(t, err)
		require.Nil(t, nonExistingEvent)
		require.False(t, found)
	})

	t.Run("create and get event", func(t *testing.T) {
		edToCreate := &storage.EventData{}
		copy(edToCreate.TxHash[:], "0x1")
		err := storageCollection.SaveEventData(edToCreate.TxHash)
		require.NoError(t, err)
		eventDataFromDB, found, err := storageCollection.GetEventData(edToCreate.TxHash)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, eventDataFromDB)
		require.Equal(t, edToCreate.TxHash, eventDataFromDB.TxHash)
	})
}

func newEventStorageForTest(logger *zap.Logger) (storage.Events, func()) {
	db, err := ssvstorage.GetStorageFactory(logger, basedb.Options{
		Type: "badger-memory",
		Path: "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := storage.NewEventsStorage(db, []byte("test"))
	return s, func() {
		db.Close(logger)
	}
}
