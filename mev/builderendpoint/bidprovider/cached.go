package bidprovider

import (
	"context"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/domain"
)

// Cached wraps an underlying BidProvider with a Cache lookup.
// If a cache hit exists it is returned immediately; otherwise the call is delegated to Fallback.
type Cached struct {
	Cache    *bidcache.Cache
	Fallback domain.BidProvider
}

func (c *Cached) BuilderBid(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error) {
	if c != nil && c.Cache != nil {
		if ent, ok := c.Cache.Get(bidcache.Key{Slot: slot, ParentHash: parentHash, Pubkey: pubkey}); ok {
			return ent.Bid, nil
		}
	}
	if c == nil || c.Fallback == nil {
		return nil, nil
	}
	return c.Fallback.BuilderBid(ctx, slot, parentHash, pubkey)
}
