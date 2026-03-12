package bidcache

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	builderdeneb "github.com/attestantio/go-builder-client/api/deneb"
	builderspec "github.com/attestantio/go-builder-client/spec"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	consensusdeneb "github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
)

func TestCachePutGetAndTTLEviction(t *testing.T) {
	t.Parallel()

	const relayA = "relay-a"

	now := time.Unix(1, 0)
	c := New(10 * time.Second)
	c.now = func() time.Time { return now }
	key := Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}

	execBlockHash := phase0.Hash32{7}
	bid := &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &builderdeneb.SignedBuilderBid{
			Message: &builderdeneb.BuilderBid{
				Header: &consensusdeneb.ExecutionPayloadHeader{BaseFeePerGas: uint256.NewInt(0), BlockHash: execBlockHash},
				Value:  uint256.NewInt(1),
			},
		},
	}

	c.Put(key, bid, relayA)
	ent, ok := c.Get(key)
	if !ok || ent.Bid == nil || ent.Provenance != relayA {
		t.Fatalf("expected cache hit")
	}
	if prov, ok2 := c.GetProvenanceByBlockHash(key.Slot, execBlockHash); !ok2 || prov != relayA {
		t.Fatalf("expected provenance hit")
	}

	now = now.Add(11 * time.Second)
	_, ok = c.Get(key)
	if ok {
		t.Fatalf("expected cache miss after TTL")
	}
	if _, ok := c.GetProvenanceByBlockHash(key.Slot, execBlockHash); ok {
		t.Fatalf("expected provenance miss after TTL")
	}
}

func TestCacheCleanupExpiredRemovesEntriesEvenWithoutReads(t *testing.T) {
	t.Parallel()

	const relayA = "relay-a"

	now := time.Unix(1, 0)
	c := New(10 * time.Second)
	c.now = func() time.Time { return now }
	key := Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}

	execBlockHash := phase0.Hash32{7}
	bid := &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &builderdeneb.SignedBuilderBid{
			Message: &builderdeneb.BuilderBid{
				Header: &consensusdeneb.ExecutionPayloadHeader{BaseFeePerGas: uint256.NewInt(0), BlockHash: execBlockHash},
				Value:  uint256.NewInt(1),
			},
		},
	}

	c.Put(key, bid, relayA)

	// Advance time past TTL to make entries expired, then run proactive cleanup.
	now = now.Add(11 * time.Second)
	c.CleanupExpired()

	// Move time backwards: without cleanup, the entry would appear valid again and Get() would hit.
	// Cleanup should have removed it already.
	now = time.Unix(5, 0)
	if _, ok := c.Get(key); ok {
		t.Fatalf("expected cache miss after cleanup")
	}
	if _, ok := c.GetProvenanceByBlockHash(key.Slot, execBlockHash); ok {
		t.Fatalf("expected provenance miss after cleanup")
	}
}

func TestCachePutIfBetterKeepsBestValueAndUpdatesProvenance(t *testing.T) {
	t.Parallel()

	now := time.Unix(1, 0)
	c := New(10 * time.Second)
	c.now = func() time.Time { return now }

	key := Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}

	lowHash := phase0.Hash32{7}
	low := &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &builderdeneb.SignedBuilderBid{
			Message: &builderdeneb.BuilderBid{
				Header: &consensusdeneb.ExecutionPayloadHeader{BaseFeePerGas: uint256.NewInt(0), BlockHash: lowHash},
				Value:  uint256.NewInt(1),
			},
		},
	}

	highHash := phase0.Hash32{8}
	high := &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &builderdeneb.SignedBuilderBid{
			Message: &builderdeneb.BuilderBid{
				Header: &consensusdeneb.ExecutionPayloadHeader{BaseFeePerGas: uint256.NewInt(0), BlockHash: highHash},
				Value:  uint256.NewInt(2),
			},
		},
	}

	first, updated := c.PutIfBetter(key, low, "relay-a")
	if !first || !updated {
		t.Fatalf("expected first insert")
	}

	_, updated = c.PutIfBetter(key, low, "relay-b")
	if updated {
		t.Fatalf("expected no update for equal value")
	}

	first, updated = c.PutIfBetter(key, high, "relay-b")
	if first || !updated {
		t.Fatalf("expected update for higher value")
	}

	ent, ok := c.Get(key)
	if !ok || ent.Bid == nil || ent.Provenance != "relay-b" {
		t.Fatalf("expected updated cache entry")
	}
	val, err := ent.Bid.Value()
	if err != nil || val.Cmp(uint256.NewInt(2)) != 0 {
		t.Fatalf("unexpected cached value: %v (err=%v)", val, err)
	}

	if prov, ok := c.GetProvenanceByBlockHash(key.Slot, lowHash); ok || prov != "" {
		t.Fatalf("expected old provenance mapping to be removed")
	}
	if prov, ok := c.GetProvenanceByBlockHash(key.Slot, highHash); !ok || prov != "relay-b" {
		t.Fatalf("expected provenance for best bid")
	}
}

type fakeFetcher struct {
	calls int32
	bid   *builderspec.VersionedSignedBuilderBid
	block chan struct{}
}

func (f *fakeFetcher) FetchBestBid(ctx context.Context, _ Key) (*builderspec.VersionedSignedBuilderBid, string, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.block != nil {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-f.block:
		}
	}
	return f.bid, "relay-a", nil
}

func TestPrefetcherWarmsCache(t *testing.T) {
	t.Parallel()

	c := New(10 * time.Second)
	key := Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}

	fetcher := &fakeFetcher{
		bid: &builderspec.VersionedSignedBuilderBid{Version: consensusspec.DataVersionDeneb, Deneb: &builderdeneb.SignedBuilderBid{}},
	}

	p := NewPrefetcher(c, fetcher, 10)
	p.Prefetch(context.Background(), key)

	deadline := time.Now().Add(1 * time.Second)
	for {
		if _, ok := c.Get(key); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache not warmed in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&fetcher.calls) != 1 {
		t.Fatalf("expected 1 fetch, got %d", atomic.LoadInt32(&fetcher.calls))
	}
}

func TestPrefetcherInFlightLimit(t *testing.T) {
	t.Parallel()

	c := New(10 * time.Second)
	block := make(chan struct{})
	fetcher := &fakeFetcher{
		bid:   &builderspec.VersionedSignedBuilderBid{Version: consensusspec.DataVersionDeneb, Deneb: &builderdeneb.SignedBuilderBid{}},
		block: block,
	}

	p := NewPrefetcher(c, fetcher, 1)
	p.Prefetch(context.Background(), Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}})
	p.Prefetch(context.Background(), Key{Slot: 1, ParentHash: phase0.Hash32{2}, Pubkey: phase0.BLSPubKey{2}})

	time.Sleep(20 * time.Millisecond)
	if atomic.LoadInt32(&fetcher.calls) != 1 {
		t.Fatalf("expected only 1 in-flight fetch, got %d", atomic.LoadInt32(&fetcher.calls))
	}

	close(block)
}
