package validator

import (
	"context"
	"encoding/hex"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	logging "github.com/ipfs/go-log"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

var logger = logging.Logger("ssv/protocol/ssv/validator").Desugar()

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *zap.Logger

	DutyRunners runner.DutyRunners
	Beacon      specssv.BeaconNode
	Share       *types.SSVShare
	Signer      spectypes.KeyManager

	Storage *storage.QBFTStores
	Network specqbft.Network

	Q          queue.Queue
	queueState *queue.State

	state uint32
}

// NewValidator creates a new instance of Validator.
func NewValidator(pctx context.Context, options Options) *Validator {
	options.defaults()
	ctx, cancel := context.WithCancel(pctx)
	logger := logger.With(zap.String("validator", hex.EncodeToString(options.SSVShare.ValidatorPubKey)))
	v := &Validator{
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger.With(zap.String("validator", hex.EncodeToString(options.SSVShare.ValidatorPubKey))),
		DutyRunners: options.DutyRunners,
		Network:     options.Network,
		Beacon:      options.Beacon,
		Storage:     options.Storage,
		Share:       options.SSVShare,
		Signer:      options.Signer,
		Q:           queue.New(nil),
		queueState: &queue.State{
			HasRunningInstance: false,
			Height:             0,
			Slot:               0,
			Quorum:             options.SSVShare.Quorum,
		},
		state: uint32(NotStarted),
	}

	return v
}

// StartDuty starts a duty for the validator
func (v *Validator) StartDuty(duty *spectypes.Duty) error {
	dutyRunner := v.DutyRunners[duty.Type]
	if dutyRunner == nil {
		return errors.Errorf("duty type %s not supported", duty.Type.String())
	}
	return dutyRunner.StartNewDuty(duty)
}

// ProcessMessage processes Network Message of all types
func (v *Validator) ProcessMessage(msg *spectypes.SSVMessage) error {
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(msg.GetID())
	if dutyRunner == nil {
		return errors.Errorf("could not get duty runner for msg ID")
	}

	if err := validateMessage(v.Share.Share, msg); err != nil {
		return errors.Wrap(err, "Message invalid")
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		signedMsg := &specqbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get consensus Message from network Message")
		}
		if signedMsg == nil {
			return nil
		}
		//var ctrl_h uint64
		//if c := dutyRunner.GetBaseRunner().QBFTController; c != nil {
		//	ctrl_h = uint64(c.Height)
		//}
		//v.logger.Debug("got valid consensus message",
		//	zap.Int64("type", int64(signedMsg.Message.MsgType)),
		//	zap.Int64("msg_height", int64(signedMsg.Message.Height)),
		//	zap.Int64("msg_round", int64(signedMsg.Message.Round)),
		//	zap.Any("ctrl_h", ctrl_h),
		//)
		return dutyRunner.ProcessConsensus(signedMsg)
	case spectypes.SSVPartialSignatureMsgType:
		signedMsg := &specssv.SignedPartialSignatureMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		if signedMsg == nil {
			return nil
		}
		//var ctrl_h uint64
		//if c := dutyRunner.GetBaseRunner().QBFTController; c != nil {
		//	ctrl_h = uint64(c.Height)
		//}
		//v.logger.Debug("got valid post consensus message",
		//	zap.Int64("type", int64(signedMsg.Message.Type)),
		//	zap.Any("ctrl_h", ctrl_h),
		//)
		if signedMsg.Message.Type == specssv.PostConsensusPartialSig {
			//var runningInstanceHeight, controllerHeight uint64
			//var hasRunningInstance bool
			//currentState := dutyRunner.GetBaseRunner().State
			//if currentState != nil && currentState.RunningInstance != nil {
			//	runningInstanceHeight = uint64(currentState.RunningInstance.State.Height)
			//	hasRunningInstance = true
			//}
			//if dutyRunner.GetBaseRunner().QBFTController != nil {
			//	controllerHeight = uint64(dutyRunner.GetBaseRunner().QBFTController.Height)
			//}
			//v.logger.Debug("process post consensus", zap.String("identifier", hex.EncodeToString(v.Share.ValidatorPubKey)),
			//	zap.Bool("dutyRunnerStateNotNil", currentState != nil),
			//	zap.Bool("hasRunningInstance", hasRunningInstance),
			//	zap.Uint64("runningInstanceHeight", runningInstanceHeight),
			//	zap.Uint64("controllerHeight", controllerHeight))
			return dutyRunner.ProcessPostConsensus(signedMsg)
		}
		return dutyRunner.ProcessPreConsensus(signedMsg)
	default:
		return errors.New("unknown msg")
	}
}

func validateMessage(share spectypes.Share, msg *spectypes.SSVMessage) error {
	if !share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
