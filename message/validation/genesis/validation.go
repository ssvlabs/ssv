// Package validation provides functions and structures for validating messages.
package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cornelk/hashmap"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/slices"

	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	alanspecqbft "github.com/ssvlabs/ssv-spec/qbft"
	alanspectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/monitoring/metricsreporter"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/keys"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	genesisqueue "github.com/ssvlabs/ssv/protocol/genesis/ssv/genesisqueue"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxMessageSize             = maxConsensusMsgSize
	maxConsensusMsgSize        = 6291829
	maxPartialSignatureMsgSize = 1952
	allowedRoundsInFuture      = 1
	allowedRoundsInPast        = 2
	lateSlotAllowance          = 2
	signatureSize              = 96
	maxDutiesPerEpoch          = 2
)

// MessageValidator is an interface that combines both PubsubMessageValidator and SSVMessageValidator.
type MessageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
	Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

type messageValidator struct {
	logger                  *zap.Logger
	metrics                 metricsreporter.MetricsReporter
	netCfg                  networkconfig.NetworkConfig
	index                   sync.Map
	nodeStorage             operatorstorage.Storage
	dutyStore               *dutystore.Store
	operatorIDToPubkeyCache *hashmap.Map[spectypes.OperatorID, keys.OperatorPublicKey]

	// validationLocks is a map of lock per SSV message ID to
	// prevent concurrent access to the same state.
	validationLocks map[spectypes.MessageID]*sync.Mutex
	validationMutex sync.Mutex

	selfPID    peer.ID
	selfAccept bool
}

// New returns a new MessageValidator with the given network configuration and options.
func New(netCfg networkconfig.NetworkConfig, opts ...Option) MessageValidator {
	mv := &messageValidator{
		logger:                  zap.NewNop(),
		metrics:                 metricsreporter.NewNop(),
		netCfg:                  netCfg,
		operatorIDToPubkeyCache: hashmap.New[spectypes.OperatorID, keys.OperatorPublicKey](),
		validationLocks:         make(map[spectypes.MessageID]*sync.Mutex),
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
func WithMetrics(metrics metricsreporter.MetricsReporter) Option {
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

// WithNodeStorage sets the node storage for the messageValidator.
func WithNodeStorage(nodeStorage operatorstorage.Storage) Option {
	return func(mv *messageValidator) {
		mv.nodeStorage = nodeStorage
	}
}

// WithSelfAccept blindly accepts messages sent from self. Useful for testing.
func WithSelfAccept(selfPID peer.ID, selfAccept bool) Option {
	return func(mv *messageValidator) {
		mv.selfPID = selfPID
		mv.selfAccept = selfAccept
	}
}

// ConsensusDescriptor provides details about the consensus for a message. It's used for logging and metrics.
type ConsensusDescriptor struct {
	Round           specqbft.Round
	QBFTMessageType specqbft.MessageType
	Signers         []spectypes.OperatorID
	Committee       []*alanspectypes.ShareMember
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
		//fields.Role(d.Role),
		//zap.String("ssv_message_type", ssvmessage.MsgTypeToString(d.SSVMessageType)),
		fields.Slot(d.Slot),
	}

	if d.Consensus != nil {
		var committee []spectypes.OperatorID
		for _, o := range d.Consensus.Committee {
			committee = append(committee, o.Signer)
		}

		result = append(result,
			//fields.Round(d.Consensus.Round),
			//zap.String("qbft_message_type", ssvmessage.QBFTMsgTypeToString(d.Consensus.QBFTMessageType)),
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
		ssvmessage.MsgTypeToString(alanspectypes.MsgType(d.SSVMessageType)),
		d.Slot,
	))

	if d.Consensus != nil {
		var committee []spectypes.OperatorID
		for _, o := range d.Consensus.Committee {
			committee = append(committee, o.Signer)
		}

		sb.WriteString(fmt.Sprintf(", round: %v, qbft message type: %v, signers: %v, committee: %v",
			d.Consensus.Round,
			ssvmessage.QBFTMsgTypeToString(alanspecqbft.MessageType(d.Consensus.QBFTMessageType)),
			d.Consensus.Signers,
			committee,
		))
	}

	return sb.String()
}

// ValidatorForTopic returns a validation function for the given topic.
// This function can be used to validate messages within the libp2p pubsub framework.
func (mv *messageValidator) ValidatorForTopic(_ string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return mv.Validate
}

// Validate validates the given pubsub message.
// Depending on the outcome, it will return one of the pubsub validation results (Accept, Ignore, or Reject).
func (mv *messageValidator) Validate(_ context.Context, peerID peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	if mv.selfAccept && peerID == mv.selfPID {
		signedSSVMsg := &spectypes.SignedSSVMessage{}
		if err := signedSSVMsg.Decode(pmsg.GetData()); err != nil {
			mv.logger.Error("failed to decode signed ssv message", zap.Error(err))
			return pubsub.ValidationReject
		}

		msg, err := signedSSVMsg.GetSSVMessageFromData()
		if err != nil {
			mv.logger.Error("failed to decode network message", zap.Error(err))
			return pubsub.ValidationReject
		}

		// skipping the error check for testing simplifying
		decMsg, _ := genesisqueue.DecodeGenesisSSVMessage(msg)
		pmsg.ValidatorData = decMsg
		return pubsub.ValidationAccept
	}

	start := time.Now()
	var validationDurationLabels []string // TODO: implement

	defer func() {
		sinceStart := time.Since(start)
		mv.metrics.MessageValidationDuration(sinceStart, validationDurationLabels...)
	}()

	decodedMessage, descriptor, err := mv.validateP2PMessage(pmsg, time.Now())
	round := specqbft.Round(0)
	if descriptor.Consensus != nil {
		round = descriptor.Consensus.Round
	}

	f := append(descriptor.Fields(), fields.PeerID(peerID))

	if err != nil {
		var valErr Error
		if errors.As(err, &valErr) {
			if valErr.Reject() {
				if !valErr.Silent() {
					f = append(f, zap.Error(err))
					mv.logger.Debug("rejecting invalid message", f...)
				}

				mv.metrics.GenesisMessageRejected(valErr.Text(), descriptor.Role, round)
				return pubsub.ValidationReject
			}

			if !valErr.Silent() {
				f = append(f, zap.Error(err))
				mv.logger.Debug("ignoring invalid message", f...)
			}
			mv.metrics.GenesisMessageIgnored(valErr.Text(), descriptor.Role, round)
			return pubsub.ValidationIgnore
		}

		mv.metrics.GenesisMessageIgnored(err.Error(), descriptor.Role, round)
		f = append(f, zap.Error(err))
		mv.logger.Debug("ignoring invalid message", f...)
		return pubsub.ValidationIgnore
	}

	pmsg.ValidatorData = decodedMessage

	mv.metrics.GenesisMessageAccepted(descriptor.Role, round)

	return pubsub.ValidationAccept
}

// ValidateSSVMessage validates the given SSV message.
// If successful, it returns the decoded message and its descriptor. Otherwise, it returns an error.
func (mv *messageValidator) ValidateSSVMessage(ssvMessage *genesisqueue.GenesisSSVMessage) (*genesisqueue.GenesisSSVMessage, Descriptor, error) {
	return mv.validateSSVMessage(ssvMessage, time.Now(), nil)
}

func (mv *messageValidator) validateP2PMessage(pMsg *pubsub.Message, receivedAt time.Time) (*genesisqueue.GenesisSSVMessage, Descriptor, error) {
	topic := pMsg.GetTopic()

	mv.metrics.ActiveMsgValidation(topic)
	mv.metrics.MessagesReceivedFromPeer(pMsg.ReceivedFrom)
	mv.metrics.MessagesReceivedTotal()

	defer mv.metrics.ActiveMsgValidationDone(topic)

	encMessageData := pMsg.GetData()

	if len(encMessageData) == 0 {
		return nil, Descriptor{}, ErrPubSubMessageHasNoData
	}

	signedSSVMsg := &spectypes.SignedSSVMessage{}
	if err := signedSSVMsg.Decode(encMessageData); err != nil {
		e := ErrMalformedSignedMessage
		e.innerErr = err
		return nil, Descriptor{}, e
	}

	messageData := signedSSVMsg.GetData()
	if len(messageData) == 0 {
		return nil, Descriptor{}, ErrDecodedPubSubMessageHasEmptyData
	}

	signatureVerifier := func() error {
		mv.metrics.MessageValidationRSAVerifications()
		return mv.verifySignature(signedSSVMsg)
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

	msg, err := genesisqueue.DecodeGenesisSignedSSVMessage(signedSSVMsg)
	if err != nil {
		if errors.Is(err, genesisqueue.ErrDecodeNetworkMsg) {
			e := ErrMalformedPubSubMessage
			e.innerErr = err
			return nil, Descriptor{}, e
		}
		if errors.Is(err, genesisqueue.ErrUnknownMessageType) {
			e := ErrUnknownSSVMessageType
			e.innerErr = err
			return nil, Descriptor{}, e
		}
		e := ErrMalformedMessage
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

	mv.metrics.GenesisSSVMessageType(msg.MsgType)

	return mv.validateSSVMessage(msg, receivedAt, signatureVerifier)
}

func (mv *messageValidator) validateSSVMessage(msg *genesisqueue.GenesisSSVMessage, receivedAt time.Time, signatureVerifier func() error) (*genesisqueue.GenesisSSVMessage, Descriptor, error) {
	var descriptor Descriptor
	ssvMessage := msg.SSVMessage

	if len(ssvMessage.Data) == 0 {
		return nil, descriptor, ErrEmptyData
	}

	if len(ssvMessage.Data) > maxMessageSize {
		err := ErrSSVDataTooBig
		err.got = len(ssvMessage.Data)
		err.want = maxMessageSize
		return nil, descriptor, err
	}
	domain := mv.netCfg.DomainType()
	if !bytes.Equal(ssvMessage.MsgID.GetDomain(), domain[:]) {
		err := ErrWrongDomain
		err.got = hex.EncodeToString(ssvMessage.MsgID.GetDomain())
		err.want = hex.EncodeToString(domain[:])
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
		shareStorage := mv.nodeStorage.Shares()
		share = shareStorage.Get(nil, publicKey.Serialize())
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

		if !share.IsAttesting(mv.netCfg.Beacon.EstimatedCurrentEpoch()) {
			err := ErrValidatorNotAttesting
			err.got = share.BeaconMetadata.Status.String()
			return nil, descriptor, err
		}
	}

	// Lock this SSV message ID to prevent concurrent access to the same state.
	mv.validationMutex.Lock()
	mutex, ok := mv.validationLocks[msg.GetID()]
	if !ok {
		mutex = &sync.Mutex{}
		mv.validationLocks[msg.GetID()] = mutex
	}
	mutex.Lock()
	defer mutex.Unlock()
	mv.validationMutex.Unlock()

	descriptor.SSVMessageType = ssvMessage.MsgType

	if mv.nodeStorage != nil {
		switch ssvMessage.MsgType {
		case spectypes.SSVConsensusMsgType:
			if len(ssvMessage.Data) > maxConsensusMsgSize {
				e := ErrSSVDataTooBig
				e.got = len(ssvMessage.Data)
				e.want = maxConsensusMsgSize
				return nil, descriptor, e
			}

			signedMessage := msg.Body.(*specqbft.SignedMessage)
			consensusDescriptor, slot, err := mv.validateConsensusMessage(share, signedMessage, msg.GetID(), receivedAt, signatureVerifier)
			descriptor.Consensus = &consensusDescriptor
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case spectypes.SSVPartialSignatureMsgType:
			if len(ssvMessage.Data) > maxPartialSignatureMsgSize {
				e := ErrSSVDataTooBig
				e.got = len(ssvMessage.Data)
				e.want = maxPartialSignatureMsgSize
				return nil, descriptor, e
			}

			partialSignatureMessage := msg.Body.(*spectypes.SignedPartialSignatureMessage)
			slot, err := mv.validatePartialSignatureMessage(share, partialSignatureMessage, msg.GetID(), signatureVerifier, receivedAt)
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case spectypes.MsgType(ssvmessage.SSVEventMsgType):
			return nil, descriptor, ErrEventMessage

		case spectypes.DKGMsgType:
			return nil, descriptor, ErrDKGMessage

		default:
			return nil, descriptor, ErrUnknownSSVMessageType
		}
	}

	return msg, descriptor, nil
}

func (mv *messageValidator) containsSignerFunc(signer spectypes.OperatorID) func(shareMember *alanspectypes.ShareMember) bool {
	return func(shareMember *alanspectypes.ShareMember) bool {
		return shareMember.Signer == signer
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
	case spectypes.BNRoleValidatorRegistration, spectypes.BNRoleVoluntaryExit:
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
