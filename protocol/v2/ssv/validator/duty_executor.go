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
	isBooleFork := v.NetworkConfig.BooleForkAtSlot(duty.Slot)
	role := types.RunnerRoleForValidatorDuty(duty, isBooleFork)

	domain := v.NetworkConfig.DomainTypeAtSlot(duty.Slot)
	ssvMsg, err := createDutyExecuteMsg(duty, duty.PubKey, domain, role)
	if err != nil {
		return fmt.Errorf("create duty execute msg: %w", err)
	}
	dec, err := queue.DecodeSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("decode duty execute msg: %w", err)
	}

	// Queues only has entries for roles with runners; validators built post-fork have no legacy
	// Aggregator/SyncCommitteeContribution runners, so guard the lookup rather than panicking on
	// a nil queue (same as queue_validator.go and timer.go).
	q, ok := v.Queues[role]
	if !ok {
		return fmt.Errorf("no queue for role %s", types.RunnerRoleToString(role))
	}

	if pushed := q.TryPush(dec); !pushed {
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
		observability.RunnerRoleAttribute(types.RunnerRoleForValidatorDuty(duty, v.NetworkConfig.BooleForkAtSlot(duty.Slot))),
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
func createDutyExecuteMsg(
	duty *spectypes.ValidatorDuty,
	pubKey phase0.BLSPubKey,
	domain spectypes.DomainType,
	runnerRole spectypes.RunnerRole,
) (*spectypes.SSVMessage, error) {
	executeDutyData := types.ExecuteDutyData{Duty: duty}
	data, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute duty data: %w", err)
	}

	return dutyDataToSSVMsg(domain, spectypes.ValidatorPK(pubKey), runnerRole, data)
}

func dutyDataToSSVMsg(
	domain spectypes.DomainType,
	validatorPK spectypes.ValidatorPK,
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
		MsgID:   spectypes.NewValidatorMsgID(domain, validatorPK, runnerRole),
		Data:    msgData,
	}, nil
}
