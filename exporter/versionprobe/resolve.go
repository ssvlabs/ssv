package versionprobe

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// AddrSource is one place to look up a peer's dialable addresses (e.g. the discovered-peers
// pool, the main host's peerstore, a discv5 lookup). Lookup must not dial the peer.
type AddrSource struct {
	Name   string
	Lookup func(ctx context.Context, id peer.ID) ([]ma.Multiaddr, error)
}

// Resolve tries sources in order and returns the first non-empty list of dialable addresses
// together with the name of the source that produced it. Only public-IP TCP addresses with a
// non-zero port are kept (see dialable); a source that fails, returns nothing, or returns
// nothing dialable is skipped. If no source yields addresses, it returns the last error seen,
// including "<source>: no public tcp address" when a source's addresses were all filtered out
// (nil when every source was simply empty). It stops with ctx.Err() as soon as ctx is done.
func Resolve(ctx context.Context, id peer.ID, sources []AddrSource) (addrs []ma.Multiaddr, source string, err error) {
	var lastErr error
	for _, s := range sources {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		got, err := s.Lookup(ctx, id)
		if err != nil {
			lastErr = err
			continue
		}
		if len(got) == 0 {
			continue
		}
		var kept []ma.Multiaddr
		for _, a := range got {
			if dialable(a) {
				kept = append(kept, a)
			}
		}
		if len(kept) > 0 {
			return kept, s.Name, nil
		}
		lastErr = fmt.Errorf("%s: no public tcp address", s.Name)
	}
	return nil, "", lastErr
}

// dialable reports whether the probe host (TCP transport only) can usefully dial a: an ip4/ip6
// address with a non-zero TCP port, optionally followed by /p2p/<id>, whose IP is public.
// Private, loopback, unspecified and other non-routable IPs are rejected so the probe never
// dials into the exporter's own network.
func dialable(a ma.Multiaddr) bool {
	if len(a) < 2 {
		return false
	}
	switch a[0].Protocol().Code {
	case ma.P_IP4, ma.P_IP6:
	default:
		return false
	}
	if a[1].Protocol().Code != ma.P_TCP || a[1].Value() == "0" {
		return false
	}
	for _, c := range a[2:] {
		if c.Protocol().Code != ma.P_P2P {
			return false
		}
	}
	return manet.IsPublicAddr(a)
}

// Throttled wraps src so that at most n of its lookups run at once (n < 1 is treated as 1).
// Both waiting for a slot and waiting for the lookup respect ctx: the lookup runs in its own
// goroutine (src.Lookup may ignore ctx and block), and the slot is released only when that
// goroutine finishes, so the in-flight cap holds even after a caller gave up.
func Throttled(src AddrSource, n int) AddrSource {
	if n < 1 {
		n = 1
	}
	sem := make(chan struct{}, n)
	type lookupResult struct {
		addrs []ma.Multiaddr
		err   error
	}
	return AddrSource{
		Name: src.Name,
		Lookup: func(ctx context.Context, id peer.ID) ([]ma.Multiaddr, error) {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			out := make(chan lookupResult, 1)
			go func() {
				defer func() { <-sem }()
				addrs, err := src.Lookup(ctx, id)
				out <- lookupResult{addrs, err}
			}()
			select {
			case r := <-out:
				return r.addrs, r.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
}
