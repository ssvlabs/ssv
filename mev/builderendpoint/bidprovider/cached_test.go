package bidprovider_test

import (
	"context"
	"sync/atomic"
	"testing"

	builderdeneb "github.com/attestantio/go-builder-client/api/deneb"
	builderspec "github.com/attestantio/go-builder-client/spec"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	consensusdeneb "github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bidprovider"
	"github.com/ssvlabs/ssv/mev/builderendpoint/domain"
)

type countingProvider struct {
	calls int32
}

func (p *countingProvider) BuilderBid(context.Context, phase0.Slot, phase0.Hash32, phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error) {
	atomic.AddInt32(&p.calls, 1)
	return nil, nil
}

func TestCachedProviderHitAvoidsFallback(t *testing.T) {
	t.Parallel()

	cache := bidcache.New(0)
	key := bidcache.Key{Slot: 1, ParentHash: phase0.Hash32{1}, Pubkey: phase0.BLSPubKey{2}}

	bid := &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &builderdeneb.SignedBuilderBid{
			Message: &builderdeneb.BuilderBid{
				Header: &consensusdeneb.ExecutionPayloadHeader{BaseFeePerGas: uint256.NewInt(0)},
				Value:  uint256.NewInt(1),
			},
		},
	}
	cache.Put(key, bid, "relay-a")

	fallback := &countingProvider{}
	p := &bidprovider.Cached{Cache: cache, Fallback: fallback}

	got, err := p.BuilderBid(context.Background(), key.Slot, key.ParentHash, key.Pubkey)
	if err != nil {
		t.Fatalf("bid: %v", err)
	}
	if got == nil {
		t.Fatalf("expected bid")
	}
	if atomic.LoadInt32(&fallback.calls) != 0 {
		t.Fatalf("expected fallback not called")
	}
}

var _ domain.BidProvider = (*bidprovider.Cached)(nil)
