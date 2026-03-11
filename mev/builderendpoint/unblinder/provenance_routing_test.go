package unblinder_test

import (
	"context"
	"encoding/binary"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	builderapi "github.com/attestantio/go-builder-client/api"
	eth2api "github.com/attestantio/go-eth2-client/api"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	consensusdeneb "github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/unblinder"
)

type fakeProvCache struct {
	m map[[40]byte]string
}

func cacheKey(slot phase0.Slot, blockHash phase0.Hash32) [40]byte {
	var k [40]byte
	binary.LittleEndian.PutUint64(k[0:8], uint64(slot))
	copy(k[8:], blockHash[:])
	return k
}

func (c *fakeProvCache) GetProvenanceByBlockHash(slot phase0.Slot, blockHash phase0.Hash32) (string, bool) {
	if c == nil {
		return "", false
	}
	v, ok := c.m[cacheKey(slot, blockHash)]
	return v, ok
}

type countingProvider struct {
	address string
	delay   time.Duration
	resp    *eth2api.VersionedSignedProposal
	err     error

	calls   int32
	started chan struct{}
}

func (p *countingProvider) Address() string { return p.address }

func (p *countingProvider) UnblindProposal(ctx context.Context, _ *builderapi.UnblindProposalOpts) (*builderapi.Response[*eth2api.VersionedSignedProposal], error) {
	atomic.AddInt32(&p.calls, 1)
	if p.started != nil {
		select {
		case <-p.started:
		default:
			close(p.started)
		}
	}
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.delay):
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return &builderapi.Response[*eth2api.VersionedSignedProposal]{Data: p.resp}, nil
}

func denebBlindedBlock(slot phase0.Slot, hash phase0.Hash32) *eth2api.VersionedSignedBlindedBeaconBlock {
	return &eth2api.VersionedSignedBlindedBeaconBlock{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlindedBeaconBlock{
			Message: &apiv1deneb.BlindedBeaconBlock{
				Slot: slot,
				Body: &apiv1deneb.BlindedBeaconBlockBody{
					ExecutionPayloadHeader: &consensusdeneb.ExecutionPayloadHeader{
						BlockHash: hash,
					},
				},
			},
		},
	}
}

func TestProvenanceRoutingUnblinder_PrimaryWins_NoFallbackCalls(t *testing.T) {
	t.Parallel()

	slot := phase0.Slot(1)
	hash := phase0.Hash32{9}

	cache := &fakeProvCache{
		m: map[[40]byte]string{
			cacheKey(slot, hash): "primary",
		},
	}

	primary := &countingProvider{
		address: "primary",
		delay:   5 * time.Millisecond,
		resp:    &eth2api.VersionedSignedProposal{},
	}
	fallback := &countingProvider{
		address: "fallback",
		resp:    &eth2api.VersionedSignedProposal{},
	}

	u := &unblinder.ProvenanceRoutingUnblinder{
		Cache:            cache,
		Providers:        []unblinder.UnblindProvider{primary, fallback},
		PrimaryHeadStart: 50 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	got, err := u.UnblindBlock(ctx, denebBlindedBlock(slot, hash))
	if err != nil {
		t.Fatalf("unblind: %v", err)
	}
	if got == nil {
		t.Fatalf("expected proposal")
	}
	if atomic.LoadInt32(&primary.calls) == 0 {
		t.Fatalf("expected primary to be called")
	}
	if atomic.LoadInt32(&fallback.calls) != 0 {
		t.Fatalf("expected fallback not to be called, got %d", atomic.LoadInt32(&fallback.calls))
	}
}

func TestProvenanceRoutingUnblinder_PrimaryFails_FallsBack(t *testing.T) {
	t.Parallel()

	slot := phase0.Slot(2)
	hash := phase0.Hash32{8}

	cache := &fakeProvCache{
		m: map[[40]byte]string{
			cacheKey(slot, hash): "primary",
		},
	}

	primary := &countingProvider{
		address: "primary",
		delay:   5 * time.Millisecond,
		err:     errors.New("boom"),
	}
	fallback := &countingProvider{
		address: "fallback",
		delay:   5 * time.Millisecond,
		resp:    &eth2api.VersionedSignedProposal{},
	}

	u := &unblinder.ProvenanceRoutingUnblinder{
		Cache:            cache,
		Providers:        []unblinder.UnblindProvider{primary, fallback},
		PrimaryHeadStart: 10 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	got, err := u.UnblindBlock(ctx, denebBlindedBlock(slot, hash))
	if err != nil {
		t.Fatalf("unblind: %v", err)
	}
	if got == nil {
		t.Fatalf("expected proposal")
	}
	if atomic.LoadInt32(&primary.calls) == 0 || atomic.LoadInt32(&fallback.calls) == 0 {
		t.Fatalf("expected both primary and fallback to be called")
	}
}

func TestProvenanceRoutingUnblinder_NoProvenance_Fanout(t *testing.T) {
	t.Parallel()

	p1Started := make(chan struct{})
	p2Started := make(chan struct{})

	p1 := &countingProvider{address: "a", delay: 5 * time.Millisecond, resp: &eth2api.VersionedSignedProposal{}, started: p1Started}
	p2 := &countingProvider{address: "b", delay: 20 * time.Millisecond, err: errors.New("boom"), started: p2Started}

	u := &unblinder.ProvenanceRoutingUnblinder{
		Cache:     &fakeProvCache{m: map[[40]byte]string{}},
		Providers: []unblinder.UnblindProvider{p1, p2},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	// Missing execution payload header -> provenance extraction fails -> fanout.
	block := &eth2api.VersionedSignedBlindedBeaconBlock{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlindedBeaconBlock{
			Message: &apiv1deneb.BlindedBeaconBlock{
				Slot: 1,
				Body: &apiv1deneb.BlindedBeaconBlockBody{},
			},
		},
	}

	got, err := u.UnblindBlock(ctx, block)
	if err != nil {
		t.Fatalf("unblind: %v", err)
	}
	if got == nil {
		t.Fatalf("expected proposal")
	}

	waitStarted := func(ch chan struct{}) {
		select {
		case <-ch:
		case <-time.After(250 * time.Millisecond):
			t.Fatalf("provider did not start in time")
		}
	}
	waitStarted(p1Started)
	waitStarted(p2Started)
}
