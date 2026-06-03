package topics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordPubsubMessageMetrics(t *testing.T) {
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
	recordPubsubMessageValidated(t.Context(), topic, "reject", 150*time.Millisecond)
	recordPubsubMessageValidated(t.Context(), topic, "reject", 250*time.Millisecond)
	recordPubsubMessageHandlerError(t.Context(), topic)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	requireMetricSum(t, rm, "ssv.p2p.pubsub.messages.received", 2, map[string]string{
		pubsubTopicAttributeKey: topic,
	})
	requireMetricSum(t, rm, "ssv.p2p.pubsub.messages.validated", 2, map[string]string{
		pubsubTopicAttributeKey:            topic,
		pubsubValidationResultAttributeKey: "reject",
	})
	requireMetricHistogram(t, rm, "ssv.p2p.pubsub.messages.validation_duration", 2, map[string]string{
		pubsubTopicAttributeKey:            topic,
		pubsubValidationResultAttributeKey: "reject",
	})
	requireMetricSum(t, rm, "ssv.p2p.pubsub.messages.handler_errors", 1, map[string]string{
		pubsubTopicAttributeKey: topic,
	})
}

func requireMetricSum(t *testing.T, rm metricdata.ResourceMetrics, metricName string, value int64, attrs map[string]string) {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != metricName {
				continue
			}

			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			require.EqualValues(t, value, sum.DataPoints[0].Value)
			requireMetricAttributes(t, sum.DataPoints[0].Attributes, attrs)
			return
		}
	}

	t.Fatalf("%s metric was not collected", metricName)
}

func requireMetricHistogram(t *testing.T, rm metricdata.ResourceMetrics, metricName string, count uint64, attrs map[string]string) {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != metricName {
				continue
			}

			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.Len(t, histogram.DataPoints, 1)
			require.EqualValues(t, count, histogram.DataPoints[0].Count)
			requireMetricAttributes(t, histogram.DataPoints[0].Attributes, attrs)
			return
		}
	}

	t.Fatalf("%s metric was not collected", metricName)
}

func requireMetricAttributes(t *testing.T, set attribute.Set, attrs map[string]string) {
	t.Helper()

	for key, expected := range attrs {
		value, ok := set.Value(attribute.Key(key))
		require.True(t, ok)
		require.Equal(t, expected, value.AsString())
	}
}
