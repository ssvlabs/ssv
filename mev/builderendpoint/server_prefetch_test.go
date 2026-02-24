package builderendpoint

import (
	"context"
	"testing"
	"time"

	builderdeneb "github.com/attestantio/go-builder-client/api/deneb"
	builderspec "github.com/attestantio/go-builder-client/spec"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
)

type fakeFetcher struct {
	bid *builderspec.VersionedSignedBuilderBid
}

func (f *fakeFetcher) FetchBestBid(context.Context, bidcache.Key) (*builderspec.VersionedSignedBuilderBid, string, error) {
	return f.bid, "relay-a", nil
}

func TestServerPrefetchBidWarmsCache(t *testing.T) {
	t.Parallel()

	cache := bidcache.New(10 * time.Second)
	fetcher := &fakeFetcher{bid: &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionDeneb,
		Deneb:   &builderdeneb.SignedBuilderBid{},
	}}
	prefetcher := bidcache.NewPrefetcher(cache, fetcher, 10)

	s := &Server{
		cache:    cache,
		prefetch: prefetcher,
	}

	key := bidcache.Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}
	s.PrefetchBid(context.Background(), key.Slot, key.ParentHash, key.Pubkey)

	deadline := time.Now().Add(1 * time.Second)
	for {
		if _, ok := cache.Get(key); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache not warmed in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
