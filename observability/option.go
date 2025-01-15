package observability

type (
	Option func(*Config)
)

func WithMetrics() Option {
	return func(cfg *Config) {
		cfg.metricsEnabled = true
	}
}
