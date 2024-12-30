package observability

import (
	"math"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
)

const (
	RunnerRoleAttrKey = "ssv.runner.role"
)

type Slot interface {
	qbft.Height | phase0.Slot
}

func BeaconRoleAttribute(role types.BeaconRole) attribute.KeyValue {
	return attribute.String("ssv.beacon.role", role.String())
}

func BeaconEpochAttribute(epoch phase0.Epoch) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.beacon.epoch",
		Value: Uint64AttributeValue(uint64(epoch)),
	}
}

func RunnerRoleAttribute(role types.RunnerRole) attribute.KeyValue {
	return attribute.String(RunnerRoleAttrKey, role.String())
}

func BeaconSlotAttribute[T Slot](slot T) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.beacon.slot",
		Value: Uint64AttributeValue(uint64(slot)),
	}
}

func DutyRoundAttribute(round qbft.Round) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.duty.round",
		Value: Uint64AttributeValue(uint64(round)),
	}
}

func DutyPeriodAttribute(period uint64) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.duty.period",
		Value: Uint64AttributeValue(period),
	}
}

func ValidatorMsgTypeAttribute(msgType types.MsgType) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.msg_type",
		Value: Uint64AttributeValue(uint64(msgType)),
	}
}

func ValidatorPartialSigMsgTypeAttribute(msgType types.PartialSigMsgType) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.partial_sig_msg_type",
		Value: Uint64AttributeValue(uint64(msgType)),
	}
}

func ValidatorIndexAttribute(index phase0.ValidatorIndex) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.index",
		Value: Uint64AttributeValue(uint64(index)),
	}
}

func ValidatorSignerAttribute(signer types.OperatorID) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.signer",
		Value: Uint64AttributeValue(signer),
	}
}

func NetworkDirectionAttribute(direction network.Direction) attribute.KeyValue {
	return attribute.String("ssv.p2p.connection.direction", strings.ToLower(direction.String()))
}

func Uint64AttributeValue(value uint64) attribute.Value {
	if value <= math.MaxInt64 {
		return attribute.Int64Value(int64(value))
	}
	return attribute.StringValue(strconv.FormatUint(value, 10))
}
