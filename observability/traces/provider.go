package traces

import (
	"context"
	"fmt"
	"time"

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
		noOpShutdown := func(ctx context.Context) error {
			return nil
		}
		return noop.NewTracerProvider(), span.NewNoOpSpanProcessor(), noOpShutdown, nil
	}

	spanExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to instantiate Trace Auto exporter: %w", err)
	}

	spanDropProcessor := span.NewSubtreeDropProcessor(sdktrace.NewBatchSpanProcessor(spanExporter))

	// batchSize must be large enough to accommodate a full trace for a spanDropProcessor to be able to work
	// correctly, we've observed that some traces can contain up to 10k spans in them - we must set it to a
	// higher value than that. Note, spanDropProcessor will drop some of those spans to keep the amount of
	// data emitted manageable.
	const batchSize = 65536
	// batchTimeout is similar to batchSize, but "limits" how long a trace can be in terms of a duration. For
	// spanDropProcessor to work correctly, it needs to be longer than the longest trace we have.
	const batchTimeout = 120 * time.Second

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resources),
		sdktrace.WithSpanProcessor(spanDropProcessor),
		sdktrace.WithBatcher(
			span.NewSubtreeDropExporter(spanExporter, spanDropProcessor),
			sdktrace.WithMaxExportBatchSize(batchSize),
			sdktrace.WithBatchTimeout(batchTimeout),
		),
	)

	return provider, spanDropProcessor, spanExporter.Shutdown, nil
}
