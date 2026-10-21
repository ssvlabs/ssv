package controller

import (
	"go.opentelemetry.io/otel"
)

const (
	// Pre-/v2 path on purpose: exported as the otel_scope_name metric label.
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	observabilityNamespace = "ssv.validator"
)

var (
	tracer = otel.Tracer(observabilityName)
)
