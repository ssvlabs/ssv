// Package msgvalidation provides functions and structures for validating messages.
package msgvalidation

// validator.go contains main code for validation and most of the rule checks.

import (
	"context"
	"encoding/hex"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/networkconfig"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/operator/keys"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

// MessageValidator defines methods for validating pubsub messages.
type MessageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
	Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

type messageValidator struct {
	logger                    *zap.Logger
	metrics                   metricsreporter.MetricsReporter
	netCfg                    networkconfig.NetworkConfig
	consensusStateIndex       map[consensusID]*consensusState
	consensusStateIndexMu     sync.Mutex
	validatorStore            ValidatorStore
	operatorStore             OperatorStore
	dutyStore                 *dutystore.Store
	operatorDataStore         operatordatastore.OperatorDataStore
	operatorIDToPubkeyCache   map[spectypes.OperatorID]keys.OperatorPublicKey
	operatorIDToPubkeyCacheMu sync.Mutex

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
		operatorIDToPubkeyCache: make(map[spectypes.OperatorID]keys.OperatorPublicKey),
		validationLocks:         make(map[spectypes.MessageID]*sync.Mutex),
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

func (mv *messageValidator) handlePubsubMessage(pMsg *pubsub.Message, receivedAt time.Time) (*DecodedMessage, error) {
	if err := mv.validatePubSubMessage(pMsg); err != nil {
		return nil, err
	}

	signedSSVMessage, err := mv.decodeSignedSSVMessage(pMsg)
	if err != nil {
		return nil, err
	}

	return mv.handleSignedSSVMessage(signedSSVMessage, pMsg.GetTopic(), receivedAt)
}

func (mv *messageValidator) handleSignedSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage, topic string, receivedAt time.Time) (*DecodedMessage, error) {
	if err := mv.validateSignedSSVMessage(signedSSVMessage); err != nil {
		return nil, err
	}

	if err := mv.validateSSVMessage(signedSSVMessage.GetSSVMessage(), topic); err != nil {
		return nil, err
	}

	committee, validatorIndices, err := mv.getCommitteeAndValidatorIndices(signedSSVMessage.GetSSVMessage().GetID())
	if err != nil {
		return nil, err
	}

	if err := mv.belongsToCommittee(signedSSVMessage.GetOperatorIDs(), committee); err != nil {
		return nil, err
	}

	validationMu := mv.obtainValidationLock(signedSSVMessage.GetSSVMessage().GetID())

	validationMu.Lock()
	defer validationMu.Unlock()

	decodedMessage := &DecodedMessage{
		SignedSSVMessage: signedSSVMessage,
	}

	switch signedSSVMessage.GetSSVMessage().MsgType {
	case spectypes.SSVConsensusMsgType:
		consensusMessage, err := mv.validateConsensusMessage(signedSSVMessage, committee, validatorIndices, receivedAt)
		if err != nil {
			return nil, err
		}

		decodedMessage.Body = consensusMessage
	case spectypes.SSVPartialSignatureMsgType:
		partialSignatureMessages, err := mv.validatePartialSignatureMessage(signedSSVMessage, committee, validatorIndices, receivedAt)
		if err != nil {
			return nil, err
		}

		decodedMessage.Body = partialSignatureMessages
	default:
		panic("message type assertion should have been done")
	}

	return decodedMessage, nil
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

func (mv *messageValidator) getCommitteeAndValidatorIndices(msgID spectypes.MessageID) ([]spectypes.OperatorID, []phase0.ValidatorIndex, error) {
	if mv.committeeRole(msgID.GetRoleType()) {
		// TODO: add metrics and logs for committee role
		committeeID := CommitteeID(msgID.GetSenderID()[16:])
		committee := mv.validatorStore.Committee(committeeID) // TODO: consider passing whole senderID
		if committee == nil {
			e := ErrNonExistentCommitteeID
			e.got = hex.EncodeToString(committeeID[:])
			return nil, nil, e
		}

		validatorIndices := make([]phase0.ValidatorIndex, 0)
		for _, v := range committee.Validators {
			if v.BeaconMetadata != nil {
				validatorIndices = append(validatorIndices, v.BeaconMetadata.Index)
			}
		}

		if len(validatorIndices) == 0 {
			return nil, nil, ErrNoValidators
		}

		return committee.Operators, validatorIndices, nil
	}

	publicKey, err := ssvtypes.DeserializeBLSPublicKey(msgID.GetSenderID())
	if err != nil {
		e := ErrDeserializePublicKey
		e.innerErr = err
		return nil, nil, e
	}

	validator := mv.validatorStore.Validator(publicKey.Serialize())
	if validator == nil {
		e := ErrUnknownValidator
		e.got = publicKey.SerializeToHexStr()
		return nil, nil, e
	}

	if validator.Liquidated {
		return nil, nil, ErrValidatorLiquidated
	}

	if validator.BeaconMetadata == nil {
		return nil, nil, ErrNoShareMetadata
	}

	if !validator.IsAttesting(mv.netCfg.Beacon.EstimatedCurrentEpoch()) {
		e := ErrValidatorNotAttesting
		e.got = validator.BeaconMetadata.Status.String()
		return nil, nil, e
	}

	var committee []spectypes.OperatorID
	for _, c := range validator.Committee {
		committee = append(committee, c.OperatorID)
	}

	return committee, []phase0.ValidatorIndex{validator.BeaconMetadata.Index}, nil
}

func (mv *messageValidator) consensusState(messageID spectypes.MessageID) *consensusState {
	mv.consensusStateIndexMu.Lock()
	defer mv.consensusStateIndexMu.Unlock()

	id := consensusID{
		SenderID: string(messageID.GetSenderID()),
		Role:     messageID.GetRoleType(),
	}

	if _, ok := mv.consensusStateIndex[id]; !ok {
		cs := &consensusState{
			signers: make(map[spectypes.OperatorID]*SignerState),
		}
		mv.consensusStateIndex[id] = cs
	}

	return mv.consensusStateIndex[id]
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
