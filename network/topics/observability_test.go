package topics

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordPubsubMessageReceived(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previousProvider := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previousProvider)
		require.NoError(t, provider.Shutdown(t.Context()))
	})

	const topic = "ssv.v2.42"
	recordPubsubMessageReceived(t.Context(), topic)
	recordPubsubMessageReceived(t.Context(), topic)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "ssv.p2p.pubsub.messages.received" {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			require.EqualValues(t, 2, sum.DataPoints[0].Value)

			topicAttr, ok := sum.DataPoints[0].Attributes.Value(pubsubTopicAttributeKey)
			require.True(t, ok)
			require.Equal(t, topic, topicAttr.AsString())
			return
		}
	}

	t.Fatal("pubsub received metric was not collected")
}
