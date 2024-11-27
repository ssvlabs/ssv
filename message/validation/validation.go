// Package validation provides functions and structures for validating messages.
package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/monitoring/metricsreporter"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/registry/storage"
)

// MessageValidator defines methods for validating pubsub messages.
type MessageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
	Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

type messageValidator struct {
	logger            *zap.Logger
	metrics           metricsreporter.MetricsReporter
	netCfg            networkconfig.NetworkConfig
	state             *ttlcache.Cache[spectypes.MessageID, *ValidatorState]
	validatorStore    storage.ValidatorStore
	dutyStore         *dutystore.Store
	signatureVerifier signatureverifier.SignatureVerifier // TODO: use spectypes.SignatureVerifier

	// validationLocks is a map of lock per SSV message ID to
	// prevent concurrent access to the same state.
	validationLocks map[spectypes.MessageID]*sync.Mutex
	validationMutex sync.Mutex

	selfPID    peer.ID
	selfAccept bool
}

// New returns a new MessageValidator with the given network configuration and options.
// It starts a goroutine that cleans up the state.
func New(
	netCfg networkconfig.NetworkConfig,
	validatorStore storage.ValidatorStore,
	dutyStore *dutystore.Store,
	signatureVerifier signatureverifier.SignatureVerifier,
	opts ...Option,
) MessageValidator {
	ttl := time.Duration(MaxStoredSlots(netCfg)) * netCfg.SlotDurationSec()

	mv := &messageValidator{
		logger:  zap.NewNop(),
		metrics: metricsreporter.NewNop(),
		netCfg:  netCfg,
		state: ttlcache.New(
			ttlcache.WithTTL[spectypes.MessageID, *ValidatorState](ttl),
		),
		validationLocks:   make(map[spectypes.MessageID]*sync.Mutex),
		validatorStore:    validatorStore,
		dutyStore:         dutyStore,
		signatureVerifier: signatureVerifier,
	}

	for _, opt := range opts {
		opt(mv)
	}

	go mv.state.Start()

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

func (mv *messageValidator) handlePubsubMessage(pMsg *pubsub.Message, receivedAt time.Time) (*queue.SSVMessage, error) {
	if err := mv.validatePubSubMessage(pMsg); err != nil {
		return nil, err
	}

	signedSSVMessage, err := mv.decodeSignedSSVMessage(pMsg)
	if err != nil {
		return nil, err
	}

	return mv.handleSignedSSVMessage(signedSSVMessage, pMsg.GetTopic(), receivedAt)
}

func (mv *messageValidator) handleSignedSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage, topic string, receivedAt time.Time) (*queue.SSVMessage, error) {
	decodedMessage := &queue.SSVMessage{
		SignedSSVMessage: signedSSVMessage,
	}

	if err := mv.validateSignedSSVMessage(signedSSVMessage); err != nil {
		return decodedMessage, err
	}

	decodedMessage.SSVMessage = signedSSVMessage.SSVMessage

	if err := mv.validateSSVMessage(signedSSVMessage.SSVMessage); err != nil {
		return decodedMessage, err
	}

	// TODO: leverage the ValidatorStore to keep track of committees' indices and return them in Committee methods (which already return a Committee struct that we should add an Indices filter to): https://github.com/ssvlabs/ssv/pull/1393#discussion_r1667681686
	committeeInfo, err := mv.getCommitteeAndValidatorIndices(signedSSVMessage.SSVMessage.GetID())
	if err != nil {
		return decodedMessage, err
	}

	if err := mv.committeeChecks(signedSSVMessage, committeeInfo, topic); err != nil {
		return decodedMessage, err
	}

	validationMu := mv.obtainValidationLock(signedSSVMessage.SSVMessage.GetID())

	validationMu.Lock()
	defer validationMu.Unlock()

	switch signedSSVMessage.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		consensusMessage, err := mv.validateConsensusMessage(signedSSVMessage, committeeInfo, receivedAt)
		decodedMessage.Body = consensusMessage
		if err != nil {
			return decodedMessage, err
		}

	case spectypes.SSVPartialSignatureMsgType:
		partialSignatureMessages, err := mv.validatePartialSignatureMessage(signedSSVMessage, committeeInfo, receivedAt)
		decodedMessage.Body = partialSignatureMessages
		if err != nil {
			return decodedMessage, err
		}

	default:
		return decodedMessage, fmt.Errorf("unreachable: message type assertion should have been done")
	}

	return decodedMessage, nil
}

func (mv *messageValidator) committeeChecks(signedSSVMessage *spectypes.SignedSSVMessage, committeeInfo CommitteeInfo, topic string) error {
	if err := mv.belongsToCommittee(signedSSVMessage.OperatorIDs, committeeInfo.committee); err != nil {
		return err
	}

	// Rule: Check if message was sent in the correct topic
	messageTopics := commons.CommitteeTopicID(committeeInfo.committeeID)
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
	// TODO: make sure that we check that message ID exists in advance
	mutex, ok := mv.validationLocks[messageID]
	if !ok {
		mutex = &sync.Mutex{}
		mv.validationLocks[messageID] = mutex
		// TODO: Clean the map when mutex won't be needed anymore. Now it's a mutex leak...
	}
	mv.validationMutex.Unlock()

	return mutex
}

func (mv *messageValidator) getCommitteeAndValidatorIndices(msgID spectypes.MessageID) (CommitteeInfo, error) {
	if mv.committeeRole(msgID.GetRoleType()) {
		// TODO: add metrics and logs for committee role
		committeeID := spectypes.CommitteeID(msgID.GetDutyExecutorID()[16:])

		// Rule: Cluster does not exist
		committee, exists := mv.validatorStore.Committee(committeeID) // TODO: consider passing whole duty executor ID
		if !exists {
			e := ErrNonExistentCommitteeID
			e.got = hex.EncodeToString(committeeID[:])
			return CommitteeInfo{}, e
		}

		if len(committee.Indices) == 0 {
			return CommitteeInfo{}, ErrNoValidators
		}

		return newCommitteeInfo(committeeID, committee.Operators, committee.Indices), nil
	}

	validator, exists := mv.validatorStore.Validator(msgID.GetDutyExecutorID())
	if !exists {
		e := ErrUnknownValidator
		e.got = hex.EncodeToString(msgID.GetDutyExecutorID())
		return CommitteeInfo{}, e
	}

	// Rule: If validator is liquidated
	if validator.Liquidated {
		return CommitteeInfo{}, ErrValidatorLiquidated
	}

	if validator.BeaconMetadata == nil {
		return CommitteeInfo{}, ErrNoShareMetadata
	}

	// Rule: If validator is not active
	if !validator.IsAttesting(mv.netCfg.Beacon.EstimatedCurrentEpoch()) {
		e := ErrValidatorNotAttesting
		e.got = validator.BeaconMetadata.Status.String()
		return CommitteeInfo{}, e
	}

	var operators []spectypes.OperatorID
	for _, c := range validator.Committee {
		operators = append(operators, c.Signer)
	}

	indices := []phase0.ValidatorIndex{validator.BeaconMetadata.Index}
	return newCommitteeInfo(validator.CommitteeID(), operators, indices), nil
}

func (mv *messageValidator) validatorState(messageID spectypes.MessageID, committee []spectypes.OperatorID) *ValidatorState {
	if v := mv.state.Get(messageID); v != nil {
		return v.Value()
	}

	cs := &ValidatorState{
		operators:       make([]*OperatorState, len(committee)),
		storedSlotCount: MaxStoredSlots(mv.netCfg),
	}
	mv.state.Set(messageID, cs, ttlcache.DefaultTTL)
	return cs
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

// MaxStoredSlots stores max amount of slots message validation stores.
// It's exported to allow usage outside of message validation
func MaxStoredSlots(netCfg networkconfig.NetworkConfig) phase0.Slot {
	return phase0.Slot(netCfg.Beacon.SlotsPerEpoch()) + lateSlotAllowance
}
