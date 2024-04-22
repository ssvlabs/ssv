// Package validation provides functions and structures for validating messages.
package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"context"
	"sync"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/networkconfig"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/operator/keys"
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

// MessageValidator defines methods for validating pubsub messages.
type MessageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
	ValidatePubsubMessage(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
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

// NewMessageValidator returns a new MessageValidator with the given network configuration and options.
func NewMessageValidator(netCfg networkconfig.NetworkConfig, opts ...Option) MessageValidator {
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

	decodedMessage, err := mv.validateP2PMessage(pmsg, time.Now())

	loggerFields := mv.buildLoggerFields(decodedMessage)

	f := append(loggerFields.Fields(), fields.PeerID(peerID))

	if err != nil {
		var valErr Error
		if errors.As(err, &valErr) {
			if valErr.Reject() {
				if !valErr.Silent() {
					f = append(f, zap.Error(err))
					mv.logger.Debug("rejecting invalid message", f...)
				}

				mv.metrics.MessageRejected(valErr.Text(), loggerFields.Role, loggerFields.Consensus.Round)
				return pubsub.ValidationReject
			}

			if !valErr.Silent() {
				f = append(f, zap.Error(err))
				mv.logger.Debug("ignoring invalid message", f...)
			}
			mv.metrics.MessageIgnored(valErr.Text(), loggerFields.Role, loggerFields.Consensus.Round)
			return pubsub.ValidationIgnore
		}

		mv.metrics.MessageIgnored(err.Error(), loggerFields.Role, loggerFields.Consensus.Round)
		f = append(f, zap.Error(err))
		mv.logger.Debug("ignoring invalid message", f...)
		return pubsub.ValidationIgnore
	}

	pmsg.ValidatorData = decodedMessage

	mv.metrics.MessageAccepted(loggerFields.Role, loggerFields.Consensus.Round)

	return pubsub.ValidationAccept
}

func (mv *messageValidator) validateP2PMessage(pMsg *pubsub.Message, receivedAt time.Time) (*DecodedMessage, error) {
	mv.metrics.ActiveMsgValidation(pMsg.GetTopic())
	mv.metrics.MessagesReceivedFromPeer(pMsg.ReceivedFrom)
	mv.metrics.MessagesReceivedTotal()
	mv.metrics.MessageSize(len(pMsg.GetData()))
	defer mv.metrics.ActiveMsgValidationDone(pMsg.GetTopic())

	if err := mv.validatePubSubMessage(pMsg); err != nil {
		return nil, err
	}

	signedSSVMessage, err := mv.decodeSignedSSVMessage(pMsg)
	if err != nil {
		return nil, err
	}

	if err := mv.validateSignedSSVMessage(signedSSVMessage); err != nil {
		return nil, err
	}

	return mv.validateSSVMessage(signedSSVMessage, pMsg.GetTopic(), receivedAt)
}

func (mv *messageValidator) validateSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage, topic string, receivedAt time.Time) (*DecodedMessage, error) {
	if err := mv.ssvMessageValidation(signedSSVMessage, topic); err != nil {
		return nil, err
	}

	ssvMessage := signedSSVMessage.GetSSVMessage()

	committee, err := mv.getCommittee(ssvMessage.GetID())
	if err != nil {
		return nil, err
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

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		consensusMessage, err := mv.validateConsensusMessage(signedSSVMessage, committee, receivedAt)
		if err != nil {
			return nil, err
		}

		decodedMessage := &DecodedMessage{
			SignedSSVMessage: signedSSVMessage,
			Body:             consensusMessage,
		}
		return decodedMessage, nil

	case spectypes.SSVPartialSignatureMsgType:
		partialSignatureMessages, err := mv.validatePartialSignatureMessage(signedSSVMessage, committee)
		if err != nil {
			return nil, err
		}

		decodedMessage := &DecodedMessage{
			SignedSSVMessage: signedSSVMessage,
			Body:             partialSignatureMessages,
		}
		return decodedMessage, nil

	default:
		e := ErrWrongSSVMessageType
		e.got = ssvMessage.GetType()
		return nil, e
	}
}

func (mv *messageValidator) getCommittee(msgID spectypes.MessageID) ([]spectypes.OperatorID, error) {
	if msgID.GetRoleType() == spectypes.RoleCommittee {
		// TODO: add metrics and logs for committee role
		return mv.validatorStore.Committee(CommitteeID(msgID.GetSenderID()[16:])).Operators, nil // TODO: consider passing whole senderID
	}

	publicKey, err := ssvtypes.DeserializeBLSPublicKey(msgID.GetSenderID())
	if err != nil {
		e := ErrDeserializePublicKey
		e.innerErr = err
		return nil, e
	}

	validator := mv.validatorStore.Validator(publicKey.Serialize())
	if validator == nil {
		e := ErrUnknownValidator
		e.got = publicKey.SerializeToHexStr()
		return nil, e
	}

	if validator.Liquidated {
		return nil, ErrValidatorLiquidated
	}

	if validator.BeaconMetadata == nil {
		return nil, ErrNoShareMetadata
	}

	if !validator.IsAttesting(mv.netCfg.Beacon.EstimatedCurrentEpoch()) {
		e := ErrValidatorNotAttesting
		e.got = validator.BeaconMetadata.Status.String()
		return nil, e
	}

	var committee []spectypes.OperatorID
	for _, c := range validator.Committee {
		committee = append(committee, c.OperatorID)
	}

	return committee, nil
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

func (mv *messageValidator) commonSignerValidation(signer spectypes.OperatorID, committee []spectypes.OperatorID) error {
	if signer == 0 {
		return ErrZeroSigner
	}

	if !slices.Contains(committee, signer) {
		return ErrSignerNotInCommittee
	}

	return nil
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

// inCommittee should be called only when WithOwnOperatorID is set
func (mv *messageValidator) inCommittee(committee []spectypes.OperatorID) bool {
	return slices.ContainsFunc(committee, func(operatorID spectypes.OperatorID) bool {
		return operatorID == mv.operatorDataStore.GetOperatorID()
	})
}
