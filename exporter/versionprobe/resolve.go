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
// Waiting for a slot respects ctx.
func Throttled(src AddrSource, n int) AddrSource {
	if n < 1 {
		n = 1
	}
	sem := make(chan struct{}, n)
	return AddrSource{
		Name: src.Name,
		Lookup: func(ctx context.Context, id peer.ID) ([]ma.Multiaddr, error) {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			defer func() { <-sem }()
			return src.Lookup(ctx, id)
		},
	}
}
