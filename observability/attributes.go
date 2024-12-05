package observability

import (
	"go.opentelemetry.io/otel/attribute"

	"github.com/ssvlabs/ssv-spec/types"
)

func BeaconRoleAttribute(role types.BeaconRole) attribute.KeyValue {
	const eventNameAttrName = "ssv.beacon.role"
	return attribute.String(eventNameAttrName, role.String())
}
