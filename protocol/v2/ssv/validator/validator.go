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
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("got validator message to process")

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

	// Get runner
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(msgID)
	if dutyRunner == nil {
		return fmt.Errorf("could not get duty runner for msg ID %v", msgID)
	}

	// Validate message for runner
	if err := validateMessage(v.Share.Share, msg); err != nil {
		return fmt.Errorf("message invalid for msg ID %v: %w", msgID, err)
	}
	switch msgType {
	case spectypes.SSVConsensusMsgType:
		span.AddEvent("process validator message = consensus message")

		qbftMsg, ok := msg.Body.(*specqbft.Message)
		if !ok {
			return fmt.Errorf("could not decode consensus message from network message")
		}
		if err := qbftMsg.Validate(); err != nil {
			return fmt.Errorf("invalid QBFT Message: %w", err)
		}

		if err := dutyRunner.ProcessConsensus(ctx, logger, msg.SignedSSVMessage); err != nil {
			return fmt.Errorf("process consensus message: %w", err)
		}

		return nil
	case spectypes.SSVPartialSignatureMsgType:
		signedMsg, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok {
			return fmt.Errorf("could not decode post consensus message from network message")
		}

		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return fmt.Errorf("PartialSignatureMessage has more than 1 signer")
		}

		if err := signedMsg.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return fmt.Errorf("invalid PartialSignatureMessages: %w", err)
		}

		if signedMsg.Type == spectypes.PostConsensusPartialSig {
			span.AddEvent("process validator message = post-consensus message")
			if err := dutyRunner.ProcessPostConsensus(ctx, logger, signedMsg); err != nil {
				return fmt.Errorf("process post-consensus message: %w", err)
			}
			return nil
		}

		span.AddEvent("process validator message = pre-consensus message")
		if err := dutyRunner.ProcessPreConsensus(ctx, logger, signedMsg); err != nil {
			return fmt.Errorf("process pre-consensus message: %w", err)
		}

		return nil
	case message.SSVEventMsgType:
		eventMsg, ok := msg.Body.(*ssvtypes.EventMsg)
		if !ok {
			return fmt.Errorf("could not decode event message")
		}

		switch eventMsg.Type {
		case ssvtypes.Timeout:
			span.AddEvent("process validator message = event(timeout)")

			timeoutData, err := eventMsg.GetTimeoutData()
			if err != nil {
				return fmt.Errorf("get timeout data: %w", err)
			}

			if err := dutyRunner.OnTimeoutQBFT(ctx, logger, timeoutData); err != nil {
				return fmt.Errorf("timeout event: %w", err)
			}

			return nil
		case ssvtypes.ExecuteDuty:
			span.AddEvent("process validator message = event(execute duty)")

			if err := v.OnExecuteDuty(ctx, logger, eventMsg); err != nil {
				return fmt.Errorf("execute duty event: %w", err)
			}

			return nil
		default:
			return fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
		}
	default:
		return fmt.Errorf("unknown message type %d", msgType)
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
