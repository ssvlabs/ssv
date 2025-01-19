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

	resources, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(appName),
		semconv.ServiceVersion(appVersion),
	))
	if err != nil {
		err = errors.Join(errors.New("failed to instantiate observability resources"), err)
		return shutdown, err
	}

	if config.metrics.enabled {
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
		shutdownFuncs = append(shutdownFuncs, promExporter.Shutdown)
	}

	if config.traces.enabled {
		if config.traces.exporterEndpoint == "" {
			return shutdown, errors.New("traces configuration: the exporter endpoint was an empty string")
		}

		options := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(config.traces.exporterEndpoint),
		}
		if config.traces.insecureEndpoint {
			options = append(options, otlptracegrpc.WithInsecure())
		}

		gRPCExporter, err := otlptracegrpc.New(ctx, options...)

		if err != nil {
			err = errors.Join(errors.New("failed to instantiate traces gRPC exporter"), err)
			return shutdown, err
		}

		traceProvider := trace.NewTracerProvider(
			trace.WithResource(resources),
			trace.WithBatcher(gRPCExporter, trace.WithBatchTimeout(time.Second)),
		)
		otel.SetTracerProvider(traceProvider)
		shutdownFuncs = append(shutdownFuncs, gRPCExporter.Shutdown)
	}

	return shutdown, err
}
