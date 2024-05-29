package validator

import (
	"context"
	"fmt"
	"sync"

	"github.com/cornelk/hashmap"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	mtx    *sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	DutyRunners       runner.DutyRunners
	Network           specqbft.Network
	Share             *types.SSVShare
	Signer            spectypes.KeyManager
	OperatorSigner    spectypes.OperatorSigner
	SignatureVerifier spectypes.SignatureVerifier

	Storage *storage.QBFTStores
	Queues  map[spectypes.BeaconRole]queueContainer

	// dutyIDs is a map for logging a unique ID for a given duty
	dutyIDs *hashmap.Map[spectypes.BeaconRole, string]

	state uint32

	messageValidator validation.MessageValidator
}

// NewValidator creates a new instance of Validator.
func NewValidator(pctx context.Context, cancel func(), options Options) *Validator {
	options.defaults()

	if options.Metrics == nil {
		options.Metrics = &NopMetrics{}
	}

	v := &Validator{
		mtx:               &sync.RWMutex{},
		ctx:               pctx,
		cancel:            cancel,
		DutyRunners:       options.DutyRunners,
		Network:           options.Network,
		Storage:           options.Storage,
		Share:             options.SSVShare,
		Signer:            options.Signer,
		OperatorSigner:    options.OperatorSigner,
		SignatureVerifier: options.SignatureVerifier,
		Queues:            make(map[spectypes.BeaconRole]queueContainer),
		state:             uint32(NotStarted),
		dutyIDs:           hashmap.New[spectypes.BeaconRole, string](),
		messageValidator:  options.MessageValidator,
	}

	for _, dutyRunner := range options.DutyRunners {
		// Set timeout function.
		dutyRunner.GetBaseRunner().TimeoutF = v.onTimeout

		// Setup the queue.
		role := dutyRunner.GetBaseRunner().BeaconRoleType

		v.Queues[role] = queueContainer{
			Q: queue.WithMetrics(queue.New(options.QueueSize), options.Metrics),
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             0,
				Slot:               0,
				//Quorum:             options.SSVShare.Share,// TODO
			},
		}
	}

	return v
}

// StartDuty starts a duty for the validator
func (v *Validator) StartDuty(logger *zap.Logger, duty *spectypes.Duty) error {
	dutyRunner := v.DutyRunners[duty.Type]
	if dutyRunner == nil {
		return errors.Errorf("no runner for duty type %s", duty.Type.String())
	}

	// Log with duty ID.
	baseRunner := dutyRunner.GetBaseRunner()
	v.dutyIDs.Set(duty.Type, fields.FormatDutyID(baseRunner.BeaconNetwork.EstimatedEpochAtSlot(duty.Slot), duty))
	logger = v.loggerForDuty(logger, duty.Type, duty.Slot)

	// Log with height.
	if baseRunner.QBFTController != nil {
		logger = logger.With(fields.Height(baseRunner.QBFTController.Height))
	}

	logger.Info("ℹ️ starting duty processing")

	return dutyRunner.StartNewDuty(logger, duty)
}

// ProcessMessage processes Network Message of all types
func (v *Validator) ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) error {
	if msg.GetType() != message.SSVEventMsgType {
		// Validate message
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return errors.Wrap(err, "invalid SignedSSVMessage")
		}

		// Verify SignedSSVMessage's signature
		if err := v.SignatureVerifier.Verify(msg.SignedSSVMessage, v.Share.Committee); err != nil {
			return errors.Wrap(err, "SignedSSVMessage has an invalid signature")
		}
	}

	messageID := msg.GetID()
	// Get runner
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(messageID)
	if dutyRunner == nil {
		return fmt.Errorf("could not get duty runner for msg ID %v", messageID)
	}

	// Validate message for runner
	if err := validateMessage(v.Share.Share, msg); err != nil {
		return fmt.Errorf("message invalid for msg ID %v: %w", messageID, err)
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
		if !ok {
			return errors.New("could not decode consensus message from network message")
		}
		logger = v.loggerForDuty(logger, messageID.GetRoleType(), phase0.Slot(signedMsg.Message.Height))

		// Check signer consistency
		if !signedMsg.CommonSigners([]spectypes.OperatorID{msg.GetOperatorID()}) {
			return errors.New("SignedSSVMessage's signer not consistent with SignedMessage's signers")
		}

		logger = logger.With(fields.Height(signedMsg.Message.Height))
		// Process
		return dutyRunner.ProcessConsensus(logger, signedMsg)
	case spectypes.SSVPartialSignatureMsgType:
		signedMsg, ok := msg.Body.(*spectypes.SignedPartialSignatureMessage)
		if !ok {
			return errors.New("could not decode post consensus message from network message")
		}
		logger = v.loggerForDuty(logger, messageID.GetRoleType(), signedMsg.Message.Slot)

		// Check signer consistency
		if signedMsg.Signer != msg.GetOperatorID() {
			return errors.New("SignedSSVMessage's signer not consistent with SignedPartialSignatureMessage's signer")
		}

		if signedMsg.Message.Type == spectypes.PostConsensusPartialSig {
			return dutyRunner.ProcessPostConsensus(logger, signedMsg)
		}
		return dutyRunner.ProcessPreConsensus(logger, signedMsg)
	case message.SSVEventMsgType:
		return v.handleEventMessage(logger, msg, dutyRunner)
	default:
		return errors.New("unknown msg")
	}
}

func (v *Validator) loggerForDuty(logger *zap.Logger, role spectypes.BeaconRole, slot phase0.Slot) *zap.Logger {
	logger = logger.With(fields.Slot(slot))
	if dutyID, ok := v.dutyIDs.Get(role); ok {
		return logger.With(fields.DutyID(dutyID))
	}
	return logger
}

func validateMessage(share spectypes.Share, msg *queue.DecodedSSVMessage) error {
	if !share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.SSVMessage.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
