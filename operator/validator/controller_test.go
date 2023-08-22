package validator

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/network/forks/genesis"
	"github.com/bloxapp/ssv/operator/validator/mocks"
	"github.com/bloxapp/ssv/protocol/v2/queue/worker"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
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
	validatorsMap map[string]*validator.Validator
	operatorData  *registrystorage.OperatorData
	sharesStorage SharesStorage
}

func TestHandleNonCommitteeMessages(t *testing.T) {
	logger := logging.TestLogger(t)
	controllerOptions := MockControllerOptions{
		validatorsMap: map[string]*validator.Validator{},
	}
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

func TestAssertionOperatorData(t *testing.T) {
	// Setup logger and mock controller
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	sharesStorage := mocks.NewMockSharesStorage(ctrl)

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
		sharesStorage: sharesStorage,
		operatorData:  operatorData,
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

	// Set up the controller with mock data
	controllerOptions := MockControllerOptions{
		validatorsMap: map[string]*validator.Validator{
			"0": testValidator,
		},
	}
	ctr := setupController(logger, controllerOptions)

	// Execute the function under test and validate results
	_, found := ctr.GetValidator("0")
	require.True(t, found)
	_, found = ctr.GetValidator("1")
	require.False(t, found)
}

func TestGetValidatorStats(t *testing.T) {
	// Setup logger and mock controller
	logger := logging.TestLogger(t)
	ctrl := gomock.NewController(t)
	sharesStorage := mocks.NewMockSharesStorage(ctrl)

	// Define constants and sample data
	passedEpoch := phase0.Epoch(1)
	operatorIds := []uint64{1, 2}

	// Create operators
	operators := make([]*spectypes.Operator, len(operatorIds))
	for i, id := range operatorIds {
		operators[i] = &spectypes.Operator{OperatorID: id}
	}

	// Create a sample SSVShare slice
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
					Status:          1, // ValidatorStatePendingInitialized
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
					Status:          1, // ValidatorStatePendingInitialized
					Index:           0,
					ActivationEpoch: passedEpoch,
				},
			},
		},
	}

	// Set mock expectations
	sharesStorage.EXPECT().List(nil).Return(sharesSlice).Times(1)

	// Set up the controller with mock data
	controllerOptions := MockControllerOptions{
		sharesStorage: sharesStorage,
		operatorData: &registrystorage.OperatorData{
			ID:           1,
			PublicKey:    []byte("samplePublicKeyData"),
			OwnerAddress: common.Address([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
		},
	}
	ctr := setupController(logger, controllerOptions)

	// Execute the function under test and validate results
	allShares, activeShares, operatorShares, err := ctr.GetValidatorStats()
	require.NoError(t, err, "Failed to get validator stats")
	require.Equal(t, len(sharesSlice), int(allShares), "Unexpected total shares count")
	require.Equal(t, 1, int(activeShares), "Unexpected active shares count")
	require.Equal(t, 2, int(operatorShares), "Unexpected operator shares count")
}

func TestUpdateFeeRecipient(t *testing.T) {
	// Setup logger for testing
	logger := logging.TestLogger(t)

	// Decode owner and fee recipient addresses from hex strings
	ownerAddressBytes, err := hex.DecodeString("67Ce5c69260bd819B4e0AD13f4b873074D479811")
	require.NoError(t, err, "Failed to decode owner address")

	newFeeRecipientBytes, err := hex.DecodeString("45E668aba4b7fc8761331EC3CE77584B7A99A51A")
	require.NoError(t, err, "Failed to decode new fee recipient address")

	// Initialize a test validator with the decoded owner address
	testValidator := &validator.Validator{
		Share: &types.SSVShare{
			Metadata: types.Metadata{
				OwnerAddress: common.Address(ownerAddressBytes),
			},
		},
	}

	// Set up the controller with the initialized validator
	controllerOptions := MockControllerOptions{
		validatorsMap: map[string]*validator.Validator{
			"0": testValidator,
		},
	}
	ctr := setupController(logger, controllerOptions)

	// Update the fee recipient in the controller and check for errors
	err = ctr.UpdateFeeRecipient(common.Address(ownerAddressBytes), common.Address(newFeeRecipientBytes))
	require.NoError(t, err, "Failed to update fee recipient")

	// Convert the FeeRecipientAddress to a slice for comparison
	actualFeeRecipient := testValidator.Share.FeeRecipientAddress[:]

	// Assert that the fee recipient address was updated correctly
	require.Equal(t, newFeeRecipientBytes, actualFeeRecipient, "Fee recipient address mismatch")
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

	logger := logging.TestLogger(t)
	controllerOptions := MockControllerOptions{
		validatorsMap: validators,
	}
	ctr := setupController(logger, controllerOptions)

	activeIndicesForCurrentEpoch := ctr.ActiveValidatorIndices(currentEpoch)
	require.Equal(t, 2, len(activeIndicesForCurrentEpoch)) // should return only active indices

	activeIndicesForNextEpoch := ctr.ActiveValidatorIndices(currentEpoch + 1)
	require.Equal(t, 3, len(activeIndicesForNextEpoch)) // should return including ValidatorStatePendingQueued
}

func setupController(logger *zap.Logger, controllerOptions MockControllerOptions) controller {
	return controller{
		context:                    context.Background(),
		operatorData:               controllerOptions.operatorData,
		sharesStorage:              controllerOptions.sharesStorage,
		beacon:                     nil,
		logger:                     logger,
		keyManager:                 nil,
		shareEncryptionKeyProvider: nil,
		validatorsMap: &validatorsMap{
			ctx:           context.Background(),
			lock:          sync.RWMutex{},
			validatorsMap: controllerOptions.validatorsMap,
		},
		metadataUpdateQueue:    nil,
		metadataUpdateInterval: 0,
		messageRouter:          newMessageRouter(genesis.New().MsgID()),
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
