package bidcache

import (
	"context"
	"sync"

	builderspec "github.com/attestantio/go-builder-client/spec"
)

// Fetcher fetches the best bid for a key and returns it along with provenance (relay address) if known.
type Fetcher interface {
	FetchBestBid(ctx context.Context, key Key) (*builderspec.VersionedSignedBuilderBid, string, error)
}

// Prefetcher runs background fetches and stores results in Cache.
type Prefetcher struct {
	cache *Cache

	fetcher     Fetcher
	maxInFlight int

	mu       sync.Mutex
	inFlight map[Key]struct{}
}

func NewPrefetcher(cache *Cache, fetcher Fetcher, maxInFlight int) *Prefetcher {
	return &Prefetcher{
		cache:       cache,
		fetcher:     fetcher,
		maxInFlight: maxInFlight,
		inFlight:    make(map[Key]struct{}),
	}
}

func (p *Prefetcher) Prefetch(ctx context.Context, key Key) {
	if p == nil || p.cache == nil || p.fetcher == nil {
		return
	}

	p.mu.Lock()
	if _, ok := p.inFlight[key]; ok {
		p.mu.Unlock()
		return
	}
	if p.maxInFlight > 0 && len(p.inFlight) >= p.maxInFlight {
		p.mu.Unlock()
		return
	}
	p.inFlight[key] = struct{}{}
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.inFlight, key)
			p.mu.Unlock()
		}()

		bid, provenance, err := p.fetcher.FetchBestBid(ctx, key)
		if err != nil {
			return
		}
		if bid == nil {
			return
		}
		p.cache.Put(key, bid, provenance)
	}()
}
