package versionprobe

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func src(name string, addrs []ma.Multiaddr, err error, calls *[]string) AddrSource {
	return AddrSource{Name: name, Lookup: func(ctx context.Context, id peer.ID) ([]ma.Multiaddr, error) {
		*calls = append(*calls, name)
		return addrs, err
	}}
}

// a1 is a public TCP address, so Resolve accepts it.
var a1 = ma.StringCast("/ip4/1.2.3.4/tcp/13001")

func TestResolveOrderFirstNonEmptyWins(t *testing.T) {
	var calls []string
	addrs, name, err := Resolve(context.Background(), pid(1), []AddrSource{
		src("pool", nil, nil, &calls),
		src("peerstore", []ma.Multiaddr{a1}, nil, &calls),
		src("discv5", []ma.Multiaddr{a1}, nil, &calls),
	})
	require.NoError(t, err)
	require.Equal(t, "peerstore", name)
	require.Equal(t, []ma.Multiaddr{a1}, addrs)
	require.Equal(t, []string{"pool", "peerstore"}, calls) // discv5 not called
}

func TestResolveSkipsEmptyAndErroringSources(t *testing.T) {
	var calls []string
	addrs, name, err := Resolve(context.Background(), pid(1), []AddrSource{
		src("pool", nil, nil, &calls),
		src("discv5", nil, errors.New("invalid ip address"), &calls),
	})
	require.Nil(t, addrs)
	require.Empty(t, name)
	require.EqualError(t, err, "invalid ip address")
}

func TestResolveAllEmptyNoError(t *testing.T) {
	var calls []string
	addrs, _, err := Resolve(context.Background(), pid(1), []AddrSource{src("pool", nil, nil, &calls)})
	require.Nil(t, addrs)
	require.NoError(t, err)
}

func TestResolveStopsOnCancelledContext(t *testing.T) {
	var calls []string
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Resolve(ctx, pid(1), []AddrSource{src("pool", []ma.Multiaddr{a1}, nil, &calls)})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, calls)
}

func TestThrottledCapsConcurrency(t *testing.T) {
	var inFlight, peak atomic.Int32
	slow := AddrSource{Name: "discv5", Lookup: func(ctx context.Context, id peer.ID) ([]ma.Multiaddr, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		return nil, nil
	}}
	th := Throttled(slow, 1)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = th.Lookup(context.Background(), pid(1)) }()
	}
	wg.Wait()
	require.Equal(t, int32(1), peak.Load())
}

func TestThrottledReturnsOnCancel(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	finished := make(chan struct{})
	blocking := AddrSource{Name: "discv5", Lookup: func(context.Context, peer.ID) ([]ma.Multiaddr, error) {
		close(started)
		<-release
		close(finished)
		return nil, nil
	}}
	th := Throttled(blocking, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }() // cancel only once the lookup is running and blocked
	done := make(chan error, 1)
	go func() { _, err := th.Lookup(ctx, pid(1)); done <- err }()
	// The property is "returns instead of blocking forever", not a latency bound: the bound is
	// generous so it holds under -race -p 16.
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Throttled lookup did not return after cancel")
	}
	close(release) // unblock the underlying lookup so nothing leaks
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("underlying lookup did not finish")
	}
}

func TestResolveFiltersUndialableAndFallsThrough(t *testing.T) {
	for name, bad := range map[string]string{
		"private":     "/ip4/10.0.0.1/tcp/13001",
		"loopback":    "/ip4/127.0.0.1/tcp/13001",
		"loopback6":   "/ip6/::1/tcp/13001",
		"unspecified": "/ip4/0.0.0.0/tcp/13001",
		"port0":       "/ip4/1.2.3.4/tcp/0",
		"udp":         "/ip4/1.2.3.4/udp/12001",
		"quic":        "/ip4/1.2.3.4/udp/12001/quic-v1",
		"ws":          "/ip4/1.2.3.4/tcp/13001/ws",
		"dns":         "/dns4/example.com/tcp/13001",
	} {
		t.Run(name, func(t *testing.T) {
			var calls []string
			addrs, src2, err := Resolve(context.Background(), pid(1), []AddrSource{
				src("discovered", []ma.Multiaddr{ma.StringCast(bad)}, nil, &calls),
				src("peerstore", []ma.Multiaddr{a1}, nil, &calls),
			})
			require.NoError(t, err)
			require.Equal(t, "peerstore", src2)
			require.Equal(t, []ma.Multiaddr{a1}, addrs)
			require.Equal(t, []string{"discovered", "peerstore"}, calls)
		})
	}
}

func TestResolveKeepsOnlyDialableAddrs(t *testing.T) {
	var calls []string
	withPeer := ma.StringCast("/ip4/5.6.7.8/tcp/13001/p2p/12D3KooWLRPJAA5o6dG1gcTxDY6WqGjW7rTgnhZ6Pxm8qY2dDeNp")
	v6 := ma.StringCast("/ip6/2001:4860:4860::8888/tcp/13001")
	addrs, name, err := Resolve(context.Background(), pid(1), []AddrSource{
		src("peerstore", []ma.Multiaddr{
			ma.StringCast("/ip4/192.168.1.2/tcp/13001"), a1, ma.StringCast("/ip4/1.2.3.4/udp/12001"), withPeer, v6,
		}, nil, &calls),
	})
	require.NoError(t, err)
	require.Equal(t, "peerstore", name)
	require.Equal(t, []ma.Multiaddr{a1, withPeer, v6}, addrs)
}

func TestResolveAllFilteredReportsWhy(t *testing.T) {
	var calls []string
	addrs, name, err := Resolve(context.Background(), pid(1), []AddrSource{
		src("discovered", nil, errors.New("boom"), &calls),
		src("peerstore", []ma.Multiaddr{ma.StringCast("/ip4/127.0.0.1/tcp/13001")}, nil, &calls),
		src("discv5", nil, nil, &calls),
	})
	require.Nil(t, addrs)
	require.Empty(t, name)
	require.EqualError(t, err, "peerstore: no public tcp address")

	// A later error replaces the note.
	calls = nil
	_, _, err = Resolve(context.Background(), pid(1), []AddrSource{
		src("peerstore", []ma.Multiaddr{ma.StringCast("/ip4/127.0.0.1/tcp/13001")}, nil, &calls),
		src("discv5", nil, errors.New("invalid ip address"), &calls),
	})
	require.EqualError(t, err, "invalid ip address")
}
