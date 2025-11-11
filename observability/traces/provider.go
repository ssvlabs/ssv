package traces

import (
	"context"
	"fmt"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func InitializeProvider(ctx context.Context, resources *resource.Resource, isEnabled bool) (trace.TracerProvider, func(context.Context) error, error) {
	if !isEnabled {
		noopShutdown := func(ctx context.Context) error {
			return nil
		}
		return noop.NewTracerProvider(), noopShutdown, nil
	}

	autoExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate Trace Auto exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resources),
		sdktrace.WithBatcher(newFilteringExporter(autoExporter)),
	)

	return provider, autoExporter.Shutdown, nil
}

type filteringExporter struct {
	delegate sdktrace.SpanExporter
}

func newFilteringExporter(delegate sdktrace.SpanExporter) *filteringExporter {
	return &filteringExporter{
		delegate: delegate,
	}
}

func (fe *filteringExporter) ExportSpans(ctx context.Context, rs []sdktrace.ReadOnlySpan) error {
	hasBoolAttr := func(attrs []attribute.KeyValue, key string) bool {
		for _, kv := range attrs {
			if string(kv.Key) == key && kv.Value.Type() == attribute.BOOL && kv.Value.AsBool() {
				return true
			}
		}
		return false
	}

	result := make([]sdktrace.ReadOnlySpan, 0, len(rs))
	for _, s := range rs {
		if hasBoolAttr(s.Attributes(), droppableSpan) {
			continue // drop this span
		}
		result = append(result, s)
	}

	if len(result) == 0 {
		return nil
	}

	return fe.delegate.ExportSpans(ctx, result)
}

func (fe *filteringExporter) Shutdown(ctx context.Context) error {
	return fe.delegate.Shutdown(ctx)
}
