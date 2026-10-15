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

var a1 = ma.StringCast("/ip4/10.0.0.1/tcp/13001")

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
