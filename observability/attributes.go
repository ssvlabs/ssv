package observability

import (
	"go.opentelemetry.io/otel/attribute"

	"github.com/ssvlabs/ssv-spec/types"
)

func BeaconRoleAttribute(role types.BeaconRole) attribute.KeyValue {
	return attribute.String("ssv.beacon.role", role.String())
}

func RunnerRoleAttribute(role types.RunnerRole) attribute.KeyValue {
	return attribute.String("ssv.runner.role", role.String())
}
