package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxMessageSize             = maxConsensusMsgSize
	maxConsensusMsgSize        = 8388608
	maxPartialSignatureMsgSize = 1952
	allowedRoundsInFuture      = 1
	allowedRoundsInPast        = 2
	lateSlotAllowance          = 2
	signatureSize              = 96
	maxDutiesPerEpoch          = 2
)

type ConsensusID struct {
	PubKey phase0.BLSPubKey
	Role   spectypes.BeaconRole
}

type ConsensusState struct {
	// TODO: consider evicting old data to avoid excessive memory consumption
	Signers *hashmap.Map[spectypes.OperatorID, *SignerState]
}

func (cs *ConsensusState) GetSignerState(signer spectypes.OperatorID) *SignerState {
	signerState, ok := cs.Signers.Get(signer)
	if !ok {
		return nil
	}
	return signerState
}

func (cs *ConsensusState) CreateSignerState(signer spectypes.OperatorID) *SignerState {
	signerState := &SignerState{}
	cs.Signers.Set(signer, signerState)

	return signerState
}

type MessageValidator struct {
	logger       *zap.Logger
	metrics      metrics
	netCfg       networkconfig.NetworkConfig
	index        sync.Map
	shareStorage registrystorage.Shares
}

func NewMessageValidator(netCfg networkconfig.NetworkConfig, opts ...Option) *MessageValidator {
	mv := &MessageValidator{
		logger:  zap.NewNop(),
		metrics: &nopMetrics{},
		netCfg:  netCfg,
	}

	for _, opt := range opts {
		opt(mv)
	}

	return mv
}

type Option func(validator *MessageValidator)

func WithLogger(logger *zap.Logger) Option {
	return func(mv *MessageValidator) {
		mv.logger = logger
	}
}

func WithMetrics(metrics metrics) Option {
	return func(mv *MessageValidator) {
		mv.metrics = metrics
	}
}

func WithShareStorage(shareStorage registrystorage.Shares) Option {
	return func(mv *MessageValidator) {
		mv.shareStorage = shareStorage
	}
}

type ConsensusDescriptor struct {
	Round           specqbft.Round
	QBFTMessageType specqbft.MessageType
	Signers         []spectypes.OperatorID
	Committee       []*spectypes.Operator
}

type Descriptor struct {
	ValidatorPK    spectypes.ValidatorPK
	Role           spectypes.BeaconRole
	SSVMessageType spectypes.MsgType
	Slot           phase0.Slot
	Consensus      *ConsensusDescriptor
}

func (d Descriptor) Fields() []zapcore.Field {
	result := []zapcore.Field{
		fields.Validator(d.ValidatorPK),
		fields.Role(d.Role),
		zap.String("ssv_message_type", ssvmessage.MsgTypeToString(d.SSVMessageType)),
		fields.Slot(d.Slot),
	}

	if d.Consensus != nil {
		var committee []spectypes.OperatorID
		for _, o := range d.Consensus.Committee {
			committee = append(committee, o.OperatorID)
		}

		result = append(result,
			fields.Round(d.Consensus.Round),
			zap.String("qbft_message_type", ssvmessage.QBFTMsgTypeToString(d.Consensus.QBFTMessageType)),
			zap.Uint64s("signers", d.Consensus.Signers),
			zap.Uint64s("committee", committee),
		)
	}

	return result
}

func (d Descriptor) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("validator PK: %v, role: %v, ssv message type: %v, slot: %v",
		hex.EncodeToString(d.ValidatorPK),
		d.Role.String(),
		ssvmessage.MsgTypeToString(d.SSVMessageType),
		d.Slot,
	))

	if d.Consensus != nil {
		var committee []spectypes.OperatorID
		for _, o := range d.Consensus.Committee {
			committee = append(committee, o.OperatorID)
		}

		sb.WriteString(fmt.Sprintf(", round: %v, qbft message type: %v, signers: %v, committee: %v",
			d.Consensus.Round,
			ssvmessage.QBFTMsgTypeToString(d.Consensus.QBFTMessageType),
			d.Consensus.Signers,
			committee,
		))
	}

	return sb.String()
}

func (mv *MessageValidator) ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return mv.ValidateP2PMessage
}

func (mv *MessageValidator) ValidateP2PMessage(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return mv.validateP2PMessage(ctx, p, pmsg, time.Now())
}

func (mv *MessageValidator) validateP2PMessage(ctx context.Context, p peer.ID, pmsg *pubsub.Message, receivedAt time.Time) pubsub.ValidationResult {
	start := time.Now()
	var validationDurationLabels []string // TODO: implement

	defer func() {
		sinceStart := time.Since(start)
		mv.metrics.MessageValidationDuration(sinceStart, validationDurationLabels...)
		//mv.logger.Debug("processed message", zap.Duration("took", sinceStart))
	}()

	topic := pmsg.GetTopic()

	mv.metrics.ActiveMsgValidation(topic)
	defer mv.metrics.ActiveMsgValidationDone(topic)

	messageData := pmsg.GetData()
	if len(messageData) == 0 {
		mv.metrics.MessageRejected("no data", nil, math.MaxUint64, 0, 0)
		return pubsub.ValidationReject
	}

	mv.metrics.MessageSize(len(messageData))

	// Max possible MsgType + MsgID + Data plus 10% for encoding overhead
	// TODO: check if we need to add 10% here
	const maxMsgSize = 4 + 56 + 8388668
	const maxEncodedMsgSize = maxMsgSize + maxMsgSize/10
	if len(messageData) > maxEncodedMsgSize {
		mv.metrics.MessageRejected("message is too big", nil, math.MaxUint64, 0, 0)
		return pubsub.ValidationReject
	}

	msg, err := commons.DecodeNetworkMsg(messageData)
	if err != nil {
		// can't decode message
		// logger.Debug("invalid: can't decode message", zap.Error(err))
		mv.metrics.MessageRejected("could not decode network message", nil, math.MaxUint64, 0, 0)
		return pubsub.ValidationReject
	}
	if msg == nil {
		mv.metrics.MessageRejected("message is nil", nil, math.MaxUint64, 0, 0)
		return pubsub.ValidationReject
	}

	// Check if the message was sent on the right topic.
	currentTopic := pmsg.GetTopic()
	currentTopicBaseName := commons.GetTopicBaseName(currentTopic)
	topics := commons.ValidatorTopicID(msg.GetID().GetPubKey())

	topicFound := false
	for _, tp := range topics {
		if tp == currentTopicBaseName {
			topicFound = true
			break
		}
	}
	if !topicFound {
		mv.metrics.MessageRejected("topic not found", msg.GetID().GetPubKey(), math.MaxUint64, 0, 0)
		return pubsub.ValidationReject
	}

	mv.metrics.SSVMessageType(msg.MsgType)

	decodedMessage, descriptor, err := mv.validateSSVMessage(msg, receivedAt)
	round := specqbft.Round(0)
	if descriptor.Consensus != nil {
		round = descriptor.Consensus.Round
	}

	if err != nil {
		var valErr Error
		if errors.As(err, &valErr) {
			if valErr.Reject() {
				if !valErr.Silent() {
					f := append(descriptor.Fields(), zap.Error(err))
					mv.logger.Debug("rejecting invalid message", f...)
				}

				mv.metrics.MessageRejected(
					valErr.Text(),
					descriptor.ValidatorPK,
					descriptor.Role,
					descriptor.Slot,
					round,
				)
				return pubsub.ValidationReject
			} else {
				if !valErr.Silent() {
					f := append(descriptor.Fields(), zap.Error(err))
					mv.logger.Debug("ignoring invalid message", f...)
				}
				mv.metrics.MessageIgnored(
					valErr.Text(),
					descriptor.ValidatorPK,
					descriptor.Role,
					descriptor.Slot,
					round,
				)
				return pubsub.ValidationIgnore
			}
		} else {
			mv.metrics.MessageIgnored(
				err.Error(),
				descriptor.ValidatorPK,
				descriptor.Role,
				descriptor.Slot,
				round,
			)
			f := append(descriptor.Fields(), zap.Error(err))
			mv.logger.Debug("ignoring invalid message", f...)
			return pubsub.ValidationIgnore
		}
	}

	pmsg.ValidatorData = decodedMessage

	mv.metrics.MessageAccepted(
		descriptor.ValidatorPK,
		descriptor.Role,
		descriptor.Slot,
		round,
	)

	return pubsub.ValidationAccept
}

func (mv *MessageValidator) ValidateSSVMessage(ssvMessage *spectypes.SSVMessage) (*queue.DecodedSSVMessage, Descriptor, error) {
	return mv.validateSSVMessage(ssvMessage, time.Now())
}

func (mv *MessageValidator) validateSSVMessage(ssvMessage *spectypes.SSVMessage, receivedAt time.Time) (*queue.DecodedSSVMessage, Descriptor, error) {
	var descriptor Descriptor

	if len(ssvMessage.Data) == 0 {
		return nil, descriptor, ErrEmptyData
	}

	if len(ssvMessage.Data) > maxMessageSize {
		err := ErrDataTooBig
		err.got = len(ssvMessage.Data)
		err.want = maxMessageSize
		return nil, descriptor, err
	}

	if !bytes.Equal(ssvMessage.MsgID.GetDomain(), mv.netCfg.Domain[:]) {
		err := ErrWrongDomain
		err.got = hex.EncodeToString(ssvMessage.MsgID.GetDomain())
		err.want = hex.EncodeToString(mv.netCfg.Domain[:])
		return nil, descriptor, err
	}

	validatorPK := ssvMessage.GetID().GetPubKey()
	role := ssvMessage.GetID().GetRoleType()
	descriptor.Role = role
	descriptor.ValidatorPK = validatorPK

	if !mv.validRole(role) {
		return nil, descriptor, ErrInvalidRole
	}

	publicKey, err := ssvtypes.DeserializeBLSPublicKey(validatorPK)
	if err != nil {
		e := ErrDeserializePublicKey
		e.innerErr = err
		return nil, descriptor, e
	}

	var share *ssvtypes.SSVShare
	if mv.shareStorage != nil {
		share = mv.shareStorage.Get(nil, publicKey.Serialize())
		if share == nil {
			e := ErrUnknownValidator
			e.got = publicKey.SerializeToHexStr()
			return nil, descriptor, e
		}

		if share.Liquidated {
			return nil, descriptor, ErrValidatorLiquidated
		}

		// TODO: check if need to return error if no metadata
		if share.BeaconMetadata != nil && !share.BeaconMetadata.IsAttesting() {
			err := ErrValidatorNotAttesting
			err.got = share.BeaconMetadata.Status.String()
			return nil, descriptor, err
		}
	}

	msg, err := queue.DecodeSSVMessage(ssvMessage)
	if err != nil {
		if errors.Is(err, queue.ErrUnknownMessageType) {
			e := ErrUnknownSSVMessageType
			e.got = ssvMessage.GetType()
			return nil, descriptor, e
		}

		e := ErrMalformedMessage
		e.innerErr = err
		return nil, descriptor, e
	}

	descriptor.SSVMessageType = ssvMessage.MsgType

	if mv.shareStorage != nil {
		switch ssvMessage.MsgType {
		case spectypes.SSVConsensusMsgType:
			consensusDescriptor, slot, err := mv.validateConsensusMessage(share, msg, receivedAt)
			descriptor.Consensus = &consensusDescriptor
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case spectypes.SSVPartialSignatureMsgType:
			slot, err := mv.validatePartialSignatureMessage(share, msg)
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case ssvmessage.SSVEventMsgType:
			return nil, descriptor, ErrEventMessage

		case spectypes.DKGMsgType:
			// TODO: handle
		}
	}

	return msg, descriptor, nil
}

func (mv *MessageValidator) containsSignerFunc(signer spectypes.OperatorID) func(operator *spectypes.Operator) bool {
	return func(operator *spectypes.Operator) bool {
		return operator.OperatorID == signer
	}
}

func (mv *MessageValidator) validateSignatureFormat(signature []byte) error {
	if len(signature) != signatureSize {
		e := ErrWrongSignatureSize
		e.got = len(signature)
		return e
	}

	if [signatureSize]byte(signature) == [signatureSize]byte{} {
		return ErrZeroSignature
	}
	return nil
}

func (mv *MessageValidator) commonSignerValidation(signer spectypes.OperatorID, share *ssvtypes.SSVShare) error {
	if signer == 0 {
		return ErrZeroSigner
	}

	if !slices.ContainsFunc(share.Committee, mv.containsSignerFunc(signer)) {
		return ErrSignerNotInCommittee
	}

	return nil
}

func (mv *MessageValidator) validateSlotTime(messageSlot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) error {
	if mv.earlyMessage(messageSlot, receivedAt) {
		return ErrEarlyMessage
	}

	if lateness := mv.lateMessage(messageSlot, role, receivedAt); lateness > 0 {
		e := ErrLateMessage
		e.got = fmt.Sprintf("late by %v", lateness)
		return e
	}

	return nil
}

func (mv *MessageValidator) earlyMessage(slot phase0.Slot, receivedAt time.Time) bool {
	return mv.netCfg.Beacon.GetSlotEndTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		Add(-clockErrorTolerance).Before(mv.netCfg.Beacon.GetSlotStartTime(slot))
}

func (mv *MessageValidator) lateMessage(slot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) time.Duration {
	var ttl phase0.Slot
	switch role {
	case spectypes.BNRoleProposer, spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		ttl = 1 + lateSlotAllowance
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator:
		ttl = 32 + lateSlotAllowance
	case spectypes.BNRoleValidatorRegistration:
		return 0
	}

	deadline := mv.netCfg.Beacon.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin).Add(clockErrorTolerance)

	return mv.netCfg.Beacon.GetSlotStartTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		Sub(deadline)
}

func (mv *MessageValidator) consensusState(id ConsensusID) *ConsensusState {
	if _, ok := mv.index.Load(id); !ok {
		cs := &ConsensusState{
			Signers: hashmap.New[spectypes.OperatorID, *SignerState](),
		}
		mv.index.Store(id, cs)
	}

	cs, _ := mv.index.Load(id)
	return cs.(*ConsensusState)
}
