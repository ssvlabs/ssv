package observability

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
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
		gRPCExporter, err := otlptracegrpc.New(context.TODO(),
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint("stage-alloy.alloy.svc:4317"))
		if err != nil {
			err = errors.Join(errors.New("failed to instantiate traces gRPC exporter"), err)
			return shutdown, err
		}

		traceProvider := trace.NewTracerProvider(
			trace.WithResource(resources),
			trace.WithBatcher(gRPCExporter, trace.WithBatchTimeout(time.Second)),
		)
		shutdown = gRPCExporter.Shutdown
		otel.SetTracerProvider(traceProvider)
	}

	return shutdown, err
}
