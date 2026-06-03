package streams

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordStreamError(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previousProvider := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previousProvider)
		require.NoError(t, provider.Shutdown(t.Context()))
	})

	recordStreamError(t.Context(), protocol.ID("/ssv/test"), streamOperationReadResponse, streamErrorReasonTimeout)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "ssv.p2p.stream.errors" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			require.EqualValues(t, 1, sum.DataPoints[0].Value)
			requireStreamMetricAttribute(t, sum.DataPoints[0].Attributes, "ssv.p2p.protocol.id", "/ssv/test")
			requireStreamMetricAttribute(t, sum.DataPoints[0].Attributes, streamOperationAttribute, streamOperationReadResponse)
			requireStreamMetricAttribute(t, sum.DataPoints[0].Attributes, streamErrorReasonAttribute, streamErrorReasonTimeout)
			return
		}
	}

	t.Fatal("stream error metric was not collected")
}

func requireStreamMetricAttribute(t *testing.T, set attribute.Set, key string, expected string) {
	t.Helper()

	value, ok := set.Value(attribute.Key(key))
	require.True(t, ok)
	require.Equal(t, expected, value.AsString())
}
