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

	"github.com/ssvlabs/ssv/utils/casts"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"tailscale.com/util/singleflight"

	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// MessageValidator defines methods for validating pubsub messages.
type MessageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
	Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

// operators defines the minimal interface needed for validation
type operators interface {
	OperatorsExist(r basedb.Reader, ids []spectypes.OperatorID) (bool, error)
}

// validatorStore defines the minimal interface needed for validation
type validatorStore interface {
	Validator(pubKey []byte) (*ssvtypes.SSVShare, bool)
	Committee(id spectypes.CommitteeID) (*registrystorage.Committee, bool)
}

type messageValidator struct {
	logger          *zap.Logger
	netCfg          networkconfig.NetworkConfig
	pectraForkEpoch phase0.Epoch

	consensusStateIndex   map[consensusID]*consensusState
	consensusStateIndexMu sync.Mutex

	validatorStore validatorStore
	operators      operators
	dutyStore      *dutystore.Store

	signatureVerifier signatureverifier.SignatureVerifier // TODO: use spectypes.SignatureVerifier

	// validationLockCache is a map of locks (SSV message ID -> lock) to ensure messages with
	// same ID apply any state modifications (during message validation - which is not
	// stateless) in isolated synchronised manner with respect to each other.
	validationLockCache *ttlcache.Cache[spectypes.MessageID, *sync.Mutex]
	// validationLocksInflight helps us prevent generating 2 different validation locks
	// for messages that must lock on the same lock (messages with same ID) when undergoing
	// validation (that validation is not stateless - it often requires messageValidator to
	// update some state).
	validationLocksInflight singleflight.Group[spectypes.MessageID, *sync.Mutex]

	selfPID    peer.ID
	selfAccept bool
}

// New returns a new MessageValidator with the given network configuration and options.
func New(
	netCfg networkconfig.NetworkConfig,
	validatorStore validatorStore,
	operators operators,
	dutyStore *dutystore.Store,
	signatureVerifier signatureverifier.SignatureVerifier,
	pectraForkEpoch phase0.Epoch,
	opts ...Option,
) MessageValidator {
	mv := &messageValidator{
		logger:              zap.NewNop(),
		netCfg:              netCfg,
		consensusStateIndex: make(map[consensusID]*consensusState),
		validationLockCache: ttlcache.New[spectypes.MessageID, *sync.Mutex](),
		validatorStore:      validatorStore,
		operators:           operators,
		dutyStore:           dutyStore,
		signatureVerifier:   signatureVerifier,
		pectraForkEpoch:     pectraForkEpoch,
	}

	for _, opt := range opts {
		opt(mv)
	}

	// Start automatic expired item deletion for validationLockCache.
	go mv.validationLockCache.Start()

	return mv
}

// ValidatorForTopic returns a validation function for the given topic.
// This function can be used to validate messages within the libp2p pubsub framework.
func (mv *messageValidator) ValidatorForTopic(_ string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return mv.Validate
}

// Validate validates the given pubsub message.
// Depending on the outcome, it will return one of the pubsub validation results (Accept, Ignore, or Reject).
func (mv *messageValidator) Validate(ctx context.Context, peerID peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	if mv.selfAccept && peerID == mv.selfPID {
		return mv.validateSelf(pmsg)
	}

	validationStart := time.Now()
	defer func() {
		messageValidationDurationHistogram.Record(ctx, time.Since(validationStart).Seconds())
	}()

	recordMessage(ctx)

	decodedMessage, err := mv.handlePubsubMessage(pmsg, time.Now())
	if err != nil {
		return mv.handleValidationError(ctx, peerID, decodedMessage, err)
	}

	pmsg.ValidatorData = decodedMessage

	return mv.handleValidationSuccess(ctx, decodedMessage)
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

	// TODO: leverage the validatorStore to keep track of committees' indices and return them in Committee methods (which already return a Committee struct that we should add an Indices filter to): https://github.com/ssvlabs/ssv/pull/1393#discussion_r1667681686
	committeeInfo, err := mv.getCommitteeAndValidatorIndices(signedSSVMessage.SSVMessage.GetID())
	if err != nil {
		return decodedMessage, err
	}

	if err := mv.committeeChecks(signedSSVMessage, committeeInfo, topic); err != nil {
		return decodedMessage, err
	}

	validationMu := mv.getValidationLock(signedSSVMessage.SSVMessage.GetID())
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
	if err := mv.belongsToCommittee(signedSSVMessage.OperatorIDs, committeeInfo.operatorIDs); err != nil {
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

func (mv *messageValidator) getValidationLock(messageID spectypes.MessageID) *sync.Mutex {
	lock, _, _ := mv.validationLocksInflight.Do(messageID, func() (*sync.Mutex, error) {
		cachedLock := mv.validationLockCache.Get(messageID)
		if cachedLock != nil {
			return cachedLock.Value(), nil
		}

		lock := &sync.Mutex{}

		epochDuration := casts.DurationFromUint64(mv.netCfg.Beacon.SlotsPerEpoch()) * mv.netCfg.Beacon.SlotDurationSec()
		// validationLockTTL specifies how much time a particular validation lock is meant to
		// live. It must be large enough for validation lock to never expire while we still are
		// expecting to process messages targeting that same validation lock. For a message
		// that will get rejected due to being stale (even after acquiring some validation lock)
		// it doesn't matter which exact lock will get acquired (because no state updates will
		// be allowed to take place).
		// 2 epoch duration is a safe TTL to use - message validation will reject processing
		// for any message older than that.
		validationLockTTL := 2 * epochDuration
		mv.validationLockCache.Set(messageID, lock, validationLockTTL)

		return lock, nil
	})
	return lock
}

type CommitteeInfo struct {
	operatorIDs []spectypes.OperatorID
	indices     []phase0.ValidatorIndex
	committeeID spectypes.CommitteeID
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

		return CommitteeInfo{
			operatorIDs: committee.Operators,
			indices:     committee.Indices,
			committeeID: committeeID,
		}, nil
	}

	share, exists := mv.validatorStore.Validator(msgID.GetDutyExecutorID())
	if !exists {
		e := ErrUnknownValidator
		e.got = hex.EncodeToString(msgID.GetDutyExecutorID())
		return CommitteeInfo{}, e
	}

	// Rule: If validator is liquidated
	if share.Liquidated {
		return CommitteeInfo{}, ErrValidatorLiquidated
	}

	if !share.HasBeaconMetadata() {
		return CommitteeInfo{}, ErrNoShareMetadata
	}

	// Rule: If validator is not active
	if !share.IsAttesting(mv.netCfg.Beacon.EstimatedCurrentEpoch()) {
		e := ErrValidatorNotAttesting
		e.got = share.Status.String()
		return CommitteeInfo{}, e
	}

	var operators []spectypes.OperatorID
	for _, c := range share.Committee {
		operators = append(operators, c.Signer)
	}

	return CommitteeInfo{
		operatorIDs: operators,
		indices:     []phase0.ValidatorIndex{share.ValidatorIndex},
		committeeID: share.CommitteeID(),
	}, nil
}

func (mv *messageValidator) consensusState(messageID spectypes.MessageID) *consensusState {
	mv.consensusStateIndexMu.Lock()
	defer mv.consensusStateIndexMu.Unlock()

	id := consensusID{
		DutyExecutorID: string(messageID.GetDutyExecutorID()),
		Role:           messageID.GetRoleType(),
	}

	if _, ok := mv.consensusStateIndex[id]; !ok {
		cs := &consensusState{
			state:           make(map[spectypes.OperatorID]*OperatorState),
			storedSlotCount: phase0.Slot(mv.netCfg.Beacon.SlotsPerEpoch()) * 2, // store last two epochs to calculate duty count
		}
		mv.consensusStateIndex[id] = cs
	}

	return mv.consensusStateIndex[id]
}
