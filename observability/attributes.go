package observability

import (
	"math"
	"strconv"
	"strings"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
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

func RoundChangeReasonAttribute(reason string) attribute.KeyValue {
	return attribute.String("ssv.validator.duty.round.change_reason", reason)
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
