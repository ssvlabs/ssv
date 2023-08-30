package validator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/bloxapp/ssv/ekm"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network/forks/genesis"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/validator/mocks"
	"github.com/bloxapp/ssv/protocol/v2/queue/worker"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
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
	metrics           Metrics
	recipientsStorage Recipients
	sharesStorage     SharesStorage
	validatorsMap     *validatorsMap
	beacon            beacon.BeaconNode
	signer            spectypes.KeyManager
	operatorData      *registrystorage.OperatorData
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
	// Setup logger and mock controller
	logger := logging.TestLogger(t)

	// Init global variables
	passedEpoch := phase0.Epoch(1)
	operatorIds := []uint64{1, 2, 3, 4}

	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	km, keyManagerSignerError := ekm.NewETHKeyManagerSigner(logger, db, networkconfig.TestNetwork, true, "")
	require.NoError(t, keyManagerSignerError)
	validatorKey, err := createKey() // Assuming createKey() returns ([]byte, error)
	require.NoError(t, err)

	var validatorPublicKey phase0.BLSPubKey // Assuming phase0.BLSPubKey is a type alias for [48]byte

	// Check the length before copying
	if len(validatorKey) == len(validatorPublicKey) {
		copy(validatorPublicKey[:], validatorKey)
	} else {
		// Handle the error, lengths don't match
		t.Fatalf("Length mismatch: validatorKey has length %d, but expected %d", len(validatorKey), len(validatorPublicKey))
	}

	operators := make([]*spectypes.Operator, len(operatorIds))
	for i, id := range operatorIds {
		operatorKey, keyError := createKey()
		require.NoError(t, keyError)
		operators[i] = &spectypes.Operator{OperatorID: id, PubKey: operatorKey}
	}

	shareWithMetaData := &types.SSVShare{
		Share: spectypes.Share{
			OperatorID:      2,
			Committee:       operators,
			ValidatorPubKey: validatorKey,
		},
		Metadata: types.Metadata{
			OwnerAddress: common.BytesToAddress([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			BeaconMetadata: &beacon.ValidatorMetadata{
				Balance:         0,
				Status:          3, // ValidatorStateActiveOngoing
				Index:           1,
				ActivationEpoch: passedEpoch,
			},
		},
	}

	shareWithoutMetaData := &types.SSVShare{
		Share: spectypes.Share{
			OperatorID:      2,
			Committee:       operators,
			ValidatorPubKey: validatorKey,
		},
		Metadata: types.Metadata{
			OwnerAddress:   common.BytesToAddress([]byte("62Ce5c69260bd819B4e0AD13f4b873074D479811")),
			BeaconMetadata: nil,
		},
	}

	operatorData := buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811")
	recipientData := buildFeeRecipient("67Ce5c69260bd819B4e0AD13f4b873074D479811", "45E668aba4b7fc8761331EC3CE77584B7A99A51A")
	ownerAddressBytes := decodeHex(t, "67Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode owner address")
	feeRecipientBytes := decodeHex(t, "45E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode second fee recipient address")

	storageMu := sync.Mutex{}
	storageData := make(map[string]*beacon.ValidatorMetadata)
	testValidator := setupTestValidator(ownerAddressBytes, feeRecipientBytes)
	bcResponse := map[phase0.ValidatorIndex]*eth2apiv1.Validator{
		0: {
			Balance: 0,
			Status:  3,
			Index:   2,
			Validator: &phase0.Validator{
				ActivationEpoch: passedEpoch,
				PublicKey:       validatorPublicKey,
			},
		},
	}

	testCases := []struct {
		recipientMockTimes int
		bcMockTimes        int
		recipientFound     bool
		recipientErr       error
		bcResponseErr      error
		name               string
		shares             []*types.SSVShare
		comparisonObject   *setUpValidatorsResult
		recipientData      *registrystorage.RecipientData
		bcResponse         map[phase0.ValidatorIndex]*eth2apiv1.Validator
	}{
		{
			name:               "setting fee recipient to storage data",
			shares:             []*types.SSVShare{shareWithMetaData, shareWithoutMetaData},
			recipientData:      recipientData,
			recipientFound:     true,
			recipientErr:       nil,
			recipientMockTimes: 1,
			comparisonObject: &setUpValidatorsResult{
				Shares:          2,
				Started:         1,
				Failures:        0,
				MissingMetadata: 1,
			},
			bcResponse:    bcResponse,
			bcResponseErr: nil,
			bcMockTimes:   1,
		},
		{
			name:               "setting fee recipient to owner address",
			shares:             []*types.SSVShare{shareWithMetaData, shareWithoutMetaData},
			recipientData:      nil,
			recipientFound:     false,
			recipientErr:       nil,
			recipientMockTimes: 1,
			comparisonObject: &setUpValidatorsResult{
				Shares:          2,
				Started:         1,
				Failures:        0,
				MissingMetadata: 1,
			},
			bcResponse:    bcResponse,
			bcResponseErr: nil,
			bcMockTimes:   1,
		},
		{
			name:               "failed to set fee recipient",
			shares:             []*types.SSVShare{shareWithMetaData},
			recipientData:      nil,
			recipientFound:     false,
			recipientErr:       errors.New("some error"),
			recipientMockTimes: 1,
			comparisonObject: &setUpValidatorsResult{
				Shares:          1,
				Started:         0,
				Failures:        1,
				MissingMetadata: 0,
			},
			bcResponse:    bcResponse,
			bcResponseErr: nil,
			bcMockTimes:   0,
		},
		{
			name:               "start share with metadata",
			shares:             []*types.SSVShare{shareWithMetaData},
			recipientData:      nil,
			recipientFound:     false,
			recipientErr:       nil,
			recipientMockTimes: 1,
			comparisonObject: &setUpValidatorsResult{
				Shares:          1,
				Started:         1,
				Failures:        0,
				MissingMetadata: 0,
			},
			bcResponse:    bcResponse,
			bcResponseErr: nil,
			bcMockTimes:   0,
		},
		{
			name:               "start share without metadata",
			bcMockTimes:        1,
			bcResponseErr:      nil,
			recipientMockTimes: 0,
			recipientData:      nil,
			recipientErr:       nil,
			recipientFound:     false,
			bcResponse:         bcResponse,
			shares:             []*types.SSVShare{shareWithoutMetaData},
			comparisonObject: &setUpValidatorsResult{
				Shares:          1,
				Started:         0,
				Failures:        0,
				MissingMetadata: 1,
			},
		},
		{
			name:               "failed to get GetValidatorData",
			recipientMockTimes: 0,
			bcMockTimes:        1,
			recipientData:      nil,
			recipientErr:       nil,
			bcResponse:         nil,
			recipientFound:     false,
			comparisonObject:   nil,
			bcResponseErr:      errors.New("some error"),
			shares:             []*types.SSVShare{shareWithoutMetaData},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			bc := beacon.NewMockBeaconNode(ctrl)
			metrics := mocks.NewMockMetrics(ctrl)
			storageMap := ibftstorage.NewStores()
			recipientStorage := mocks.NewMockRecipients(ctrl)
			sharesStorage := mocks.NewMockSharesStorage(ctrl)

			sharesStorage.EXPECT().Get(gomock.Any(), gomock.Any()).Return(shareWithMetaData).AnyTimes()
			sharesStorage.EXPECT().UpdateValidatorMetadata(gomock.Any(), gomock.Any()).DoAndReturn(func(pk string, metadata *beacon.ValidatorMetadata) error {
				storageMu.Lock()
				defer storageMu.Unlock()

				storageData[pk] = metadata

				return nil
			}).AnyTimes()

			mockValidatorMap := &validatorsMap{
				ctx: context.Background(),
				optsTemplate: &validator.Options{
					Network: nil,
					Signer:  nil,
					Storage: storageMap,
				},
				validatorsMap: map[string]*validator.Validator{"0": testValidator},
			}

			// Set up the controller with mock data
			controllerOptions := MockControllerOptions{
				beacon:            bc,
				signer:            km,
				metrics:           metrics,
				sharesStorage:     sharesStorage,
				operatorData:      operatorData,
				recipientsStorage: recipientStorage,
				validatorsMap:     mockValidatorMap,
			}

			recipientStorage.EXPECT().GetRecipientData(gomock.Any(), gomock.Any()).Return(tc.recipientData, tc.recipientFound, tc.recipientErr).Times(tc.recipientMockTimes)
			bc.EXPECT().GetValidatorData(gomock.Any()).Return(tc.bcResponse, tc.bcResponseErr).Times(tc.bcMockTimes)
			ctr := setupController(logger, controllerOptions)
			disableMetrics(metrics)
			result, setupValidatorError := ctr.setupValidators(tc.shares)
			require.Equal(t, tc.comparisonObject, result, "Unexpected result")
			if tc.bcResponseErr != nil {
				require.Error(t, setupValidatorError, "Unexpected error")
			} else {
				require.NoError(t, setupValidatorError, "Unexpected error")
			}
			// Add any assertions here to validate the behavior
		})
	}
}

func TestAssertionOperatorData(t *testing.T) {
	// Setup logger and mock controller
	logger := logging.TestLogger(t)
	operatorData := buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811")
	newOperatorData := buildOperatorData(2, "64Ce5c69260bd819B4e0AD13f4b873074D479811")

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
			operatorData:  buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"),
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
			operatorData:  buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"),
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
			operatorData:  buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"),
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
			operatorData:  buildOperatorData(1, "67Ce5c69260bd819B4e0AD13f4b873074D479811"),
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

	ownerAddressBytes := decodeHex(t, "67Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode owner address")
	fakeOwnerAddressBytes := decodeHex(t, "61Ce5c69260bd819B4e0AD13f4b873074D479811", "Failed to decode fake owner address")
	firstFeeRecipientBytes := decodeHex(t, "41E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode first fee recipient address")
	secondFeeRecipientBytes := decodeHex(t, "45E668aba4b7fc8761331EC3CE77584B7A99A51A", "Failed to decode second fee recipient address")

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

		err := ctr.UpdateFeeRecipient(common.BytesToAddress(ownerAddressBytes), common.BytesToAddress(secondFeeRecipientBytes))
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

		err := ctr.UpdateFeeRecipient(common.BytesToAddress(fakeOwnerAddressBytes), common.BytesToAddress(secondFeeRecipientBytes))
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
		keyManager:                 nil,
		shareEncryptionKeyProvider: nil,
		beacon:                     opts.beacon,
		metrics:                    opts.metrics,
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
				FeeRecipientAddress: common.BytesToAddress(feeRecipientBytes),
			},
			Metadata: types.Metadata{
				OwnerAddress: common.BytesToAddress(ownerAddressBytes),
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

func disableMetrics(metrics *mocks.MockMetrics) {
	metrics.EXPECT().ValidatorError(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorExiting(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorInactive(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorNotActivated(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorUnknown(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorSlashed(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorReady(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorPending(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorRemoved(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorError(gomock.Any()).Return().AnyTimes()
	metrics.EXPECT().ValidatorNotFound(gomock.Any()).Return().AnyTimes()
}

func decodeHex(t *testing.T, hexStr string, errMsg string) []byte {
	bytes, err := hex.DecodeString(hexStr)
	require.NoError(t, err, errMsg)
	return bytes
}

func buildOperatorData(id uint64, ownerAddress string) *registrystorage.OperatorData {
	return &registrystorage.OperatorData{
		ID:           id,
		PublicKey:    []byte("samplePublicKey"),
		OwnerAddress: common.BytesToAddress([]byte(ownerAddress)),
	}
}

func buildFeeRecipient(Owner string, FeeRecipient string) *registrystorage.RecipientData {
	feeRecipientSlice := []byte(FeeRecipient) // Assuming FeeRecipient is a string or similar
	var executionAddress bellatrix.ExecutionAddress
	copy(executionAddress[:], feeRecipientSlice)
	return &registrystorage.RecipientData{
		Owner:        common.BytesToAddress([]byte(Owner)),
		FeeRecipient: executionAddress,
		Nonce:        nil,
	}
}

func createKey() ([]byte, error) {
	pubKey := make([]byte, 48)
	_, err := rand.Read(pubKey)
	if err != nil {
		fmt.Println("Error generating random bytes:", err)
		return nil, err
	}
	return pubKey, nil
}
