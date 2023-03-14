package validator

import (
	"context"
	"sync"
	"testing"
	"time"

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

func TestHandleNonCommitteeMessages(t *testing.T) {
	logger := logging.TestLogger(t)
	ctr := setupController(logger, map[string]*validator.Validator{}) // none committee
	go ctr.handleRouterMessages(logger)

	var wg sync.WaitGroup

	ctr.messageWorker.UseHandler(func(logger *zap.Logger, msg *spectypes.SSVMessage) error {
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

func TestGetIndices(t *testing.T) {
	validators := map[string]*validator.Validator{
		"0": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  0, // ValidatorStateUnknown
			Index:   3,
		}),
		"1": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  1, // ValidatorStatePendingInitialized
			Index:   3,
		}),
		"2": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  2, // ValidatorStatePendingQueued
			Index:   3,
		}),

		"3": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  3, // ValidatorStateActiveOngoing
			Index:   3,
		}),
		"4": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  4, // ValidatorStateActiveExiting
			Index:   4,
		}),
		"5": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  5, // ValidatorStateActiveSlashed
			Index:   5,
		}),

		"6": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  6, // ValidatorStateExitedUnslashed
			Index:   6,
		}),
		"7": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  7, // ValidatorStateExitedSlashed
			Index:   7,
		}),
		"8": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  8, // ValidatorStateWithdrawalPossible
			Index:   8,
		}),
		"9": newValidator(&beacon.ValidatorMetadata{
			Balance: 0,
			Status:  9, // ValidatorStateWithdrawalDone
			Index:   9,
		}),
	}

	logger := logging.TestLogger(t)
	ctr := setupController(logger, validators)
	indices := ctr.GetValidatorsIndices(logger)
	logger.Info("result", zap.Any("indices", indices))
	require.Equal(t, 1, len(indices)) // should return only active indices
}

func setupController(logger *zap.Logger, validators map[string]*validator.Validator) controller {
	return controller{
		context:                    context.Background(),
		collection:                 nil,
		beacon:                     nil,
		keyManager:                 nil,
		shareEncryptionKeyProvider: nil,
		validatorsMap: &validatorsMap{
			ctx:           context.Background(),
			lock:          sync.RWMutex{},
			validatorsMap: validators,
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
