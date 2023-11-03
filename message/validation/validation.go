// Package validation provides functions and structures for validating messages.
package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
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
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/utils/rsaencryption"
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

// PubsubMessageValidator defines methods for validating pubsub messages.
type PubsubMessageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
	ValidatePubsubMessage(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

// SSVMessageValidator defines methods for validating SSV messages.
type SSVMessageValidator interface {
	ValidateSSVMessage(ssvMessage *spectypes.SSVMessage) (*queue.DecodedSSVMessage, Descriptor, error)
}

// MessageValidator is an interface that combines both PubsubMessageValidator and SSVMessageValidator.
type MessageValidator interface {
	PubsubMessageValidator
	SSVMessageValidator
}

type messageValidator struct {
	logger              *zap.Logger
	metrics             metrics
	netCfg              networkconfig.NetworkConfig
	index               sync.Map
	nodeStorage         operatorstorage.Storage
	dutyStore           *dutystore.Store
	ownOperatorID       spectypes.OperatorID
	operatorPubKeyCache *hashmap.Map[spectypes.OperatorID, *rsa.PublicKey]
}

// NewMessageValidator returns a new MessageValidator with the given network configuration and options.
func NewMessageValidator(netCfg networkconfig.NetworkConfig, opts ...Option) MessageValidator {
	mv := &messageValidator{
		logger:              zap.NewNop(),
		metrics:             &nopMetrics{},
		netCfg:              netCfg,
		operatorPubKeyCache: hashmap.New[spectypes.OperatorID, *rsa.PublicKey](),
	}

	for _, opt := range opts {
		opt(mv)
	}

	return mv
}

// Option represents a functional option for configuring a messageValidator.
type Option func(validator *messageValidator)

// WithLogger sets the logger for the messageValidator.
func WithLogger(logger *zap.Logger) Option {
	return func(mv *messageValidator) {
		mv.logger = logger
	}
}

// WithMetrics sets the metrics for the messageValidator.
func WithMetrics(metrics metrics) Option {
	return func(mv *messageValidator) {
		mv.metrics = metrics
	}
}

// WithDutyStore sets the duty store for the messageValidator.
func WithDutyStore(dutyStore *dutystore.Store) Option {
	return func(mv *messageValidator) {
		mv.dutyStore = dutyStore
	}
}

// WithOwnOperatorID sets the operator ID for the messageValidator.
func WithOwnOperatorID(id spectypes.OperatorID) Option {
	return func(mv *messageValidator) {
		mv.ownOperatorID = id
	}
}

// WithNodeStorage sets the node storage for the messageValidator.
func WithNodeStorage(nodeStorage operatorstorage.Storage) Option {
	return func(mv *messageValidator) {
		mv.nodeStorage = nodeStorage
	}
}

// ConsensusDescriptor provides details about the consensus for a message. It's used for logging and metrics.
type ConsensusDescriptor struct {
	Round           specqbft.Round
	QBFTMessageType specqbft.MessageType
	Signers         []spectypes.OperatorID
	Committee       []*spectypes.Operator
}

// Descriptor provides details about a message. It's used for logging and metrics.
type Descriptor struct {
	ValidatorPK    spectypes.ValidatorPK
	Role           spectypes.BeaconRole
	SSVMessageType spectypes.MsgType
	Slot           phase0.Slot
	Consensus      *ConsensusDescriptor
}

// Fields returns zap logging fields for the descriptor.
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

// String provides a string representation of the descriptor. It may be useful for logging.
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

// ValidatorForTopic returns a validation function for the given topic.
// This function can be used to validate messages within the libp2p pubsub framework.
func (mv *messageValidator) ValidatorForTopic(_ string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return mv.ValidatePubsubMessage
}

// ValidatePubsubMessage validates the given pubsub message.
// Depending on the outcome, it will return one of the pubsub validation results (Accept, Ignore, or Reject).
func (mv *messageValidator) ValidatePubsubMessage(_ context.Context, peerID peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	start := time.Now()
	var validationDurationLabels []string // TODO: implement

	defer func() {
		sinceStart := time.Since(start)
		mv.metrics.MessageValidationDuration(sinceStart, validationDurationLabels...)
	}()

	// Ignore messages from peers that are not connected.
	// TODO:
	// - pass the existing PeerInfoIndex to NewMessageValidator
	// - add a separate metric to track # of messages ignored here
	// - add unit tests with a PeerInfoIndex mock
	//if mv.peersIndex != nil && mv.peersIndex.State(peerID) != peers.StateConnected {
	//	return pubsub.ValidationIgnore
	//}

	decodedMessage, descriptor, err := mv.validateP2PMessage(pmsg, time.Now())
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

				mv.metrics.MessageRejected(valErr.Text(), descriptor.Role, round)
				return pubsub.ValidationReject
			}

			if !valErr.Silent() {
				f := append(descriptor.Fields(), zap.Error(err))
				mv.logger.Debug("ignoring invalid message", f...)
			}
			mv.metrics.MessageIgnored(valErr.Text(), descriptor.Role, round)
			return pubsub.ValidationIgnore
		}

		mv.metrics.MessageIgnored(err.Error(), descriptor.Role, round)
		f := append(descriptor.Fields(), zap.Error(err))
		mv.logger.Debug("ignoring invalid message", f...)
		return pubsub.ValidationIgnore
	}

	pmsg.ValidatorData = decodedMessage

	mv.metrics.MessageAccepted(descriptor.Role, round)

	return pubsub.ValidationAccept
}

// ValidateSSVMessage validates the given SSV message.
// If successful, it returns the decoded message and its descriptor. Otherwise, it returns an error.
func (mv *messageValidator) ValidateSSVMessage(ssvMessage *spectypes.SSVMessage) (*queue.DecodedSSVMessage, Descriptor, error) {
	return mv.validateSSVMessage(ssvMessage, time.Now())
}

func (mv *messageValidator) validateP2PMessage(pMsg *pubsub.Message, receivedAt time.Time) (*queue.DecodedSSVMessage, Descriptor, error) {
	topic := pMsg.GetTopic()

	mv.metrics.ActiveMsgValidation(topic)
	mv.metrics.MessagesReceivedFromPeer(pMsg.ReceivedFrom.String())
	mv.metrics.MessagesReceivedTotal()

	defer mv.metrics.ActiveMsgValidationDone(topic)

	messageData := pMsg.GetData()
	currentEpoch := mv.netCfg.Beacon.EstimatedCurrentEpoch()

	if mv.netCfg.RSAMessageFork(currentEpoch) {
		//mv.logger.Info("RSA message fork happened, verifying message signature",
		//	zap.Uint64("current_epoch", uint64(currentEpoch)),
		//	zap.Uint64("fork_epoch", uint64(mv.netCfg.RSAMessageForkEpoch())),
		//)

		decMessageData, operatorID, signature, err := commons.DecodeSignedSSVMessage(messageData)
		if err != nil {
			e := ErrMalformedSignedMessage
			e.innerErr = err
			return nil, Descriptor{}, e
		}

		rsaPubKey, ok := mv.operatorPubKeyCache.Get(operatorID)
		if !ok {
			operator, found, err := mv.nodeStorage.GetOperatorData(nil, operatorID)
			if err != nil {
				e := ErrOperatorNotFound
				e.got = operatorID
				e.innerErr = err
				return nil, Descriptor{}, e
			}
			if !found {
				e := ErrOperatorNotFound
				e.got = operatorID
				return nil, Descriptor{}, e
			}

			operatorPubKey, err := base64.StdEncoding.DecodeString(string(operator.PublicKey))
			if err != nil {
				e := ErrRSADecryption
				e.innerErr = fmt.Errorf("decode public key: %w", err)
				return nil, Descriptor{}, e
			}

			rsaPubKey, err = rsaencryption.ConvertPemToPublicKey(operatorPubKey)
			if err != nil {
				e := ErrRSADecryption
				e.innerErr = fmt.Errorf("convert PEM: %w", err)
				return nil, Descriptor{}, e
			}

			mv.operatorPubKeyCache.Set(operatorID, rsaPubKey)
		}

		messageHash := sha256.Sum256(messageData)

		if err := rsa.VerifyPKCS1v15(rsaPubKey, crypto.SHA256, messageHash[:], signature); err != nil {
			e := ErrRSADecryption
			e.innerErr = fmt.Errorf("verify signature: %w", err)
			return nil, Descriptor{}, e
		}

		messageData = decMessageData
	} else {
		//mv.logger.Info("RSA message fork didn't happen, not verifying message signature",
		//	zap.Uint64("current_epoch", uint64(currentEpoch)),
		//	zap.Uint64("fork_epoch", uint64(mv.netCfg.RSAMessageForkEpoch())),
		//)
	}

	if len(messageData) == 0 {
		return nil, Descriptor{}, ErrPubSubMessageHasNoData
	}

	mv.metrics.MessageSize(len(messageData))

	// Max possible MsgType + MsgID + Data plus 10% for encoding overhead
	const maxMsgSize = 4 + 56 + 8388668
	const maxEncodedMsgSize = maxMsgSize + maxMsgSize/10
	if len(messageData) > maxEncodedMsgSize {
		e := ErrPubSubDataTooBig
		e.got = len(messageData)
		return nil, Descriptor{}, e
	}

	msg, err := commons.DecodeNetworkMsg(messageData)
	if err != nil {
		e := ErrMalformedPubSubMessage
		e.innerErr = err
		return nil, Descriptor{}, e
	}

	if msg == nil {
		return nil, Descriptor{}, ErrEmptyPubSubMessage
	}

	// Check if the message was sent on the right topic.
	currentTopic := pMsg.GetTopic()
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
		return nil, Descriptor{}, ErrTopicNotFound
	}

	mv.metrics.SSVMessageType(msg.MsgType)

	return mv.validateSSVMessage(msg, receivedAt)
}

func (mv *messageValidator) validateSSVMessage(ssvMessage *spectypes.SSVMessage, receivedAt time.Time) (*queue.DecodedSSVMessage, Descriptor, error) {
	var descriptor Descriptor

	if len(ssvMessage.Data) == 0 {
		return nil, descriptor, ErrEmptyData
	}

	if len(ssvMessage.Data) > maxMessageSize {
		err := ErrSSVDataTooBig
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
	if mv.nodeStorage != nil {
		share = mv.nodeStorage.Shares().Get(nil, publicKey.Serialize())
		if share == nil {
			e := ErrUnknownValidator
			e.got = publicKey.SerializeToHexStr()
			return nil, descriptor, e
		}

		if share.Liquidated {
			return nil, descriptor, ErrValidatorLiquidated
		}

		if share.BeaconMetadata == nil {
			return nil, descriptor, ErrNoShareMetadata
		}

		if !share.BeaconMetadata.IsAttesting() {
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

	if mv.nodeStorage != nil {
		switch ssvMessage.MsgType {
		case spectypes.SSVConsensusMsgType:
			if len(msg.Data) > maxConsensusMsgSize {
				e := ErrSSVDataTooBig
				e.got = len(ssvMessage.Data)
				e.want = maxConsensusMsgSize
				return nil, descriptor, e
			}

			consensusDescriptor, slot, err := mv.validateConsensusMessage(share, msg.Body.(*specqbft.SignedMessage), msg.GetID(), receivedAt)
			descriptor.Consensus = &consensusDescriptor
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case spectypes.SSVPartialSignatureMsgType:
			if len(msg.Data) > maxPartialSignatureMsgSize {
				e := ErrSSVDataTooBig
				e.got = len(ssvMessage.Data)
				e.want = maxPartialSignatureMsgSize
				return nil, descriptor, e
			}

			slot, err := mv.validatePartialSignatureMessage(share, msg.Body.(*spectypes.SignedPartialSignatureMessage), msg.GetID())
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case ssvmessage.SSVEventMsgType:
			return nil, descriptor, ErrEventMessage

		case spectypes.DKGMsgType:
			return nil, descriptor, ErrDKGMessage
		}
	}

	return msg, descriptor, nil
}

func (mv *messageValidator) containsSignerFunc(signer spectypes.OperatorID) func(operator *spectypes.Operator) bool {
	return func(operator *spectypes.Operator) bool {
		return operator.OperatorID == signer
	}
}

func (mv *messageValidator) validateSignatureFormat(signature []byte) error {
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

func (mv *messageValidator) commonSignerValidation(signer spectypes.OperatorID, share *ssvtypes.SSVShare) error {
	if signer == 0 {
		return ErrZeroSigner
	}

	if !slices.ContainsFunc(share.Committee, mv.containsSignerFunc(signer)) {
		return ErrSignerNotInCommittee
	}

	return nil
}

func (mv *messageValidator) validateSlotTime(messageSlot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) error {
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

func (mv *messageValidator) earlyMessage(slot phase0.Slot, receivedAt time.Time) bool {
	return mv.netCfg.Beacon.GetSlotEndTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		Add(-clockErrorTolerance).Before(mv.netCfg.Beacon.GetSlotStartTime(slot))
}

func (mv *messageValidator) lateMessage(slot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) time.Duration {
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

func (mv *messageValidator) consensusState(messageID spectypes.MessageID) *ConsensusState {
	id := ConsensusID{
		PubKey: phase0.BLSPubKey(messageID.GetPubKey()),
		Role:   messageID.GetRoleType(),
	}

	if _, ok := mv.index.Load(id); !ok {
		cs := &ConsensusState{
			Signers: hashmap.New[spectypes.OperatorID, *SignerState](),
		}
		mv.index.Store(id, cs)
	}

	cs, _ := mv.index.Load(id)
	return cs.(*ConsensusState)
}
