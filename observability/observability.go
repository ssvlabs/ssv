package observability

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var (
	once   sync.Once
	config Config
)

func Initialize(appName, appVersion string, options ...Option) (shutdown func(context.Context) error, err error) {
	var (
		initError     error
		shutdownFuncs []func(context.Context) error
	)

	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	once.Do(func() {
		for _, option := range options {
			option(&config)
		}

		resources, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(appName),
			semconv.ServiceVersion(appVersion),
		))
		if err != nil {
			initError = errors.Join(errors.New("failed to instantiate observability resources"), err)
			return
		}

		if config.metricsEnabled {
			promExporter, err := prometheus.New()
			if err != nil {
				initError = errors.Join(errors.New("failed to instantiate metric Prometheus exporter"), err)
				return
			}
			meterProvider := metric.NewMeterProvider(
				metric.WithResource(resources),
				metric.WithReader(promExporter),
			)
			otel.SetMeterProvider(meterProvider)
			shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
		}

		if config.tracesEnabled {
			traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
			if err != nil {
				initError = errors.Join(errors.New("failed to instantiate traces stdout exporter"), err)
				return
			}

			traceProvider := trace.NewTracerProvider(
				trace.WithResource(resources),
				trace.WithBatcher(traceExporter, trace.WithBatchTimeout(time.Second)),
			)
			shutdownFuncs = append(shutdownFuncs, traceExporter.Shutdown)
			otel.SetTracerProvider(traceProvider)
		}
	})

	return shutdown, initError
}
