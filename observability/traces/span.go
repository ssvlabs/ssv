package traces

import (
	"go.opentelemetry.io/otel/attribute"
)

const (
	droppableSpan = "span.droppable"
)

func DroppableSpan() attribute.KeyValue {
	return attribute.Bool(droppableSpan, true)
}
