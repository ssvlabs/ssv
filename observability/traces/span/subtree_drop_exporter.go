package span

import (
	"context"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SubtreeDropExporter implements trace.SpanExporter interface.
// It allows us to make a decision on whether we want to drop some spans right before
// exporting spans to the server (which is the latest point in span-life-cycle).
type SubtreeDropExporter struct {
	delegate sdktrace.SpanExporter

	dropProcessor *SubtreeDropProcessor
}

func NewSubtreeDropExporter(delegate sdktrace.SpanExporter, dropProcessor *SubtreeDropProcessor) *SubtreeDropExporter {
	return &SubtreeDropExporter{
		delegate:      delegate,
		dropProcessor: dropProcessor,
	}
}

func (fe *SubtreeDropExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	result := make([]sdktrace.ReadOnlySpan, 0, len(spans))
	for _, s := range spans {
		if fe.dropProcessor.ShouldDrop(s) {
			continue // drop this span
		}
		result = append(result, s)
	}

	if len(result) == 0 {
		return nil
	}

	return fe.delegate.ExportSpans(ctx, result)
}

func (fe *SubtreeDropExporter) Shutdown(ctx context.Context) error {
	return fe.delegate.Shutdown(ctx)
}
