package observability

import (
	"math"

	"go.opentelemetry.io/otel/attribute"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
)

type Slot interface {
	qbft.Height | phase0.Slot
}

func BeaconRoleAttribute(role types.BeaconRole) attribute.KeyValue {
	return attribute.String("ssv.beacon.role", role.String())
}

func RunnerRoleAttribute(role types.RunnerRole) attribute.KeyValue {
	return attribute.String("ssv.runner.role", role.String())
}

func BeaconEpochAttribute(epoch phase0.Epoch) attribute.KeyValue {
	return uint64Attribute("ssv.beacon.epoch", uint64(epoch))
}

func BeaconSlotAttribute[T Slot](slot T) attribute.KeyValue {
	return uint64Attribute("ssv.beacon.slot", uint64(slot))
}

func uint64Attribute(name string, value uint64) attribute.KeyValue {
	var v int64
	if value <= math.MaxInt64 {
		v = int64(v)
	}
	return attribute.Int64(name, v)
}
