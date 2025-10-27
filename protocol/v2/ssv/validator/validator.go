package validator

import (
	"context"
	"fmt"
	"sync"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

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
)

// Validator represents an SSV ETH consensus validator Share assigned, coordinates duty execution and more.
// Every validator has a validatorID which is validator's public key.
// Each validator has multiple DutyRunners - one per duty-type.
type Validator struct {
	logger *zap.Logger

	// mtx ensures the consistent Validator lifecycle (the correct usage of Start and Stop methods),
	// as well as syncs access to validator-managed data (such as Queues) across go-routines.
	mtx sync.RWMutex

	// Started reflects whether this validator has already been started. Once the Validator has been stopped, it
	// cannot be restarted.
	started bool
	// Stopped reflects whether this validator has already been stopped.
	stopped bool

	ctx    context.Context
	cancel context.CancelFunc

	NetworkConfig *networkconfig.Network
	Network       specqbft.Network

	Operator       *spectypes.CommitteeMember
	Share          *ssvtypes.SSVShare
	Signer         ekm.BeaconSigner
	OperatorSigner ssvtypes.OperatorSigner

	Queues map[spectypes.RunnerRole]queue.Queue

	DutyRunners runner.ValidatorDutyRunners

	messageValidator validation.MessageValidator
}

// NewValidator creates a new instance of Validator.
func NewValidator(ctx context.Context, cancel func(), logger *zap.Logger, options *Options) *Validator {
	v := &Validator{
		logger:           logger.Named(log.NameValidator).With(fields.PubKey(options.SSVShare.ValidatorPubKey[:])),
		ctx:              ctx,
		cancel:           cancel,
		NetworkConfig:    options.NetworkConfig,
		DutyRunners:      options.DutyRunners,
		Network:          options.Network,
		Operator:         options.Operator,
		Share:            options.SSVShare,
		Signer:           options.Signer,
		OperatorSigner:   options.OperatorSigner,
		Queues:           make(map[spectypes.RunnerRole]queue.Queue),
		messageValidator: options.MessageValidator,
	}

	// some additional steps to prepare duty runners for handling duties
	for _, dutyRunner := range options.DutyRunners {
		dutyRunner.SetTimeoutFunc(v.onTimeout)
		v.Queues[dutyRunner.GetRole()] = queue.New(
			logger,
			options.QueueSize,
			queue.WithInboxSizeMetric(
				queue.InboxSizeMetric,
				queue.ValidatorQueueMetricType,
				queue.ValidatorMetricID(dutyRunner.GetRole()),
			),
		)
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

	const eventMsg = "ℹ️ starting duty processing"
	logger.Info(eventMsg)
	span.AddEvent(eventMsg)

	if err := dutyRunner.StartNewDuty(ctx, logger, vDuty, v.Operator.GetQuorum()); err != nil {
		return traces.Errorf(span, "could not start duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// ProcessMessage processes p2p message of all types
func (v *Validator) ProcessMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	msgType := msg.GetType()
	msgID := msg.GetID()

	// Validate message (+ verify SignedSSVMessage's signature)
	if msgType != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return fmt.Errorf("invalid SignedSSVMessage: %w", err)
		}
		if err := spectypes.Verify(msg.SignedSSVMessage, v.Operator.Committee); err != nil {
			return spectypes.WrapError(spectypes.SSVMessageHasInvalidSignatureErrorCode, fmt.Errorf("SignedSSVMessage has an invalid signature: %w", err))
		}
	}

	slot, err := msg.Slot()
	if err != nil {
		return fmt.Errorf("couldn't get message slot: %w", err)
	}
	dutyID := fields.BuildDutyID(v.NetworkConfig.EstimatedEpochAtSlot(slot), slot, msgID.GetRoleType(), v.Share.ValidatorIndex)

	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "process_message"),
		trace.WithAttributes(
			observability.ValidatorMsgTypeAttribute(msgType),
			observability.ValidatorMsgIDAttribute(msgID),
			observability.RunnerRoleAttribute(msgID.GetRoleType()),
			observability.BeaconSlotAttribute(slot),
			observability.DutyIDAttribute(dutyID),
		),
	)
	defer span.End()

	// Get runner
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(msgID)
	if dutyRunner == nil {
		return traces.Errorf(span, "could not get duty runner for msg ID %v", msgID)
	}

	// Validate message for runner
	if err := validateMessage(v.Share.Share, msg); err != nil {
		return traces.Errorf(span, "message invalid for msg ID %v: %w", msgID, err)
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

		span.SetAttributes(observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type))

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
		eventMsg, ok := msg.Body.(*ssvtypes.EventMsg)
		if !ok {
			return traces.Errorf(span, "could not decode event message")
		}

		span.SetAttributes(observability.ValidatorEventTypeAttribute(eventMsg.Type))

		switch eventMsg.Type {
		case ssvtypes.Timeout:
			timeoutData, err := eventMsg.GetTimeoutData()
			if err != nil {
				return traces.Errorf(span, "get timeout data: %w", err)
			}

			if err := dutyRunner.OnTimeoutQBFT(ctx, logger, timeoutData); err != nil {
				return traces.Errorf(span, "timeout event: %w", err)
			}

			span.SetStatus(codes.Ok, "")
			return nil
		case ssvtypes.ExecuteDuty:
			if err := v.OnExecuteDuty(ctx, logger, eventMsg); err != nil {
				return traces.Errorf(span, "execute duty event: %w", err)
			}

			span.SetStatus(codes.Ok, "")
			return nil
		default:
			return traces.Errorf(span, "unknown event msg - %s", eventMsg.Type.String())
		}
	default:
		return traces.Errorf(span, "unknown message type %d", msgType)
	}
}

func validateMessage(share spectypes.Share, msg *queue.SSVMessage) error {
	if !share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return fmt.Errorf("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return fmt.Errorf("msg data is invalid")
	}

	return nil
}
