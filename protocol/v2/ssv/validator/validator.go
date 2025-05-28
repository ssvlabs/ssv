package validator

import (
	"context"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
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

	NetworkConfig networkconfig.Network
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
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "start_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())),
	)
	defer span.End()

	vDuty, ok := duty.(*spectypes.ValidatorDuty)
	if !ok {
		err := fmt.Errorf("expected ValidatorDuty, got %T", duty)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	dutyRunner := v.DutyRunners[spectypes.MapDutyToRunnerRole(vDuty.Type)]
	if dutyRunner == nil {
		err := errors.Errorf("no runner for duty type %s", vDuty.Type.String())
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Log with duty ID.
	baseRunner := dutyRunner.GetBaseRunner()
	v.dutyIDs.Set(spectypes.MapDutyToRunnerRole(vDuty.Type), fields.FormatDutyID(baseRunner.NetworkConfig.EstimatedEpochAtSlot(vDuty.Slot), vDuty.Slot, vDuty.Type, vDuty.ValidatorIndex))
	logger = v.withDutyID(logger, spectypes.MapDutyToRunnerRole(vDuty.Type))

	// Log with height.
	if baseRunner.QBFTController != nil {
		logger = logger.With(fields.Height(baseRunner.QBFTController.Height))
	}

	const eventMsg = "ℹ️ starting duty processing"
	logger.Info(eventMsg)
	span.AddEvent(eventMsg)

	if err := dutyRunner.StartNewDuty(ctx, logger, vDuty, v.Operator.GetQuorum()); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// ProcessMessage processes Network Message of all types
func (v *Validator) ProcessMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	traceCtx, dutyID := v.fetchTraceContext(ctx, msg.GetID())
	ctx, span := tracer.Start(traceCtx,
		observability.InstrumentName(observabilityNamespace, "process_message"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(msg.GetID().GetRoleType()),
			observability.DutyIDAttribute(dutyID)),
		trace.WithLinks(trace.LinkFromContext(msg.TraceContext)))
	defer span.End()

	msgType := msg.GetType()
	span.SetAttributes(observability.ValidatorMsgTypeAttribute(msgType))

	if msgType != message.SSVEventMsgType {
		span.AddEvent("validating message and signature")
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			err = errors.Wrap(err, "invalid SignedSSVMessage")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		// Verify SignedSSVMessage's signature
		if err := spectypes.Verify(msg.SignedSSVMessage, v.Operator.Committee); err != nil {
			err = errors.Wrap(err, "SignedSSVMessage has an invalid signature")
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}

	messageID := msg.GetID()
	span.SetAttributes(observability.ValidatorMsgIDAttribute(messageID))
	// Get runner
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(messageID)
	if dutyRunner == nil {
		err := fmt.Errorf("could not get duty runner for msg ID %v", messageID)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Validate message for runner
	if err := validateMessage(v.Share.Share, msg); err != nil {
		err := fmt.Errorf("message invalid for msg ID %v: %w", messageID, err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	switch msgType {
	case spectypes.SSVConsensusMsgType:
		qbftMsg, ok := msg.Body.(*specqbft.Message)
		if !ok {
			err := errors.New("could not decode consensus message from network message")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if err := qbftMsg.Validate(); err != nil {
			err := errors.Wrap(err, "invalid qbft Message")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if dutyID, ok := v.dutyIDs.Get(messageID.GetRoleType()); ok {
			span.SetAttributes(observability.DutyIDAttribute(dutyID))
			logger = logger.With(fields.DutyID(dutyID))
		}

		logger = logger.
			With(fields.Height(qbftMsg.Height)).
			With(fields.Slot(phase0.Slot(qbftMsg.Height)))

		if err := dutyRunner.ProcessConsensus(ctx, logger, msg.SignedSSVMessage); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case spectypes.SSVPartialSignatureMsgType:
		signedMsg, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok {
			err := errors.New("could not decode post consensus message from network message")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if dutyID, ok := v.dutyIDs.Get(messageID.GetRoleType()); ok {
			span.SetAttributes(observability.DutyIDAttribute(dutyID))
			logger = logger.With(fields.DutyID(dutyID))
		}
		span.SetAttributes(observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type))
		logger = logger.With(fields.Slot(signedMsg.Slot))

		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			err := errors.New("PartialSignatureMessage has more than 1 signer")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if err := signedMsg.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			err := errors.Wrap(err, "invalid PartialSignatureMessages")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if signedMsg.Type == spectypes.PostConsensusPartialSig {
			span.AddEvent("processing post-consensus message")
			if err := dutyRunner.ProcessPostConsensus(ctx, logger, signedMsg); err != nil {
				span.SetStatus(codes.Error, err.Error())
				return err
			}
			span.SetStatus(codes.Ok, "")
			return nil
		}
		span.AddEvent("processing pre-consensus message")
		if err := dutyRunner.ProcessPreConsensus(ctx, logger, signedMsg); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case message.SSVEventMsgType:
		if err := v.handleEventMessage(ctx, logger, msg, dutyRunner); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		err := errors.New("unknown msg")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
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

// withDutyID returns a logger with the duty ID for the given role.
func (v *Validator) withDutyID(logger *zap.Logger, role spectypes.RunnerRole) *zap.Logger {
	if dutyID, ok := v.dutyIDs.Get(role); ok {
		return logger.With(fields.DutyID(dutyID))
	}

	return logger
}

func (v *Validator) fetchTraceContext(ctx context.Context, msgID spectypes.MessageID) (traceCtx context.Context, dutyID string) {
	if dutyID, ok := v.dutyIDs.Get(msgID.GetRoleType()); ok {
		return observability.TraceContext(ctx, dutyID), dutyID
	}
	return ctx, ""
}
