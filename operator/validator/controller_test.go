package validator

import (
	"context"
	"sync"
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/queue/worker"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zap.DebugLevel, nil)
}

func TestHandleNonCommitteeMessages(t *testing.T) {
	logger := logex.GetLogger()
	ctr := setupController(logger, map[string]validator.IValidator{}) // none committee
	go ctr.handleRouterMessages()

	var wg sync.WaitGroup

	ctr.messageWorker.UseHandler(func(msg *spectypes.SSVMessage) error {
		wg.Done()
		return nil
	})

	wg.Add(2)

	identifier := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
	ctr.messageRouter.Route(spectypes.SSVMessage{
		MsgType: spectypes.SSVDecidedMsgType,
		MsgID:   identifier,
		Data:    []byte("data"),
	})

	ctr.messageRouter.Route(spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   identifier,
		Data:    generateChangeRoundMsg(t, identifier),
	})

	ctr.messageRouter.Route(spectypes.SSVMessage{ // checks that not process unnecessary message
		MsgType: message.SSVSyncMsgType,
		MsgID:   identifier,
		Data:    []byte("data"),
	})
	ctr.messageRouter.Route(spectypes.SSVMessage{ // checks that not process unnecessary message
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
	validators := map[string]validator.IValidator{
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

	logger := logex.GetLogger()
	ctr := setupController(logger, validators)
	indices := ctr.GetValidatorsIndices()
	logger.Info("result", zap.Any("indices", indices))
	require.Equal(t, 1, len(indices)) // should return only active indices
}

func setupController(logger *zap.Logger, validators map[string]validator.IValidator) controller {
	return controller{
		context:                    context.Background(),
		collection:                 nil,
		logger:                     logger,
		beacon:                     nil,
		keyManager:                 nil,
		shareEncryptionKeyProvider: nil,
		validatorsMap: &validatorsMap{
			logger:        logger.With(zap.String("component", "validatorsMap")),
			ctx:           context.Background(),
			lock:          sync.RWMutex{},
			validatorsMap: validators,
		},
		metadataUpdateQueue:    nil,
		metadataUpdateInterval: 0,
		messageRouter:          newMessageRouter(logger),
		messageWorker: worker.NewWorker(&worker.Config{
			Ctx:          context.Background(),
			Logger:       logger,
			WorkersCount: 1,
			Buffer:       100,
		}),
	}
}

func newValidator(metaData *beacon.ValidatorMetadata) validator.IValidator {
	return &validator.Validator{Share: &beacon.Share{
		NodeID:    0,
		PublicKey: nil,
		Committee: nil,
		Metadata:  metaData,
	}}
}

func generateChangeRoundMsg(t *testing.T, identifier spectypes.MessageID) []byte {
	crd := specqbft.RoundChangeData{
		PreparedValue:            nil,
		PreparedRound:            0,
		NextProposalData:         nil,
		RoundChangeJustification: nil,
	}
	encoded, err := crd.Encode()
	require.NoError(t, err)
	sm := specqbft.SignedMessage{
		Signature: []byte("sig"),
		Signers:   []spectypes.OperatorID{1},
		Message: &specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     0,
			Round:      1,
			Identifier: identifier[:],
			Data:       encoded,
		},
	}
	res, err := sm.Encode()
	require.NoError(t, err)
	return res
}
