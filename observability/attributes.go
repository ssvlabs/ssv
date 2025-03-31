package observability

import (
	"math"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
)

func RunnerRoleAttribute(role types.RunnerRole) attribute.KeyValue {
	const runnerRoleAttrKey = "ssv.runner.role"
	return attribute.String(runnerRoleAttrKey, role.String())
}

func BeaconRoleAttribute(role types.BeaconRole) attribute.KeyValue {
	const eventNameAttrName = "ssv.beacon.role"
	return attribute.String(eventNameAttrName, role.String())
}

func DutyRoundAttribute(round qbft.Round) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.validator.duty.round",
		Value: Uint64AttributeValue(uint64(round)),
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
