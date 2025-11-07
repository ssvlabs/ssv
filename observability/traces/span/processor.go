package span

import (
	"go.opentelemetry.io/otel/trace"
)

type Processor interface {
	MarkDropSubtree(sc trace.SpanContext)
}
