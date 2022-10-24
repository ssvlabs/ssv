package validator

import (
	"context"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/msgqueue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync/atomic"
)

type Options struct {
	logger  *zap.Logger
	Network ssv.Network
	Beacon  ssv.BeaconNode
	Storage ssv.Storage
	Share   *types.Share
	Signer  types.KeyManager
	Runners runner.DutyRunners
}

func (o *Options) defaults() {
	if o.logger == nil {
		o.logger = zap.L()
	}
}

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *zap.Logger

	DutyRunners runner.DutyRunners

	Share  *types.Share
	Beacon ssv.BeaconNode
	Signer types.KeyManager

	Storage ssv.Storage // TODO: change?
	Network Network

	Q msgqueue.MsgQueue

	mode int32
}

type ValidatorMode int32

var (
	ModeRW ValidatorMode = 0
	ModeR  ValidatorMode = 1
)

func NewValidator(pctx context.Context, options Options) *Validator {
	options.defaults()
	ctx, cancel := context.WithCancel(pctx) // TODO: pass context

	// makes sure that we have a sufficient interface, otherwise wrap it
	n, ok := options.Network.(Network)
	if !ok {
		n = newNilNetwork(options.Network)
	}
	s, ok := options.Storage.(qbft.Storage)
	if !ok {
		options.logger.Warn("incompatible storage") // TODO: handle
	}

	var q msgqueue.MsgQueue
	//if mode == int32(ModeRW) {
	q, _ = msgqueue.New(options.logger) // TODO: handle error
	//}

	return &Validator{
		ctx:         ctx,
		cancel:      cancel,
		logger:      options.logger,
		DutyRunners: options.Runners,
		Network:     n,
		Beacon:      options.Beacon,
		Storage:     s,
		Share:       options.Share,
		Signer:      options.Signer,
		Q:           q,
		mode:        int32(ModeRW),
	}
}

func (v *Validator) Start() error {
	identifiers := v.DutyRunners.Identifiers()
	for _, identifier := range identifiers {
		if err := v.Network.Subscribe(identifier.GetPubKey()); err != nil {
			return err
		}
		go v.StartQueueConsumer(identifier, v.ProcessMessage)
	}
	return nil
}

func (v *Validator) Stop() error {
	v.cancel()
	// clear the msg q
	v.Q.Clean(func(index msgqueue.Index) bool {
		return true
	})
	return nil
}

// StartDuty starts a duty for the validator
func (v *Validator) StartDuty(duty *types.Duty) error {
	dutyRunner := v.DutyRunners[duty.Type]
	if dutyRunner == nil {
		return errors.Errorf("duty type %s not supported", duty.Type.String())
	}
	return dutyRunner.StartNewDuty(duty)
}

func (v *Validator) HandleMessage(msg *types.SSVMessage) {
	if atomic.LoadInt32(&v.mode) == int32(ModeR) {
		err := v.ProcessMessage(msg)
		if err != nil {
			v.logger.Warn("could not handle msg", zap.Error(err))
		}
		return
	}
	fields := []zap.Field{
		zap.Int("queue_len", v.Q.Len()),
		zap.String("msgType", message.MsgTypeToString(msg.MsgType)),
	}
	v.logger.Debug("got message, add to queue", fields...)
	v.Q.Add(msg)
}

// ProcessMessage processes Network Message of all types
func (v *Validator) ProcessMessage(msg *types.SSVMessage) error {
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(msg.GetID())
	if dutyRunner == nil {
		return errors.Errorf("could not get duty runner for msg ID")
	}

	if err := v.validateMessage(dutyRunner, msg); err != nil {
		return errors.Wrap(err, "Message invalid")
	}

	switch msg.GetType() {
	case types.SSVConsensusMsgType:
		signedMsg := &qbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get consensus Message from network Message")
		}
		return dutyRunner.ProcessConsensus(signedMsg)
	case types.SSVPartialSignatureMsgType:
		signedMsg := &ssv.SignedPartialSignatureMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from network Message")
		}

		if signedMsg.Message.Type == ssv.PostConsensusPartialSig {
			return dutyRunner.ProcessPostConsensus(signedMsg)
		}
		return dutyRunner.ProcessPreConsensus(signedMsg)
	default:
		return errors.New("unknown msg")
	}
}

func (v *Validator) validateMessage(runner runner.Runner, msg *types.SSVMessage) error {
	if !v.Share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
