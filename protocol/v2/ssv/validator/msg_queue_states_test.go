package validator

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
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

// A message dropped as stale has its span ended (with a terminal status) and its state removed right
// away, instead of being left for the TTL to evict with no terminal status. A message with no tracked
// state is a no-op.
func TestEndStaleMessageState(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	msgID := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, make([]byte, 48), spectypes.RoleProposer)
	msg := partialSigMsg(t, msgID, phase0.Slot(5))
	key, err := mKey(msg)
	require.NoError(t, err)

	states := newMessageStates(time.Minute)

	// A tracked message: its span is ended and exported with an error status, and the state is removed.
	_, span := provider.Tracer("test").Start(context.Background(), "message")
	states.Set(key, &messageProcessingState{span: span}, ttlcache.DefaultTTL)

	endStaleMessageState(states, zap.NewNop(), msg)

	require.Nil(t, states.Get(key), "the state is removed")
	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "the span is ended and exported")
	require.Equal(t, codes.Error, spans[0].Status.Code)

	// An untracked message: no state to close out, so it is a no-op — no panic, nothing more exported.
	require.NotPanics(t, func() {
		endStaleMessageState(states, zap.NewNop(), partialSigMsg(t, msgID, phase0.Slot(6)))
	})
	require.Len(t, exporter.GetSpans(), 1, "no extra span is exported for an untracked message")
}
