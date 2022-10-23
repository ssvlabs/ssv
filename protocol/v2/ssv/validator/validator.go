package validator

import (
	"context"
	"github.com/attestantio/go-eth2-client/spec/phase0"
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

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	ctx context.Context
	logger *zap.Logger
	DutyRunners runner.DutyRunners
	Network     Network
	Beacon      ssv.BeaconNode
	Storage     ssv.Storage
	Share       *types.Share
	Signer      types.KeyManager
	Q 			msgqueue.MsgQueue

	// TODO: move somewhere else
	Identifiers [][]byte

	mode int32
}

type ValidatorMode int32

var (
	ModeRW ValidatorMode = 0
	ModeR ValidatorMode = 1
)

func NewValidator(
	network ssv.Network,
	beacon ssv.BeaconNode,
	storage ssv.Storage,
	share *types.Share,
	signer types.KeyManager,
	runners runner.DutyRunners,
) *Validator {
	// makes sure that we have a sufficient interface, otherwise wrap it
	n, ok := network.(Network)
	if !ok {
		n = newNilNetwork(network)
	}
	l := zap.L() // TODO: real logger
	// TODO: handle error
	q, _ := msgqueue.New(l)
	return &Validator{
		ctx: context.Background(), // TODO: real context
		logger: l,
		DutyRunners: runners,
		Network:     n,
		Beacon:      beacon,
		Storage:     storage,
		Share:       share,
		Signer:      signer,
		Q: 			 q,
		mode: int32(ModeRW),
	}
}

func (v *Validator) Start() error {
	for _, identifier := range v.Identifiers {
		go v.StartQueueConsumer(identifier, v.ProcessMessage)
	}
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

// GetLastHeight returns the last height for the given identifier
func (v *Validator) GetLastHeight(identifier []byte) qbft.Height {
	return qbft.Height(0)
}

// GetLastSlot returns the last slot for the given identifier
func (v *Validator) GetLastSlot(identifier []byte) phase0.Slot {
	return phase0.Slot(0)
}
