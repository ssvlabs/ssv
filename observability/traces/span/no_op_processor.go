package span

import (
	"go.opentelemetry.io/otel/trace"
)

type NoOpSpanProcessor struct{}

func NewNoOpSpanProcessor() *NoOpSpanProcessor {
	return &NoOpSpanProcessor{}
}

func (p *NoOpSpanProcessor) MarkDropSubtree(_ trace.SpanContext) {}
