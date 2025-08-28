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

// testRecipientStorage wraps real recipient storage but provides test-specific behavior
type testRecipientStorage struct {
	customRecipients map[common.Address]bellatrix.ExecutionAddress
}

func newTestRecipientStorage() *testRecipientStorage {
	return &testRecipientStorage{
		customRecipients: make(map[common.Address]bellatrix.ExecutionAddress),
	}
}

func (trs *testRecipientStorage) SetCustomRecipient(owner common.Address, recipient bellatrix.ExecutionAddress) {
	trs.customRecipients[owner] = recipient
}

func (trs *testRecipientStorage) GetFeeRecipient(owner common.Address) (bellatrix.ExecutionAddress, error) {
	if recipient, found := trs.customRecipients[owner]; found {
		return recipient, nil
	}
	// Return error to simulate no custom recipient (fallback will be handled by ValidatorStore)
	return bellatrix.ExecutionAddress{}, fmt.Errorf("no custom recipient for owner %s", owner.Hex())
}

// testValidatorProvider implements ValidatorProvider for testing
type testValidatorProvider struct {
	shares           registrystorage.Shares
	recipientStorage *testRecipientStorage
	operatorID       spectypes.OperatorID
}

func newTestValidatorProvider(shares registrystorage.Shares, recipients *testRecipientStorage, operatorID spectypes.OperatorID) *testValidatorProvider {
	return &testValidatorProvider{
		shares:           shares,
		recipientStorage: recipients,
		operatorID:       operatorID,
	}
}

func (tvp *testValidatorProvider) SelfValidators() []*types.SSVShare {
	return tvp.shares.List(nil, registrystorage.ByOperatorID(tvp.operatorID), registrystorage.ByNotLiquidated())
}

func (tvp *testValidatorProvider) GetFeeRecipient(validatorPK spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error) {
	// Find the share to get the owner
	shares := tvp.shares.List(nil)
	for _, share := range shares {
		if share.ValidatorPubKey == validatorPK {
			// Get fee recipient from test storage
			if tvp.recipientStorage != nil {
				recipient, err := tvp.recipientStorage.GetFeeRecipient(share.OwnerAddress)
				if err == nil {
					return recipient, nil
				}
			}
			// Fallback to owner address if no custom recipient
			var defaultRecipient bellatrix.ExecutionAddress
			copy(defaultRecipient[:], share.OwnerAddress.Bytes())
			return defaultRecipient, nil
		}
	}
	// Return error for validator not found
	return bellatrix.ExecutionAddress{}, fmt.Errorf("validator not found: %x", validatorPK)
}

func TestSubmitProposal(t *testing.T) {
	logger := log.TestLogger(t)
	ctrl := gomock.NewController(t)

	operatorData := &registrystorage.OperatorData{
		ID: 123456789,
	}

	operatorDataStore := operatordatastore.New(operatorData)

	db, shareStorage := createStorage(t)
	defer func() { require.NoError(t, db.Close()) }()

	beaconConfig := networkconfig.TestNetwork.Beacon
	populateStorage(t, shareStorage, operatorData)

	t.Run("custom fee recipients from storage", func(t *testing.T) {
		// Create test recipient storage
		testRecipientStorage := newTestRecipientStorage()

		// Create ValidatorProvider for testing
		validatorProvider := newTestValidatorProvider(shareStorage, testRecipientStorage, operatorData.ID)

		frCtrl := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider,
			OperatorDataStore: operatorDataStore,
		})
		// Define custom recipients for testing
		type recipientConfig struct {
			owner     common.Address
			recipient bellatrix.ExecutionAddress
			index     phase0.ValidatorIndex // expected validator index
		}

		customRecipients := []recipientConfig{
			{
				owner:     common.HexToAddress("0x0000000000000000000000000000000000000000"),
				recipient: bellatrix.ExecutionAddress{0x11, 0x22, 0x33, 0x44, 0x55},
				index:     0,
			},
			{
				owner:     common.HexToAddress("0x0000000000000000000000000000000000000001"),
				recipient: bellatrix.ExecutionAddress{0xaa, 0xbb, 0xcc, 0xdd, 0xee},
				index:     1,
			},
		}

		// Set custom recipients in test storage
		for _, config := range customRecipients {
			testRecipientStorage.SetCustomRecipient(config.owner, config.recipient)
		}

		var capturedRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
				capturedRecipients = feeRecipients
				return nil
			}).Times(1)

		frCtrl.beaconClient = client

		frCtrl.prepareAndSubmit(t.Context())

		// Verify custom recipients are used for configured validators
		for _, config := range customRecipients {
			require.Equal(t, config.recipient, capturedRecipients[config.index],
				"validator %d should have custom recipient", config.index)
		}

		// Verify owner address is used as default for non-configured validators
		owner2Addr := common.HexToAddress("0x0000000000000000000000000000000000000002")
		var expectedDefault bellatrix.ExecutionAddress
		copy(expectedDefault[:], owner2Addr.Bytes())
		require.Equal(t, expectedDefault, capturedRecipients[2],
			"validator 2 should use owner address as default")
	})

	t.Run("owner address as default fee recipient", func(t *testing.T) {
		// Create fresh storage without custom recipients
		db2, shareStorage2 := createStorage(t)
		defer func() { require.NoError(t, db2.Close()) }()
		populateStorage(t, shareStorage2, operatorData)

		// Create test recipient storage without custom recipients
		testRecipientStorage2 := newTestRecipientStorage()

		// Create ValidatorProvider for test
		validatorProvider2 := newTestValidatorProvider(shareStorage2, testRecipientStorage2, operatorData.ID)

		frCtrl2 := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider2,
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

		frCtrl2.prepareAndSubmit(t.Context())

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
		// Create test recipient storage
		testRecipientStorage := newTestRecipientStorage()

		// Create ValidatorProvider for testing
		validatorProvider := newTestValidatorProvider(shareStorage, testRecipientStorage, operatorData.ID)

		frCtrl := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider,
			OperatorDataStore: operatorDataStore,
		})

		var capturedRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
				capturedRecipients = feeRecipients
				return nil
			}).Times(1)

		frCtrl.beaconClient = client

		frCtrl.prepareAndSubmit(t.Context())

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
		db2, shareStorage2 := createStorage(t)
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

		// Create test recipient storage
		testRecipientStorage2 := newTestRecipientStorage()

		// Create ValidatorProvider for test
		validatorProvider2 := newTestValidatorProvider(shareStorage2, testRecipientStorage2, operatorData.ID)

		frCtrl2 := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider2,
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

		frCtrl2.prepareAndSubmit(t.Context())

		// Should have exactly 1 batch of 500
		require.Equal(t, 500, batchSize)
	})

	t.Run("error handling", func(t *testing.T) {
		// Test that errors are handled gracefully
		db3, shareStorage3 := createStorage(t)
		defer func() { require.NoError(t, db3.Close()) }()
		populateStorage(t, shareStorage3, operatorData)

		// Create test recipient storage
		testRecipientStorage3 := newTestRecipientStorage()

		// Create ValidatorProvider for test
		validatorProvider3 := newTestValidatorProvider(shareStorage3, testRecipientStorage3, operatorData.ID)

		frCtrl3 := NewController(logger, &ControllerOptions{
			Ctx:               t.Context(),
			BeaconConfig:      beaconConfig,
			ValidatorProvider: validatorProvider3,
			OperatorDataStore: operatorDataStore,
		})

		client := beacon.NewMockBeaconNode(ctrl)
		client.EXPECT().SubmitProposalPreparation(gomock.Any(), gomock.Any()).Return(errors.New("failed to submit")).Times(1)

		frCtrl3.beaconClient = client

		// Should handle error gracefully without panic
		frCtrl3.prepareAndSubmit(t.Context())
	})
}

func createStorage(t *testing.T) (basedb.Database, registrystorage.Shares) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	// Create a minimal recipient storage just for SharesStorage initialization
	recipientStorage, err := registrystorage.NewRecipientsStorage(logger, db, []byte("test"))
	require.NoError(t, err)
	shareStorage, _, err := registrystorage.NewSharesStorage(networkconfig.TestNetwork.Beacon, db, recipientStorage.GetFeeRecipient, []byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	return db, shareStorage
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
