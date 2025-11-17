package validator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (v *Validator) ExecuteDuty(ctx context.Context, logger *zap.Logger, duty *spectypes.ValidatorDuty) error {
	ssvMsg, err := createDutyExecuteMsg(duty, duty.PubKey, v.NetworkConfig.DomainType)
	if err != nil {
		return fmt.Errorf("create duty execute msg: %w", err)
	}
	dec, err := queue.DecodeSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("decode duty execute msg: %w", err)
	}

	if pushed := v.Queues[duty.RunnerRole()].TryPush(dec); !pushed {
		return fmt.Errorf("dropping ExecuteDuty message for validator %s because the queue is full", duty.PubKey.String())
	}

	return nil
}

func (v *Validator) OnExecuteDuty(ctx context.Context, logger *zap.Logger, msg *types.EventMsg) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "on_execute_duty"),
		trace.WithAttributes(
			observability.ValidatorEventTypeAttribute(msg.Type),
		))
	defer span.End()

	executeDutyData, err := msg.GetExecuteDutyData()
	if err != nil {
		return traces.Errorf(span, "failed to get execute duty data: %w", err)
	}
	duty := executeDutyData.Duty

	span.SetAttributes(
		observability.BeaconSlotAttribute(duty.Slot),
		observability.RunnerRoleAttribute(duty.RunnerRole()),
	)

	// force the validator to be started (subscribed to validator's topic and synced)
	span.AddEvent("start validator")
	if _, err := v.Start(); err != nil {
		return traces.Errorf(span, "could not start validator: %w", err)
	}

	span.AddEvent("start duty")
	if err := v.StartDuty(ctx, logger, duty); err != nil {
		return traces.Errorf(span, "could not start duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// createDutyExecuteMsg returns ssvMsg with event type of execute duty
func createDutyExecuteMsg(duty *spectypes.ValidatorDuty, pubKey phase0.BLSPubKey, domain spectypes.DomainType) (*spectypes.SSVMessage, error) {
	executeDutyData := types.ExecuteDutyData{Duty: duty}
	data, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute duty data: %w", err)
	}

	return dutyDataToSSVMsg(domain, pubKey[:], duty.RunnerRole(), data)
}

func dutyDataToSSVMsg(
	domain spectypes.DomainType,
	msgIdentifier []byte,
	runnerRole spectypes.RunnerRole,
	data []byte,
) (*spectypes.SSVMessage, error) {
	msg := types.EventMsg{
		Type: types.ExecuteDuty,
		Data: data,
	}
	msgData, err := msg.Encode()
	if err != nil {
		return nil, fmt.Errorf("failed to encode event msg: %w", err)
	}

	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   spectypes.NewMsgID(domain, msgIdentifier, runnerRole),
		Data:    msgData,
	}, nil
}
