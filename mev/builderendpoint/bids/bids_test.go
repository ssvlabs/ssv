package bids_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"golang.org/x/sync/singleflight"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bids"
)

type countingFetcher struct {
	calls int32
	bid   *builderspec.VersionedSignedBuilderBid
	delay time.Duration
}

func (f *countingFetcher) FetchBestBid(context.Context, bidcache.Key) (*builderspec.VersionedSignedBuilderBid, string, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return f.bid, "relay-a", nil
}

func TestGetBidCachesOnMiss(t *testing.T) {
	t.Parallel()

	cache := bidcache.New(0)
	fetcher := &countingFetcher{bid: &builderspec.VersionedSignedBuilderBid{Version: consensusspec.DataVersionDeneb}}

	key := bidcache.Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}

	got, err := bids.GetBid(context.Background(), cache, fetcher, key)
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
	got2, err := bids.GetBid(context.Background(), cache, fetcher, key)
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

func TestGetBidSingleflightCoalescesConcurrentMisses(t *testing.T) {
	t.Parallel()

	cache := bidcache.New(0)
	fetcher := &countingFetcher{
		bid:   &builderspec.VersionedSignedBuilderBid{Version: consensusspec.DataVersionDeneb},
		delay: 50 * time.Millisecond,
	}
	key := bidcache.Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}

	var sf singleflight.Group

	start := make(chan struct{})
	const n = 16
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		go func() {
			<-start
			bid, err := bids.GetBidSingleflight(context.Background(), cache, fetcher, &sf, key)
			if err != nil {
				errCh <- err
				return
			}
			if bid == nil {
				errCh <- context.Canceled
				return
			}
			errCh <- nil
		}()
	}
	close(start)

	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("call failed: %v", err)
		}
	}

	if atomic.LoadInt32(&fetcher.calls) != 1 {
		t.Fatalf("expected 1 fetch, got %d", atomic.LoadInt32(&fetcher.calls))
	}
}
