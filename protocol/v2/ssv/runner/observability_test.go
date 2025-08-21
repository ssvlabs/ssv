package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestEpochMetricRecorder(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	testMeter := provider.Meter("test")

	const gaugeName = "test_gauge"
	gauge, err := testMeter.Int64Gauge(gaugeName)
	assert.NoError(t, err)

	sut := EpochMetricRecorder{
		data:  make(map[types.BeaconRole]epochCounter),
		gauge: gauge,
	}

	t.Run("records gauge per epoch per role", func(t *testing.T) {
		epoch := phase0.Epoch(1)

		sut.Record(t.Context(), 10, epoch, types.BNRoleAggregator)
		sut.Record(t.Context(), 20, epoch, types.BNRoleAggregator)

		sut.Record(t.Context(), 20, epoch, types.BNRoleAttester)
		sut.Record(t.Context(), 40, epoch, types.BNRoleAttester)

		aggregatorCounter := sut.data[types.BNRoleAggregator]
		assert.Equal(t, uint32(30), aggregatorCounter.count)
		assert.Equal(t, epoch, aggregatorCounter.epoch)

		attesterCounter := sut.data[types.BNRoleAttester]
		assert.Equal(t, uint32(60), attesterCounter.count)
		assert.Equal(t, epoch, attesterCounter.epoch)

		nextEpoch := phase0.Epoch(2)
		sut.Record(t.Context(), 1, nextEpoch, types.BNRoleAttester)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(t.Context(), &rm)
		require.NoError(t, err)

		for _, scopeMetrics := range rm.ScopeMetrics {
			assert.Equal(t, 1, len(scopeMetrics.Metrics))
			metric := scopeMetrics.Metrics[0]

			require.Equal(t, gaugeName, metric.Name)

			gaugeData, ok := metric.Data.(metricdata.Gauge[int64])
			require.True(t, ok)

			assert.Equal(t, 2, len(gaugeData.DataPoints))

			for _, data := range gaugeData.DataPoints {
				val, exist := data.Attributes.Value("ssv.beacon.role")
				require.True(t, exist)

				if val.AsString() == types.BNRoleAggregator.String() {
					assert.Equal(t, int64(30), data.Value)
				} else if val.AsString() == types.BNRoleAttester.String() {
					assert.Equal(t, int64(60), data.Value)
				} else {
					assert.Fail(t, "could not find metric recording for roles")
				}
			}
		}
	})
}
