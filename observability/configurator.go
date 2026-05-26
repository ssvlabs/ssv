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

// InitializeLogger configures the global zap logger. It must be called before Initialize,
// and is split from Initialize so the logger is available during early startup (before
// process-global facts like operator_id are known, which delays metric/trace provider
// initialization — see Initialize and WithResourceAttributes).
func InitializeLogger(level, levelFormat, format, filePath string, fileSize, fileBackups int) error {
	err := log.SetGlobal(
		level,
		levelFormat,
		format,
		&log.LogFileOptions{
			FilePath:   filePath,
			MaxSize:    fileSize,
			MaxBackups: fileBackups,
		},
	)
	if err != nil {
		return fmt.Errorf("could not setup global logger: %w", err)
	}
	// Propagate the configured logger to observability sub-packages so they can log.
	_ = initLogger(zap.L())
	return nil
}

// Initialize configures the OTel metric and trace providers. The global zap logger must
// already be set up (see InitializeLogger); this function intentionally does not touch the
// logger, so that callers can defer Initialize until late-known facts like operator_id are
// available to pass via WithResourceAttributes.
func Initialize(ctx context.Context, appName, appVersion string, options ...Option) (shutdown func(context.Context) error, err error) {
	var (
		config        Config
		shutdownFuncs []func(context.Context) error
	)

	for _, option := range options {
		option(&config)
	}

	// Derive a named logger for Initialize's own log messages. metrics/traces internal
	// loggers are propagated by InitializeLogger (which must be called first); we don't
	// repeat that here to avoid the surprising side effect of replacing them mid-startup.
	localLogger := zap.L().Named(log.NameObservability)

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

	localLogger.Info("building OTel resources")
	resources, err := buildResources(appName, appVersion, config.resourceAttrs, localLogger)
	if err != nil {
		return nil, fmt.Errorf("could not build OTel resources: %w", err)
	}

	localLogger.
		With(zap.Bool("metrics_enabled", config.metrics.enabled)).
		Info("fetching Metrics provider")

	meterProvider, shutdownFnc, err := metrics.InitializeProvider(ctx, resources, config.metrics.enabled)
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
