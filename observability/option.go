package observability

type Option func(*Config)

func WithMetrics() Option {
	return func(cfg *Config) {
		cfg.metrics.enabled = true
	}
}

func WithTraces(exporterEndpoint string, insecureEndpoint bool) Option {
	return func(cfg *Config) {
		cfg.traces.enabled = true
		cfg.traces.exporterEndpoint = exporterEndpoint
		cfg.traces.insecureEndpoint = insecureEndpoint
	}
}
