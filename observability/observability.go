package observability

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	metric_noop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	trace_noop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

var (
	config Config

	defaultMeterProvider  = metric_noop.NewMeterProvider()
	defaultTracerProvider = trace_noop.NewTracerProvider()
)

func Initialize(ctx context.Context, appName, appVersion string, l *zap.Logger, options ...Option) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error
	logger := initLogger(l)

	shutdown = func(ctx context.Context) error {
		var joinedErr error
		logger.Info("shutting down observability stack")
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

	logger.Info("building OTel resources")
	resources, err := buildResources(appName, appVersion)
	if err != nil {
		logger.Error("could not build OTel resources", zap.Error(err))
		return shutdown, err
	}

	if config.metrics.enabled {
		logger.Info("metrics are enabled, setting up Prometheus exporter")
		promExporter, err := prometheus.New()
		if err != nil {
			logger.Error("could not instantiate Metrics Prometheus exporter", zap.Error(err))
			return shutdown, fmt.Errorf("failed to instantiate Metric Prometheus exporter: %w", err)
		}
		meterProvider := metric.NewMeterProvider(
			metric.WithResource(resources),
			metric.WithReader(promExporter),
		)
		shutdownFuncs = append(shutdownFuncs, promExporter.Shutdown)

		otel.SetMeterProvider(meterProvider)
	} else {
		logger.Info("metrics were disabled. Setting noop MeterProvider")
		otel.SetMeterProvider(defaultMeterProvider)
	}

	if config.traces.enabled {
		logger.Info("traces are enabled, setting up Auto exporter")
		autoExporter, err := autoexport.NewSpanExporter(ctx)
		if err != nil {
			logger.Error("could not instantiate Tracing Auto exporter", zap.Error(err))
			return shutdown, fmt.Errorf("failed to instantiate Trace auto exporter: %w", err)
		}

		traceProvider := trace.NewTracerProvider(
			trace.WithResource(resources),
			trace.WithBatcher(autoExporter),
		)

		otel.SetTracerProvider(traceProvider)
		shutdownFuncs = append(shutdownFuncs, autoExporter.Shutdown)
	} else {
		logger.Info("traces were disabled. Setting noop TracerProvider")
		otel.SetTracerProvider(defaultTracerProvider)
	}

	logger.Info("observability stack initialized")

	return shutdown, nil
}

func buildResources(appName, appVersion string) (*resource.Resource, error) {
	hostName, err := os.Hostname()
	if err != nil {
		const defaultHostname = "unknown"
		logger.Warn("fetching hostname returned an error. Setting hostname to default",
			zap.Error(err),
			zap.String("default_hostname", defaultHostname))
		hostName = defaultHostname
	}

	const errMsg = "failed to merge OTeL Resources"
	resources, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(appName),
		semconv.ServiceVersion(appVersion),
		semconv.HostName(hostName),
	))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errMsg, err)
	}

	resources, err = resource.Merge(resources, resource.Environment())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errMsg, err)
	}

	return resources, nil
}
