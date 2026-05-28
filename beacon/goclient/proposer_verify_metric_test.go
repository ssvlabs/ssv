package goclient

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"
)

// TestVerifyProposalParent_EmitsLabeledMetric verifies that each branch of
// verifyProposalParent fires the expected counter carrying the ssv.beacon.client attribute.
//
// All branches are exercised in one Test (with subtests) on purpose. OTel Go's global
// meter provider re-binds package-level instruments only on the first SetMeterProvider
// call after package init; subsequent provider swaps leave already-bound instruments
// pointed at the original target. Sharing a single provider across the subtests sidesteps
// that quirk. The provider is restored on cleanup so other tests in the package are
// unaffected.
func TestVerifyProposalParent_EmitsLabeledMetric(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(t.Context())
	})

	// Each subtest collects the cumulative counter values seen so far and asserts the
	// delta introduced by its own verifyProposalParent call.
	type counterDelta struct {
		name   string
		before int64
		want   int64
	}

	collect := func() map[string]int64 {
		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(t.Context(), &rm))
		out := make(map[string]int64)
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					continue
				}
				// Sum across all data points with the testBeaconClientAddr label.
				for _, dp := range sum.DataPoints {
					v, ok := dp.Attributes.Value("ssv.beacon.client")
					if ok && v.AsString() == testBeaconClientAddr {
						out[m.Name] += dp.Value
					}
				}
			}
		}
		return out
	}

	newGC := func() *GoClient {
		return &GoClient{
			log:       zap.NewNop(),
			headCache: ttlcache.New[phase0.Slot, phase0.Root](),
		}
	}

	assertDeltas := func(t *testing.T, before map[string]int64, after map[string]int64, deltas []counterDelta) {
		t.Helper()
		for _, d := range deltas {
			got := after[d.name] - before[d.name]
			assert.Equal(t, d.want, got,
				"counter %q delta: want +%d, got +%d (before=%d, after=%d)",
				d.name, d.want, got, before[d.name], after[d.name])
		}
	}

	t.Run("cache miss labels verify and cache_miss counters", func(t *testing.T) {
		before := collect()
		gc := newGC()
		proposal := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)
		proposal.Electra.Block.Slot = 100

		gc.verifyProposalParent(t.Context(), gc.log, proposal.Electra.Block.Slot, proposal, testBeaconClientAddr)

		after := collect()
		assertDeltas(t, before, after, []counterDelta{
			{name: "ssv.cl.proposal.parent_verify", want: 1},
			{name: "ssv.cl.proposal.parent_cache_miss", want: 1},
		})
	})

	t.Run("match labels verify and match counters", func(t *testing.T) {
		before := collect()
		gc := newGC()
		proposal := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)
		proposal.Electra.Block.Slot = 100
		gc.headCache.Set(proposal.Electra.Block.Slot-1, proposal.Electra.Block.ParentRoot, ttlcache.NoTTL)

		gc.verifyProposalParent(t.Context(), gc.log, proposal.Electra.Block.Slot, proposal, testBeaconClientAddr)

		after := collect()
		assertDeltas(t, before, after, []counterDelta{
			{name: "ssv.cl.proposal.parent_verify", want: 1},
			{name: "ssv.cl.proposal.parent_match", want: 1},
		})
	})

	t.Run("mismatch labels verify and mismatch counters", func(t *testing.T) {
		before := collect()
		gc := newGC()
		proposal := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)
		proposal.Electra.Block.Slot = 100
		cachedRoot := phase0.Root{0xAA}
		require.NotEqual(t, cachedRoot, proposal.Electra.Block.ParentRoot)
		gc.headCache.Set(proposal.Electra.Block.Slot-1, cachedRoot, ttlcache.NoTTL)

		gc.verifyProposalParent(t.Context(), gc.log, proposal.Electra.Block.Slot, proposal, testBeaconClientAddr)

		after := collect()
		assertDeltas(t, before, after, []counterDelta{
			{name: "ssv.cl.proposal.parent_verify", want: 1},
			{name: "ssv.cl.proposal.parent_mismatch", want: 1},
		})
	})
}
