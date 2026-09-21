package goclient

import (
	"maps"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
)

// A client whose fork schedule differs from the node's only about a fork ahead of the chain is a lagging
// client, not a client on another network: the node warns, naming both schedules and the client, keeps its
// own schedule and keeps serving from the client. A disagreement about a fork already active still fails.
func TestApplyBeaconConfig_ForkScheduleLagWarns(t *testing.T) {
	withGloas := func(epoch phase0.Epoch) *networkconfig.Beacon {
		b := *networkconfig.TestNetwork.Beacon
		b.Forks = maps.Clone(b.Forks)
		delete(b.Forks, networkconfig.DataVersionGloas)
		if epoch != networkconfig.FarFutureEpoch {
			b.Forks[networkconfig.DataVersionGloas] = phase0.Fork{Epoch: epoch, CurrentVersion: phase0.Version{0x80}}
		}
		return &b
	}
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	ours := withGloas(now + 100)

	core, logs := observer.New(zapcore.WarnLevel)
	gc := &GoClient{log: zap.New(core), beaconConfig: ours, beaconConfigSource: "http://first:5052", beaconConfigInit: make(chan struct{})}

	got, err := gc.applyBeaconConfig("http://lagging:5052", withGloas(networkconfig.FarFutureEpoch))
	require.NoError(t, err, "a lagging client is tolerated")
	require.Same(t, ours, got, "the node keeps the schedule it started with")
	warnings := logs.FilterMessageSnippet("disagree about a future fork")
	require.Equal(t, 1, warnings.Len())
	fields := warnings.All()[0].ContextMap()
	require.Equal(t, "http://lagging:5052", fields["address"])
	require.Equal(t, "http://first:5052", fields["node_config_source"])
	require.Equal(t, "not scheduled", fields["client"])

	_, err = gc.applyBeaconConfig("http://other:5052", withGloas(now))
	require.ErrorContains(t, err, "beacon config misalign", "a fork active on one client and not the other is a different chain")
}
