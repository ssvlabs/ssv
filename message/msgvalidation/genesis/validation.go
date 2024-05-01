// Package msgvalidation provides functions and structures for validating messages.
package msgvalidation

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
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/operator/keys"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
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

// MessageValidator defines methods for validating pubsub messages.
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
	operatorDataStore       operatordatastore.OperatorDataStore
	operatorIDToPubkeyCache *hashmap.Map[genesisspectypes.OperatorID, keys.OperatorPublicKey]

	// validationLocks is a map of lock per SSV message ID to
	// prevent concurrent access to the same state.
	validationLocks map[genesisspectypes.MessageID]*sync.Mutex
	validationMutex sync.Mutex

	selfPID    peer.ID
	selfAccept bool
}

// New returns a new MessageValidator with the given network configuration and options.
// DEPRECATED, TODO: remove post-fork
func New(netCfg networkconfig.NetworkConfig, opts ...Option) MessageValidator {
	mv := &messageValidator{
		logger:                  zap.NewNop(),
		metrics:                 metricsreporter.NewNop(),
		netCfg:                  netCfg,
		operatorIDToPubkeyCache: hashmap.New[genesisspectypes.OperatorID, keys.OperatorPublicKey](),
		validationLocks:         make(map[genesisspectypes.MessageID]*sync.Mutex),
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
	Round           genesisspecqbft.Round
	QBFTMessageType genesisspecqbft.MessageType
	Signers         []genesisspectypes.OperatorID
	Committee       []*genesisspectypes.Operator
}

// Descriptor provides details about a message. It's used for logging and metrics.
type Descriptor struct {
	ValidatorPK    genesisspectypes.ValidatorPK
	Role           genesisspectypes.BeaconRole
	SSVMessageType genesisspectypes.MsgType
	Slot           phase0.Slot
	Consensus      *ConsensusDescriptor
}

// Fields returns zap logging fields for the descriptor.
func (d Descriptor) Fields() []zapcore.Field {
	result := []zapcore.Field{
		fields.Validator(d.ValidatorPK),
		fields.BeaconRole(spectypes.BeaconRole(d.Role)),
		zap.String("ssv_message_type", ssvmessage.MsgTypeToString(spectypes.MsgType(d.SSVMessageType))),
		fields.Slot(d.Slot),
	}

	if d.Consensus != nil {
		var committee []genesisspectypes.OperatorID
		for _, o := range d.Consensus.Committee {
			committee = append(committee, o.OperatorID)
		}

		result = append(result,
			fields.Round(specqbft.Round(d.Consensus.Round)),
			zap.String("qbft_message_type", ssvmessage.QBFTMsgTypeToString(specqbft.MessageType(d.Consensus.QBFTMessageType))),
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
		ssvmessage.MsgTypeToString(spectypes.MsgType(d.SSVMessageType)),
		d.Slot,
	))

	if d.Consensus != nil {
		var committee []genesisspectypes.OperatorID
		for _, o := range d.Consensus.Committee {
			committee = append(committee, o.OperatorID)
		}

		sb.WriteString(fmt.Sprintf(", round: %v, qbft message type: %v, signers: %v, committee: %v",
			d.Consensus.Round,
			ssvmessage.QBFTMsgTypeToString(specqbft.MessageType(d.Consensus.QBFTMessageType)),
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
		rawMsgPayload, _, _, err := commons.DecodeSignedSSVMessage(pmsg.Data)
		if err != nil {
			mv.logger.Error("failed to decode signed ssv message", zap.Error(err))
			return pubsub.ValidationReject
		}
		msg, err := commons.DecodeNetworkMsg(rawMsgPayload)
		if err != nil {
			mv.logger.Error("failed to decode network message", zap.Error(err))
			return pubsub.ValidationReject
		}
		// skipping the error check for testing simplifying
		decMsg, _ := queue.DecodeGenesisSSVMessage(msg)
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
	round := genesisspecqbft.Round(0)
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

func (mv *messageValidator) validateP2PMessage(pMsg *pubsub.Message, receivedAt time.Time) (*queue.DecodedSSVMessage, Descriptor, error) {
	topic := pMsg.GetTopic()

	mv.metrics.ActiveMsgValidation(topic)
	mv.metrics.MessagesReceivedFromPeer(pMsg.ReceivedFrom)
	mv.metrics.MessagesReceivedTotal()

	defer mv.metrics.ActiveMsgValidationDone(topic)

	encMessageData := pMsg.GetData()

	var signatureVerifier func() error

	if len(encMessageData) == 0 {
		return nil, Descriptor{}, ErrPubSubMessageHasNoData
	}

	messageData, operatorID, signature, err := commons.DecodeSignedSSVMessage(encMessageData)

	if err != nil {
		e := ErrMalformedSignedMessage
		e.innerErr = err
		return nil, Descriptor{}, e
	}

	if len(messageData) == 0 {
		return nil, Descriptor{}, ErrDecodedPubSubMessageHasEmptyData
	}

	signatureVerifier = func() error {
		mv.metrics.MessageValidationRSAVerifications()
		return mv.verifySignature(messageData, operatorID, signature)
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

	mv.metrics.SSVMessageType(spectypes.MsgType(msg.MsgType))

	return mv.validateSSVMessage(msg, receivedAt, signatureVerifier)
}

func (mv *messageValidator) validateSSVMessage(ssvMessage *genesisspectypes.SSVMessage, receivedAt time.Time, signatureVerifier func() error) (*queue.DecodedSSVMessage, Descriptor, error) {
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

		if !share.IsAttesting(mv.netCfg.Beacon.EstimatedCurrentEpoch()) {
			err := ErrValidatorNotAttesting
			err.got = share.BeaconMetadata.Status.String()
			return nil, descriptor, err
		}
	}

	queueMsg, err := queue.DecodeGenesisSSVMessage(ssvMessage)
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

	msg := queueMsg.SSVMessage.(queue.WrappedGenesisMessage).SSVMessage

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
		// Other types are caught in queue.DecodeGenesisSSVMessage, so the default case is not necessary,
		// although it'd need to be added if queue.DecodeGenesisSSVMessage is removed.
		switch ssvMessage.MsgType {
		case genesisspectypes.SSVConsensusMsgType:
			if len(msg.Data) > maxConsensusMsgSize {
				e := ErrSSVDataTooBig
				e.got = len(ssvMessage.Data)
				e.want = maxConsensusMsgSize
				return nil, descriptor, e
			}

			signedMessage := queueMsg.Body.(*genesisspecqbft.SignedMessage)
			consensusDescriptor, slot, err := mv.validateConsensusMessage(share, signedMessage, msg.GetID(), receivedAt, signatureVerifier)
			descriptor.Consensus = &consensusDescriptor
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case genesisspectypes.SSVPartialSignatureMsgType:
			if len(msg.Data) > maxPartialSignatureMsgSize {
				e := ErrSSVDataTooBig
				e.got = len(ssvMessage.Data)
				e.want = maxPartialSignatureMsgSize
				return nil, descriptor, e
			}

			partialSignatureMessage := queueMsg.Body.(*genesisspectypes.SignedPartialSignatureMessage)
			slot, err := mv.validatePartialSignatureMessage(share, partialSignatureMessage, msg.GetID(), signatureVerifier)
			descriptor.Slot = slot
			if err != nil {
				return nil, descriptor, err
			}

		case genesisspectypes.MsgType(ssvmessage.SSVEventMsgType):
			return nil, descriptor, ErrEventMessage

		case genesisspectypes.DKGMsgType:
			return nil, descriptor, ErrDKGMessage
		}
	}

	return queueMsg, descriptor, nil
}

func (mv *messageValidator) containsSignerFunc(signer genesisspectypes.OperatorID) func(operator *genesisspectypes.Operator) bool {
	return func(operator *genesisspectypes.Operator) bool {
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

func (mv *messageValidator) commonSignerValidation(signer genesisspectypes.OperatorID, share *ssvtypes.SSVShare) error {
	if signer == 0 {
		return ErrZeroSigner
	}

	if !slices.ContainsFunc(share.Committee, mv.containsSignerFunc(signer)) {
		return ErrSignerNotInCommittee
	}

	return nil
}

func (mv *messageValidator) validateSlotTime(messageSlot phase0.Slot, role genesisspectypes.BeaconRole, receivedAt time.Time) error {
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

func (mv *messageValidator) lateMessage(slot phase0.Slot, role genesisspectypes.BeaconRole, receivedAt time.Time) time.Duration {
	var ttl phase0.Slot
	switch role {
	case genesisspectypes.BNRoleProposer, genesisspectypes.BNRoleSyncCommittee, genesisspectypes.BNRoleSyncCommitteeContribution:
		ttl = 1 + lateSlotAllowance
	case genesisspectypes.BNRoleAttester, genesisspectypes.BNRoleAggregator:
		ttl = 32 + lateSlotAllowance
	case genesisspectypes.BNRoleValidatorRegistration, genesisspectypes.BNRoleVoluntaryExit:
		return 0
	}

	deadline := mv.netCfg.Beacon.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin).Add(clockErrorTolerance)

	return mv.netCfg.Beacon.GetSlotStartTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		Sub(deadline)
}

func (mv *messageValidator) consensusState(messageID genesisspectypes.MessageID) *ConsensusState {
	id := ConsensusID{
		PubKey: phase0.BLSPubKey(messageID.GetPubKey()),
		Role:   messageID.GetRoleType(),
	}

	if _, ok := mv.index.Load(id); !ok {
		cs := &ConsensusState{
			Signers: hashmap.New[genesisspectypes.OperatorID, *SignerState](),
		}
		mv.index.Store(id, cs)
	}

	cs, _ := mv.index.Load(id)
	return cs.(*ConsensusState)
}

// inCommittee should be called only when WithOwnOperatorID is set
func (mv *messageValidator) inCommittee(share *ssvtypes.SSVShare) bool {
	return slices.ContainsFunc(share.Committee, func(operator *genesisspectypes.Operator) bool {
		return operator.OperatorID == mv.operatorDataStore.GetOperatorID()
	})
}
