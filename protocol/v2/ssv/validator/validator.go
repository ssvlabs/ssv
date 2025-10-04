package validator

import (
	"context"
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
