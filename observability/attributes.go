package observability

import (
	"go.opentelemetry.io/otel/attribute"

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
