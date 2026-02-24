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
