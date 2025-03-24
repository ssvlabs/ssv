package observability

import (
	"context"
	"errors"
	"go.opentelemetry.io/otel/exporters/prometheus"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func Initialize(appName, appVersion string, options ...Option) (shutdown func(context.Context) error, err error) {
	cfg := Config{}
	deps := defaultDependencies()

	for _, opt := range options {
		opt(&cfg, &deps)
	}

	shutdown = func(ctx context.Context) error { return nil }

	resources, err := deps.ResourceMerge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(appName),
		semconv.ServiceVersion(appVersion),
	))

	if err != nil {
		return shutdown, errors.Join(errors.New("failed to instantiate observability resources"), err)
	}

	if cfg.metricsEnabled {
		var promExporter *prometheus.Exporter
		promExporter, err = deps.PrometheusNew()

		if err != nil {
			return shutdown, errors.Join(errors.New("failed to instantiate metric Prometheus exporter"), err)
		}

		meterProvider := metric.NewMeterProvider(
			metric.WithResource(resources),
			metric.WithReader(promExporter),
		)
		otel.SetMeterProvider(meterProvider)
		shutdown = meterProvider.Shutdown
	}

	return shutdown, nil
}
