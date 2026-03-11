package bids

import (
	"context"
	"fmt"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"golang.org/x/sync/singleflight"

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

type getBidResult struct {
	bid *builderspec.VersionedSignedBuilderBid
}

// GetBidSingleflight is like GetBid, but coalesces concurrent cache misses for the same key.
//
// This avoids redundant relay polling when multiple beacon clients hit the builder endpoint simultaneously.
func GetBidSingleflight(
	ctx context.Context,
	cache *bidcache.Cache,
	fetcher bidcache.Fetcher,
	sf *singleflight.Group,
	key bidcache.Key,
) (*builderspec.VersionedSignedBuilderBid, error) {
	if sf == nil {
		return GetBid(ctx, cache, fetcher, key)
	}

	// Fast path: warm cache.
	if cache != nil {
		if ent, ok := cache.Get(key); ok {
			return ent.Bid, nil
		}
	}

	v, err, _ := sf.Do(singleflightKey(key), func() (any, error) {
		bid, err := GetBid(ctx, cache, fetcher, key)
		return getBidResult{bid: bid}, err
	})
	if err != nil {
		return nil, err
	}
	res := v.(getBidResult)
	return res.bid, nil
}

func singleflightKey(key bidcache.Key) string {
	return fmt.Sprintf("%d:%x:%x", uint64(key.Slot), key.ParentHash[:], key.Pubkey[:])
}
