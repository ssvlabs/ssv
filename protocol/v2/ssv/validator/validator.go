package validator

import (
	"context"
	"github.com/bloxapp/ssv-spec/p2p"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/roundrobin"
	typesv1 "github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/protocol/v2/ssv/msgqueue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

type Options struct {
	Logger      *zap.Logger
	Network     qbft.Network
	Beacon      ssv.BeaconNode
	Storage     qbft.Storage
	Share       *beacon.Share
	Signer      types.KeyManager
	DutyRunners runner.DutyRunners
	Mode        ValidatorMode
}

func (o *Options) defaults() {
	if o.Logger == nil {
		o.Logger = zap.L()
	}
}

// set of states for the controller
const (
	NotStarted uint32 = iota
	Started
)

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *zap.Logger

	DomainType types.DomainType

	DutyRunners runner.DutyRunners

	Share  *beacon.Share
	Beacon ssv.BeaconNode
	Signer types.KeyManager

	Storage qbft.Storage // TODO: change?
	Network qbft.Network

	Q msgqueue.MsgQueue

	mode int32

	State uint32
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
		indexers := msgqueue.WithIndexers( /*msgqueue.DefaultMsgIndexer(), */ msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer())
		q, _ = msgqueue.New(options.Logger, indexers) // TODO: handle error
	}

	v := &Validator{
		ctx:         ctx,
		cancel:      cancel,
		logger:      options.Logger,
		DomainType:  typesv1.GetDefaultDomain(),
		DutyRunners: options.DutyRunners,
		Network:     options.Network,
		Beacon:      options.Beacon,
		Storage:     options.Storage,
		Share:       options.Share,
		Signer:      options.Signer,
		Q:           q,
		mode:        int32(options.Mode),
		State:       NotStarted,
	}

	return v
}

func (v *Validator) Start() error {
	if atomic.CompareAndSwapUint32(&v.State, NotStarted, Started) {
		n, ok := v.Network.(p2p.Subscriber)
		if !ok {
			return nil
		}
		identifiers := v.DutyRunners.Identifiers()
		for _, identifier := range identifiers {
			_, err := v.loadLastHeight(identifier)
			if err != nil {
				v.logger.Warn("could not load highest", zap.String("identifier", identifier.String()), zap.Error(err))
			}
			if err := n.Subscribe(identifier.GetPubKey()); err != nil {
				return err
			}
			go v.StartQueueConsumer(identifier, v.ProcessMessage)
			go v.sync(identifier)
		}
	}
	return nil
}

func (v *Validator) sync(mid types.MessageID) {
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()

	// TODO: config?
	interval := time.Second
	retries := 3

	for ctx.Err() == nil {
		err := v.Network.SyncHighestDecided(mid)
		if err != nil {
			v.logger.Debug("could not sync highest decided", zap.String("identifier", mid.String()))
			retries--
			if retries > 0 {
				interval *= 2
				time.Sleep(interval)
				continue
			}
		}
		return
	}
}

func (v *Validator) Stop() error {
	v.cancel()
	// clear the msg q
	v.Q.Clean(func(index msgqueue.Index) bool {
		return true
	})
	return nil
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
		zap.String("msgID", msg.MsgID.String()),
	}
	v.logger.Debug("got message, add to queue", fields...)
	v.Q.Add(msg)
}

// StartDuty starts a duty for the validator
func (v *Validator) StartDuty(duty *types.Duty) error {
	dutyRunner := v.DutyRunners[duty.Type]
	if dutyRunner == nil {
		return errors.Errorf("duty type %s not supported", duty.Type.String())
	}
	return dutyRunner.StartNewDuty(duty)
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
			v.logger.Info("process post consensus")
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

func (v *Validator) GetShare() *beacon.Share {
	return v.Share // temp solution
}

func (v *Validator) loadLastHeight(identifier types.MessageID) (qbft.Height, error) {
	knownDecided, err := v.Storage.GetHighestDecided(identifier[:])
	if err != nil {
		return qbft.Height(0), errors.Wrap(err, "failed to get heights decided")
	}
	if knownDecided == nil {
		return qbft.Height(0), nil
	}
	r := v.DutyRunners.DutyRunnerForMsgID(identifier)

	if r == nil || r.GetBaseRunner() == nil {
		return qbft.Height(0), errors.New("runner is nil")
	}
	r.GetBaseRunner().QBFTController.Height = knownDecided.Message.Height
	return knownDecided.Message.Height, nil
}

// ToSSVShare convert spec share struct to ssv share struct (mainly for testing purposes)
func ToSSVShare(specShare *types.Share) (*beacon.Share, error) {
	vpk := &bls.PublicKey{}
	if err := vpk.Deserialize(specShare.ValidatorPubKey); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize validator public key")
	}

	var operatorsId []uint64
	ssvCommittee := map[types.OperatorID]*beacon.Node{}
	for _, op := range specShare.Committee {
		operatorsId = append(operatorsId, uint64(op.OperatorID))
		ssvCommittee[op.OperatorID] = &beacon.Node{
			IbftID: uint64(op.GetID()),
			Pk:     op.GetPublicKey(),
		}
	}

	return &beacon.Share{
		NodeID:       specShare.OperatorID,
		PublicKey:    vpk,
		Committee:    ssvCommittee,
		Metadata:     nil,
		OwnerAddress: "",
		Operators:    nil,
		OperatorIds:  operatorsId,
		Liquidated:   false,
	}, nil
}

// ToSpecShare convert spec share to ssv share struct
func ToSpecShare(share *beacon.Share) *types.Share {
	var sharePK []byte
	for id, node := range share.Committee {
		if id == share.NodeID {
			sharePK = node.Pk
		}
	}

	return &types.Share{
		OperatorID:      share.NodeID,
		ValidatorPubKey: share.PublicKey.Serialize(),
		SharePubKey:     sharePK,
		Committee:       roundrobin.MapCommittee(share),
		Quorum:          3,                          // temp
		PartialQuorum:   2,                          // temp
		DomainType:      typesv1.GetDefaultDomain(), // temp
		Graffiti:        nil,
	}
}
