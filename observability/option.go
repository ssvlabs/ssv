package observability

import "go.opentelemetry.io/otel/attribute"

type Option func(*Config)

// WithResourceAttributes adds custom attributes to the OTel resource describing this
// process. Every metric and trace emitted by the SDK is automatically annotated with
// the resource attributes — useful for late-known process-global facts like operator_id,
// which is only available after the operator data store is initialized.
func WithResourceAttributes(attrs ...attribute.KeyValue) Option {
	return func(cfg *Config) {
		cfg.resourceAttrs = append(cfg.resourceAttrs, attrs...)
	}
}

// WithMetrics enables OpenTelemetry metrics collection for the application.
// When enabled, a Prometheus provider will be initialized.
// This means Prometheus will scrape metrics from a specific HTTP endpoint,
// so ensure the Prometheus HTTP handler is configured properly in the app.
func WithMetrics() Option {
	return func(cfg *Config) {
		cfg.metrics.enabled = true
	}
}

// WithTraces enables OpenTelemetry tracing for the application.
// When traces are enabled, the OTEL_EXPORTER_OTLP_TRACES_ENDPOINT environment variable must be set.
// Additional configuration can be provided through other OTEL_* environment variables
// as described in the official OpenTelemetry documentation.
func WithTraces() Option {
	return func(cfg *Config) {
		cfg.traces.enabled = true
	}
}
