package versionprobe

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// AddrSource is one place to look up a peer's dialable addresses (e.g. the discovered-peers
// pool, the main host's peerstore, a discv5 lookup). Lookup must not dial the peer.
type AddrSource struct {
	Name   string
	Lookup func(ctx context.Context, id peer.ID) ([]ma.Multiaddr, error)
}

// Resolve tries sources in order and returns the first non-empty address list together with
// the name of the source that produced it. Sources that fail or return no addresses are
// skipped. If no source yields addresses, it returns the last error seen (nil when every
// source was simply empty). It stops with ctx.Err() as soon as ctx is done.
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
		if len(got) > 0 {
			return got, s.Name, nil
		}
	}
	return nil, "", lastErr
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
