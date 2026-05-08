package validator

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/v2/networkconfig"
	"github.com/ssvlabs/ssv/v2/observability/log"
	"github.com/ssvlabs/ssv/v2/protocol/v2/ssv/queue"
)

func TestRouter(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	logger := log.TestLogger(t)

	router := newMessageRouter(logger)

	expectedCount := 1000
	count := 0

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msg, ok := router.Receive(ctx)
			if !ok {
				return
			}
			require.NotNil(t, msg)
			count++
			if ctx.Err() != nil || count >= expectedCount {
				return
			}
		}
	}()

	for i := 0; i < expectedCount; i++ {
		msg := &queue.SSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.MsgType(i % 3),
				MsgID:   spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, []byte{1, 1, 1, 1, 1}, spectypes.RoleCommittee),
				Data:    []byte(fmt.Sprintf("data-%d", i)),
			},
		}

		router.Route(context.TODO(), msg)
		if i%2 == 0 {
			go router.Route(context.TODO(), msg)
		}
	}

	wg.Wait()

	require.Equal(t, count, expectedCount)
}

func TestRouter_DropsWhenContextCanceled(t *testing.T) {
	t.Parallel()

	reader, droppedCounter, bufferFillGauge := newRouterTestMetrics(t)
	router := newMessageRouterWithMetrics(log.TestLogger(t), droppedCounter, bufferFillGauge)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	msg := &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, []byte{1, 1, 1, 1, 1}, spectypes.RoleCommittee),
			Data:    []byte("data"),
		},
	}

	router.Route(ctx, msg)

	require.Zero(t, router.Len())
	requireRouterDropCounter(t, reader, routerDropReasonContextCanceled, 1)
}

func TestRouter_DropsWhenBufferFull(t *testing.T) {
	t.Parallel()

	reader, droppedCounter, bufferFillGauge := newRouterTestMetrics(t)
	router := newMessageRouterWithMetrics(log.TestLogger(t), droppedCounter, bufferFillGauge)

	msg := &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, []byte{1, 1, 1, 1, 1}, spectypes.RoleCommittee),
			Data:    []byte("data"),
		},
	}

	for range bufSize {
		router.ch <- msg
	}

	router.Route(context.Background(), msg)

	require.Equal(t, bufSize, router.Len())
	requireRouterDropCounter(t, reader, routerDropReasonBufferFull, 1)
}

func newRouterTestMetrics(t *testing.T) (*metric.ManualReader, otelmetric.Int64Counter, otelmetric.Int64Gauge) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	testMeter := provider.Meter("test")

	counter, err := testMeter.Int64Counter("test_router_dropped")
	require.NoError(t, err)

	gauge, err := testMeter.Int64Gauge("test_router_buffer_fill")
	require.NoError(t, err)

	return reader, counter, gauge
}

// requireRouterDropCounter asserts that the test counter recorded `expected`
// for the given drop reason attribute.
func requireRouterDropCounter(t *testing.T, reader *metric.ManualReader, reason string, expected int64) {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "test_router_dropped" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "expected counter, got %T", m.Data)
			for _, dp := range sum.DataPoints {
				v, ok := dp.Attributes.Value("ssv.validator.router.drop_reason")
				if !ok || v.AsString() != reason {
					continue
				}
				require.Equal(t, expected, dp.Value)
				return
			}
		}
	}
	t.Fatalf("no drop counter data point found for reason=%q", reason)
}
