package observability

type (
	tracesConfig struct {
		enabled bool
	}

	metricsConfig struct {
		enabled bool
	}

	Config struct {
		traces  tracesConfig
		metrics metricsConfig
	}
)
