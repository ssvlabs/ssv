package bidfetcher

import (
	"context"
	"time"

	builderclient "github.com/attestantio/go-builder-client"
	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bidstrategy"
)

type BidProviderFactory interface {
	FetchBidProvider(address string) (builderclient.BuilderBidProvider, error)
}

type RelayFetcher struct {
	Factory       BidProviderFactory
	Relays        []string
	SlotStartTime func(phase0.Slot) time.Time
	Strategy      bidstrategy.DeadlineStrategy
}

func (f *RelayFetcher) FetchBestBid(ctx context.Context, key bidcache.Key) (*builderspec.VersionedSignedBuilderBid, string, error) {
	if f == nil || f.Factory == nil || len(f.Relays) == 0 {
		return nil, "", nil
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

	return f.Strategy.BestBid(ctx, slotStart, providers, key.Slot, key.ParentHash, key.Pubkey)
}
