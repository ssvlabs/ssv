package observability

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/common/model"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/metrics"
	"github.com/ssvlabs/ssv/observability/traces"
)

func init() {
	// Force Prometheus to use legacy metric name validation scheme.
	//
	// Starting from github.com/prometheus/client_golang v1.21.1,
	// the default NameValidationScheme changed to UTF8Validation,
	// which allows non-traditional delimiters like dots (.) in metric names.
	// This change was adopted in OpenTelemetry-Go v1.36.0:
	// https://github.com/open-telemetry/opentelemetry-go/releases/tag/v1.36.0
	//
	// However, systems like Grafana Mimir currently do not support UTF-8 metric names
	// and expect underscores (_) as delimiters:
	// https://github.com/grafana/mimir/issues/10283
	//
	// Reverting to LegacyValidation ensures compatibility across the observability stack.
	// Suggestion: keep this until model.NameValidationScheme setting is deprecated
	model.NameValidationScheme = model.LegacyValidation // nolint: staticcheck
}

func Initialize(ctx context.Context, appName, appVersion string, options ...Option) (shutdown func(context.Context) error, err error) {
	var (
		localLogger   = zap.NewNop()
		config        Config
		shutdownFuncs []func(context.Context) error
	)

	shutdown = func(ctx context.Context) error {
		var joinedErr error
		localLogger.Info("shutting down observability stack")
		for _, f := range shutdownFuncs {
			if err := f(ctx); err != nil {
				joinedErr = errors.Join(joinedErr, err)
			}
		}
		return joinedErr
	}

	for _, option := range options {
		option(&config)
	}

	if config.logger.enabled {
		err = log.SetGlobal(
			config.logger.level,
			config.logger.levelFormat,
			config.logger.format,
			&log.LogFileOptions{
				FilePath:   config.logger.filePath,
				MaxSize:    config.logger.fileSize,
				MaxBackups: config.logger.fileBackups,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("could not setup global logger: %w", err)
		}

		localLogger = initLogger(zap.L())

		localLogger.Info("global logger initialized")
	}

	localLogger.Info("building OTel resources")
	resources, err := buildResources(appName, appVersion, localLogger)
	if err != nil {
		return nil, fmt.Errorf("could not build OTel resources: %w", err)
	}

	localLogger.
		With(zap.Bool("metrics_enabled", config.metrics.enabled)).
		Info("fetching Metrics provider")

	meterProvider, shutdownFnc, err := metrics.InitializeProvider(ctx, resources, config.traces.enabled)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate Meter provider: %w", err)
	}

	shutdownFuncs = append(shutdownFuncs, shutdownFnc)
	otel.SetMeterProvider(meterProvider)

	localLogger.
		With(zap.Bool("traces_enabled", config.traces.enabled)).
		Info("fetching Traces provider")

	traceProvider, shutdownFnc, err := traces.InitializeProvider(ctx, resources, config.traces.enabled)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate Traces provider: %w", err)
	}

	shutdownFuncs = append(shutdownFuncs, shutdownFnc)
	otel.SetTracerProvider(traceProvider)

	localLogger.Info("observability stack initialized")

	return shutdown, nil
}
