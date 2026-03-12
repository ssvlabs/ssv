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
	observer    PrefetchObserver

	mu       sync.Mutex
	inFlight map[Key]struct{}
}

type PrefetchObserver interface {
	OnPrefetchRequest(ctx context.Context)
	OnPrefetchSkip(ctx context.Context, reason string)
	OnPrefetchResult(ctx context.Context, result string)
}

type PrefetcherOption func(*Prefetcher)

func WithPrefetchObserver(observer PrefetchObserver) PrefetcherOption {
	return func(p *Prefetcher) {
		p.observer = observer
	}
}

func NewPrefetcher(cache *Cache, fetcher Fetcher, maxInFlight int, opts ...PrefetcherOption) *Prefetcher {
	p := &Prefetcher{
		cache:       cache,
		fetcher:     fetcher,
		maxInFlight: maxInFlight,
		inFlight:    make(map[Key]struct{}),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

func (p *Prefetcher) Prefetch(ctx context.Context, key Key) {
	if p == nil || p.cache == nil || p.fetcher == nil {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}
	recordPrefetchRequest(ctx)
	if p.observer != nil {
		p.observer.OnPrefetchRequest(ctx)
	}

	// Skip if already warm. Prefetching can be triggered multiple times (e.g. duty refetches).
	if _, ok := p.cache.Get(key); ok {
		recordPrefetchSkip(ctx, prefetchSkipReasonWarm)
		if p.observer != nil {
			p.observer.OnPrefetchSkip(ctx, string(prefetchSkipReasonWarm))
		}
		return
	}

	p.mu.Lock()
	if _, ok := p.inFlight[key]; ok {
		p.mu.Unlock()
		recordPrefetchSkip(ctx, prefetchSkipReasonInFlight)
		if p.observer != nil {
			p.observer.OnPrefetchSkip(ctx, string(prefetchSkipReasonInFlight))
		}
		return
	}
	if p.maxInFlight > 0 && len(p.inFlight) >= p.maxInFlight {
		p.mu.Unlock()
		recordPrefetchSkip(ctx, prefetchSkipReasonLimit)
		if p.observer != nil {
			p.observer.OnPrefetchSkip(ctx, string(prefetchSkipReasonLimit))
		}
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
			recordPrefetchResult(ctx, prefetchResultError)
			if p.observer != nil {
				p.observer.OnPrefetchResult(ctx, string(prefetchResultError))
			}
			return
		}
		if bid == nil {
			recordPrefetchResult(ctx, prefetchResultNoBid)
			if p.observer != nil {
				p.observer.OnPrefetchResult(ctx, string(prefetchResultNoBid))
			}
			return
		}

		p.cache.PutIfBetter(key, bid, provenance)
		recordPrefetchResult(ctx, prefetchResultCached)
		if p.observer != nil {
			p.observer.OnPrefetchResult(ctx, string(prefetchResultCached))
		}
	}()
}

func (p *Prefetcher) InFlight() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.inFlight)
}
