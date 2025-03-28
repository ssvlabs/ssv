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

// BeaconRoleAttribute creates a key-value attribute for the beacon role.
func BeaconRoleAttribute(role types.BeaconRole) attribute.KeyValue {
	const eventNameAttrName = "ssv.beacon.role"
	return attribute.String(eventNameAttrName, role.String())
}

// RunnerRoleAttribute creates a key-value attribute for the runner role.
// TODO: replace string with types.RunnerRole when compatible with MessageID.GetRoleType()
func RunnerRoleAttribute(role string) attribute.KeyValue {
	const runnerRoleAttrKey = "ssv.runner.role"
	return attribute.String(runnerRoleAttrKey, role)
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
