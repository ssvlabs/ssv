package validator

import (
	"context"
	"encoding/hex"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

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

	v := &Validator{
		ctx:         ctx,
		cancel:      cancel,
		logger:      options.Logger,
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
		v.logger.Debug("got valid consensus message",
			zap.Int64("type", int64(signedMsg.Message.MsgType)),
			zap.Int64("msg_height", int64(signedMsg.Message.Height)),
			zap.Int64("msg_round", int64(signedMsg.Message.Round)),
			zap.Any("ctrl", dutyRunner.GetBaseRunner().QBFTController),
			zap.Any("runner_state", dutyRunner.GetBaseRunner().State),
		)
		return dutyRunner.ProcessConsensus(signedMsg)
	case spectypes.SSVPartialSignatureMsgType:
		signedMsg := &specssv.SignedPartialSignatureMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		if signedMsg == nil {
			return nil
		}
		v.logger.Debug("got valid post consensus message",
			zap.Int64("type", int64(signedMsg.Message.Type)),
			zap.Any("ctrl", dutyRunner.GetBaseRunner().QBFTController),
			zap.Any("runner_state", dutyRunner.GetBaseRunner().State),
		)
		if signedMsg.Message.Type == specssv.PostConsensusPartialSig {
			v.logger.Info("process post consensus", zap.String("identifier", hex.EncodeToString(v.Share.ValidatorPubKey)),
				zap.Any("duty runner state", dutyRunner.GetBaseRunner().State),
				zap.Any("ctrl", dutyRunner.GetBaseRunner().QBFTController))
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
