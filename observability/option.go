package observability

type Option func(*Config)

func WithMetrics() Option {
	return func(cfg *Config) {
		cfg.metrics.enabled = true
	}
}

func WithTraces() Option {
	return func(cfg *Config) {
		cfg.traces.enabled = true
	}
}

func WithLogger() Option {
	return func(cfg *Config) {
		cfg.logger.enabled = true
	}
}
