package observability

type (
	tracesConfig struct {
		enabled bool
	}

	metricsConfig struct {
		enabled bool
	}

	loggerConfig struct {
		enabled                              bool
		level, levelFormat, format, filePath string
		fileSize, fileBackups                int
	}

	Config struct {
		traces  tracesConfig
		metrics metricsConfig
		logger  loggerConfig
	}
)
