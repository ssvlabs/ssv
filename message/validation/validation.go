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
	"go.uber.org/zap"
	"tailscale.com/util/singleflight"

	spectypes "github.com/ssvlabs/ssv-spec/types"

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
	logger         *zap.Logger
	netCfg         *networkconfig.Network
	validatorStore validatorStore
	operators      operators
	dutyStore      *dutystore.Store

	signatureVerifier signatureverifier.SignatureVerifier // TODO: use spectypes.SignatureVerifier

	// validationLockCache is a map of locks (SSV message ID -> lock) to ensure messages with
	// the same ID apply any state modifications (during message validation - which is not
	// stateless) in an isolated synchronized manner with respect to each other.
	validationLockCache *ttlcache.Cache[spectypes.MessageID, *sync.Mutex]
	// validationLocksInflight helps us prevent generating 2 different validation locks
	// for messages that must lock on the same lock (messages with the same ID) when undergoing
	// validation (that validation is not stateless - it often requires messageValidator to
	// update some state).
	validationLocksInflight singleflight.Group[spectypes.MessageID, *sync.Mutex]
	// states keeps track of signers(individual runners, of which every operator has multiple) per validator.
	states *ttlcache.Cache[spectypes.MessageID, *ValidatorState]

	selfPID    peer.ID
	selfAccept bool
}

// New returns a new MessageValidator with the given network configuration and options.
// It starts a goroutine that cleans up the state.
func New(
	netCfg *networkconfig.Network,
	validatorStore validatorStore,
	operators operators,
	dutyStore *dutystore.Store,
	signatureVerifier signatureverifier.SignatureVerifier,
	opts ...Option,
) MessageValidator {
	mv := &messageValidator{
		logger:              zap.NewNop(),
		netCfg:              netCfg,
		validationLockCache: ttlcache.New[spectypes.MessageID, *sync.Mutex](),
		validatorStore:      validatorStore,
		operators:           operators,
		dutyStore:           dutyStore,
		signatureVerifier:   signatureVerifier,
	}

	ttl := time.Duration(mv.maxStoredSlots()) * netCfg.SlotDuration // #nosec G115 -- amount of slots cannot exceed int64
	mv.states = ttlcache.New(
		ttlcache.WithTTL[spectypes.MessageID, *ValidatorState](ttl),
	)

	for _, opt := range opts {
		opt(mv)
	}

	// Start automatic expired item deletion for validationLockCache.
	go mv.validationLockCache.Start()
	// Start automatic expired item deletion for states.
	go mv.states.Start()

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

	decodedMessage, err := mv.handlePubsubMessage(pmsg, time.Now())

	defer func() {
		role := spectypes.RunnerRole(spectypes.RoleUnknown)
		if decodedMessage != nil {
			role = decodedMessage.GetID().GetRoleType()
		}
		recordMessageDuration(ctx, role, time.Since(validationStart))
	}()

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

	return mv.handleSignedSSVMessage(signedSSVMessage, pMsg.GetTopic(), pMsg.ReceivedFrom, receivedAt)
}

func (mv *messageValidator) handleSignedSSVMessage(
	signedSSVMessage *spectypes.SignedSSVMessage,
	topic string,
	receivedFrom peer.ID,
	receivedAt time.Time,
) (*queue.SSVMessage, error) {
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
		consensusMessage, err := mv.validateConsensusMessage(signedSSVMessage, committeeInfo, receivedFrom, receivedAt)
		decodedMessage.Body = consensusMessage
		if err != nil {
			return decodedMessage, err
		}

	case spectypes.SSVPartialSignatureMsgType:
		partialSignatureMessages, err := mv.validatePartialSignatureMessage(signedSSVMessage, committeeInfo, receivedFrom, receivedAt)
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

func (mv *messageValidator) getValidationLock(key spectypes.MessageID) *sync.Mutex {
	lock, _, _ := mv.validationLocksInflight.Do(key, func() (*sync.Mutex, error) {
		cachedLock := mv.validationLockCache.Get(key)
		if cachedLock != nil {
			return cachedLock.Value(), nil
		}

		lock := &sync.Mutex{}

		// validationLockTTL specifies how much time a particular validation lock is meant to
		// live. It must be large enough for validation lock to never expire while we still are
		// expecting to process messages targeting that same validation lock. For a message
		// that will get rejected due to being stale (even after acquiring some validation lock)
		// it doesn't matter which exact lock will get acquired (because no state updates will
		// be allowed to take place).
		// 2 epoch duration is a safe TTL to use - message validation will reject processing
		// for any message older than that.
		mv.validationLockCache.Set(key, lock, 2*mv.netCfg.EpochDuration())

		return lock, nil
	})
	return lock
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
	if !share.IsAttesting(mv.netCfg.EstimatedCurrentEpoch()) {
		e := ErrValidatorNotAttesting
		e.got = share.Status.String()
		return CommitteeInfo{}, e
	}

	operators := make([]spectypes.OperatorID, 0, len(share.Committee))
	for _, c := range share.Committee {
		operators = append(operators, c.Signer)
	}

	indices := []phase0.ValidatorIndex{share.ValidatorIndex}
	return newCommitteeInfo(share.CommitteeID(), operators, indices), nil
}

func (mv *messageValidator) validatorState(key spectypes.MessageID, committeeInfo CommitteeInfo) *ValidatorState {
	if v := mv.states.Get(key); v != nil {
		// Since mv.states keeps track of validator-state per operator-runner, it is possible that validator
		// has changed the committee/operator-cluster that manages this validator such that the state stored
		// in mv.states under this key is no longer relevant ... and so we need re-initialize the cached-state
		// entry in mv.states map from scratch whenever we spot the committee ID change.
		if v.Value().committeeID == committeeInfo.committeeID {
			return v.Value()
		}
	}

	cs := &ValidatorState{
		committeeID:     committeeInfo.committeeID,
		operators:       make([]*OperatorState, len(committeeInfo.committee)),
		storedSlotCount: mv.maxStoredSlots(),
	}
	mv.states.Set(key, cs, ttlcache.DefaultTTL)
	return cs
}

// maxStoredSlots stores max amount of slots message validation stores.
func (mv *messageValidator) maxStoredSlots() uint64 {
	return mv.netCfg.SlotsPerEpoch + LateSlotAllowance
}
