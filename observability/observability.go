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

func Initialize(ctx context.Context, appName, appVersion string, options ...Option) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	shutdown = func(ctx context.Context) error {
		var joinedErr error
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

	resources, err := buildResources(appName, appVersion)
	if err != nil {
		return shutdown, err
	}

	if config.metrics.enabled {
		promExporter, err := prometheus.New()
		if err != nil {
			return shutdown, fmt.Errorf("failed to instantiate Metric Prometheus exporter: %w", err)
		}
		meterProvider := metric.NewMeterProvider(
			metric.WithResource(resources),
			metric.WithReader(promExporter),
		)
		shutdownFuncs = append(shutdownFuncs, promExporter.Shutdown)
		otel.SetMeterProvider(meterProvider)
	} else {
		otel.SetMeterProvider(defaultMeterProvider)
	}

	if config.traces.enabled {
		autoExporter, err := autoexport.NewSpanExporter(ctx)
		if err != nil {
			return shutdown, fmt.Errorf("failed to instantiate Trace auto exporter: %w", err)
		}

		traceProvider := trace.NewTracerProvider(
			trace.WithResource(resources),
			trace.WithBatcher(autoExporter),
		)

		otel.SetTracerProvider(traceProvider)
		shutdownFuncs = append(shutdownFuncs, autoExporter.Shutdown)
	} else {
		otel.SetTracerProvider(defaultTracerProvider)
	}

	return shutdown, err
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
