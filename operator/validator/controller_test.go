package validator

import (
	"context"
	"encoding/hex"
	"github.com/ethereum/go-ethereum/common"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/bloxapp/ssv/logging"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/forks/genesis"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/queue/worker"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// TODO: increase test coverage, add more tests, e.g.:
// 1. a validator with a non-empty share and empty metadata - test a scenario if we cannot get metadata from beacon node

type MockControllerOptions struct {
	validatorsMap map[string]*validator.Validator
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
	println("<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<,here")
	println(len(activeIndicesForCurrentEpoch))
	println("<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<,here")
	require.Equal(t, 2, len(activeIndicesForCurrentEpoch)) // should return only active indices

	activeIndicesForNextEpoch := ctr.ActiveValidatorIndices(currentEpoch + 1)
	require.Equal(t, 3, len(activeIndicesForNextEpoch)) // should return including ValidatorStatePendingQueued
}

func setupController(logger *zap.Logger, controllerOptions MockControllerOptions) controller {
	return controller{
		context:                    context.Background(),
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
