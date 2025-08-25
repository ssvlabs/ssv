package traces

import (
	"context"
	"fmt"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func InitializeProvider(ctx context.Context, resources *resource.Resource, isEnabled bool) (trace.TracerProvider, func(context.Context) error, error) {
	if !isEnabled {
		noopShutdown := func(ctx context.Context) error { return nil }
		return noop.NewTracerProvider(), noopShutdown, nil
	}

	autoExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate Trace Auto exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resources),
		sdktrace.WithBatcher(autoExporter),
	)

	return provider, autoExporter.Shutdown, nil
}
