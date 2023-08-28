package validator

import (
	"context"
	"encoding/hex"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network/forks/genesis"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/validator/mocks"
	"github.com/bloxapp/ssv/protocol/v2/queue/worker"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/golang/mock/gomock"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/bloxapp/ssv/logging"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// TODO: increase test coverage, add more tests, e.g.:
// 1. a validator with a non-empty share and empty metadata - test a scenario if we cannot get metadata from beacon node

type MockControllerOptions struct {
	sharesStorage     SharesStorage
	recipientsStorage Recipients
	signer            spectypes.KeyManager
	operatorData      *registrystorage.OperatorData
	validatorsMap     *validatorsMap
}

func TestHandleNonCommitteeMessages(t *testing.T) {
	logger := logging.TestLogger(t)
	mockValidatorsMap := &validatorsMap{
		ctx:           context.Background(),
		lock:          sync.RWMutex{},
		validatorsMap: nil,
	}
	controllerOptions := MockControllerOptions{validatorsMap: mockValidatorsMap}
	ctr := setupController(logger, controllerOptions) // none committee
	go ctr.handleRouterMessages()

	var wg sync.WaitGroup

	ctr.messageWorker.UseHandler(func(msg *spectypes.SSVMessage) error {
		wg.Done()
		return nil
	})

	wg.Add(2)

	identifier := spectypes.NewMsgID(types.GetDefaultDomain(), []byte("pk"), spectypes.BNRoleAttester)

	ctr.messageRouter.Route(logger, spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   identifier,
		Data:    generateDecidedMessage(t, identifier),
	})

	ctr.messageRouter.Route(logger, spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   identifier,
		Data:    generateChangeRoundMsg(t, identifier),
	})

	ctr.messageRouter.Route(logger, spectypes.SSVMessage{ // checks that not process unnecessary message
		MsgType: message.SSVSyncMsgType,
		MsgID:   identifier,
		Data:    []byte("data"),
	})

	ctr.messageRouter.Route(logger, spectypes.SSVMessage{ // checks that not process unnecessary message
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   identifier,
		Data:    []byte("data"),
	})

	go func() {
		time.Sleep(time.Second * 4)
		panic("time out!")
	}()

	wg.Wait()
}

func TestSetupValidators(t *testing.T) {
	passedEpoch := phase0.Epoch(1)
	// Setup logger and mock controller
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	recipientStorage := mocks.NewMockRecipients(ctrl)
	recipientData := &registrystorage.RecipientData{
		Owner:        common.Address([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
		FeeRecipient: bellatrix.ExecutionAddress([]byte("45E668aba4b7fc8761331EC3CE77584B7A99A51A")),
		Nonce:        nil,
	}
	decodeHex := func(hexStr string, errMsg string) []byte {
		bytes, err := hex.DecodeString(hexStr)
		require.NoError(t, err, errMsg)
		return bytes
	}

	db, err := getBaseStorage(logger)
	require.NoError(t, err)

	km, err := ekm.NewETHKeyManagerSigner(logger, db, networkconfig.TestNetwork, true, "")

	ownerAddressBytes := decodeHex("67Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode owner address")
	feeRecipientBytes := decodeHex("45E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode second fee recipient address")

	testValidator := setupTestValidator(ownerAddressBytes, feeRecipientBytes)
	recipientStorage.EXPECT().GetRecipientData(gomock.Any(), gomock.Any()).Return(recipientData, true, nil).Times(1)

	mockValidatorMap := &validatorsMap{
		ctx: context.Background(),
		optsTemplate: &validator.Options{
			Network: nil,
			Storage: &storage.QBFTStores{},
			Signer:  nil,
			SSVShare: &types.SSVShare{
				Share: spectypes.Share{
					ValidatorPubKey: nil,
				},
			},
		},
		validatorsMap: map[string]*validator.Validator{"0": testValidator},
	}

	// Set up the controller with mock data
	controllerOptions := MockControllerOptions{
		signer:            km,
		recipientsStorage: recipientStorage,
		validatorsMap:     mockValidatorMap,
	}

	ctr := setupController(logger, controllerOptions)

	operatorIds := []uint64{1, 2, 3, 4}
	operators := make([]*spectypes.Operator, len(operatorIds))
	for i, id := range operatorIds {
		operators[i] = &spectypes.Operator{OperatorID: id}
	}

	sharesSlice := []*types.SSVShare{
		{
			Share: spectypes.Share{
				OperatorID: 1,
				Committee:  operators,
			},
			Metadata: types.Metadata{
				OwnerAddress: common.Address([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
				BeaconMetadata: &beacon.ValidatorMetadata{
					Balance:         0,
					Status:          3, // ValidatorStatePendingInitialized
					Index:           0,
					ActivationEpoch: passedEpoch,
				},
			},
		},
	}

	ctr.setupValidators(sharesSlice)
}

func TestAssertionOperatorData(t *testing.T) {
	// Setup logger and mock controller
	logger := logging.TestLogger(t)
	operatorData := &registrystorage.OperatorData{
		ID:           1,
		PublicKey:    []byte("samplePublicKeyData"),
		OwnerAddress: common.Address([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
	}

	newOperatorData := &registrystorage.OperatorData{
		ID:           2,
		PublicKey:    []byte("samplePublicKeyData2"),
		OwnerAddress: common.Address([]byte("64Ce5c69260bd819B4e0AD13f4b873074D479811")),
	}

	// Set up the controller with mock data
	controllerOptions := MockControllerOptions{
		operatorData: operatorData,
	}
	ctr := setupController(logger, controllerOptions)
	ctr.SetOperatorData(newOperatorData)

	// Compare the objects
	require.Equal(t, newOperatorData, ctr.GetOperatorData(), "The operator data did not match the expected value")
}

func TestGetValidator(t *testing.T) {
	// Setup logger and mock controller
	logger := logging.TestLogger(t)

	// Initialize a test validator with the decoded owner address
	testValidator := &validator.Validator{
		Share: &types.SSVShare{
			Metadata: types.Metadata{},
		},
	}

	mockValidatorsMap := &validatorsMap{
		ctx:  context.Background(),
		lock: sync.RWMutex{},
		validatorsMap: map[string]*validator.Validator{
			"0": testValidator,
		},
	}
	// Set up the controller with mock data
	controllerOptions := MockControllerOptions{
		validatorsMap: mockValidatorsMap,
	}
	ctr := setupController(logger, controllerOptions)

	// Execute the function under test and validate results
	_, found := ctr.GetValidator("0")
	require.True(t, found)
	_, found = ctr.GetValidator("1")
	require.False(t, found)
}

func TestGetValidatorStats(t *testing.T) {
	// Common setup
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	sharesStorage := mocks.NewMockSharesStorage(ctrl)
	passedEpoch := phase0.Epoch(1)

	t.Run("Test with multiple operators", func(t *testing.T) {
		// Setup for this subtest
		operatorIds := []uint64{1, 2, 3}
		operators := make([]*spectypes.Operator, len(operatorIds))
		for i, id := range operatorIds {
			operators[i] = &spectypes.Operator{OperatorID: id}
		}

		// Create a sample SSVShare slice for this subtest
		sharesSlice := []*types.SSVShare{
			{
				Share: spectypes.Share{
					OperatorID: 1,
					Committee:  operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          3, // ValidatorStatePendingInitialized
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
			{
				Share: spectypes.Share{
					OperatorID: 2,
					Committee:  operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          1, // Some other status
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
		}

		// Set mock expectations for this subtest
		sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

		// Set up the controller with mock data for this subtest
		controllerOptions := MockControllerOptions{
			sharesStorage: sharesStorage,
			operatorData: &registrystorage.OperatorData{
				ID:           1,
				PublicKey:    []byte("samplePublicKeyData"),
				OwnerAddress: common.Address([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			},
		}
		ctr := setupController(logger, controllerOptions)

		// Execute the function under test and validate results for this subtest
		allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
		require.NoError(t, err, "Failed to get validator stats")
		require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
		require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
		require.Equal(t, 1, int(operatorShares), "Unexpected operator shares count")
	})

	t.Run("Test with single operator", func(t *testing.T) {
		// Setup for this subtest
		operatorIds := []uint64{1}
		operators := make([]*spectypes.Operator, len(operatorIds))
		for i, id := range operatorIds {
			operators[i] = &spectypes.Operator{OperatorID: id}
		}

		// Create a sample SSVShare slice for this subtest
		sharesSlice := []*types.SSVShare{
			{
				Share: spectypes.Share{
					OperatorID: 1,
					Committee:  operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          3, // ValidatorStatePendingInitialized
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
		}

		// Set mock expectations for this subtest
		sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

		// Set up the controller with mock data for this subtest
		controllerOptions := MockControllerOptions{
			sharesStorage: sharesStorage,
			operatorData: &registrystorage.OperatorData{
				ID:           1,
				PublicKey:    []byte("samplePublicKeyData"),
				OwnerAddress: common.Address([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			},
		}
		ctr := setupController(logger, controllerOptions)

		// Execute the function under test and validate results for this subtest
		allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
		require.NoError(t, err, "Failed to get validator stats")
		require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
		require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
		require.Equal(t, 1, int(operatorShares), "Unexpected operator shares count")
	})

	t.Run("Test with no operators", func(t *testing.T) {
		// Create a sample SSVShare slice for this subtest
		sharesSlice := []*types.SSVShare{
			{
				Share: spectypes.Share{
					Committee: nil,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          3, // ValidatorStatePendingInitialized
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
		}

		// Set mock expectations for this subtest
		sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

		// Set up the controller with mock data for this subtest
		controllerOptions := MockControllerOptions{
			sharesStorage: sharesStorage,
			operatorData: &registrystorage.OperatorData{
				ID:           1,
				PublicKey:    []byte("samplePublicKeyData"),
				OwnerAddress: common.Address([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			},
		}
		ctr := setupController(logger, controllerOptions)

		// Execute the function under test and validate results for this subtest
		allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
		require.NoError(t, err, "Failed to get validator stats")
		require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
		require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
		require.Equal(t, 0, int(operatorShares), "Unexpected operator shares count")
	})

	t.Run("Test with varying statuses", func(t *testing.T) {
		// Setup for this subtest
		operatorIds := []uint64{1}
		operators := make([]*spectypes.Operator, len(operatorIds))
		for i, id := range operatorIds {
			operators[i] = &spectypes.Operator{OperatorID: id}
		}

		// Create a sample SSVShare slice for this subtest
		sharesSlice := []*types.SSVShare{
			{
				Share: spectypes.Share{
					OperatorID: 1,
					Committee:  operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          3, // ValidatorStatePendingInitialized
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
			{
				Share: spectypes.Share{
					OperatorID: 1,
					Committee:  operators,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Balance:         0,
						Status:          1,
						Index:           0,
						ActivationEpoch: passedEpoch,
					},
				},
			},
		}

		// Set mock expectations for this subtest
		sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

		// Set up the controller with mock data for this subtest
		controllerOptions := MockControllerOptions{
			sharesStorage: sharesStorage,
			operatorData: &registrystorage.OperatorData{
				ID:           1,
				PublicKey:    []byte("samplePublicKeyData"),
				OwnerAddress: common.Address([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			},
		}
		ctr := setupController(logger, controllerOptions)

		// Execute the function under test and validate results for this subtest
		allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
		require.NoError(t, err, "Failed to get validator stats")
		require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
		require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
		require.Equal(t, 2, int(operatorShares), "Unexpected operator shares count")
	})
}

func TestUpdateFeeRecipient(t *testing.T) {
	// Setup logger for testing
	logger := logging.TestLogger(t)

	decodeHex := func(hexStr string, errMsg string) []byte {
		bytes, err := hex.DecodeString(hexStr)
		require.NoError(t, err, errMsg)
		return bytes
	}

	ownerAddressBytes := decodeHex("67Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode owner address")
	fakeOwnerAddressBytes := decodeHex("61Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode fake owner address")
	firstFeeRecipientBytes := decodeHex("41E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode first fee recipient address")
	secondFeeRecipientBytes := decodeHex("45E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode second fee recipient address")

	t.Run("Test with right owner address", func(t *testing.T) {
		testValidator := setupTestValidator(ownerAddressBytes, firstFeeRecipientBytes)

		mockValidatorsMap := &validatorsMap{
			ctx:  context.Background(),
			lock: sync.RWMutex{},
			validatorsMap: map[string]*validator.Validator{
				"0": testValidator,
			},
		}
		controllerOptions := MockControllerOptions{validatorsMap: mockValidatorsMap}
		ctr := setupController(logger, controllerOptions)

		err := ctr.UpdateFeeRecipient(common.Address(ownerAddressBytes), common.Address(secondFeeRecipientBytes))
		require.NoError(t, err, "Unexpected error while updating fee recipient with correct owner address")

		actualFeeRecipient := testValidator.Share.FeeRecipientAddress[:]
		require.Equal(t, secondFeeRecipientBytes, actualFeeRecipient, "Fee recipient address did not update correctly")
	})

	t.Run("Test with wrong owner address", func(t *testing.T) {
		testValidator := setupTestValidator(ownerAddressBytes, firstFeeRecipientBytes)

		mockValidatorsMap := &validatorsMap{
			ctx:  context.Background(),
			lock: sync.RWMutex{},
			validatorsMap: map[string]*validator.Validator{
				"0": testValidator,
			},
		}
		controllerOptions := MockControllerOptions{validatorsMap: mockValidatorsMap}
		ctr := setupController(logger, controllerOptions)

		err := ctr.UpdateFeeRecipient(common.Address(fakeOwnerAddressBytes), common.Address(secondFeeRecipientBytes))
		require.NoError(t, err, "Unexpected error while updating fee recipient with incorrect owner address")

		actualFeeRecipient := testValidator.Share.FeeRecipientAddress[:]
		require.Equal(t, firstFeeRecipientBytes, actualFeeRecipient, "Fee recipient address should not have changed")
	})
}

func TestGetIndices(t *testing.T) {
	farFutureEpoch := phase0.Epoch(99999)
	currentEpoch := phase0.Epoch(100)
	validators := map[string]*validator.Validator{
		"0": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          0, // ValidatorStateUnknown
			Index:           0,
			ActivationEpoch: farFutureEpoch,
		}),
		"1": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          1, // ValidatorStatePendingInitialized
			Index:           0,
			ActivationEpoch: farFutureEpoch,
		}),
		"2": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          2, // ValidatorStatePendingQueued
			Index:           3,
			ActivationEpoch: phase0.Epoch(101),
		}),

		"3": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          3, // ValidatorStateActiveOngoing
			Index:           3,
			ActivationEpoch: phase0.Epoch(100),
		}),
		"4": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          4, // ValidatorStateActiveExiting
			Index:           4,
			ActivationEpoch: phase0.Epoch(100),
		}),
		"5": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          5, // ValidatorStateActiveSlashed
			Index:           5,
			ActivationEpoch: phase0.Epoch(100),
		}),

		"6": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          6, // ValidatorStateExitedUnslashed
			Index:           6,
			ActivationEpoch: phase0.Epoch(100),
		}),
		"7": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          7, // ValidatorStateExitedSlashed
			Index:           7,
			ActivationEpoch: phase0.Epoch(100),
		}),
		"8": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          8, // ValidatorStateWithdrawalPossible
			Index:           8,
			ActivationEpoch: phase0.Epoch(100),
		}),
		"9": newValidator(&beacon.ValidatorMetadata{
			Balance:         0,
			Status:          9, // ValidatorStateWithdrawalDone
			Index:           9,
			ActivationEpoch: phase0.Epoch(100),
		}),
	}
	mockValidatorsMap := &validatorsMap{
		ctx:           context.Background(),
		lock:          sync.RWMutex{},
		validatorsMap: validators,
	}
	logger := logging.TestLogger(t)
	controllerOptions := MockControllerOptions{
		validatorsMap: mockValidatorsMap,
	}
	ctr := setupController(logger, controllerOptions)

	activeIndicesForCurrentEpoch := ctr.ActiveValidatorIndices(currentEpoch)
	require.Equal(t, 2, len(activeIndicesForCurrentEpoch)) // should return only active indices

	activeIndicesForNextEpoch := ctr.ActiveValidatorIndices(currentEpoch + 1)
	require.Equal(t, 3, len(activeIndicesForNextEpoch)) // should return including ValidatorStatePendingQueued
}

func setupController(logger *zap.Logger, opts MockControllerOptions) controller {
	return controller{
		beacon:                     nil,
		keyManager:                 nil,
		shareEncryptionKeyProvider: nil,
		context:                    context.Background(),
		operatorData:               opts.operatorData,
		recipientsStorage:          opts.recipientsStorage,
		sharesStorage:              opts.sharesStorage,
		logger:                     logger,
		validatorsMap:              opts.validatorsMap,
		metadataUpdateQueue:        nil,
		metadataUpdateInterval:     0,
		messageRouter:              newMessageRouter(genesis.New().MsgID()),
		messageWorker: worker.NewWorker(logger, &worker.Config{
			Ctx:          context.Background(),
			WorkersCount: 1,
			Buffer:       100,
		}),
	}
}

func newValidator(metaData *beacon.ValidatorMetadata) *validator.Validator {
	return &validator.Validator{
		Share: &types.SSVShare{
			Metadata: types.Metadata{
				BeaconMetadata: metaData,
			},
		},
	}
}

func generateChangeRoundMsg(t *testing.T, identifier spectypes.MessageID) []byte {
	sm := specqbft.SignedMessage{
		Signature: append([]byte{1, 2, 3, 4}, make([]byte, 92)...),
		Signers:   []spectypes.OperatorID{1},
		Message: specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     0,
			Round:      1,
			Identifier: identifier[:],
			Root:       [32]byte{1, 2, 3},
		},
	}
	res, err := sm.Encode()
	require.NoError(t, err)
	return res
}

func generateDecidedMessage(t *testing.T, identifier spectypes.MessageID) []byte {
	sm := specqbft.SignedMessage{
		Signature: append([]byte{1, 2, 3, 4}, make([]byte, 92)...),
		Signers:   []spectypes.OperatorID{1, 2, 3},
		Message: specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     0,
			Round:      1,
			Identifier: identifier[:],
			Root:       [32]byte{1, 2, 3},
		},
	}
	res, err := sm.Encode()
	require.NoError(t, err)
	return res
}

func setupTestValidator(ownerAddressBytes, feeRecipientBytes []byte) *validator.Validator {
	return &validator.Validator{
		Share: &types.SSVShare{
			Share: spectypes.Share{
				FeeRecipientAddress: common.Address(feeRecipientBytes),
			},
			Metadata: types.Metadata{
				OwnerAddress: common.Address(ownerAddressBytes),
			},
		},
	}
}

func getBaseStorage(logger *zap.Logger) (basedb.Database, error) {
	return ssvstorage.GetStorageFactory(logger, basedb.Options{
		Type: "badger-memory",
		Path: "",
	})
}
