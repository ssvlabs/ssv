package fee_recipient

import (
	"context"
	"errors"
	"fmt"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestSubmitProposal(t *testing.T) {
	logger := log.TestLogger(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	operatorData := &registrystorage.OperatorData{
		ID: 123456789,
	}

	operatorDataStore := operatordatastore.New(operatorData)

	db, shareStorage, recipientStorage := createStorageWithRecipients(t)
	defer func() { require.NoError(t, db.Close()) }()

	beaconConfig := networkconfig.TestNetwork.Beacon
	populateStorage(t, shareStorage, operatorData)

	frCtrl := NewController(logger, &ControllerOptions{
		Ctx:               t.Context(),
		BeaconConfig:      beaconConfig,
		ShareStorage:      shareStorage,
		OperatorDataStore: operatorDataStore,
	})

	t.Run("custom fee recipients from storage", func(t *testing.T) {
		// Set custom fee recipients for some validators
		customRecipient1 := bellatrix.ExecutionAddress{0x11, 0x22, 0x33, 0x44, 0x55}
		customRecipient2 := bellatrix.ExecutionAddress{0xaa, 0xbb, 0xcc, 0xdd, 0xee}

		owner1 := common.HexToAddress("0x0000000000000000000000000000000000000000")
		owner2 := common.HexToAddress("0x0000000000000000000000000000000000000001")

		_, err := recipientStorage.SaveRecipientData(nil, &registrystorage.RecipientData{
			Owner:        owner1,
			FeeRecipient: customRecipient1,
		})
		require.NoError(t, err)

		_, err = recipientStorage.SaveRecipientData(nil, &registrystorage.RecipientData{
			Owner:        owner2,
			FeeRecipient: customRecipient2,
		})
		require.NoError(t, err)

		var capturedRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
				capturedRecipients = feeRecipients
				return nil
			}).Times(1)

		frCtrl.beaconClient = client

		err = frCtrl.prepareAndSubmit(t.Context())
		require.NoError(t, err)

		// Verify custom recipients are used for validators with index 0 and 1
		require.Equal(t, customRecipient1, capturedRecipients[0])
		require.Equal(t, customRecipient2, capturedRecipients[1])

		// Verify owner address is used as default for others (e.g., index 2)
		owner2Addr := common.HexToAddress("0x0000000000000000000000000000000000000002")
		var expectedDefault bellatrix.ExecutionAddress
		copy(expectedDefault[:], owner2Addr.Bytes())
		require.Equal(t, expectedDefault, capturedRecipients[2])
	})

	t.Run("owner address as default fee recipient", func(t *testing.T) {
		// Create fresh storage without custom recipients
		db2, shareStorage2, _ := createStorageWithRecipients(t)
		defer func() { require.NoError(t, db2.Close()) }()
		populateStorage(t, shareStorage2, operatorData)

		frCtrl2 := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ShareStorage:      shareStorage2,
			OperatorDataStore: operatorDataStore,
		})

		var capturedRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
				capturedRecipients = feeRecipients
				return nil
			}).Times(1)

		frCtrl2.beaconClient = client

		err := frCtrl2.prepareAndSubmit(t.Context())
		require.NoError(t, err)

		// Verify all validators use their owner address as fee recipient
		for i := 0; i < 10; i++ { // Check first 10 validators
			ownerAddr := common.HexToAddress(fmt.Sprintf("0x%040x", i))
			var expectedRecipient bellatrix.ExecutionAddress
			copy(expectedRecipient[:], ownerAddr.Bytes())
			require.Equal(t, expectedRecipient, capturedRecipients[phase0.ValidatorIndex(i)],
				"Validator %d should use owner address as default fee recipient", i)
		}
	})

	t.Run("correct validator index to fee recipient mapping", func(t *testing.T) {
		var capturedRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
				capturedRecipients = feeRecipients
				return nil
			}).Times(1)

		frCtrl.beaconClient = client

		err := frCtrl.prepareAndSubmit(t.Context())
		require.NoError(t, err)

		// Verify mapping has correct number of entries (100 committee validators)
		require.Len(t, capturedRecipients, 100)

		// Verify each validator index maps to correct recipient
		for i := 0; i < 100; i++ {
			idx := phase0.ValidatorIndex(i)
			_, exists := capturedRecipients[idx]
			require.True(t, exists, "Validator index %d should be in the mapping", i)
		}

		// Verify non-committee validator (index 2000) is not included
		_, exists := capturedRecipients[2000]
		require.False(t, exists, "Non-committee validator should not be included")
	})

	t.Run("batch processing edge cases", func(t *testing.T) {
		// Test with exactly 500 shares (one batch)
		db2, shareStorage2, _ := createStorageWithRecipients(t)
		defer func() { require.NoError(t, db2.Close()) }()

		// Create exactly 500 shares
		for i := 0; i < 500; i++ {
			ownerAddr := common.HexToAddress(fmt.Sprintf("0x%040x", i))
			share := &types.SSVShare{
				Share: spectypes.Share{
					ValidatorPubKey: spectypes.ValidatorPK([]byte(fmt.Sprintf("pk%046d", i))),
					SharePubKey:     []byte(fmt.Sprintf("pk%046d", i)),
					ValidatorIndex:  phase0.ValidatorIndex(i),
					Committee:       []*spectypes.ShareMember{{Signer: operatorData.ID}},
				},
				Status:       eth2apiv1.ValidatorStateActiveOngoing,
				OwnerAddress: ownerAddr,
				Liquidated:   false,
			}
			require.NoError(t, shareStorage2.Save(nil, share))
		}

		frCtrl2 := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ShareStorage:      shareStorage2,
			OperatorDataStore: operatorDataStore,
		})

		batchSize := 0

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
				batchSize = len(feeRecipients)
				return nil
			}).Times(1)

		frCtrl2.beaconClient = client

		err := frCtrl2.prepareAndSubmit(t.Context())
		require.NoError(t, err)

		// Should have exactly 1 batch of 500
		require.Equal(t, 500, batchSize)
	})

	t.Run("error handling", func(t *testing.T) {
		// Test that errors are handled gracefully
		db3, shareStorage3, _ := createStorageWithRecipients(t)
		defer func() { require.NoError(t, db3.Close()) }()
		populateStorage(t, shareStorage3, operatorData)

		frCtrl3 := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ShareStorage:      shareStorage3,
			OperatorDataStore: operatorDataStore,
		})

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any(), gomock.Any()).Return(errors.New("failed to submit")).Times(1)

		frCtrl3.beaconClient = client

		// Should handle error gracefully without panic
		err := frCtrl3.prepareAndSubmit(t.Context())
		require.NoError(t, err) // prepareAndSubmit logs errors but returns nil
	})
}

func createStorageWithRecipients(t *testing.T) (basedb.Database, registrystorage.Shares, registrystorage.Recipients) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	recipientStorage, setSharesUpdater := registrystorage.NewRecipientsStorage(logger, db, []byte("test"))
	shareStorage, _, err := registrystorage.NewSharesStorage(networkconfig.TestNetwork.Beacon, db, recipientStorage, []byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	setSharesUpdater(shareStorage)
	return db, shareStorage, recipientStorage
}

func populateStorage(t *testing.T, storage registrystorage.Shares, operatorData *registrystorage.OperatorData) {
	createShare := func(index int, operatorID spectypes.OperatorID) *types.SSVShare {
		// Create owner address consistently with fmt.Sprintf("0x%040x", index)
		ownerAddr := common.HexToAddress(fmt.Sprintf("0x%040x", index))

		return &types.SSVShare{
			Share: spectypes.Share{ValidatorPubKey: spectypes.ValidatorPK([]byte(fmt.Sprintf("pk%046d", index))),
				SharePubKey:    []byte(fmt.Sprintf("pk%046d", index)),
				ValidatorIndex: phase0.ValidatorIndex(index),
				Committee:      []*spectypes.ShareMember{{Signer: operatorID}},
			},
			Status:       eth2apiv1.ValidatorStateActiveOngoing,
			OwnerAddress: ownerAddr,
			Liquidated:   false,
		}
	}

	for i := 0; i < 100; i++ {
		require.NoError(t, storage.Save(nil, createShare(i, operatorData.ID)))
	}

	// add non-committee share
	require.NoError(t, storage.Save(nil, createShare(2000, spectypes.OperatorID(1))))

	all := storage.List(nil, registrystorage.ByOperatorID(operatorData.ID), registrystorage.ByNotLiquidated())
	require.Equal(t, 100, len(all))
}
