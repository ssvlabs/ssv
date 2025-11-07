package observability

import (
	"github.com/ssvlabs/ssv/observability/traces/span"
)

var (
	SpanProcessor span.Processor = span.NewNoOpSpanProcessor()
)
