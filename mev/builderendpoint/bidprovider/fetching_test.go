package bidprovider_test

import (
	"context"
	"sync/atomic"
	"testing"

	builderspec "github.com/attestantio/go-builder-client/spec"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bidprovider"
)

type countingFetcher struct {
	calls int32
	bid   *builderspec.VersionedSignedBuilderBid
}

func (f *countingFetcher) FetchBestBid(context.Context, bidcache.Key) (*builderspec.VersionedSignedBuilderBid, string, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.bid, "relay-a", nil
}

func TestFetchingCachedStoresOnMiss(t *testing.T) {
	t.Parallel()

	cache := bidcache.New(0)
	fetcher := &countingFetcher{bid: &builderspec.VersionedSignedBuilderBid{Version: consensusspec.DataVersionDeneb}}

	p := &bidprovider.FetchingCached{Cache: cache, Fetcher: fetcher}

	key := bidcache.Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}
	got, err := p.BuilderBid(context.Background(), key.Slot, key.ParentHash, key.Pubkey)
	if err != nil {
		t.Fatalf("bid: %v", err)
	}
	if got == nil {
		t.Fatalf("expected bid")
	}
	if atomic.LoadInt32(&fetcher.calls) != 1 {
		t.Fatalf("expected 1 fetch, got %d", atomic.LoadInt32(&fetcher.calls))
	}

	// Second call should come from cache.
	got2, err := p.BuilderBid(context.Background(), key.Slot, key.ParentHash, key.Pubkey)
	if err != nil {
		t.Fatalf("bid2: %v", err)
	}
	if got2 == nil {
		t.Fatalf("expected bid2")
	}
	if atomic.LoadInt32(&fetcher.calls) != 1 {
		t.Fatalf("expected no additional fetches, got %d", atomic.LoadInt32(&fetcher.calls))
	}
}
