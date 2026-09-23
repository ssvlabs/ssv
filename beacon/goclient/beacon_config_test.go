package goclient

import (
	"maps"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
	"github.com/ssvlabs/ssv/networkconfig"
)

// gloasScheduledAt is the test network's beacon config with Gloas scheduled at the epoch, or left out of
// the schedule for FarFutureEpoch.
func gloasScheduledAt(epoch phase0.Epoch) *networkconfig.Beacon {
	b := *networkconfig.TestNetwork.Beacon
	b.Forks = maps.Clone(b.Forks)
	delete(b.Forks, networkconfig.DataVersionGloas)
	if epoch != networkconfig.FarFutureEpoch {
		b.Forks[networkconfig.DataVersionGloas] = phase0.Fork{Epoch: epoch, CurrentVersion: phase0.Version{0x80}}
	}
	return &b
}

// newLagTestClient is a client that took its config from "first" and observes logs from Warn up; a Fatal log
// panics instead of exiting.
func newLagTestClient(node *networkconfig.Beacon) (*GoClient, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core, zap.WithFatalHook(zapcore.WriteThenPanic))
	return &GoClient{log: logger, beaconConfig: node, beaconConfigSource: "http://first:5052", beaconConfigInit: make(chan struct{})}, logs
}

// A client whose fork schedule differs from the node's only about a fork ahead of the chain is not a client
// on another network: the node keeps its own schedule, keeps serving from the client, and says which side
// has to move — the client, to be upgraded (a warning), or the node, which cannot adopt a fork while running
// and has to be restarted once every client schedules it (an error, and a stop close to the fork). A
// disagreement about a fork already active still fails.
func TestApplyBeaconConfig_ForkScheduleLag(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()

	t.Run("the client lags: warn to upgrade the client", func(t *testing.T) {
		ours := gloasScheduledAt(now + 100)
		gc, logs := newLagTestClient(ours)

		got, err := gc.applyBeaconConfig("http://lagging:5052", gloasScheduledAt(networkconfig.FarFutureEpoch))
		require.NoError(t, err)
		require.Same(t, ours, got, "the node keeps the schedule it started with")
		require.Zero(t, logs.FilterLevelExact(zapcore.ErrorLevel).Len())
		warnings := logs.FilterMessageSnippet("upgrade the client before that fork")
		require.Equal(t, 1, warnings.Len())
		fields := warnings.All()[0].ContextMap()
		require.Equal(t, "http://lagging:5052", fields["address"])
		require.Equal(t, "http://first:5052", fields["node_config_source"])
		require.Equal(t, "not scheduled", fields["client"])
	})

	t.Run("the node lags: error to restart the node", func(t *testing.T) {
		ours := gloasScheduledAt(networkconfig.FarFutureEpoch)
		gc, logs := newLagTestClient(ours)

		got, err := gc.applyBeaconConfig("http://upgraded:5052", gloasScheduledAt(now+100))
		require.NoError(t, err, "the node keeps serving from the client")
		require.Same(t, ours, got)
		require.Zero(t, logs.FilterLevelExact(zapcore.WarnLevel).Len())
		alarms := logs.FilterMessageSnippet("restart the node before that fork")
		require.Equal(t, 1, alarms.Len())
		require.Equal(t, zapcore.ErrorLevel, alarms.All()[0].Level)
		require.Contains(t, alarms.All()[0].Message, "the node's config source included", "a restart reads the schedule from that source")
		fields := alarms.All()[0].ContextMap()
		require.Equal(t, "http://upgraded:5052", fields["address"])
		require.Equal(t, "http://first:5052", fields["node_config_source"])
		require.Equal(t, "not scheduled", fields["node_config"])
		require.Contains(t, fields["client"], "scheduled at epoch")
	})

	// Crossing the fork on the old schedule would fail every duty, so close to it the node stops, to be
	// restarted onto the new schedule before the pre-fork windows open.
	t.Run("the node lags close to the fork: stop", func(t *testing.T) {
		gc, logs := newLagTestClient(gloasScheduledAt(networkconfig.FarFutureEpoch))

		require.Panics(t, func() {
			_, _ = gc.applyBeaconConfig("http://upgraded:5052", gloasScheduledAt(now+forkScheduleLagStopEpochs))
		})
		stops := logs.FilterLevelExact(zapcore.FatalLevel)
		require.Equal(t, 1, stops.Len())
		require.Contains(t, stops.All()[0].Message, "stopping, as the fork is too close")

		gc, logs = newLagTestClient(gloasScheduledAt(networkconfig.FarFutureEpoch))
		_, err := gc.applyBeaconConfig("http://upgraded:5052", gloasScheduledAt(now+forkScheduleLagStopEpochs+1))
		require.NoError(t, err, "an epoch further out, it's an error only")
		require.Equal(t, 1, logs.FilterLevelExact(zapcore.ErrorLevel).Len())
		require.Zero(t, logs.FilterLevelExact(zapcore.FatalLevel).Len())
	})

	t.Run("both schedule the fork, differently: error, naming both", func(t *testing.T) {
		gc, logs := newLagTestClient(gloasScheduledAt(now + 100))

		_, err := gc.applyBeaconConfig("http://other:5052", gloasScheduledAt(now+200))
		require.NoError(t, err)
		alarms := logs.FilterMessageSnippet("schedule a fork differently")
		require.Equal(t, 1, alarms.Len())
		require.Equal(t, zapcore.ErrorLevel, alarms.All()[0].Level)
		require.Contains(t, alarms.All()[0].Message, "the node's config source included")
	})

	t.Run("a fork active on one side only is a different chain", func(t *testing.T) {
		gc, _ := newLagTestClient(gloasScheduledAt(networkconfig.FarFutureEpoch))

		_, err := gc.applyBeaconConfig("http://other:5052", gloasScheduledAt(now))
		require.ErrorContains(t, err, "beacon config misalign")
	})
}

// The periodic recheck reports through the same path as the activation hook, and stays quiet for a client
// whose schedule still matches the node's.
func TestCheckForkSchedule(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	ours := gloasScheduledAt(networkconfig.FarFutureEpoch)
	gc, logs := newLagTestClient(ours)

	gc.checkForkSchedule("http://same:5052", gloasScheduledAt(networkconfig.FarFutureEpoch))
	require.Zero(t, logs.Len(), "an unchanged schedule reports nothing")

	gc.checkForkSchedule("http://upgraded:5052", gloasScheduledAt(now+100))
	require.Equal(t, 1, logs.FilterMessageSnippet("restart the node before that fork").Len(), "a fork the node started without is the node's to adopt")
	require.Panics(t, func() { gc.checkForkSchedule("http://upgraded:5052", gloasScheduledAt(now+1)) }, "and close to it the node stops")

	other := gloasScheduledAt(networkconfig.FarFutureEpoch)
	other.Name = "another network"
	gc.checkForkSchedule("http://other:5052", other)
	require.Equal(t, 1, logs.FilterMessageSnippet("no longer matches the node's").Len(), "anything but a fork-schedule lag is an error of its own")
}

// Through a real client against a fake beacon node whose spec does not change, the recheck reads the config
// back and reports nothing.
func TestRecheckForkSchedules_QuietWhenUnchanged(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	srv := mocks.NewServer(nil)
	defer srv.Close()

	client, err := New(t.Context(), zap.New(core), Options{BeaconNodeAddr: srv.URL, CommonTimeout: 400 * time.Millisecond, LongTimeout: 500 * time.Millisecond})
	require.NoError(t, err)

	client.recheckForkSchedules(t.Context())
	require.Zero(t, logs.FilterMessageSnippet("beacon config").Len())
}
