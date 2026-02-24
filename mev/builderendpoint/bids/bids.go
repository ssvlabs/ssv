package bids

import (
	"context"

	builderspec "github.com/attestantio/go-builder-client/spec"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
)

// GetBid returns a cached bid if present, otherwise fetches the best bid, caches it, and returns it.
//
// Returning (nil, nil) means "no bid available".
func GetBid(ctx context.Context, cache *bidcache.Cache, fetcher bidcache.Fetcher, key bidcache.Key) (*builderspec.VersionedSignedBuilderBid, error) {
	if cache != nil {
		if ent, ok := cache.Get(key); ok {
			return ent.Bid, nil
		}
	}
	if cache == nil || fetcher == nil {
		return nil, nil
	}

	bid, provenance, err := fetcher.FetchBestBid(ctx, key)
	if err != nil {
		return nil, err
	}
	if bid == nil {
		return nil, nil
	}

	cache.Put(key, bid, provenance)
	return bid, nil
}
