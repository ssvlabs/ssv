package validator

import (
	"context"
	"testing"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A message state that expires — its message retried and then parked in the queue past the TTL — ends
// its span on eviction, so the span is exported instead of leaking with the state.
func TestNewMessageStates_ExpiryEndsSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("test").Start(context.Background(), "message")

	states := newMessageStates(20 * time.Millisecond)
	go states.Start()
	defer states.Stop()
	states.Set("key", &messageProcessingState{ctx: ctx, span: span}, ttlcache.DefaultTTL)

	require.Eventually(t, func() bool { return len(exporter.GetSpans()) == 1 }, 2*time.Second, 5*time.Millisecond)
}
