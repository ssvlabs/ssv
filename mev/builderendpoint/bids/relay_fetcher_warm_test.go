package bids_test

import (
	"context"
	"testing"
	"time"

	builderclient "github.com/attestantio/go-builder-client"
	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bids"
)

type staticFactory struct {
	p builderclient.BuilderBidProvider
}

func (f staticFactory) FetchBidProvider(string) (builderclient.BuilderBidProvider, error) {
	return f.p, nil
}

func TestRelayFetcherOnBidWarmsCacheBeforeDeadlineCompletes(t *testing.T) {
	t.Parallel()

	cache := bidcache.New(10 * time.Second)
	key := bidcache.Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}

	warmed := make(chan struct{})
	p := &fakeProvider{
		addr:  "relay-a",
		delay: 10 * time.Millisecond,
		bids: []*builderspec.VersionedSignedBuilderBid{
			capellaBidWithValue(1),
		},
	}

	fetcher := &bids.RelayFetcher{
		Factory: staticFactory{p: p},
		Relays:  []string{"relay-a"},
		SlotStartTime: func(phase0.Slot) time.Time {
			return time.Now()
		},
		Strategy: bids.DeadlineStrategy{
			Deadline: 200 * time.Millisecond,
			BidGap:   50 * time.Millisecond,
		},
		OnBid: func(_ context.Context, got bidcache.Key, provenance string, bid *builderspec.VersionedSignedBuilderBid) {
			if got != key {
				return
			}
			first, updated := cache.PutIfBetter(got, bid, provenance)
			if first && updated {
				select {
				case <-warmed:
				default:
					close(warmed)
				}
			}
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = fetcher.FetchBestBid(context.Background(), key)
	}()

	select {
	case <-warmed:
		// Expected: first bid caches well before overall deadline.
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected cache to warm before deadline completed")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("fetch did not complete")
	}

	if _, ok := cache.Get(key); !ok {
		t.Fatalf("expected warmed cache entry")
	}
}
