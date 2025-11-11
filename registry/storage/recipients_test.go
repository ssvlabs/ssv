package storage_test

import (
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestStorage_DropRecipients(t *testing.T) {
	logger := log.TestLogger(t)
	storageCollection, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	owner := common.BytesToAddress([]byte("0x3"))
	var feeRecipient bellatrix.ExecutionAddress
	copy(feeRecipient[:], "0x3")

	// First bump nonce to create recipient with nonce
	err := storageCollection.BumpNonce(nil, owner)
	require.NoError(t, err)

	// Update fee recipient
	rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
	require.NoError(t, err)
	require.NotNil(t, rd)
	require.NotNil(t, rd.Nonce)
	require.Equal(t, storage.Nonce(0), *rd.Nonce)

	// Try to save same fee recipient again - should return nil
	rdDup, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
	require.NoError(t, err)
	require.Nil(t, rdDup)

	// Verify fee recipient is saved
	recipient, err := storageCollection.GetFeeRecipient(owner)
	require.NoError(t, err)
	require.Equal(t, feeRecipient, recipient)

	err = storageCollection.DropRecipients()
	require.NoError(t, err)

	_, err = storageCollection.GetFeeRecipient(owner)
	require.Error(t, err)
}

func TestStorage_SaveAndGetRecipientData(t *testing.T) {
	logger := log.TestLogger(t)
	storageCollection, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	t.Run("get non-existing recipient", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x1"))
		_, err := storageCollection.GetFeeRecipient(owner)
		require.Error(t, err)
	})

	t.Run("create and get recipient", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x1"))
		var feeRecipient bellatrix.ExecutionAddress
		copy(feeRecipient[:], "0x2")

		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.Equal(t, owner, rd.Owner)
		require.Equal(t, feeRecipient, rd.FeeRecipient)

		// Get from in-memory map
		recipientFromMap, err := storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)
		require.Equal(t, feeRecipient, recipientFromMap)
	})

	t.Run("create existing recipient with same fee", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x2"))
		var feeRecipient bellatrix.ExecutionAddress
		copy(feeRecipient[:], "0x2")

		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.NotNil(t, rd)

		// Save again with same fee recipient - should return nil
		rdDup, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.Nil(t, rdDup)

		// Verify it still exists
		recipient, err := storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)
		require.Equal(t, feeRecipient, recipient)
	})

	t.Run("update fee recipient preserves nonce", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x3"))
		var feeRecipient1 bellatrix.ExecutionAddress
		copy(feeRecipient1[:], "0x3")
		var feeRecipient2 bellatrix.ExecutionAddress
		copy(feeRecipient2[:], "0x4")

		// First create with nonce by bumping
		err := storageCollection.BumpNonce(nil, owner)
		require.NoError(t, err)

		// Set initial fee recipient
		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient1)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.NotNil(t, rd.Nonce)
		require.Equal(t, storage.Nonce(0), *rd.Nonce)

		// Update fee recipient
		rdNew, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient2)
		require.NoError(t, err)
		require.NotNil(t, rdNew)
		require.NotNil(t, rdNew.Nonce)
		require.Equal(t, storage.Nonce(0), *rdNew.Nonce, "nonce should be preserved")

		// Verify in-memory map is updated
		recipient, err := storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)
		require.Equal(t, feeRecipient2, recipient)
	})

	t.Run("update existing recipient", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x4"))
		var feeRecipient1 bellatrix.ExecutionAddress
		copy(feeRecipient1[:], "0x2")
		var feeRecipient2 bellatrix.ExecutionAddress
		copy(feeRecipient2[:], "0x3")

		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient1)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.Nil(t, rd.Nonce)

		rdNew, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient2)
		require.NoError(t, err)
		require.NotNil(t, rdNew)
		require.Nil(t, rdNew.Nonce)
		require.Equal(t, feeRecipient2, rdNew.FeeRecipient)

		recipient, err := storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)
		require.Equal(t, feeRecipient2, recipient)
	})

	t.Run("delete recipient", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x5"))
		var feeRecipient bellatrix.ExecutionAddress
		copy(feeRecipient[:], "0x2")

		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.NotNil(t, rd)

		err = storageCollection.DeleteRecipientData(nil, owner)
		require.NoError(t, err)

		_, err = storageCollection.GetFeeRecipient(owner)
		require.Error(t, err)
	})

	t.Run("create and get many recipients", func(t *testing.T) {
		var owners []common.Address
		var expectedRecipients []bellatrix.ExecutionAddress

		for i := 0; i < 10; i++ {
			owner := common.BytesToAddress([]byte(fmt.Sprintf("owner%d", i)))
			var feeRecipient bellatrix.ExecutionAddress
			copy(feeRecipient[:], fmt.Sprintf("fee%d", i))

			owners = append(owners, owner)
			expectedRecipients = append(expectedRecipients, feeRecipient)

			_, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
			require.NoError(t, err)
		}

		// Verify all recipients are saved correctly
		for i, owner := range owners {
			recipient, err := storageCollection.GetFeeRecipient(owner)
			require.NoError(t, err)
			require.Equal(t, expectedRecipients[i], recipient)
		}
	})

	t.Run("create recipient does not initialize nonce", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11111"))
		feeRecipient := bellatrix.ExecutionAddress(owner)

		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.Equal(t, owner, rd.Owner)
		require.Equal(t, feeRecipient, rd.FeeRecipient)
		require.Nil(t, rd.Nonce)
	})
}

func TestStorage_NonceManagement(t *testing.T) {
	logger := log.TestLogger(t)
	storageCollection, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	t.Run("bump nonce before fee recipient created", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11112"))
		expectedFeeRecipient := bellatrix.ExecutionAddress(owner)

		// Verify doesn't exist
		_, err := storageCollection.GetFeeRecipient(owner)
		require.Error(t, err)

		// Bump nonce - creates recipient with owner as default fee recipient
		err = storageCollection.BumpNonce(nil, owner)
		require.NoError(t, err)

		// Verify created with owner as fee recipient
		feeRecipient, err := storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)
		require.Equal(t, expectedFeeRecipient, feeRecipient)

		// Verify nonce is 0
		nonce, err := storageCollection.GetNextNonce(nil, owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(1), nonce)
	})

	t.Run("bump nonce after fee recipient created", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11113"))
		var feeRecipient bellatrix.ExecutionAddress
		copy(feeRecipient[:], "custom_fee")

		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.Nil(t, rd.Nonce)

		err = storageCollection.BumpNonce(nil, owner)
		require.NoError(t, err)

		// Verify fee recipient unchanged
		recipient, err := storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)
		require.Equal(t, feeRecipient, recipient)

		// Verify nonce is now 0
		nonce, err := storageCollection.GetNextNonce(nil, owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(1), nonce)
	})

	t.Run("bump non-zero nonce", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11114"))

		// Bump twice
		err := storageCollection.BumpNonce(nil, owner)
		require.NoError(t, err)

		err = storageCollection.BumpNonce(nil, owner)
		require.NoError(t, err)

		nonce, err := storageCollection.GetNextNonce(nil, owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(2), nonce)
	})

	t.Run("get next nonce before fee recipient created", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11115"))

		nonce, err := storageCollection.GetNextNonce(nil, owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(0), nonce)

		// Should not create recipient
		_, err = storageCollection.GetFeeRecipient(owner)
		require.Error(t, err)
	})

	t.Run("get next nonce after fee recipient created", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11116"))
		feeRecipient := bellatrix.ExecutionAddress(owner)

		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.NotNil(t, rd)

		nonce, err := storageCollection.GetNextNonce(nil, owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(0), nonce)
	})

	t.Run("get next nonce progression", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11117"))
		feeRecipient := bellatrix.ExecutionAddress(owner)

		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.NotNil(t, rd)

		// Initially should be 0
		nonce, err := storageCollection.GetNextNonce(nil, owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(0), nonce)

		// After bump should be 1
		err = storageCollection.BumpNonce(nil, owner)
		require.NoError(t, err)

		nonce, err = storageCollection.GetNextNonce(nil, owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(1), nonce)
	})
}

func TestStorage_Persistence(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	owner1 := common.BytesToAddress([]byte("owner1"))
	owner2 := common.BytesToAddress([]byte("owner2"))
	var feeRecipient1, feeRecipient2, newFeeRecipient bellatrix.ExecutionAddress
	copy(feeRecipient1[:], "fee1")
	copy(feeRecipient2[:], "fee2")
	copy(newFeeRecipient[:], "newfee")

	// Create first instance and save data
	{
		storage1, err := storage.NewRecipientsStorage(logger, db, []byte("test"))
		require.NoError(t, err)

		_, err = storage1.SaveRecipientData(nil, owner1, feeRecipient1)
		require.NoError(t, err)
		_, err = storage1.SaveRecipientData(nil, owner2, feeRecipient2)
		require.NoError(t, err)

		// Bump nonce for owner1
		err = storage1.BumpNonce(nil, owner1)
		require.NoError(t, err)
		err = storage1.BumpNonce(nil, owner1)
		require.NoError(t, err)
	}

	// Create second instance - should load from DB
	{
		storage2, err := storage.NewRecipientsStorage(logger, db, []byte("test"))
		require.NoError(t, err)

		// Check that in-memory map was loaded correctly
		recipient1, err := storage2.GetFeeRecipient(owner1)
		require.NoError(t, err)
		require.Equal(t, feeRecipient1, recipient1)

		recipient2, err := storage2.GetFeeRecipient(owner2)
		require.NoError(t, err)
		require.Equal(t, feeRecipient2, recipient2)

		// Check nonce was preserved
		nonce, err := storage2.GetNextNonce(nil, owner1)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(2), nonce)

		// Update fee recipient and verify in-memory map
		_, err = storage2.SaveRecipientData(nil, owner1, newFeeRecipient)
		require.NoError(t, err)

		recipient1New, err := storage2.GetFeeRecipient(owner1)
		require.NoError(t, err)
		require.Equal(t, newFeeRecipient, recipient1New)
	}

	// Create third instance to verify update was persisted
	{
		storage3, err := storage.NewRecipientsStorage(logger, db, []byte("test"))
		require.NoError(t, err)

		recipient1, err := storage3.GetFeeRecipient(owner1)
		require.NoError(t, err)
		require.Equal(t, newFeeRecipient, recipient1, "should have updated fee recipient")
	}
}

func TestGetFeeRecipient(t *testing.T) {
	logger := log.TestLogger(t)
	storageCollection, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	t.Run("get non-existing fee recipient", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x1"))
		_, err := storageCollection.GetFeeRecipient(owner)
		require.Error(t, err)
	})

	t.Run("save and get fee recipient", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x2"))
		var feeRecipient bellatrix.ExecutionAddress
		copy(feeRecipient[:], []byte("0xFEE"))

		// Save fee recipient
		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.Equal(t, owner, rd.Owner)
		require.Equal(t, feeRecipient, rd.FeeRecipient)

		// Get fee recipient from in-memory map
		recipient, err := storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)
		require.Equal(t, feeRecipient, recipient)
	})

	t.Run("update fee recipient", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x3"))
		var feeRecipient1 bellatrix.ExecutionAddress
		copy(feeRecipient1[:], []byte("0xFEE1"))
		var feeRecipient2 bellatrix.ExecutionAddress
		copy(feeRecipient2[:], []byte("0xFEE2"))

		// Save initial fee recipient
		rd, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient1)
		require.NoError(t, err)
		require.NotNil(t, rd)

		// Update fee recipient
		rd, err = storageCollection.SaveRecipientData(nil, owner, feeRecipient2)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.Equal(t, feeRecipient2, rd.FeeRecipient)

		// Verify in-memory map is updated
		recipient, err := storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)
		require.Equal(t, feeRecipient2, recipient)
	})

	t.Run("delete removes from in-memory map", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x4"))
		var feeRecipient bellatrix.ExecutionAddress
		copy(feeRecipient[:], []byte("0xFEE"))

		// Save fee recipient
		_, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
		require.NoError(t, err)

		// Verify it exists
		_, err = storageCollection.GetFeeRecipient(owner)
		require.NoError(t, err)

		// Delete
		err = storageCollection.DeleteRecipientData(nil, owner)
		require.NoError(t, err)

		// Verify it's removed from in-memory map
		_, err = storageCollection.GetFeeRecipient(owner)
		require.Error(t, err)
	})

	t.Run("drop clears in-memory map", func(t *testing.T) {
		// Add multiple recipients
		for i := 0; i < 5; i++ {
			owner := common.BytesToAddress([]byte(fmt.Sprintf("drop%d", i)))
			var feeRecipient bellatrix.ExecutionAddress
			copy(feeRecipient[:], fmt.Sprintf("fee%d", i))

			_, err := storageCollection.SaveRecipientData(nil, owner, feeRecipient)
			require.NoError(t, err)
		}

		// Verify they exist
		for i := 0; i < 5; i++ {
			owner := common.BytesToAddress([]byte(fmt.Sprintf("drop%d", i)))
			_, err := storageCollection.GetFeeRecipient(owner)
			require.NoError(t, err)
		}

		// Drop all
		err := storageCollection.DropRecipients()
		require.NoError(t, err)

		// Verify all are gone
		for i := 0; i < 5; i++ {
			owner := common.BytesToAddress([]byte(fmt.Sprintf("drop%d", i)))
			_, err := storageCollection.GetFeeRecipient(owner)
			require.Error(t, err)
		}
	})
}

func newRecipientStorageForTest(logger *zap.Logger) (storage.Recipients, func()) {
	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, func() {}
	}

	s, err := storage.NewRecipientsStorage(logger, db, []byte("test"))
	if err != nil {
		db.Close()
		return nil, func() { db.Close() }
	}
	return s, func() {
		db.Close()
	}
}
