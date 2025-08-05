package metrics

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	metric_sdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func InitializeProvider(ctx context.Context, resources *resource.Resource, isEnabled bool) (metric.MeterProvider, func(context.Context) error, error) {
	if !isEnabled {
		return noop.NewMeterProvider(), nil, nil
	}

	promExporter, err := prometheus.New()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate Metric Prometheus exporter: %w", err)
	}
	meterProvider := metric_sdk.NewMeterProvider(
		metric_sdk.WithResource(resources),
		metric_sdk.WithReader(promExporter),
	)

	return meterProvider, promExporter.Shutdown, nil
}
