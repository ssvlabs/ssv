package observability

type Option func(*Config)

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

// WithLogger configures the global application logger.
// It sets log level, format, output file settings, and enables the logger.
// If this option is not applied, a no-op logger will be used instead.
func WithLogger(level, levelFormat, format, filePath string, fileSize, fileBackups int) Option {
	return func(cfg *Config) {
		cfg.logger.enabled = true
		cfg.logger.level = level
		cfg.logger.levelFormat = levelFormat
		cfg.logger.format = format
		cfg.logger.filePath = filePath
		cfg.logger.fileSize = fileSize
		cfg.logger.fileBackups = fileBackups
	}
}
