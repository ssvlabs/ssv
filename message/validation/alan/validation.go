// Package validation provides functions and structures for validating messages.
package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/alan/qbft"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	"github.com/cornelk/hashmap"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/operator/keys"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxMessageSize             = maxConsensusMsgSize // TODO: calculate new value
	maxConsensusMsgSize        = 8388608
	maxPartialSignatureMsgSize = 1952
	allowedRoundsInFuture      = 1
	allowedRoundsInPast        = 2
	lateSlotAllowance          = 2
	signatureSize              = 96
	maxDutiesPerEpoch          = 2
)

// ConsensusDescriptor provides details about the consensus for a message. It's used for logging and metrics.
type ConsensusDescriptor struct {
	Round           specqbft.Round
	QBFTMessageType specqbft.MessageType
	Signers         []spectypes.OperatorID
	Committee       []*spectypes.Operator
}

// Descriptor provides details about a message. It's used for logging and metrics.
// TODO: consider using context.Context
type Descriptor struct {
	SenderID       []byte
	Role           spectypes.BeaconRole
	SSVMessageType spectypes.MsgType
	Slot           phase0.Slot
	Consensus      *ConsensusDescriptor
}

// MessageValidator defines methods for validating pubsub messages.
type MessageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
	ValidatePubsubMessage(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

type messageValidator struct {
	logger                  *zap.Logger
	metrics                 metricsreporter.MetricsReporter
	netCfg                  networkconfig.NetworkConfig
	consensusStateIndex     sync.Map // TODO: use a map with explicit type
	validatorStore          ValidatorStore
	dutyStore               *dutystore.Store
	operatorDataStore       operatordatastore.OperatorDataStore
	operatorIDToPubkeyCache *hashmap.Map[spectypes.OperatorID, keys.OperatorPublicKey]

	// validationLocks is a map of lock per SSV message ID to
	// prevent concurrent access to the same state.
	validationLocks map[spectypes.MessageID]*sync.Mutex
	validationMutex sync.Mutex

	selfPID    peer.ID
	selfAccept bool
}

// NewMessageValidator returns a new MessageValidator with the given network configuration and options.
func NewMessageValidator(netCfg networkconfig.NetworkConfig, opts ...Option) MessageValidator {
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

// WithOwnOperatorID sets the operator ID getter for the messageValidator.
func WithOwnOperatorID(ods operatordatastore.OperatorDataStore) Option {
	return func(mv *messageValidator) {
		mv.operatorDataStore = ods
	}
}

// WithValidatorStore sets the validator store for the messageValidator.
func WithValidatorStore(validatorStore ValidatorStore) Option {
	return func(mv *messageValidator) {
		mv.validatorStore = validatorStore
	}
}

// WithSelfAccept blindly accepts messages sent from self. Useful for testing.
func WithSelfAccept(selfPID peer.ID, selfAccept bool) Option {
	return func(mv *messageValidator) {
		mv.selfPID = selfPID
		mv.selfAccept = selfAccept
	}
}

// ValidatorForTopic returns a validation function for the given topic.
// This function can be used to validate messages within the libp2p pubsub framework.
func (mv *messageValidator) ValidatorForTopic(_ string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return mv.ValidatePubsubMessage
}

// ValidatePubsubMessage validates the given pubsub message.
// Depending on the outcome, it will return one of the pubsub validation results (Accept, Ignore, or Reject).
func (mv *messageValidator) ValidatePubsubMessage(_ context.Context, peerID peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	if mv.selfAccept && peerID == mv.selfPID {
		return mv.validateSelf(pmsg)
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

				mv.metrics.MessageRejected(valErr.Text(), descriptor.Role, round)
				return pubsub.ValidationReject
			}

			if !valErr.Silent() {
				f = append(f, zap.Error(err))
				mv.logger.Debug("ignoring invalid message", f...)
			}
			mv.metrics.MessageIgnored(valErr.Text(), descriptor.Role, round)
			return pubsub.ValidationIgnore
		}

		mv.metrics.MessageIgnored(err.Error(), descriptor.Role, round)
		f = append(f, zap.Error(err))
		mv.logger.Debug("ignoring invalid message", f...)
		return pubsub.ValidationIgnore
	}

	pmsg.ValidatorData = decodedMessage

	mv.metrics.MessageAccepted(descriptor.Role, round)

	return pubsub.ValidationAccept
}

func (mv *messageValidator) validateP2PMessage(pMsg *pubsub.Message, receivedAt time.Time) (*queue.DecodedSSVMessage, Descriptor, error) {
	mv.metrics.ActiveMsgValidation(pMsg.GetTopic())
	mv.metrics.MessagesReceivedFromPeer(pMsg.ReceivedFrom)
	mv.metrics.MessagesReceivedTotal()
	mv.metrics.MessageSize(len(pMsg.GetData()))
	defer mv.metrics.ActiveMsgValidationDone(pMsg.GetTopic())

	if err := mv.validatePubSubMessage(pMsg); err != nil {
		return nil, Descriptor{}, err
	}

	signedSSVMessage := &spectypes.SignedSSVMessage{}
	if err := signedSSVMessage.Decode(pMsg.GetData()); err != nil {
		e := ErrMalformedPubSubMessage
		e.innerErr = err
		return nil, Descriptor{}, e
	}

	return mv.validateSignedSSVMessage(signedSSVMessage, pMsg.GetTopic(), receivedAt)
}

func (mv *messageValidator) validateSignedSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage, topic string, receivedAt time.Time) (*queue.DecodedSSVMessage, Descriptor, error) {
	if signedSSVMessage == nil {
		return nil, Descriptor{}, ErrEmptyPubSubMessage
	}

	if err := signedSSVMessage.Validate(); err != nil {
		e := ErrSignedSSVMessageValidation
		e.innerErr = err
		return nil, Descriptor{}, e
	}

	ssvMessage := signedSSVMessage.GetSSVMessage()

	signatureVerifier := func() error { // get
		return mv.verifySignatures(ssvMessage, signedSSVMessage.GetOperatorIDs(), signedSSVMessage.GetSignature())
	}

	return mv.validateSSVMessage(signedSSVMessage, topic, receivedAt, signatureVerifier)
}

func (mv *messageValidator) validateSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage, topic string, receivedAt time.Time, signatureVerifier func() error) (*queue.DecodedSSVMessage, Descriptor, error) {

	var descriptor Descriptor

	ssvMessage := signedSSVMessage.GetSSVMessage()

	if ssvMessage == nil {
		return nil, descriptor, ErrNilSSVMessage
	}

	//mv.metrics.SSVMessageType(ssvMessage.MsgType) // TODO
	descriptor.SSVMessageType = ssvMessage.MsgType

	if len(ssvMessage.Data) == 0 {
		return nil, descriptor, ErrEmptyData
	}

	if len(ssvMessage.Data) > maxMessageSize {
		err := ErrSSVDataTooBig
		err.got = len(ssvMessage.Data)
		err.want = maxMessageSize
		return nil, descriptor, err
	}

	if !mv.topicMatches(ssvMessage, topic) {
		return nil, descriptor, ErrTopicNotFound
	}

	if !bytes.Equal(ssvMessage.MsgID.GetDomain(), mv.netCfg.Domain[:]) {
		err := ErrWrongDomain
		err.got = hex.EncodeToString(ssvMessage.MsgID.GetDomain())
		err.want = hex.EncodeToString(mv.netCfg.Domain[:])
		return nil, descriptor, err
	}

	role := ssvMessage.GetID().GetRoleType()
	senderID := ssvMessage.GetID().GetSenderID()
	descriptor.Role = role
	descriptor.SenderID = senderID

	if !mv.validRole(role) {
		return nil, descriptor, ErrInvalidRole
	}

	// TODO determine if message is cluster or validator message
	committeeMessage := false
	if role == spectypes.BNRoleSyncCommittee { // TODO: fix role name once it's implemented in spec
		committeeMessage = true
	}

	if mv.validatorStore != nil {
		if committeeMessage {
			committee := mv.validatorStore.Committee(CommitteeID(senderID[16:])) // TODO: consider passing whole senderID
			_ = committee                                                        // TODO: committee checks
		} else {
			publicKey, err := ssvtypes.DeserializeBLSPublicKey(senderID)
			if err != nil {
				e := ErrDeserializePublicKey
				e.innerErr = err
				return nil, descriptor, e
			}

			share := mv.validatorStore.Validator(publicKey.Serialize())
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
	}

	// Lock this SSV message ID to prevent concurrent access to the same state.
	mv.validationMutex.Lock()
	mutex, ok := mv.validationLocks[ssvMessage.GetID()]
	if !ok {
		mutex = &sync.Mutex{}
		mv.validationLocks[ssvMessage.GetID()] = mutex
	}
	mutex.Lock()
	defer mutex.Unlock()
	mv.validationMutex.Unlock()

	if mv.validatorStore != nil {
		switch ssvMessage.MsgType {
		case spectypes.SSVConsensusMsgType:
			consensusDescriptor, slot, err := mv.validateConsensusMessage(signedSSVMessage, receivedAt, signatureVerifier)
			descriptor.Consensus = &consensusDescriptor
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case spectypes.SSVPartialSignatureMsgType:
			slot, err := mv.validatePartialSignatureMessage(ssvMessage, signatureVerifier)
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		default:
			e := ErrWrongSSVMessageType
			e.got = ssvMessage.GetType()
			return nil, descriptor, e
		}
	}

	return msg, descriptor, nil
}

func (mv *messageValidator) topicMatches(ssvMessage *spectypes.SSVMessage, topic string) bool {
	// Check if the message was sent on the right topic.
	currentTopicBaseName := commons.GetTopicBaseName(topic)
	topics := commons.ValidatorTopicID(ssvMessage.GetID().GetPubKey())

	topicFound := false
	for _, tp := range topics {
		if tp == currentTopicBaseName {
			topicFound = true
			break
		}
	}

	return topicFound
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
		SenderID: string(messageID.GetSenderID()),
		Role:     messageID.GetRoleType(),
	}

	if _, ok := mv.consensusStateIndex.Load(id); !ok {
		cs := &ConsensusState{
			Signers: hashmap.New[spectypes.OperatorID, *SignerState](),
		}
		mv.consensusStateIndex.Store(id, cs)
	}

	cs, _ := mv.consensusStateIndex.Load(id)
	return cs.(*ConsensusState)
}

// inCommittee should be called only when WithOwnOperatorID is set
func (mv *messageValidator) inCommittee(share *ssvtypes.SSVShare) bool {
	return slices.ContainsFunc(share.Committee, func(operator *spectypes.Operator) bool {
		return operator.OperatorID == mv.operatorDataStore.GetOperatorID()
	})
}
