package observability

// Option modifies the configuration and dependencies.
type Option func(*Config, *Dependencies)

// WithMetrics enables metrics in the config.
func WithMetrics() Option {
	return func(cfg *Config, _ *Dependencies) {
		cfg.metricsEnabled = true
	}
}

// WithDependencies sets custom dependencies.
func WithDependencies(deps Dependencies) Option {
	return func(_ *Config, d *Dependencies) {
		*d = deps
	}
}
