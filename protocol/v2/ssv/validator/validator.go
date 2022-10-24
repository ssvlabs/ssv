package validator

import (
	"context"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	types2 "github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/protocol/v2/network"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/ssv/msgqueue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	qbft2 "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync/atomic"
)

type Options struct {
	Logger  *zap.Logger
	Network qbft.Network
	Beacon  ssv.BeaconNode
	Storage qbft.Storage
	Share   *beacon.Share
	Signer  types.KeyManager

	Mode ValidatorMode
}

func (o *Options) defaults() {
	if o.Logger == nil {
		o.Logger = zap.L()
	}
}

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *zap.Logger

	Identifier []byte
	DomainType types.DomainType

	DutyRunners runner.DutyRunners

	Share  *beacon.Share
	Beacon ssv.BeaconNode
	Signer types.KeyManager

	Storage qbft.Storage // TODO: change?
	Network network.Network

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
	ctx, cancel := context.WithCancel(pctx)

	var q msgqueue.MsgQueue
	if options.Mode == ModeRW {
		q, _ = msgqueue.New(options.Logger) // TODO: handle error
	}

	identifier := types.NewMsgID(options.Share.PublicKey.Serialize(), types.BNRoleAttester)
	v := &Validator{
		ctx:        ctx,
		cancel:     cancel,
		logger:     options.Logger,
		Identifier: identifier[:],
		DomainType: types2.GetDefaultDomain(),
		Network:    options.Network.(network.Network),
		Beacon:     options.Beacon,
		Storage:    options.Storage,
		Share:      options.Share,
		Signer:     options.Signer,
		Q:          q,
		mode:       int32(options.Mode),
	}

	v.setupRunners()

	return v
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
	specShare := ToSpecShare(v.Share) // temp solution
	if !specShare.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}

func (v *Validator) setupRunners() {
	config := &qbft2.Config{
		Signer:    v.Signer,
		SigningPK: v.Share.PublicKey.Serialize(), // TODO right val?
		Domain:    v.DomainType,
		ValueCheckF: func(data []byte) error {
			return nil // TODO need to add
		},
		ProposerF: func(state *qbft.State, round qbft.Round) types.OperatorID {
			return qbft.RoundRobinProposer(state, round)
		},
		Storage: v.Storage,
		Network: v.Network,
		Timer:   roundtimer.New(v.ctx, v.logger),
	}
	specShare := ToSpecShare(v.Share) // temp solution
	qbftQtrl := controller.NewController(v.Identifier, specShare, v.DomainType, config)
	v.DutyRunners = runner.DutyRunners{
		types.BNRoleAttester: runner.NewAttesterRunnner("", specShare, qbftQtrl, v.Beacon, v.Network, v.Signer, nil), // TODO add valcheck
		//spectypes.BNRoleProposer:                       utils.ProposerRunner(keySet),
		//spectypes.BNRoleAggregator:                     utils.AggregatorRunner(keySet),
		//spectypes.BNRoleSyncCommittee:                  utils.SyncCommitteeRunner(keySet),
		//spectypes.BNRoleSyncCommitteeContribution: utils.SyncCommitteeContributionRunner(keySet)},
	}
}

func (v *Validator) GetShare() *beacon.Share {
	return v.Share // temp solution
}

func ToSpecShare(share *beacon.Share) *types.Share {
	var specCommittee []*types.Operator
	var sharePK []byte
	for id, node := range share.Committee {
		if id == share.NodeID {
			sharePK = node.Pk
		}
		specCommittee = append(specCommittee, &types.Operator{
			OperatorID: id,
			PubKey:     node.Pk,
		})
	}

	return &types.Share{
		OperatorID:      share.NodeID,
		ValidatorPubKey: share.PublicKey.Serialize(),
		SharePubKey:     sharePK,
		Committee:       specCommittee,
		Quorum:          3,                         // temp
		PartialQuorum:   2,                         // temp
		DomainType:      types2.GetDefaultDomain(), // temp
		Graffiti:        nil,
	}
}
