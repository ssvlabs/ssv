package bids_test

import (
	"context"
	"testing"
	"time"

	builderclient "github.com/attestantio/go-builder-client"
	builderapi "github.com/attestantio/go-builder-client/api"
	buildercapella "github.com/attestantio/go-builder-client/api/capella"
	builderspec "github.com/attestantio/go-builder-client/spec"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	consensuscapella "github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bids"
)

type fakeProvider struct {
	addr     string
	delay    time.Duration
	bids     []*builderspec.VersionedSignedBuilderBid
	call     int
	callHook func()
}

func (p *fakeProvider) Name() string    { return "fake" }
func (p *fakeProvider) Address() string { return p.addr }
func (p *fakeProvider) Pubkey() *phase0.BLSPubKey {
	return nil
}

func (p *fakeProvider) BuilderBid(ctx context.Context, _ *builderapi.BuilderBidOpts) (*builderapi.Response[*builderspec.VersionedSignedBuilderBid], error) {
	if p.callHook != nil {
		p.callHook()
	}
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.delay):
		}
	}
	var bid *builderspec.VersionedSignedBuilderBid
	if len(p.bids) > 0 {
		if p.call < len(p.bids) {
			bid = p.bids[p.call]
		} else {
			bid = p.bids[len(p.bids)-1]
		}
	}
	p.call++
	return &builderapi.Response[*builderspec.VersionedSignedBuilderBid]{Data: bid}, nil
}

func capellaBidWithValue(v uint64) *builderspec.VersionedSignedBuilderBid {
	return &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionCapella,
		Capella: &buildercapella.SignedBuilderBid{
			Message: &buildercapella.BuilderBid{
				Header: &consensuscapella.ExecutionPayloadHeader{},
				Value:  uint256.NewInt(v),
			},
		},
	}
}

func TestDeadlineStrategyPicksBestBidByValue(t *testing.T) {
	t.Parallel()

	providers := []builderclient.BuilderBidProvider{
		&fakeProvider{addr: "relay-a", bids: []*builderspec.VersionedSignedBuilderBid{capellaBidWithValue(1)}},
		&fakeProvider{addr: "relay-b", bids: []*builderspec.VersionedSignedBuilderBid{capellaBidWithValue(10)}},
	}

	s := bids.DeadlineStrategy{
		Deadline: 50 * time.Millisecond,
		BidGap:   10 * time.Millisecond,
	}

	best, prov, err := s.BestBid(context.Background(), time.Now(), providers, 1, phase0.Hash32{1}, phase0.BLSPubKey{2})
	if err != nil {
		t.Fatalf("best bid: %v", err)
	}
	if best == nil {
		t.Fatalf("expected best bid")
	}
	value, err := best.Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	if value.Cmp(uint256.NewInt(10)) != 0 {
		t.Fatalf("unexpected best bid value: got %s want %s", value.String(), uint256.NewInt(10).String())
	}
	if prov != "relay-b" {
		t.Fatalf("unexpected provenance: got %q want %q", prov, "relay-b")
	}
}

func TestDeadlineStrategyRespectsMinValue(t *testing.T) {
	t.Parallel()

	providers := []builderclient.BuilderBidProvider{
		&fakeProvider{addr: "relay-a", bids: []*builderspec.VersionedSignedBuilderBid{capellaBidWithValue(1)}},
	}

	s := bids.DeadlineStrategy{
		Deadline: 50 * time.Millisecond,
		BidGap:   10 * time.Millisecond,
		MinValue: uint256.NewInt(2),
	}

	best, _, err := s.BestBid(context.Background(), time.Now(), providers, 1, phase0.Hash32{1}, phase0.BLSPubKey{2})
	if err != nil {
		t.Fatalf("best bid: %v", err)
	}
	if best != nil {
		t.Fatalf("expected no bid due to min value filter")
	}
}

func TestDeadlineStrategyPollsUntilDeadline(t *testing.T) {
	t.Parallel()

	// Provider returns a low bid first then a higher bid.
	p := &fakeProvider{
		addr: "relay-a",
		bids: []*builderspec.VersionedSignedBuilderBid{
			capellaBidWithValue(1),
			capellaBidWithValue(7),
		},
	}

	s := bids.DeadlineStrategy{
		Deadline: 80 * time.Millisecond,
		BidGap:   10 * time.Millisecond,
	}

	best, _, err := s.BestBid(context.Background(), time.Now(), []builderclient.BuilderBidProvider{p}, 1, phase0.Hash32{1}, phase0.BLSPubKey{2})
	if err != nil {
		t.Fatalf("best bid: %v", err)
	}
	if best == nil {
		t.Fatalf("expected best bid")
	}

	value, err := best.Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	if value.Cmp(uint256.NewInt(7)) != 0 {
		t.Fatalf("unexpected best bid value: got %s want %s", value.String(), uint256.NewInt(7).String())
	}
}
