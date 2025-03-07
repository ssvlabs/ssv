package observability

type (
	tracesConfig struct {
		enabled,
		insecureEndpoint bool
		exporterEndpoint string
	}

	metricsConfig struct {
		enabled bool
	}

	Config struct {
		traces  tracesConfig
		metrics metricsConfig
	}
)
