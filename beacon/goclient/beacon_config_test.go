package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// addressedClient is a Client known by its address alone, the only method the beacon-config paths call on it.
type addressedClient struct {
	Client
	address string
}

func (c *addressedClient) Address() string { return c.address }

// clientAt is a new client at the address: two calls with one address are still two clients.
func clientAt(address string) Client { return &addressedClient{address: address} }

// A client whose fork schedule differs from the node's only about a fork ahead of the chain is not a client
// on another network: the node keeps its own schedule, keeps serving from the client, and says which side
// has to move — the client, to be upgraded (a warning), or the node, which cannot adopt a fork while running
// and has to be restarted to adopt it (an error, and a stop close to the fork). A disagreement about a fork
// already active still fails.
func TestApplyBeaconConfig_ForkScheduleLag(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()

	t.Run("the client lags: warn to upgrade the client", func(t *testing.T) {
		ours := gloasScheduledAt(now + 100)
		gc, logs := newLagTestClient(ours)

		got, err := gc.applyBeaconConfig(t.Context(), clientAt("http://lagging:5052"), gloasScheduledAt(networkconfig.FarFutureEpoch))
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

		got, err := gc.applyBeaconConfig(t.Context(), clientAt("http://upgraded:5052"), gloasScheduledAt(now+100))
		require.NoError(t, err, "the node keeps serving from the client")
		require.Same(t, ours, got)
		require.Zero(t, logs.FilterLevelExact(zapcore.WarnLevel).Len())
		alarms := logs.FilterMessageSnippet("restart the node before that fork")
		require.Equal(t, 1, alarms.Len())
		require.Equal(t, zapcore.ErrorLevel, alarms.All()[0].Level)
		require.NotContains(t, alarms.All()[0].Message, "config source", "a restart takes the newest schedule, whichever client reports first")
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
			_, _ = gc.applyBeaconConfig(t.Context(), clientAt("http://upgraded:5052"), gloasScheduledAt(now+forkScheduleLagStopEpochs))
		})
		stops := logs.FilterLevelExact(zapcore.FatalLevel)
		require.Equal(t, 1, stops.Len())
		require.Contains(t, stops.All()[0].Message, "stopping, as the fork is too close")

		gc, logs = newLagTestClient(gloasScheduledAt(networkconfig.FarFutureEpoch))
		_, err := gc.applyBeaconConfig(t.Context(), clientAt("http://upgraded:5052"), gloasScheduledAt(now+forkScheduleLagStopEpochs+1))
		require.NoError(t, err, "an epoch further out, it's an error only")
		require.Equal(t, 1, logs.FilterLevelExact(zapcore.ErrorLevel).Len())
		require.Zero(t, logs.FilterLevelExact(zapcore.FatalLevel).Len())
	})

	t.Run("both schedule the fork, differently: error, naming both", func(t *testing.T) {
		gc, logs := newLagTestClient(gloasScheduledAt(now + 100))

		_, err := gc.applyBeaconConfig(t.Context(), clientAt("http://other:5052"), gloasScheduledAt(now+200))
		require.NoError(t, err)
		alarms := logs.FilterMessageSnippet("schedule a fork differently")
		require.Equal(t, 1, alarms.Len())
		require.Equal(t, zapcore.ErrorLevel, alarms.All()[0].Level)
		require.Contains(t, alarms.All()[0].Message, "the node's config source included")
	})

	t.Run("a fork active on one side only is a different chain", func(t *testing.T) {
		gc, _ := newLagTestClient(gloasScheduledAt(networkconfig.FarFutureEpoch))

		_, err := gc.applyBeaconConfig(t.Context(), clientAt("http://other:5052"), gloasScheduledAt(now))
		require.ErrorContains(t, err, "beacon config misalign")
	})
}

// newChoiceTestClient is a client with the given number of beacon nodes configured and its config not chosen yet. It
// observes logs from Info up; a Fatal log panics instead of exiting.
func newChoiceTestClient(clients int) (*GoClient, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core, zap.WithFatalHook(zapcore.WriteThenPanic))
	return &GoClient{
		log:                        logger,
		clients:                    make([]Client, clients),
		beaconConfigInit:           make(chan struct{}),
		beaconConfigCandidateAdded: make(chan struct{}, 1),
	}, logs
}

// At startup the node takes the newest compatible config its clients report, so the upgraded client of a staggered
// upgrade wins whichever client reports first. Of two clients that schedule a fork differently, the earlier report
// wins. A config that differs by more than a fork-schedule lag (another network, a fork active on one side only)
// never replaces an earlier one, and the activation hook rejects its client; reported first, it is kept, and the hook
// rejects the other clients instead.
func TestChooseBeaconConfig_TakesTheNewestCompatible(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	lagging := gloasScheduledAt(networkconfig.FarFutureEpoch)
	upgraded := gloasScheduledAt(now + 100)
	scheduledLater := gloasScheduledAt(now + 200)
	active := gloasScheduledAt(now)
	otherNetwork := gloasScheduledAt(now + 100)
	otherNetwork.Name = "devnet"

	for name, tc := range map[string]struct {
		reported []*networkconfig.Beacon
		chosen   int
	}{
		"lagging first":                     {reported: []*networkconfig.Beacon{lagging, upgraded}, chosen: 1},
		"upgraded first":                    {reported: []*networkconfig.Beacon{upgraded, lagging}, chosen: 0},
		"scheduled differently":             {reported: []*networkconfig.Beacon{upgraded, scheduledLater}, chosen: 0},
		"the earlier of two rival upgrades": {reported: []*networkconfig.Beacon{lagging, upgraded, scheduledLater}, chosen: 1},
		"another network":                   {reported: []*networkconfig.Beacon{lagging, otherNetwork}, chosen: 0},
		"another network first":             {reported: []*networkconfig.Beacon{otherNetwork, lagging}, chosen: 0},
		"a fork active on one side":         {reported: []*networkconfig.Beacon{lagging, active}, chosen: 0},
	} {
		t.Run(name, func(t *testing.T) {
			gc, logs := newChoiceTestClient(len(tc.reported))
			for i, cfg := range tc.reported {
				require.True(t, gc.offerBeaconConfig(clientAt(fmt.Sprintf("http://bn%d:5052", i)), cfg))
			}

			start := time.Now()
			require.NoError(t, gc.chooseBeaconConfig(t.Context(), time.Minute, time.Minute))
			require.Less(t, time.Since(start), 10*time.Second, "every client reported, so there is nothing to wait for")
			require.Same(t, tc.reported[tc.chosen], gc.getBeaconConfig())
			require.Equal(t, fmt.Sprintf("http://bn%d:5052", tc.chosen), gc.beaconConfigSource)
			require.False(t, gc.offerBeaconConfig(clientAt("http://late:5052"), upgraded), "the choice is final")

			initialized := logs.FilterMessage("beacon config has been initialized")
			require.Equal(t, 1, initialized.Len())
			require.EqualValues(t, len(tc.reported), initialized.All()[0].ContextMap()["clients_reported"])
		})
	}
}

// A config most of the clients that reported don't share is still taken, but with an error naming every report, as
// it may name a fork the network doesn't schedule. The warning to upgrade a client that lacks the fork says when only
// the config source reported the node's config.
func TestChooseBeaconConfig_FlagsAConfigFewShare(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	lagging, upgraded := gloasScheduledAt(networkconfig.FarFutureEpoch), gloasScheduledAt(now+100)

	for name, tc := range map[string]struct {
		reported   []*networkconfig.Beacon
		flagged    bool // most clients don't share the config: an error
		sourceOnly bool // the warning says only the config source reported it
	}{
		"one of three": {reported: []*networkconfig.Beacon{lagging, upgraded, lagging}, flagged: true, sourceOnly: true},
		"one of two":   {reported: []*networkconfig.Beacon{lagging, upgraded}, sourceOnly: true},
		"two of three": {reported: []*networkconfig.Beacon{upgraded, lagging, upgraded}},
	} {
		t.Run(name, func(t *testing.T) {
			gc, logs := newChoiceTestClient(len(tc.reported))
			for i, cfg := range tc.reported {
				require.True(t, gc.offerBeaconConfig(clientAt(fmt.Sprintf("http://bn%d:5052", i)), cfg))
			}
			require.NoError(t, gc.chooseBeaconConfig(t.Context(), time.Minute, time.Minute))
			require.Same(t, upgraded, gc.getBeaconConfig())

			alarms := logs.FilterMessageSnippet("most clients that reported at startup don't share the config the node took")
			if tc.flagged {
				require.Equal(t, 1, alarms.Len())
				require.Equal(t, zapcore.ErrorLevel, alarms.All()[0].Level)
				require.Equal(t, []any{"http://bn0:5052: gloas not scheduled", "http://bn1:5052: the node's config", "http://bn2:5052: gloas not scheduled"},
					alarms.All()[0].ContextMap()["reports"])
			} else {
				require.Zero(t, alarms.Len())
			}

			_, err := gc.applyBeaconConfig(t.Context(), clientAt("http://late:5052"), lagging)
			require.NoError(t, err)
			warnings := logs.FilterMessageSnippet("upgrade the client before that fork")
			require.Equal(t, 1, warnings.Len())
			require.Equal(t, tc.sourceOnly, strings.Contains(warnings.All()[0].Message, "a config only its source reported at startup"))
		})
	}
}

// The node waits for its other clients a bounded time once one has reported. It chooses from the clients that
// reported once all of them have, or once the wait is over, and fails, choosing nothing, when no client reports in
// time or the node shuts down first.
func TestChooseBeaconConfig_Waits(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	lagging, upgraded := gloasScheduledAt(networkconfig.FarFutureEpoch), gloasScheduledAt(now+100)

	t.Run("a client that reports during the wait is in the choice", func(t *testing.T) {
		gc, _ := newChoiceTestClient(2)
		require.True(t, gc.offerBeaconConfig(clientAt("http://lagging:5052"), lagging))
		go func() {
			time.Sleep(50 * time.Millisecond)
			gc.offerBeaconConfig(clientAt("http://upgraded:5052"), upgraded)
		}()

		start := time.Now()
		require.NoError(t, gc.chooseBeaconConfig(t.Context(), time.Minute, time.Minute))
		require.Less(t, time.Since(start), 10*time.Second, "chosen as soon as both reported")
		require.Same(t, upgraded, gc.getBeaconConfig())
	})

	t.Run("a client that doesn't report in time is left out", func(t *testing.T) {
		gc, logs := newChoiceTestClient(2)
		require.True(t, gc.offerBeaconConfig(clientAt("http://lagging:5052"), lagging))

		require.NoError(t, gc.chooseBeaconConfig(t.Context(), time.Minute, 50*time.Millisecond))
		require.Same(t, lagging, gc.getBeaconConfig())
		require.EqualValues(t, 1, logs.FilterMessage("beacon config has been initialized").All()[0].ContextMap()["clients_reported"])
	})

	t.Run("no client reports: the startup times out", func(t *testing.T) {
		gc, _ := newChoiceTestClient(2)

		require.ErrorIs(t, gc.chooseBeaconConfig(t.Context(), 50*time.Millisecond, time.Minute), context.DeadlineExceeded)
		require.Nil(t, gc.getBeaconConfig())
	})

	t.Run("the node shuts down while waiting for the others", func(t *testing.T) {
		gc, _ := newChoiceTestClient(2)
		require.True(t, gc.offerBeaconConfig(clientAt("http://lagging:5052"), lagging))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		require.ErrorIs(t, gc.chooseBeaconConfig(ctx, time.Minute, time.Minute), context.Canceled)
		require.Nil(t, gc.getBeaconConfig())
	})
}

// Candidates are keyed by client, not by the logged address: two beacon nodes that log the same masked address both
// count, and neither overwrites the other's config, while a client that reports again replaces its own config, in its
// first report's place.
func TestOfferBeaconConfig_OnePerClient(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	lagging, upgraded := gloasScheduledAt(networkconfig.FarFutureEpoch), gloasScheduledAt(now+100)
	gc, _ := newChoiceTestClient(3)

	const maskedAddress = "https://provider.example/xxxxx"
	reportingTwice := clientAt("http://bn:5052")
	require.True(t, gc.offerBeaconConfig(reportingTwice, lagging))
	require.True(t, gc.offerBeaconConfig(clientAt(maskedAddress), upgraded))
	require.True(t, gc.offerBeaconConfig(clientAt(maskedAddress), lagging))
	require.True(t, gc.offerBeaconConfig(reportingTwice, upgraded))
	require.Equal(t, 3, gc.reportedBeaconConfigs())

	require.NoError(t, gc.chooseBeaconConfig(t.Context(), time.Minute, time.Minute))
	require.Same(t, upgraded, gc.getBeaconConfig())
	require.Equal(t, "http://bn:5052", gc.beaconConfigSource, "the first report's place, with the client's latest config")
}

// A client that reports at startup waits for the choice, then is checked against it as any client is: the lagging
// client of a staggered upgrade, reporting first, gets the warning to upgrade it, and both activations go on with the
// newer schedule the node took. An activation canceled before the choice just ends.
func TestApplyBeaconConfig_WaitsForTheChoice(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	upgraded := gloasScheduledAt(now + 100)

	t.Run("checked against the choice", func(t *testing.T) {
		gc, logs := newChoiceTestClient(2)
		type applied struct {
			config *networkconfig.Beacon
			err    error
		}
		apply := func(addr string, cfg *networkconfig.Beacon) chan applied {
			done := make(chan applied, 1)
			go func() {
				config, err := gc.applyBeaconConfig(t.Context(), clientAt(addr), cfg)
				done <- applied{config, err}
			}()
			return done
		}

		laggingDone := apply("http://lagging:5052", gloasScheduledAt(networkconfig.FarFutureEpoch))
		require.Eventually(t, func() bool { return gc.reportedBeaconConfigs() == 1 }, 5*time.Second, time.Millisecond)
		upgradedDone := apply("http://upgraded:5052", upgraded)

		require.NoError(t, gc.chooseBeaconConfig(t.Context(), time.Minute, time.Minute))
		for _, done := range []chan applied{laggingDone, upgradedDone} {
			got := <-done
			require.NoError(t, got.err)
			require.Same(t, upgraded, got.config)
		}
		require.Equal(t, "http://upgraded:5052", gc.beaconConfigSource)
		warnings := logs.FilterMessageSnippet("upgrade the client before that fork")
		require.Equal(t, 1, warnings.Len())
		require.Equal(t, "http://lagging:5052", warnings.All()[0].ContextMap()["address"])
	})

	t.Run("canceled before the choice", func(t *testing.T) {
		gc, _ := newChoiceTestClient(1)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := gc.applyBeaconConfig(ctx, clientAt("http://bn:5052"), upgraded)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("the activation hook, canceled before the choice, just ends", func(t *testing.T) {
		srv := mocks.NewServer(nil)
		defer srv.Close()
		s := newHTTPService(t, srv.URL)
		gc, _ := newChoiceTestClient(2)
		ctx, cancel := context.WithCancel(t.Context())

		ended := make(chan any)
		go func() {
			defer func() { ended <- recover() }()
			gc.singleClientHooks().OnActive(ctx, s)
		}()
		require.Eventually(t, func() bool { return gc.reportedBeaconConfigs() == 1 }, 5*time.Second, time.Millisecond)
		cancel()
		require.Nil(t, <-ended, "with no config chosen, there is nothing to report against")
	})
}

// newGloasBeaconNode is a mock beacon node whose spec schedules Gloas at the epoch, answered after specDelay.
func newGloasBeaconNode(t *testing.T, epoch phase0.Epoch, specDelay time.Duration) *httptest.Server {
	srv := mocks.NewServerWithHandler(func(r *http.Request, resp mocks.Response) (mocks.Response, error) {
		if r.URL.Path != specPath {
			return resp, nil
		}
		var spec struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(resp.Body, &spec); err != nil {
			return resp, err
		}
		spec.Data["GLOAS_FORK_EPOCH"] = strconv.FormatUint(uint64(epoch), 10)
		spec.Data["GLOAS_FORK_VERSION"] = "0x07000000"
		body, err := json.Marshal(spec)
		if err != nil {
			return resp, err
		}
		resp.Body, resp.Delay = body, specDelay
		return resp, nil
	})
	t.Cleanup(srv.Close)
	return srv
}

// Through real clients at startup: with the lagging beacon node listed first, the node still takes the upgraded
// one's schedule, and warns about the lagging one.
func TestNew_TakesTheNewestForkSchedule(t *testing.T) {
	const gloasEpoch = 10_000_000 // far ahead of the mock chain, yet scheduled

	lagging := mocks.NewServer(nil) // its spec leaves Gloas unscheduled
	defer lagging.Close()
	upgraded := newGloasBeaconNode(t, gloasEpoch, 0)

	core, logs := observer.New(zapcore.InfoLevel)
	client, err := New(t.Context(), zap.New(core), Options{
		BeaconNodeAddr: lagging.URL + ";" + upgraded.URL,
		CommonTimeout:  2 * time.Second, // leaves the second client ample time to report
		LongTimeout:    10 * time.Second,
	})
	require.NoError(t, err)

	initialized := logs.FilterMessage("beacon config has been initialized")
	require.Equal(t, 1, initialized.Len())
	require.EqualValues(t, 2, initialized.All()[0].ContextMap()["clients_reported"], "chosen from both clients")
	epoch, scheduled := client.getBeaconConfig().GloasForkEpoch()
	require.True(t, scheduled)
	require.EqualValues(t, gloasEpoch, epoch)
	require.Eventually(t, func() bool {
		return logs.FilterMessageSnippet("upgrade the client before that fork").Len() == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.Zero(t, logs.FilterMessageSnippet("a fork the node started without").Len(), "the node has the fork: nothing to restart for")
}

// Dialing an unresponsive beacon node takes a full request timeout, and New dials them one by one, so the wait for the
// other clients runs from the end of the dialing, not from the first report: with two unresponsive beacon nodes listed
// between the lagging one and the upgraded one, which is a little slow to answer, the node still takes the upgraded
// one's schedule.
func TestNew_WaitsForTheOthersOnceAllAreDialed(t *testing.T) {
	const (
		gloasEpoch    = 10_000_000 // far ahead of the mock chain, yet scheduled
		commonTimeout = 500 * time.Millisecond
	)
	lagging := mocks.NewServer(nil) // its spec leaves Gloas unscheduled
	t.Cleanup(lagging.Close)
	unresponsive := func() string {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	upgraded := newGloasBeaconNode(t, gloasEpoch, commonTimeout/2)

	core, logs := observer.New(zapcore.InfoLevel)
	client, err := New(t.Context(), zap.New(core), Options{
		BeaconNodeAddr: strings.Join([]string{lagging.URL, unresponsive(), unresponsive(), upgraded.URL}, ";"),
		CommonTimeout:  commonTimeout,
		LongTimeout:    10 * time.Second,
	})
	require.NoError(t, err)

	initialized := logs.FilterMessage("beacon config has been initialized")
	require.Equal(t, 1, initialized.Len())
	require.EqualValues(t, 2, initialized.All()[0].ContextMap()["clients_reported"])
	epoch, scheduled := client.getBeaconConfig().GloasForkEpoch()
	require.True(t, scheduled, "the upgraded beacon node reported in time")
	require.EqualValues(t, gloasEpoch, epoch)
}

// The periodic recheck reports through the same path as the activation hook, and stays quiet for a client
// whose schedule still matches the node's.
func TestCheckForkSchedule(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	ours := gloasScheduledAt(networkconfig.FarFutureEpoch)
	gc, logs := newLagTestClient(ours)

	gc.checkForkSchedule("http://same:5052", gloasScheduledAt(networkconfig.FarFutureEpoch), true)
	require.Zero(t, logs.Len(), "an unchanged schedule reports nothing")

	gc.checkForkSchedule("http://upgraded:5052", gloasScheduledAt(now+100), true)
	require.Equal(t, 1, logs.FilterMessageSnippet("restart the node before that fork").Len(), "a fork the node started without is the node's to adopt")
	require.Panics(t, func() { gc.checkForkSchedule("http://upgraded:5052", gloasScheduledAt(now+1), true) }, "and close to it the node stops")

	other := gloasScheduledAt(networkconfig.FarFutureEpoch)
	other.Name = "another network"
	gc.checkForkSchedule("http://other:5052", other, true)
	require.Equal(t, 1, logs.FilterMessageSnippet("no longer matches the node's").Len(), "anything but a fork-schedule lag is an error of its own")
}

// A fork the node schedules that no active client still schedules the same way moved or was dropped after the
// node started, so the node is the stale side: an error, and close to the earlier of the two epochs a stop, as
// crossing it on a schedule no client shares fails every duty. While another client still shares the node's
// schedule, the disagreeing one is the likely stale side and gets the usual warning.
func TestCheckForkSchedule_NoClientSharesTheNodesSchedule(t *testing.T) {
	now := networkconfig.TestNetwork.EstimatedCurrentEpoch()
	const nodeStale = "no client schedules a fork as the node's config does"

	t.Run("dropped, far off: error", func(t *testing.T) {
		gc, logs := newLagTestClient(gloasScheduledAt(now + 100))

		gc.checkForkSchedule("http://only:5052", gloasScheduledAt(networkconfig.FarFutureEpoch), false)
		alarms := logs.FilterMessageSnippet(nodeStale)
		require.Equal(t, 1, alarms.Len())
		require.Equal(t, zapcore.ErrorLevel, alarms.All()[0].Level)
		require.EqualValues(t, now+100, alarms.All()[0].ContextMap()["epoch"])
	})

	t.Run("postponed, close to the node's epoch: stop", func(t *testing.T) {
		gc, logs := newLagTestClient(gloasScheduledAt(now + forkScheduleLagStopEpochs))

		require.Panics(t, func() { gc.checkForkSchedule("http://only:5052", gloasScheduledAt(now+100), false) })
		require.Equal(t, 1, logs.FilterLevelExact(zapcore.FatalLevel).Len())
	})

	t.Run("moved earlier, close to the client's epoch: stop", func(t *testing.T) {
		gc, _ := newLagTestClient(gloasScheduledAt(now + 100))

		require.Panics(t, func() {
			gc.checkForkSchedule("http://only:5052", gloasScheduledAt(now+forkScheduleLagStopEpochs), false)
		})
	})

	t.Run("another client shares the node's schedule: warn about this one", func(t *testing.T) {
		gc, logs := newLagTestClient(gloasScheduledAt(now + forkScheduleLagStopEpochs))

		gc.checkForkSchedule("http://lagging:5052", gloasScheduledAt(networkconfig.FarFutureEpoch), true)
		require.Equal(t, 1, logs.FilterMessageSnippet("upgrade the client before that fork").Len())
		require.Zero(t, logs.FilterMessageSnippet(nodeStale).Len())
	})
}

// Through a real client whose beacon node no longer schedules a fork the node's config does, with no other client
// to share the node's schedule, the recheck stops the node close to the fork.
func TestRecheckForkSchedules_StopsWhenNoClientSharesTheNodesSchedule(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	srv := mocks.NewServer(nil) // its spec leaves Gloas unscheduled
	defer srv.Close()

	logger := zap.New(core, zap.WithFatalHook(zapcore.WriteThenPanic))
	client, err := New(t.Context(), logger, Options{BeaconNodeAddr: srv.URL, CommonTimeout: 400 * time.Millisecond, LongTimeout: 500 * time.Millisecond})
	require.NoError(t, err)

	node := *client.getBeaconConfig()
	node.Forks = maps.Clone(node.Forks)
	node.Forks[networkconfig.DataVersionGloas] = phase0.Fork{Epoch: node.EstimatedCurrentEpoch() + forkScheduleLagStopEpochs, CurrentVersion: phase0.Version{0x80}}
	client.beaconConfigMu.Lock()
	client.beaconConfig = &node // the node started with Gloas scheduled; its only beacon node has since dropped it
	client.beaconConfigMu.Unlock()

	require.Panics(t, func() { client.recheckForkSchedules(t.Context()) })
	require.Equal(t, 1, logs.FilterMessageSnippet("no client schedules a fork as the node's config does").Len())
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
