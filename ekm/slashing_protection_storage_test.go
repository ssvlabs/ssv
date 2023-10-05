package ekm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/storage/kv"
)

func TestSpStorage_IsInitialized(t *testing.T) {
	logger := logging.TestLogger(t)

	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	defer db.Close()

	spStorage := NewSlashingProtectionStorage(db, logger, []byte("network"))

	// Initially, the storage should not be initialized
	isInitialized, err := spStorage.IsInitialized()
	require.NoError(t, err)
	require.False(t, isInitialized, "Storage should not be initialized initially")

	// Initialize the storage
	err = spStorage.Init()
	require.NoError(t, err, "Initializing storage should not produce an error")

	// Now, the storage should be initialized
	isInitialized, err = spStorage.IsInitialized()
	require.NoError(t, err)
	require.True(t, isInitialized, "Storage should be initialized after calling Init")

	// Add some data to the DB to simulate non-empty db with correct db type
	prefix := []byte("prefix")
	key := []byte("key")
	value := []byte("value")
	require.NoError(t, db.Set(prefix, key, value))

	// The storage should still be initialized
	isInitialized, err = spStorage.IsInitialized()
	require.NoError(t, err)
	require.True(t, isInitialized, "Storage should be initialized after adding data")

	// Simulate an incorrect db type by setting a different type
	err = db.SetType("IncorrectDBType")
	require.NoError(t, err)

	// The storage should not be initialized as the db type is incorrect
	isInitialized, err = spStorage.IsInitialized()
	require.Error(t, err, "Expected error due to incorrect db type")
	require.Contains(t, err.Error(), "db type is incorrect")
	require.False(t, isInitialized, "Storage should not be initialized with incorrect db type")

	// Simulate db type not found by deleting the db type
	require.NoError(t, db.Delete([]byte{}, []byte(kv.DBTypePrefix)))

	// The storage should not be initialized as the db type is not found
	isInitialized, err = spStorage.IsInitialized()
	require.Error(t, err, "Expected error due to db type not found")
	require.Contains(t, err.Error(), "type not found")
	require.False(t, isInitialized, "Storage should not be initialized when db type not found")
}
