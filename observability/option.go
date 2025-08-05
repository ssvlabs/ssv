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
