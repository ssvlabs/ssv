package observability

import (
	"math"
	"strconv"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
)

const (
	RunnerRoleAttrKey = "ssv.runner.role"
)

func BeaconRoleAttribute(role types.BeaconRole) attribute.KeyValue {
	const eventNameAttrName = "ssv.beacon.role"
	return attribute.String(eventNameAttrName, role.String())
}

func RunnerRoleAttribute(role types.RunnerRole) attribute.KeyValue {
	return attribute.String(RunnerRoleAttrKey, role.String())
}

func DutyRoundAttribute(round qbft.Round) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.duty.round",
		Value: Uint64AttributeValue(uint64(round)),
	}
}

func Uint64AttributeValue(value uint64) attribute.Value {
	if value <= math.MaxInt64 {
		return attribute.Int64Value(int64(value))
	}
	return attribute.StringValue(strconv.FormatUint(value, 10))
}
