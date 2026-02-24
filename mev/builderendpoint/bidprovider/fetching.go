package bidprovider

import (
	"context"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
)

// FetchingCached consults the cache and, on miss, fetches and stores the result.
type FetchingCached struct {
	Cache   *bidcache.Cache
	Fetcher bidcache.Fetcher
}

func (p *FetchingCached) BuilderBid(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error) {
	key := bidcache.Key{Slot: slot, ParentHash: parentHash, Pubkey: pubkey}

	if p != nil && p.Cache != nil {
		if ent, ok := p.Cache.Get(key); ok {
			return ent.Bid, nil
		}
	}
	if p == nil || p.Fetcher == nil || p.Cache == nil {
		return nil, nil
	}

	bid, provenance, err := p.Fetcher.FetchBestBid(ctx, key)
	if err != nil {
		return nil, err
	}
	if bid == nil {
		return nil, nil
	}

	p.Cache.Put(key, bid, provenance)
	return bid, nil
}
