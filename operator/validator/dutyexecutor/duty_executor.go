package dutyexecutor

import (
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

type DutyExecutor interface {
	ExecuteDuty(duty *spectypes.Duty)
}

type dutyExecutor struct {
	logger        *zap.Logger
	networkConfig networkconfig.NetworkConfig
	validatorsMap *validatorsmap.ValidatorsMap
}

func New(logger *zap.Logger, networkConfig networkconfig.NetworkConfig, validatorsMap *validatorsmap.ValidatorsMap) DutyExecutor {
	return &dutyExecutor{
		logger:        logger,
		networkConfig: networkConfig,
		validatorsMap: validatorsMap,
	}
}

func (de *dutyExecutor) ExecuteDuty(duty *spectypes.Duty) {
	logger := de.loggerWithDutyContext(duty)

	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	var pk phase0.BLSPubKey
	copy(pk[:], duty.PubKey[:])

	v, ok := de.validatorsMap.GetValidator(pk[:])
	if !ok {
		logger.Warn("could not find validator", fields.PubKey(duty.PubKey[:]))
	}

	ssvMsg, err := createDutyExecuteMsg(duty, pk, de.networkConfig.Domain)
	if err != nil {
		logger.Error("could not create duty execute msg", zap.Error(err))
		return
	}
	dec, err := queue.DecodeSSVMessage(ssvMsg)
	if err != nil {
		logger.Error("could not decode duty execute msg", zap.Error(err))
		return
	}
	if pushed := v.Queues[duty.Type].Q.TryPush(dec); !pushed {
		logger.Warn("dropping ExecuteDuty message because the queue is full")
		return
	}
	// logger.Debug("📬 queue: pushed message", fields.MessageID(dec.MsgID), fields.MessageType(dec.MsgType))
}

// createDutyExecuteMsg returns ssvMsg with event type of duty execute
func createDutyExecuteMsg(duty *spectypes.Duty, pubKey phase0.BLSPubKey, domain spectypes.DomainType) (*spectypes.SSVMessage, error) {
	executeDutyData := types.ExecuteDutyData{Duty: duty}
	edd, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute duty data: %w", err)
	}

	msg := types.EventMsg{
		Type: types.ExecuteDuty,
		Data: edd,
	}

	data, err := msg.Encode()
	if err != nil {
		return nil, fmt.Errorf("failed to encode event msg: %w", err)
	}

	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   spectypes.NewMsgID(domain, pubKey[:], duty.Type),
		Data:    data,
	}, nil
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (de *dutyExecutor) loggerWithDutyContext(duty *spectypes.Duty) *zap.Logger {
	return de.logger.
		With(fields.Role(duty.Type)).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(fields.CurrentSlot(de.networkConfig.Beacon.EstimatedCurrentSlot())).
		With(fields.Slot(duty.Slot)).
		With(fields.Epoch(de.networkConfig.Beacon.EstimatedEpochAtSlot(duty.Slot))).
		With(fields.PubKey(duty.PubKey[:])).
		With(fields.StartTimeUnixMilli(de.networkConfig.Beacon.GetSlotStartTime(duty.Slot)))
}
