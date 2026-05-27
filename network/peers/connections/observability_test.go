package connections

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordConnectionHandshake(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previousProvider := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previousProvider)
		require.NoError(t, provider.Shutdown(t.Context()))
	})

	recordConnectionHandshake(t.Context(), network.DirOutbound, connectionHandshakeOutcomeFailure, connectionHandshakeReasonHandshakeError, 125*time.Millisecond)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	requireConnectionMetricSum(t, rm, "ssv.p2p.connections.handshakes", map[string]string{
		"ssv.p2p.connection.direction":      "outbound",
		connectionHandshakeOutcomeAttribute: connectionHandshakeOutcomeFailure,
		connectionHandshakeReasonAttribute:  connectionHandshakeReasonHandshakeError,
	})
	requireConnectionMetricHistogram(t, rm, "ssv.p2p.connections.handshake_duration", map[string]string{
		"ssv.p2p.connection.direction":      "outbound",
		connectionHandshakeOutcomeAttribute: connectionHandshakeOutcomeFailure,
	})
}

func requireConnectionMetricSum(t *testing.T, rm metricdata.ResourceMetrics, metricName string, attrs map[string]string) {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != metricName {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			require.EqualValues(t, 1, sum.DataPoints[0].Value)
			for key, expected := range attrs {
				value, ok := sum.DataPoints[0].Attributes.Value(attribute.Key(key))
				require.True(t, ok)
				require.Equal(t, expected, value.AsString())
			}
			return
		}
	}

	t.Fatalf("%s metric was not collected", metricName)
}

func requireConnectionMetricHistogram(t *testing.T, rm metricdata.ResourceMetrics, metricName string, attrs map[string]string) {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != metricName {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.Len(t, histogram.DataPoints, 1)
			require.EqualValues(t, 1, histogram.DataPoints[0].Count)
			for key, expected := range attrs {
				value, ok := histogram.DataPoints[0].Attributes.Value(attribute.Key(key))
				require.True(t, ok)
				require.Equal(t, expected, value.AsString())
			}
			return
		}
	}

	t.Fatalf("%s metric was not collected", metricName)
}
