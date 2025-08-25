package storage_test

import (
	"crypto/rand"
	"fmt"
	"testing"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestStorage_DropRecipients(t *testing.T) {
	logger := log.TestLogger(t)
	storageCollection, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	var nonce storage.Nonce
	rdToSave := &storage.RecipientData{
		Owner: common.BytesToAddress([]byte("0x3")),
		Nonce: &nonce,
	}
	copy(rdToSave.FeeRecipient[:], "0x3")

	rd, err := storageCollection.SaveRecipientData(nil, rdToSave)
	require.NoError(t, err)
	require.NotNil(t, rd)
	require.NotNil(t, rd.Nonce)
	require.Equal(t, storage.Nonce(0), *rd.Nonce)

	rdToSave, found, err := storageCollection.GetRecipientData(nil, rd.Owner)
	require.NoError(t, err)
	require.True(t, found)
	rdDup, err := storageCollection.SaveRecipientData(nil, rdToSave)
	require.NoError(t, err)
	require.Nil(t, rdDup)
	require.NotNil(t, rd.Nonce)
	require.Equal(t, storage.Nonce(0), *rd.Nonce)

	rdFromDB, found, err := storageCollection.GetRecipientData(nil, rd.Owner)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, rdFromDB.Nonce)
	require.Equal(t, storage.Nonce(0), *rdFromDB.Nonce)

	err = storageCollection.DropRecipients()
	require.NoError(t, err)

	_, found, err = storageCollection.GetRecipientData(nil, rd.Owner)
	require.NoError(t, err)
	require.False(t, found)
}

func TestStorage_GetRecipientsPrefix(t *testing.T) {
	logger := log.TestLogger(t)
	storageCollection, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	require.Equal(t, []byte("recipients"), storageCollection.GetRecipientsPrefix())
}

func TestStorage_SaveAndGetRecipientData(t *testing.T) {
	logger := log.TestLogger(t)
	storageCollection, done := newRecipientStorageForTest(logger)
	require.NotNil(t, storageCollection)
	defer done()

	recipientData := &storage.RecipientData{
		Owner: common.BytesToAddress([]byte("0x1")),
	}
	copy(recipientData.FeeRecipient[:], "0x2")

	t.Run("get non-existing recipient", func(t *testing.T) {
		nonExistingRecipient, found, err := storageCollection.GetRecipientData(nil, recipientData.Owner)
		require.NoError(t, err)
		require.Nil(t, nonExistingRecipient)
		require.False(t, found)
	})

	t.Run("create and get recipient", func(t *testing.T) {
		rd, err := storageCollection.SaveRecipientData(nil, recipientData)
		require.NoError(t, err)

		recipientDataFromDB, found, err := storageCollection.GetRecipientData(nil, recipientData.Owner)
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

		rd, err := storageCollection.SaveRecipientData(nil, rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)

		rdDup, err := storageCollection.SaveRecipientData(nil, rdToSave)
		require.NoError(t, err)
		require.Nil(t, rdDup)

		rdFromDB, found, err := storageCollection.GetRecipientData(nil, rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, rdFromDB)
	})

	t.Run("save/get/save fee recipient address without overwriting nonce", func(t *testing.T) {
		var nonce storage.Nonce
		rdToSave := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x3")),
			Nonce: &nonce,
		}
		copy(rdToSave.FeeRecipient[:], "0x3")

		rd, err := storageCollection.SaveRecipientData(nil, rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.NotNil(t, rd.Nonce)
		require.Equal(t, storage.Nonce(0), *rd.Nonce)

		rdToSave, found, err := storageCollection.GetRecipientData(nil, rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		rdDup, err := storageCollection.SaveRecipientData(nil, rdToSave)
		require.NoError(t, err)
		require.Nil(t, rdDup)
		require.NotNil(t, rd.Nonce)
		require.Equal(t, storage.Nonce(0), *rd.Nonce)

		rdFromDB, found, err := storageCollection.GetRecipientData(nil, rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, rdFromDB.Nonce)
		require.Equal(t, storage.Nonce(0), *rdFromDB.Nonce)
	})

	t.Run("update existing recipient", func(t *testing.T) {
		rdToSave := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x3")),
		}
		copy(rdToSave.FeeRecipient[:], "0x2")

		rd, err := storageCollection.SaveRecipientData(nil, rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)
		require.Nil(t, rd.Nonce)

		copy(rdToSave.FeeRecipient[:], "0x3")
		rdNew, err := storageCollection.SaveRecipientData(nil, rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rdNew)
		require.Nil(t, rd.Nonce)

		rdFromDB, found, err := storageCollection.GetRecipientData(nil, rd.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, rdNew.Owner, rdFromDB.Owner)
		require.Equal(t, rdNew.FeeRecipient, rdFromDB.FeeRecipient)
		require.Nil(t, rd.Nonce)
	})

	t.Run("delete recipient", func(t *testing.T) {
		rdToSave := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x4")),
		}
		copy(rdToSave.FeeRecipient[:], "0x2")

		rd, err := storageCollection.SaveRecipientData(nil, rdToSave)
		require.NoError(t, err)
		require.NotNil(t, rd)

		err = storageCollection.DeleteRecipientData(nil, rd.Owner)
		require.NoError(t, err)

		rdFromDB, found, err := storageCollection.GetRecipientData(nil, rd.Owner)
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, rdFromDB)
	})

	t.Run("create and get many recipients", func(t *testing.T) {
		var ownerAddresses []common.Address
		var savedRecipients []*storage.RecipientData
		for i := 0; i < 10; i++ {
			rd := storage.RecipientData{
				Owner: common.BytesToAddress([]byte(fmt.Sprintf("0x%d", i))),
			}
			copy(recipientData.FeeRecipient[:], fmt.Sprintf("0x%d", i))
			ownerAddresses = append(ownerAddresses, rd.Owner)

			_, err := storageCollection.SaveRecipientData(nil, &rd)
			require.NoError(t, err)

			savedRecipients = append(savedRecipients, &rd)
		}

		recipients, err := storageCollection.GetRecipientDataMany(nil, ownerAddresses)
		require.NoError(t, err)
		require.Equal(t, len(ownerAddresses), len(recipients))

		for _, r := range savedRecipients {
			require.Equal(t, r.FeeRecipient, recipients[r.Owner])
		}
	})

	t.Run("create recipient should not initializing nonce", func(t *testing.T) {
		rdToCreate := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x11111")),
		}

		rd, err := storageCollection.SaveRecipientData(nil, rdToCreate)
		require.NoError(t, err)

		recipientDataFromDB, found, err := storageCollection.GetRecipientData(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, rdToCreate.Owner, recipientDataFromDB.Owner)
		require.Equal(t, rdToCreate.FeeRecipient, recipientDataFromDB.FeeRecipient)
		require.Nil(t, recipientDataFromDB.Nonce)
		require.Equal(t, rdToCreate.Owner, rd.Owner)
		require.Equal(t, rdToCreate.FeeRecipient, rd.FeeRecipient)
		require.Nil(t, rd.Nonce)
	})

	t.Run("bump nonce before fee recipient created", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11112"))
		var feeRecipient bellatrix.ExecutionAddress
		copy(feeRecipient[:], owner.Bytes())

		data, found, err := storageCollection.GetRecipientData(nil, owner)
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, data)

		err = storageCollection.BumpNonce(nil, owner)
		require.NoError(t, err)

		data, found, err = storageCollection.GetRecipientData(nil, owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, data)
		require.Equal(t, owner, data.Owner)
		require.Equal(t, feeRecipient, data.FeeRecipient)
		require.Equal(t, storage.Nonce(0), *data.Nonce)
	})

	t.Run("bump nonce after fee recipient created", func(t *testing.T) {
		rdToCreate := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x11113")),
		}
		copy(rdToCreate.FeeRecipient[:], rdToCreate.Owner.Bytes())
		rd, err := storageCollection.SaveRecipientData(nil, rdToCreate)
		require.NoError(t, err)
		require.NotNil(t, rd)

		err = storageCollection.BumpNonce(nil, rdToCreate.Owner)
		require.NoError(t, err)

		data, found, err := storageCollection.GetRecipientData(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, data)
		require.Equal(t, storage.Nonce(0), *data.Nonce)
	})

	t.Run("bump non-zero nonce", func(t *testing.T) {
		rdToCreate := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x11114")),
		}
		nonce := storage.Nonce(0)
		copy(rdToCreate.FeeRecipient[:], rdToCreate.Owner.Bytes())
		rdToCreate.Nonce = &nonce

		rd, err := storageCollection.SaveRecipientData(nil, rdToCreate)
		require.NoError(t, err)
		require.NotNil(t, rd)

		err = storageCollection.BumpNonce(nil, rdToCreate.Owner)
		require.NoError(t, err)

		data, found, err := storageCollection.GetRecipientData(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, data)
		require.Equal(t, storage.Nonce(1), *data.Nonce)
	})

	t.Run("get next nonce before fee recipient created - should be 0", func(t *testing.T) {
		owner := common.BytesToAddress([]byte("0x11115"))
		var feeRecipient bellatrix.ExecutionAddress
		copy(feeRecipient[:], owner.Bytes())

		data, found, err := storageCollection.GetRecipientData(nil, owner)
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, data)

		nonce, err := storageCollection.GetNextNonce(nil, owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(0), nonce)

		data, found, err = storageCollection.GetRecipientData(nil, owner)
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, data)
	})

	t.Run("get next nonce after fee recipient created - should be 0", func(t *testing.T) {
		rdToCreate := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x11116")),
		}
		copy(rdToCreate.FeeRecipient[:], rdToCreate.Owner.Bytes())

		rd, err := storageCollection.SaveRecipientData(nil, rdToCreate)
		require.NoError(t, err)
		require.NotNil(t, rd)

		nonce, err := storageCollection.GetNextNonce(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(0), nonce)

		data, found, err := storageCollection.GetRecipientData(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, data)
		require.Nil(t, data.Nonce)
	})

	t.Run("get next nonce before bump", func(t *testing.T) {
		rdToCreate := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x11117")),
		}
		copy(rdToCreate.FeeRecipient[:], rdToCreate.Owner.Bytes())

		rd, err := storageCollection.SaveRecipientData(nil, rdToCreate)
		require.NoError(t, err)
		require.NotNil(t, rd)

		nonce, err := storageCollection.GetNextNonce(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(0), nonce)

		err = storageCollection.BumpNonce(nil, rdToCreate.Owner)
		require.NoError(t, err)

		data, found, err := storageCollection.GetRecipientData(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, data)
		require.Equal(t, storage.Nonce(0), *data.Nonce)
	})

	t.Run("get next nonce after bump", func(t *testing.T) {
		rdToCreate := &storage.RecipientData{
			Owner: common.BytesToAddress([]byte("0x11118")),
		}
		copy(rdToCreate.FeeRecipient[:], rdToCreate.Owner.Bytes())

		rd, err := storageCollection.SaveRecipientData(nil, rdToCreate)
		require.NoError(t, err)
		require.NotNil(t, rd)

		err = storageCollection.BumpNonce(nil, rdToCreate.Owner)
		require.NoError(t, err)

		nonce, err := storageCollection.GetNextNonce(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.Equal(t, storage.Nonce(1), nonce)

		data, found, err := storageCollection.GetRecipientData(nil, rdToCreate.Owner)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, data)
		require.Equal(t, storage.Nonce(0), *data.Nonce)
	})
}

// TestSharesUpdaterIntegration tests that when fee recipient is updated, shares are updated in memory
func TestSharesUpdaterIntegration(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	// Create recipients and shares storage with proper wiring
	recipientStorage, setSharesUpdater := storage.NewRecipientsStorage(logger, db, []byte("test"))
	sharesStorage, _, err := storage.NewSharesStorage(networkconfig.TestNetwork.Beacon, db, recipientStorage, []byte("test"))
	require.NoError(t, err)
	setSharesUpdater(sharesStorage)

	owner := common.HexToAddress("0x1234567890123456789012345678901234567890")
	initialFeeRecipient := common.HexToAddress("0xAAAA567890123456789012345678901234567890")
	updatedFeeRecipient := common.HexToAddress("0xBBBB678901234567890123456789012345678901")

	// Create and save shares with the owner
	share1 := &types.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey: generateRandomPubKey(),
			ValidatorIndex:  1,
			Committee:       []*spectypes.ShareMember{{Signer: 1, SharePubKey: []byte("key1")}},
			DomainType:      networkconfig.TestNetwork.DomainType,
		},
		Status:       v1.ValidatorStateActiveOngoing,
		OwnerAddress: owner,
	}

	share2 := &types.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey: generateRandomPubKey(),
			ValidatorIndex:  2,
			Committee:       []*spectypes.ShareMember{{Signer: 1, SharePubKey: []byte("key2")}},
			DomainType:      networkconfig.TestNetwork.DomainType,
		},
		Status:       v1.ValidatorStateActiveOngoing,
		OwnerAddress: owner,
	}

	require.NoError(t, sharesStorage.Save(nil, share1, share2))

	// Set initial fee recipient
	var initialFeeRecipientAddr bellatrix.ExecutionAddress
	copy(initialFeeRecipientAddr[:], initialFeeRecipient.Bytes())
	_, err = recipientStorage.SaveRecipientData(nil, &storage.RecipientData{
		Owner:        owner,
		FeeRecipient: initialFeeRecipientAddr,
	})
	require.NoError(t, err)

	// Verify shares have the initial fee recipient
	loadedShare1, found := sharesStorage.Get(nil, share1.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(initialFeeRecipientAddr), loadedShare1.FeeRecipientAddress)

	// Update fee recipient through recipients storage
	var updatedFeeRecipientAddr bellatrix.ExecutionAddress
	copy(updatedFeeRecipientAddr[:], updatedFeeRecipient.Bytes())
	_, err = recipientStorage.SaveRecipientData(nil, &storage.RecipientData{
		Owner:        owner,
		FeeRecipient: updatedFeeRecipientAddr,
	})
	require.NoError(t, err)

	// Verify shares are automatically updated with new fee recipient
	loadedShare1, found = sharesStorage.Get(nil, share1.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(updatedFeeRecipientAddr), loadedShare1.FeeRecipientAddress)

	loadedShare2, found := sharesStorage.Get(nil, share2.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(updatedFeeRecipientAddr), loadedShare2.FeeRecipientAddress)
}

func generateRandomPubKey() spectypes.ValidatorPK {
	pk := make([]byte, 48)
	_, _ = rand.Read(pk)
	return spectypes.ValidatorPK(pk)
}

func newRecipientStorageForTest(logger *zap.Logger) (storage.Recipients, func()) {
	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, func() {}
	}

	s, setSharesUpdater := storage.NewRecipientsStorage(logger, db, []byte("test"))
	// Set a no-op shares updater for tests that don't need share update functionality
	setSharesUpdater(&noOpSharesUpdater{})
	return s, func() {
		db.Close()
	}
}

// noOpSharesUpdater is a no-op implementation of SharesUpdater for tests
type noOpSharesUpdater struct{}

func (n *noOpSharesUpdater) UpdateFeeRecipientForOwner(owner common.Address, feeRecipient bellatrix.ExecutionAddress) {
	// No-op - used in tests that don't need share updates
}
