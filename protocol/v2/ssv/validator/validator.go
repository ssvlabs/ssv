package validator

import (
	"context"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners, for each duty type.
type Validator struct {
	mtx    *sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	NetworkConfig networkconfig.NetworkConfig
	DutyRunners   runner.ValidatorDutyRunners
	Network       specqbft.Network

	Operator       *spectypes.CommitteeMember
	Share          *ssvtypes.SSVShare
	Signer         ekm.BeaconSigner
	OperatorSigner ssvtypes.OperatorSigner

	Queues map[spectypes.RunnerRole]queueContainer

	// dutyIDs is a map for logging a unique ID for a given duty
	dutyIDs *hashmap.Map[spectypes.RunnerRole, string]

	state uint32

	messageValidator validation.MessageValidator
}

// NewValidator creates a new instance of Validator.
func NewValidator(pctx context.Context, cancel func(), options Options) *Validator {
	options.defaults()

	v := &Validator{
		mtx:              &sync.RWMutex{},
		ctx:              pctx,
		cancel:           cancel,
		NetworkConfig:    options.NetworkConfig,
		DutyRunners:      options.DutyRunners,
		Network:          options.Network,
		Operator:         options.Operator,
		Share:            options.SSVShare,
		Signer:           options.Signer,
		OperatorSigner:   options.OperatorSigner,
		Queues:           make(map[spectypes.RunnerRole]queueContainer),
		state:            uint32(NotStarted),
		dutyIDs:          hashmap.New[spectypes.RunnerRole, string](), // TODO: use beaconrole here?
		messageValidator: options.MessageValidator,
	}

	for _, dutyRunner := range options.DutyRunners {
		// Set timeout function.
		dutyRunner.GetBaseRunner().TimeoutF = v.onTimeout

		//Setup the queue.
		role := dutyRunner.GetBaseRunner().RunnerRoleType

		v.Queues[role] = queueContainer{
			Q: queue.New(options.QueueSize),
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
func (v *Validator) StartDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	vDuty, ok := duty.(*spectypes.ValidatorDuty)
	if !ok {
		return fmt.Errorf("expected ValidatorDuty, got %T", duty)
	}

	dutyRunner := v.DutyRunners[spectypes.MapDutyToRunnerRole(vDuty.Type)]
	if dutyRunner == nil {
		return errors.Errorf("no runner for duty type %s", vDuty.Type.String())
	}

	// Log with duty ID.
	baseRunner := dutyRunner.GetBaseRunner()
	v.dutyIDs.Set(spectypes.MapDutyToRunnerRole(vDuty.Type), fields.FormatDutyID(baseRunner.BeaconNetwork.EstimatedEpochAtSlot(vDuty.Slot), vDuty.Slot, vDuty.Type.String(), vDuty.ValidatorIndex))
	logger = trySetDutyID(logger, v.dutyIDs, spectypes.MapDutyToRunnerRole(vDuty.Type))

	// Log with height.
	if baseRunner.QBFTController != nil {
		logger = logger.With(fields.Height(baseRunner.QBFTController.Height))
	}

	logger.Info("ℹ️ starting duty processing")

	return dutyRunner.StartNewDuty(ctx, logger, vDuty, v.Operator.GetQuorum())
}

// ProcessMessage processes Network Message of all types
func (v *Validator) ProcessMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	if msg.GetType() != message.SSVEventMsgType {
		// Validate message
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return errors.Wrap(err, "invalid SignedSSVMessage")
		}

		// Verify SignedSSVMessage's signature
		if err := spectypes.Verify(msg.SignedSSVMessage, v.Operator.Committee); err != nil {
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
		if err := qbftMsg.Validate(); err != nil {
			return errors.Wrap(err, "invalid qbft Message")
		}
		logger = v.loggerForDuty(logger, messageID.GetRoleType(), phase0.Slot(qbftMsg.Height))
		logger = logger.With(fields.Height(qbftMsg.Height))
		return dutyRunner.ProcessConsensus(ctx, logger, msg.SignedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		logger = trySetDutyID(logger, v.dutyIDs, messageID.GetRoleType())

		signedMsg, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok {
			return errors.New("could not decode post consensus message from network message")
		}
		logger = v.loggerForDuty(logger, messageID.GetRoleType(), signedMsg.Slot)

		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return errors.New("PartialSignatureMessage has more than 1 signer")
		}

		if err := signedMsg.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return errors.Wrap(err, "invalid PartialSignatureMessages")
		}

		if signedMsg.Type == spectypes.PostConsensusPartialSig {
			return dutyRunner.ProcessPostConsensus(ctx, logger, signedMsg)
		}
		return dutyRunner.ProcessPreConsensus(ctx, logger, signedMsg)
	case message.SSVEventMsgType:
		return v.handleEventMessage(ctx, logger, msg, dutyRunner)
	default:
		return errors.New("unknown msg")
	}
}

func (v *Validator) loggerForDuty(logger *zap.Logger, role spectypes.RunnerRole, slot phase0.Slot) *zap.Logger {
	logger = logger.With(fields.Slot(slot))
	if dutyID, ok := v.dutyIDs.Get(role); ok {
		return logger.With(fields.DutyID(dutyID))
	}
	return logger
}

func validateMessage(share spectypes.Share, msg *queue.SSVMessage) error {
	if !share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
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
