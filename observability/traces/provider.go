package traces

import (
	"context"
	"fmt"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/ssvlabs/ssv/observability/traces/span"
)

func InitializeProvider(ctx context.Context, resources *resource.Resource, isEnabled bool) (
	trace.TracerProvider,
	span.Processor,
	func(context.Context) error,
	error,
) {
	if !isEnabled {
		noopShutdown := func(ctx context.Context) error {
			return nil
		}
		return noop.NewTracerProvider(), span.NewNoOpSpanProcessor(), noopShutdown, nil
	}

	spanExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to instantiate Trace Auto exporter: %w", err)
	}

	spanDropProcessor := span.NewSubtreeDropProcessor(sdktrace.NewBatchSpanProcessor(spanExporter))

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resources),
		sdktrace.WithSpanProcessor(spanDropProcessor),
		sdktrace.WithBatcher(spanExporter),
	)

	return provider, spanDropProcessor, spanExporter.Shutdown, nil
}
