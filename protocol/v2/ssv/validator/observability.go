package validator

import (
	"go.opentelemetry.io/otel"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	observabilityNamespace = "ssv.validator"
)

var tracer = otel.Tracer(observabilityName)
