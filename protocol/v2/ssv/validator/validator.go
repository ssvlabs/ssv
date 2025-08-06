package validator

import (
	"context"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
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
	logger *zap.Logger

	mtx *sync.RWMutex

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
func NewValidator(pctx context.Context, cancel func(), logger *zap.Logger, options *Options) *Validator {
	v := &Validator{
		logger:           logger.Named(log.NameValidator).With(fields.PubKey(options.SSVShare.ValidatorPubKey[:])),
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
		return traces.Errorf(span, "expected ValidatorDuty, got %T", duty)
	}

	dutyRunner := v.DutyRunners[spectypes.MapDutyToRunnerRole(vDuty.Type)]
	if dutyRunner == nil {
		return traces.Errorf(span, "no duty runner for role %s", vDuty.Type.String())
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
		return traces.Errorf(span, "could not start duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// ProcessMessage processes Network Message of all types
func (v *Validator) ProcessMessage(ctx context.Context, msg *queue.SSVMessage) error {
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

	logger := v.logger.With(zap.String("message_type", message.MsgTypeToString(msgType)))

	if msgType != message.SSVEventMsgType {
		span.AddEvent("validating message and signature")
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return traces.Errorf(span, "invalid SignedSSVMessage: %w", err)
		}

		// Verify SignedSSVMessage's signature
		if err := spectypes.Verify(msg.SignedSSVMessage, v.Operator.Committee); err != nil {
			return traces.Errorf(span, "SignedSSVMessage has an invalid signature: %w", err)
		}
	}

	messageID := msg.GetID()
	span.SetAttributes(observability.ValidatorMsgIDAttribute(messageID))
	// Get runner
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(messageID)
	if dutyRunner == nil {
		return traces.Errorf(span, "could not get duty runner for msg ID %v", messageID)
	}

	// Validate message for runner
	if err := validateMessage(v.Share.Share, msg); err != nil {
		return traces.Errorf(span, "message invalid for msg ID %v: %w", messageID, err)
	}
	switch msgType {
	case spectypes.SSVConsensusMsgType:
		qbftMsg, ok := msg.Body.(*specqbft.Message)
		if !ok {
			return traces.Errorf(span, "could not decode consensus message from network message")
		}

		if err := qbftMsg.Validate(); err != nil {
			return traces.Errorf(span, "invalid QBFT Message: %w", err)
		}

		if dutyID, ok := v.dutyIDs.Get(messageID.GetRoleType()); ok {
			span.SetAttributes(observability.DutyIDAttribute(dutyID))
			logger = logger.With(fields.DutyID(dutyID))
		}

		logger = logger.
			With(fields.Height(qbftMsg.Height)).
			With(fields.Slot(phase0.Slot(qbftMsg.Height)))

		if err := dutyRunner.ProcessConsensus(ctx, logger, msg.SignedSSVMessage); err != nil {
			return traces.Error(span, err)
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case spectypes.SSVPartialSignatureMsgType:
		signedMsg, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok {
			return traces.Errorf(span, "could not decode post consensus message from network message")
		}

		if dutyID, ok := v.dutyIDs.Get(messageID.GetRoleType()); ok {
			span.SetAttributes(observability.DutyIDAttribute(dutyID))
			logger = logger.With(fields.DutyID(dutyID))
		}
		span.SetAttributes(observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type))
		logger = logger.With(fields.Slot(signedMsg.Slot))

		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return traces.Errorf(span, "PartialSignatureMessage has more than 1 signer")
		}

		if err := signedMsg.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return traces.Errorf(span, "invalid PartialSignatureMessages: %w", err)
		}

		if signedMsg.Type == spectypes.PostConsensusPartialSig {
			span.AddEvent("processing post-consensus message")
			if err := dutyRunner.ProcessPostConsensus(ctx, logger, signedMsg); err != nil {
				return traces.Error(span, err)
			}
			span.SetStatus(codes.Ok, "")
			return nil
		}
		span.AddEvent("processing pre-consensus message")
		if err := dutyRunner.ProcessPreConsensus(ctx, logger, signedMsg); err != nil {
			return traces.Error(span, err)
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case message.SSVEventMsgType:
		if err := v.handleEventMessage(ctx, logger, msg, dutyRunner); err != nil {
			return traces.Errorf(span, "could not handle event message: %w", err)
		}
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		return traces.Errorf(span, "unknown message type %d", msgType)
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
		return traces.Context(ctx, dutyID), dutyID
	}
	return ctx, ""
}
