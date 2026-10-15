package versionprobe

import (
	"context"
	"crypto/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/attribution"
	"github.com/ssvlabs/ssv/network/records"
)

type fakeEvidence []attribution.OperatorEvidence

func (f fakeEvidence) Snapshot([]spectypes.OperatorID) []attribution.OperatorEvidence { return f }

type fakePeers struct {
	connected map[peer.ID]bool
	versions  map[peer.ID]string
}

func (f fakePeers) Connected(id peer.ID) bool          { return f.connected[id] }
func (f fakePeers) HandshakeVersion(id peer.ID) string { return f.versions[id] }

func ev(op uint64, top peer.ID, count uint64) attribution.OperatorEvidence {
	return attribution.OperatorEvidence{OperatorID: op, Samples: count, Candidates: []attribution.Candidate{{PeerID: top, Count: count, Share: 1}}}
}

func staticSource(addrs ...ma.Multiaddr) []AddrSource {
	return []AddrSource{{Name: "pool", Lookup: func(context.Context, peer.ID) ([]ma.Multiaddr, error) { return addrs, nil }}}
}

type fakeIndex map[peer.ID]*records.NodeInfo

func (f fakeIndex) NodeInfo(id peer.ID) *records.NodeInfo { return f[id] }

type fakeConn map[peer.ID]network.Connectedness

func (f fakeConn) Connectedness(id peer.ID) network.Connectedness { return f[id] }

func TestTargetsThresholdAndTopOnly(t *testing.T) {
	e := fakeEvidence{
		ev(1, pid(1), 20),
		ev(2, pid(2), 19), // below minTargetCount
		{OperatorID: 3, Samples: 50, Candidates: []attribution.Candidate{{PeerID: pid(3), Count: 30}, {PeerID: pid(4), Count: 20}}},
	}
	p := mustNew(t, Config{Logger: zap.NewNop(), Evidence: e, Peers: fakePeers{}, Sources: staticSource(a1), Request: func() Request { return testReq }})
	require.Equal(t, []peer.ID{pid(1), pid(3)}, p.Targets(t0))
}

func TestTargetsSkipConnectedAndVersioned(t *testing.T) {
	e := fakeEvidence{ev(1, pid(1), 30), ev(2, pid(2), 30), ev(3, pid(3), 30)}
	peers := fakePeers{connected: map[peer.ID]bool{pid(1): true}, versions: map[peer.ID]string{pid(2): "v2.4.3"}}
	p := mustNew(t, Config{Logger: zap.NewNop(), Evidence: e, Peers: peers, Sources: staticSource(a1), Request: func() Request { return testReq }})
	require.Equal(t, []peer.ID{pid(3)}, p.Targets(t0))
}

func TestTargetsSkipNotDue(t *testing.T) {
	c := NewCache(10)
	c.Record(pid(1), Result{Outcome: OutcomeTimeout}, t0)
	p := mustNew(t, Config{Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30)}, Peers: fakePeers{}, Cache: c, Sources: staticSource(a1), Request: func() Request { return testReq }})
	require.Empty(t, p.Targets(t0.Add(time.Minute)))
	require.Equal(t, []peer.ID{pid(1)}, p.Targets(t0.Add(15*time.Minute)))
}

func TestTargetsDeduplicate(t *testing.T) {
	p := mustNew(t, Config{Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(7), 30), ev(2, pid(7), 30)}, Peers: fakePeers{}, Sources: staticSource(a1), Request: func() Request { return testReq }})
	require.Equal(t, []peer.ID{pid(7)}, p.Targets(t0))
}

func TestRunCycleRecordsResultsAndNoAddr(t *testing.T) {
	var mu sync.Mutex
	var probed []peer.ID
	sources := []AddrSource{{Name: "pool", Lookup: func(_ context.Context, id peer.ID) ([]ma.Multiaddr, error) {
		if id == pid(2) {
			return nil, nil
		}
		return []ma.Multiaddr{a1}, nil
	}}}
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30), ev(2, pid(2), 30)}, Peers: fakePeers{},
		Sources: sources, Request: func() Request { return testReq },
		Probe: func(_ context.Context, target peer.AddrInfo, _ Request) Result {
			mu.Lock()
			probed = append(probed, target.ID)
			mu.Unlock()
			return Result{Outcome: OutcomeOK, Version: "v2.5.0"}
		},
		Now:  func() time.Time { return t0 },
		Wait: func(context.Context, time.Duration) error { return nil },
	})
	p.RunCycle(context.Background())
	require.Equal(t, []peer.ID{pid(1)}, probed)
	v, _, ok := p.Cache().Version(pid(1))
	require.True(t, ok)
	require.Equal(t, "v2.5.0", v)
	es := p.Cache().Entries()
	require.Len(t, es, 2)
	byID := map[peer.ID]Entry{es[0].PeerID: es[0], es[1].PeerID: es[1]}
	require.Equal(t, OutcomeNoAddr, byID[pid(2)].Outcome)
}

func TestRunCycleSpacingAndConcurrency(t *testing.T) {
	var waits []time.Duration
	var inFlight, peak atomic.Int32
	e := fakeEvidence{ev(1, pid(1), 30), ev(2, pid(2), 30), ev(3, pid(3), 30), ev(4, pid(4), 30)}
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: e, Peers: fakePeers{}, Sources: staticSource(a1), Request: func() Request { return testReq },
		Probe: func(context.Context, peer.AddrInfo, Request) Result {
			n := inFlight.Add(1)
			for {
				pk := peak.Load()
				if n <= pk || peak.CompareAndSwap(pk, n) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			inFlight.Add(-1)
			return Result{Outcome: OutcomeOK, Version: "v"}
		},
		Now:  func() time.Time { return t0 }, // frozen clock: every gap after the first is a full minSpacing
		Wait: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	})
	p.RunCycle(context.Background())
	require.Equal(t, []time.Duration{minSpacing, minSpacing, minSpacing}, waits)
	require.LessOrEqual(t, peak.Load(), int32(maxConcurrent))
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30)}, Peers: fakePeers{}, Sources: staticSource(a1), Request: func() Request { return testReq },
		Probe: func(ctx context.Context, _ peer.AddrInfo, _ Request) Result {
			started <- struct{}{}
			<-ctx.Done()
			return Result{Outcome: OutcomeTimeout, Err: ctx.Err().Error()}
		},
	})
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	<-started
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// Cancellation is not a peer failure: an interrupted probe must leave no cache entry.
func TestRunCycleCancelledProbeNotRecorded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30)}, Peers: fakePeers{}, Sources: staticSource(a1), Request: func() Request { return testReq },
		Probe: func(ctx context.Context, _ peer.AddrInfo, _ Request) Result {
			started <- struct{}{}
			<-ctx.Done()
			return Result{Outcome: OutcomeDialFailed, Err: ctx.Err().Error()}
		},
	})
	done := make(chan struct{})
	go func() { p.RunCycle(ctx); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunCycle did not return after cancel")
	}
	require.Empty(t, p.Cache().Entries())
}

func TestNewPeerState(t *testing.T) {
	idx := fakeIndex{pid(1): {Metadata: &records.NodeMetadata{NodeVersion: "v2.4.3"}}, pid(2): {}}
	net := fakeConn{pid(1): network.Connected}
	ps := NewPeerState(idx, net)
	require.True(t, ps.Connected(pid(1)))
	require.False(t, ps.Connected(pid(2)))
	require.Equal(t, "v2.4.3", ps.HandshakeVersion(pid(1)))
	require.Equal(t, "", ps.HandshakeVersion(pid(2))) // nil metadata
	require.Equal(t, "", ps.HandshakeVersion(pid(3))) // unknown
}

// A panicking probe is recorded as bad_response and doesn't take RunCycle (or the node) down.
func TestRunCyclePanickingProbeRecordedAsBadResponse(t *testing.T) {
	var probed atomic.Int32
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30), ev(2, pid(2), 30)}, Peers: fakePeers{},
		Sources: staticSource(a1), Request: func() Request { return testReq },
		Probe: func(_ context.Context, target peer.AddrInfo, _ Request) Result {
			probed.Add(1)
			if target.ID == pid(1) {
				panic("boom")
			}
			return Result{Outcome: OutcomeOK, Version: "v2.5.0"}
		},
		Now:  func() time.Time { return t0 },
		Wait: func(context.Context, time.Duration) error { return nil },
	})
	require.NotPanics(t, func() { p.RunCycle(context.Background()) })
	require.Equal(t, int32(2), probed.Load()) // the panic freed its slot: the next peer still ran
	byID := map[peer.ID]Entry{}
	for _, e := range p.Cache().Entries() {
		byID[e.PeerID] = e
	}
	require.Equal(t, OutcomeBadResponse, byID[pid(1)].Outcome)
	require.Equal(t, "panic: boom", byID[pid(1)].Error)
	require.Equal(t, t0.Add(backoffSchedule[0]), byID[pid(1)].NextAttempt)
	require.Equal(t, OutcomeOK, byID[pid(2)].Outcome)
}

// flippingPeers reports a peer as connected once Targets() has been computed.
type flippingPeers struct {
	mu        sync.Mutex
	connected map[peer.ID]bool
}

func (f *flippingPeers) Connected(id peer.ID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected[id]
}
func (f *flippingPeers) HandshakeVersion(peer.ID) string { return "" }
func (f *flippingPeers) connect(id peer.ID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected[id] = true
}

// A target that connects to the main host after Targets() was computed (e.g. during the spacing
// wait) is skipped, not probed.
func TestRunCycleSkipsPeerConnectedAfterTargets(t *testing.T) {
	peers := &flippingPeers{connected: map[peer.ID]bool{}}
	var mu sync.Mutex
	var probed []peer.ID
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30), ev(2, pid(2), 30), ev(3, pid(3), 30)}, Peers: peers,
		Sources: staticSource(a1), Request: func() Request { return testReq },
		Probe: func(_ context.Context, target peer.AddrInfo, _ Request) Result {
			mu.Lock()
			probed = append(probed, target.ID)
			mu.Unlock()
			return Result{Outcome: OutcomeOK, Version: "v2.5.0"}
		},
		Now: func() time.Time { return t0 },
		// The first spacing wait (before pid(2)) is when pid(2) connects.
		Wait: func(context.Context, time.Duration) error { peers.connect(pid(2)); return nil },
	})
	require.Equal(t, []peer.ID{pid(1), pid(2), pid(3)}, p.Targets(t0))
	p.RunCycle(context.Background())
	require.ElementsMatch(t, []peer.ID{pid(1), pid(3)}, probed)
	for _, e := range p.Cache().Entries() {
		require.NotEqual(t, pid(2), e.PeerID, "skipped peer must not be recorded")
	}
}

// The cache entry records where the dialed addresses came from and the first few of them.
func TestRunCycleRecordsAddrSourceAndAddrs(t *testing.T) {
	addrs := []ma.Multiaddr{
		ma.StringCast("/ip4/1.1.1.1/tcp/13001"), ma.StringCast("/ip4/1.1.1.2/tcp/13001"), ma.StringCast("/ip4/1.1.1.3/tcp/13001"),
		ma.StringCast("/ip4/1.1.1.4/tcp/13001"), ma.StringCast("/ip4/1.1.1.5/tcp/13001"),
	}
	sources := []AddrSource{
		{Name: "discovered", Lookup: func(context.Context, peer.ID) ([]ma.Multiaddr, error) {
			return []ma.Multiaddr{ma.StringCast("/ip4/10.0.0.1/tcp/13001")}, nil // filtered out
		}},
		{Name: "peerstore", Lookup: func(context.Context, peer.ID) ([]ma.Multiaddr, error) { return addrs, nil }},
	}
	var dialed []ma.Multiaddr
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30)}, Peers: fakePeers{},
		Sources: sources, Request: func() Request { return testReq },
		Probe: func(_ context.Context, target peer.AddrInfo, _ Request) Result {
			dialed = target.Addrs
			return Result{Outcome: OutcomeDialFailed, Err: "connection refused"}
		},
		Now:  func() time.Time { return t0 },
		Wait: func(context.Context, time.Duration) error { return nil },
	})
	p.RunCycle(context.Background())
	require.Equal(t, addrs, dialed) // the probe gets every address; only the record is capped
	es := p.Cache().Entries()
	require.Len(t, es, 1)
	require.Equal(t, "peerstore", es[0].AddrSource)
	require.Equal(t, []string{
		"/ip4/1.1.1.1/tcp/13001", "/ip4/1.1.1.2/tcp/13001", "/ip4/1.1.1.3/tcp/13001", "/ip4/1.1.1.4/tcp/13001",
	}, es[0].Addrs)
}

func mustNew(t *testing.T, cfg Config) *Prober {
	t.Helper()
	p, err := New(cfg)
	require.NoError(t, err)
	return p
}

func TestProberUsesOneKeyPerProcess(t *testing.T) {
	var mu sync.Mutex
	var keys []crypto.PrivKey
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30), ev(2, pid(2), 30)}, Peers: fakePeers{},
		Sources: staticSource(a1), Request: func() Request { return testReq },
		Probe: func(_ context.Context, _ peer.AddrInfo, req Request) Result {
			mu.Lock()
			keys = append(keys, req.Key)
			mu.Unlock()
			return Result{Outcome: OutcomeOK, Version: "v2.5.0"}
		},
		Now:  func() time.Time { return t0 },
		Wait: func(context.Context, time.Duration) error { return nil },
	})
	p.RunCycle(context.Background())
	require.Len(t, keys, 2)
	require.NotNil(t, keys[0])
	require.NotNil(t, keys[1])
	require.True(t, keys[0].Equals(keys[1]))
	id, err := peer.IDFromPrivateKey(keys[0])
	require.NoError(t, err)
	require.Equal(t, id, p.PeerID())
}

func TestProberConfigKeyHonoured(t *testing.T) {
	key, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	var got crypto.PrivKey
	p := mustNew(t, Config{
		Logger: zap.NewNop(), Evidence: fakeEvidence{ev(1, pid(1), 30)}, Peers: fakePeers{},
		Sources: staticSource(a1), Request: func() Request { return testReq }, Key: key,
		Probe: func(_ context.Context, _ peer.AddrInfo, req Request) Result {
			got = req.Key
			return Result{Outcome: OutcomeOK, Version: "v2.5.0"}
		},
		Now:  func() time.Time { return t0 },
		Wait: func(context.Context, time.Duration) error { return nil },
	})
	p.RunCycle(context.Background())
	require.NotNil(t, got)
	require.True(t, key.Equals(got))
	want, err := peer.IDFromPrivateKey(key)
	require.NoError(t, err)
	require.Equal(t, want, p.PeerID())
}
