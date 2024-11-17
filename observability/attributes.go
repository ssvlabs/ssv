package observability

import (
	"go.opentelemetry.io/otel/attribute"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec/types"
)

type BeaconRole interface {
	types.BeaconRole | genesisspectypes.BeaconRole
	String() string
}

func BeaconRoleAttribute[T BeaconRole](role T) attribute.KeyValue {
	const eventNameAttrName = "ssv.beacon.role"
	return attribute.String(eventNameAttrName, role.String())
}
