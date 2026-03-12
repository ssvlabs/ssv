package bids

import (
	"context"
	"time"

	builderclient "github.com/attestantio/go-builder-client"
	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
)

type BidProviderFactory interface {
	FetchBidProvider(address string) (builderclient.BuilderBidProvider, error)
}

// RelayFetcher obtains the best bid across a configured relay set using a strategy.
type RelayFetcher struct {
	Factory       BidProviderFactory
	Relays        []string
	SlotStartTime func(phase0.Slot) time.Time
	Strategy      DeadlineStrategy

	// OnBid is an optional callback invoked on every successfully fetched (non-empty) bid during polling.
	// This is primarily intended for cache warming during background prefetching.
	OnBid func(ctx context.Context, key bidcache.Key, provenance string, bid *builderspec.VersionedSignedBuilderBid)
}

func (f *RelayFetcher) FetchBestBid(ctx context.Context, key bidcache.Key) (*builderspec.VersionedSignedBuilderBid, string, error) {
	if f == nil || f.Factory == nil || len(f.Relays) == 0 {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	providers := make([]builderclient.BuilderBidProvider, 0, len(f.Relays))
	for _, relay := range f.Relays {
		p, err := f.Factory.FetchBidProvider(relay)
		if err != nil {
			continue
		}
		providers = append(providers, p)
	}
	if len(providers) == 0 {
		return nil, "", nil
	}

	slotStart := time.Now()
	if f.SlotStartTime != nil {
		slotStart = f.SlotStartTime(key.Slot)
	}

	if f.OnBid == nil {
		return f.Strategy.BestBid(ctx, slotStart, providers, key.Slot, key.ParentHash, key.Pubkey)
	}

	observer := func(p builderclient.BuilderBidProvider, bid *builderspec.VersionedSignedBuilderBid) {
		if bid == nil || bid.IsEmpty() {
			return
		}
		f.OnBid(ctx, key, p.Address(), bid)
	}
	return f.Strategy.BestBidWithObserver(ctx, slotStart, providers, key.Slot, key.ParentHash, key.Pubkey, observer)
}

var _ bidcache.Fetcher = (*RelayFetcher)(nil)
