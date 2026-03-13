package validator

import (
	"go.opentelemetry.io/otel"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/v2/protocol/v2/ssv/validator"
	observabilityNamespace = "ssv.validator"
)

var tracer = otel.Tracer(observabilityName)
