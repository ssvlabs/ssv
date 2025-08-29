package controller

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// OnTimeout is trigger upon timeout for the given height
func (c *Controller) OnTimeout(ctx context.Context, logger *zap.Logger, msg types.EventMsg) error {
	// TODO add validation
	ctx, span := tracer.Start(ctx, observability.InstrumentName(observabilityNamespace, "on_timeout"))
	defer span.End()

	timeoutData, err := msg.GetTimeoutData()
	if err != nil {
		return traces.Errorf(span, "failed to get timeout data: %w", err)
	}

	span.SetAttributes(
		observability.DutyRoundAttribute(timeoutData.Round),
		observability.BeaconSlotAttribute(phase0.Slot(timeoutData.Height)),
	)

	instance := c.StoredInstances.FindInstance(timeoutData.Height)
	if instance == nil {
		return traces.Errorf(span, "instance is nil")
	}

	if timeoutData.Round < instance.State.Round {
		const eventMsg = "timeout for old round"
		logger.Debug(eventMsg, zap.Uint64("timeout round", uint64(timeoutData.Round)), zap.Uint64("instance round", uint64(instance.State.Round)))
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}

	if decided, _ := instance.IsDecided(); decided {
		span.AddEvent("QBFT instance is already decided")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	if err := instance.UponRoundTimeout(ctx, logger); err != nil {
		return traces.Errorf(span, "failed to handle round timeout: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}
