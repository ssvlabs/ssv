package controller

import (
	"go.opentelemetry.io/otel"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	observabilityNamespace = "ssv.validator"
)

var (
	tracer = otel.Tracer(observabilityName)
)
