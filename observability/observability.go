package observability

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var config Config

func Initialize(appName, appVersion string, options ...Option) (shutdown func(context.Context) error, err error) {
	shutdown = func(ctx context.Context) error { return nil }

	for _, option := range options {
		option(&config)
	}

	resources, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(appName),
		semconv.ServiceVersion(appVersion),
	))
	if err != nil {
		err = errors.Join(errors.New("failed to instantiate observability resources"), err)
		return shutdown, err
	}
	if config.metricsEnabled {
		promExporter, err := prometheus.New()
		if err != nil {
			err = errors.Join(errors.New("failed to instantiate metric Prometheus exporter"), err)
			return shutdown, err
		}
		meterProvider := metric.NewMeterProvider(
			metric.WithResource(resources),
			metric.WithReader(promExporter),
		)
		otel.SetMeterProvider(meterProvider)
		shutdown = meterProvider.Shutdown
	}
	if config.tracesEnabled {
		traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			err = errors.Join(errors.New("failed to instantiate traces stdout exporter"), err)
			return shutdown, err
		}

		traceProvider := trace.NewTracerProvider(
			trace.WithResource(resources),
			trace.WithBatcher(traceExporter, trace.WithBatchTimeout(time.Second)),
		)
		otel.SetTracerProvider(traceProvider)
		shutdown = traceProvider.Shutdown

	}

	return shutdown, err
}
