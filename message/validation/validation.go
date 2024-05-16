// Package validation provides functions and structures for validating messages.
package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/message/signatureverifier"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/registry/storage"
)

// MessageValidator defines methods for validating pubsub messages.
type MessageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
	Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

type messageValidator struct {
	logger                *zap.Logger
	metrics               metricsreporter.MetricsReporter
	netCfg                networkconfig.NetworkConfig
	consensusStateIndex   map[phase0.Slot]map[consensusID]*consensusState
	consensusStateIndexMu sync.Mutex
	validatorStore        storage.ValidatorStore
	dutyStore             *dutystore.Store
	signatureVerifier     signatureverifier.SignatureVerifier // TODO: use spectypes.SignatureVerifier

	// validationLocks is a map of lock per SSV message ID to
	// prevent concurrent access to the same state.
	validationLocks map[spectypes.MessageID]*sync.Mutex
	validationMutex sync.Mutex

	selfPID    peer.ID
	selfAccept bool
}

// New returns a new MessageValidator with the given network configuration and options.
func New(
	netCfg networkconfig.NetworkConfig,
	validatorStore storage.ValidatorStore,
	dutyStore *dutystore.Store,
	signatureVerifier signatureverifier.SignatureVerifier,
	opts ...Option,
) MessageValidator {
	mv := &messageValidator{
		logger:              zap.NewNop(),
		metrics:             metricsreporter.NewNop(),
		netCfg:              netCfg,
		consensusStateIndex: make(map[phase0.Slot]map[consensusID]*consensusState),
		validationLocks:     make(map[spectypes.MessageID]*sync.Mutex),
		validatorStore:      validatorStore,
		dutyStore:           dutyStore,
		signatureVerifier:   signatureVerifier,
	}

	for _, opt := range opts {
		opt(mv)
	}

	return mv
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
		return mv.validateSelf(pmsg)
	}

	reportDone := mv.reportPubSubMetrics(pmsg)
	defer reportDone()

	decodedMessage, err := mv.handlePubsubMessage(pmsg, time.Now())
	if err != nil {
		return mv.handleValidationError(peerID, decodedMessage, err)
	}

	pmsg.ValidatorData = decodedMessage

	return mv.handleValidationSuccess(decodedMessage)
}

func (mv *messageValidator) handlePubsubMessage(pMsg *pubsub.Message, receivedAt time.Time) (*queue.DecodedSSVMessage, error) {
	if err := mv.validatePubSubMessage(pMsg); err != nil {
		return nil, err
	}

	signedSSVMessage, err := mv.decodeSignedSSVMessage(pMsg)
	if err != nil {
		return nil, err
	}

	return mv.handleSignedSSVMessage(signedSSVMessage, pMsg.GetTopic(), receivedAt)
}

func (mv *messageValidator) handleSignedSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage, topic string, receivedAt time.Time) (*queue.DecodedSSVMessage, error) {
	decodedMessage := &queue.DecodedSSVMessage{
		SignedSSVMessage: signedSSVMessage,
		SSVMessage:       signedSSVMessage.SSVMessage,
	}

	if err := mv.validateSignedSSVMessage(signedSSVMessage); err != nil {
		return decodedMessage, err
	}

	if err := mv.validateSSVMessage(signedSSVMessage.SSVMessage, topic); err != nil {
		return decodedMessage, err
	}

	committeeData, err := mv.getCommitteeAndValidatorIndices(signedSSVMessage.SSVMessage.GetID())
	if err != nil {
		return decodedMessage, err
	}

	if err := mv.committeeChecks(signedSSVMessage, committeeData, topic); err != nil {
		return decodedMessage, err
	}

	validationMu := mv.obtainValidationLock(signedSSVMessage.SSVMessage.GetID())

	validationMu.Lock()
	defer validationMu.Unlock()

	switch signedSSVMessage.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		consensusMessage, err := mv.validateConsensusMessage(signedSSVMessage, committeeData, receivedAt)
		if err != nil {
			return decodedMessage, err
		}

		decodedMessage.Body = consensusMessage
	case spectypes.SSVPartialSignatureMsgType:
		partialSignatureMessages, err := mv.validatePartialSignatureMessage(signedSSVMessage, committeeData, receivedAt)
		if err != nil {
			return decodedMessage, err
		}

		decodedMessage.Body = partialSignatureMessages
	default:
		panic("unreachable: message type assertion should have been done")
	}

	return decodedMessage, nil
}

func (mv *messageValidator) committeeChecks(signedSSVMessage *spectypes.SignedSSVMessage, committeeData CommitteeData, topic string) error {
	if err := mv.belongsToCommittee(signedSSVMessage.GetOperatorIDs(), committeeData.operatorIDs); err != nil {
		return err
	}

	messageTopics := commons.CommitteeTopicID(committeeData.committeeID[:])
	topicBaseName := commons.GetTopicBaseName(topic)
	if !slices.Contains(messageTopics, topicBaseName) {
		e := ErrIncorrectTopic
		e.got = fmt.Sprintf("topic %v / base name %v", topic, topicBaseName)
		e.want = messageTopics
		return e
	}
	return nil
}

func (mv *messageValidator) obtainValidationLock(messageID spectypes.MessageID) *sync.Mutex {
	// Lock this SSV message ID to prevent concurrent access to the same state.
	mv.validationMutex.Lock()
	mutex, ok := mv.validationLocks[messageID]
	if !ok {
		mutex = &sync.Mutex{}
		mv.validationLocks[messageID] = mutex
	}
	mv.validationMutex.Unlock()

	return mutex
}

type CommitteeData struct {
	operatorIDs []spectypes.OperatorID
	indices     []phase0.ValidatorIndex
	committeeID spectypes.ClusterID
}

func (mv *messageValidator) getCommitteeAndValidatorIndices(msgID spectypes.MessageID) (CommitteeData, error) {
	if mv.committeeRole(msgID.GetRoleType()) {
		// TODO: add metrics and logs for committee role
		committeeID := spectypes.ClusterID(msgID.GetSenderID()[16:])
		committee := mv.validatorStore.Committee(committeeID) // TODO: consider passing whole senderID
		if committee == nil {
			e := ErrNonExistentCommitteeID
			e.got = hex.EncodeToString(committeeID[:])
			return CommitteeData{}, e
		}

		validatorIndices := make([]phase0.ValidatorIndex, 0)
		for _, v := range committee.Validators {
			if v.BeaconMetadata != nil {
				validatorIndices = append(validatorIndices, v.BeaconMetadata.Index)
			}
		}

		if len(validatorIndices) == 0 {
			return CommitteeData{}, ErrNoValidators
		}

		return CommitteeData{
			operatorIDs: committee.Operators,
			indices:     validatorIndices,
			committeeID: committeeID,
		}, nil
	}

	publicKey, err := ssvtypes.DeserializeBLSPublicKey(msgID.GetSenderID())
	if err != nil {
		e := ErrDeserializePublicKey
		e.innerErr = err
		return CommitteeData{}, e
	}

	validator := mv.validatorStore.Validator(publicKey.Serialize())
	if validator == nil {
		e := ErrUnknownValidator
		e.got = publicKey.SerializeToHexStr()
		return CommitteeData{}, e
	}

	if validator.Liquidated {
		return CommitteeData{}, ErrValidatorLiquidated
	}

	if validator.BeaconMetadata == nil {
		return CommitteeData{}, ErrNoShareMetadata
	}

	if !validator.IsAttesting(mv.netCfg.Beacon.EstimatedCurrentEpoch()) {
		e := ErrValidatorNotAttesting
		e.got = validator.BeaconMetadata.Status.String()
		return CommitteeData{}, e
	}

	var operators []spectypes.OperatorID
	for _, c := range validator.Committee {
		operators = append(operators, c.Signer)
	}

	return CommitteeData{
		operatorIDs: operators,
		indices:     []phase0.ValidatorIndex{validator.BeaconMetadata.Index},
		committeeID: validator.CommitteeID(),
	}, nil
}

func (mv *messageValidator) consensusState(messageID spectypes.MessageID, slot phase0.Slot) *consensusState {
	mv.consensusStateIndexMu.Lock()
	defer mv.consensusStateIndexMu.Unlock()

	id := consensusID{
		SenderID: string(messageID.GetSenderID()),
		Role:     messageID.GetRoleType(),
	}

	if _, ok := mv.consensusStateIndex[slot]; !ok {
		mv.consensusStateIndex[slot] = make(map[consensusID]*consensusState)
		mv.cleanupOldConsensusStateSlots(slot)
	}

	if _, ok := mv.consensusStateIndex[slot][id]; !ok {
		cs := &consensusState{
			state: make(map[spectypes.OperatorID]*SignerState),
		}
		mv.consensusStateIndex[slot][id] = cs
	}

	return mv.consensusStateIndex[slot][id]
}

func (mv *messageValidator) cleanupOldConsensusStateSlots(latestSlot phase0.Slot) {
	var oldSlots []phase0.Slot

	// The map may contain max 34 slots, so iterating the whole map is likely still ok.
	for slot := range mv.consensusStateIndex {
		if slot < latestSlot-phase0.Slot(mv.netCfg.Beacon.SlotsPerEpoch())+lateSlotAllowance {
			oldSlots = append(oldSlots, slot)
		}
	}

	for _, slot := range oldSlots {
		delete(mv.consensusStateIndex, slot)
	}
}

func (mv *messageValidator) reportPubSubMetrics(pmsg *pubsub.Message) (done func()) {
	mv.metrics.ActiveMsgValidation(pmsg.GetTopic())
	mv.metrics.MessagesReceivedFromPeer(pmsg.ReceivedFrom)
	mv.metrics.MessagesReceivedTotal()
	mv.metrics.MessageSize(len(pmsg.GetData()))

	start := time.Now()

	return func() {
		sinceStart := time.Since(start)

		mv.metrics.MessageValidationDuration(sinceStart)
		mv.metrics.ActiveMsgValidationDone(pmsg.GetTopic())
	}
}
