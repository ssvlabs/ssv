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

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	mtx    *sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	DutyRunners runner.ValidatorDutyRunners
	Network     specqbft.Network

	Operator          *spectypes.Operator
	Share             *types.SSVShare
	Signer            spectypes.BeaconSigner
	OperatorSigner    spectypes.OperatorSigner
	SignatureVerifier spectypes.SignatureVerifier

	Storage *storage.QBFTStores
	Queues  map[spectypes.RunnerRole]queueContainer

	// dutyIDs is a map for logging a unique ID for a given duty
	dutyIDs *hashmap.Map[spectypes.RunnerRole, string]

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
		Operator:          options.Operator,
		Share:             options.SSVShare,
		Signer:            options.Signer,
		OperatorSigner:    options.OperatorSigner,
		SignatureVerifier: options.SignatureVerifier,
		Queues:            make(map[spectypes.RunnerRole]queueContainer),
		state:             uint32(NotStarted),
		dutyIDs:           hashmap.New[spectypes.RunnerRole, string](), // TODO: use beaconrole here?
		messageValidator:  options.MessageValidator,
	}

	for _, dutyRunner := range options.DutyRunners {
		// Set timeout function.
		dutyRunner.GetBaseRunner().TimeoutF = v.onTimeout

		//Setup the queue.
		role := dutyRunner.GetBaseRunner().RunnerRoleType

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
func (v *Validator) StartDuty(logger *zap.Logger, iduty spectypes.Duty) error {

	duty := iduty.(*spectypes.BeaconDuty) // TODO: err handling

	dutyRunner := v.DutyRunners[spectypes.MapDutyToRunnerRole(duty.Type)]
	if dutyRunner == nil {
		return errors.Errorf("no runner for duty type %s", duty.Type.String())
	}

	// Log with duty ID.
	baseRunner := dutyRunner.GetBaseRunner()
	v.dutyIDs.Set(spectypes.MapDutyToRunnerRole(duty.Type), fields.FormatDutyID(baseRunner.BeaconNetwork.EstimatedEpochAtSlot(duty.Slot), duty))
	logger = trySetDutyID(logger, v.dutyIDs, spectypes.MapDutyToRunnerRole(duty.Type))

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
		if err := v.SignatureVerifier.Verify(msg.SignedSSVMessage, v.Operator.Committee); err != nil {
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
		logger = trySetDutyID(logger, v.dutyIDs, messageID.GetRoleType())

		qbftMsg, ok := msg.Body.(*specqbft.Message)
		if !ok {
			return errors.New("could not decode consensus message from network message")
		}

		// Check signer consistency
		if !msg.SignedSSVMessage.CommonSigners([]spectypes.OperatorID{msg.SignedSSVMessage.OperatorIDs[0]}) { // todo: array check
			return errors.New("SignedSSVMessage's signer not consistent with SignedMessage's signers")
		}

		logger = logger.With(fields.Height(qbftMsg.Height))
		// Process
		return dutyRunner.ProcessConsensus(logger, msg.SignedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		logger = trySetDutyID(logger, v.dutyIDs, messageID.GetRoleType())

		signedMsg, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok {
			return errors.New("could not decode post consensus message from network message")
		}

		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return errors.New("PartialSignatureMessage has more than 1 signer")
		}

		if err := signedMsg.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return errors.Wrap(err, "invalid PartialSignatureMessages")
		}
		// Check signer consistency
		if signedMsg.Messages[0].Signer != msg.SignedSSVMessage.OperatorIDs[0] {
			return errors.New("SignedSSVMessage's signer not consistent with SignedPartialSignatureMessage's signer")
		}

		if signedMsg.Type == spectypes.PostConsensusPartialSig {
			return dutyRunner.ProcessPostConsensus(logger, signedMsg)
		}
		return dutyRunner.ProcessPreConsensus(logger, signedMsg)
	case message.SSVEventMsgType:
		return v.handleEventMessage(logger, msg, dutyRunner)
	default:
		return errors.New("unknown msg")
	}
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

func trySetDutyID(logger *zap.Logger, dutyIDs *hashmap.Map[spectypes.RunnerRole, string], role spectypes.RunnerRole) *zap.Logger {
	if dutyID, ok := dutyIDs.Get(role); ok {
		return logger.With(fields.DutyID(dutyID))
	}
	return logger
}
