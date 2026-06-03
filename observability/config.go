package observability

import "go.opentelemetry.io/otel/attribute"

type (
	tracesConfig struct {
		enabled bool
	}

	metricsConfig struct {
		enabled bool
	}

	Config struct {
		traces        tracesConfig
		metrics       metricsConfig
		resourceAttrs []attribute.KeyValue
	}
)
