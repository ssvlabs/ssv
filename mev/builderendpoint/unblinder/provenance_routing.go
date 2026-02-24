package unblinder

import (
	"context"
	"sync"
	"time"

	builderapi "github.com/attestantio/go-builder-client/api"
	eth2api "github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"

	"github.com/ssvlabs/ssv/mev/builderendpoint/provenance"
)

type ProvenanceCache interface {
	GetProvenanceByBlockHash(slot phase0.Slot, blockHash phase0.Hash32) (string, bool)
}

// ProvenanceRoutingUnblinder routes unblind requests to the relay that provided the winning bid.
//
// It tries the provenance relay first and only falls back to other relays if needed.
// This reduces unblind fanout I/O in the common case.
type ProvenanceRoutingUnblinder struct {
	Cache     ProvenanceCache
	Providers []UnblindProvider

	// PrimaryHeadStart gives the primary relay a short head start before we fan out to the rest.
	// If 0, we fan out immediately (i.e. no head start).
	PrimaryHeadStart time.Duration

	// Retries is the number of retries per provider (in addition to the first attempt).
	Retries int

	RetryInterval time.Duration
}

func (u *ProvenanceRoutingUnblinder) UnblindBlock(ctx context.Context, block *eth2api.VersionedSignedBlindedBeaconBlock) (*eth2api.VersionedSignedProposal, error) {
	if u == nil {
		return nil, nil
	}
	if len(u.Providers) == 0 {
		return nil, nil
	}
	if block == nil {
		return nil, errors.New("nil blinded beacon block")
	}

	// If we can't derive provenance, fall back to fanout behavior.
	key, ok := provenance.FromBlindedBlock(block)
	if !ok || u.Cache == nil {
		return (&FanoutUnblinder{Providers: u.Providers, Retries: u.Retries, RetryInterval: u.RetryInterval}).UnblindBlock(ctx, block)
	}
	provRelay, ok := u.Cache.GetProvenanceByBlockHash(key.Slot, key.BlockHash)
	if !ok || provRelay == "" {
		return (&FanoutUnblinder{Providers: u.Providers, Retries: u.Retries, RetryInterval: u.RetryInterval}).UnblindBlock(ctx, block)
	}

	var primary UnblindProvider
	others := make([]UnblindProvider, 0, len(u.Providers)-1)
	for _, p := range u.Providers {
		if p.Address() == provRelay && primary == nil {
			primary = p
			continue
		}
		others = append(others, p)
	}
	if primary == nil {
		return (&FanoutUnblinder{Providers: u.Providers, Retries: u.Retries, RetryInterval: u.RetryInterval}).UnblindBlock(ctx, block)
	}

	proposal := toSignedBlindedProposal(block)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	respCh := make(chan *eth2api.VersionedSignedProposal, 1)

	// Start primary.
	primaryDone := make(chan struct{})
	go func() {
		defer close(primaryDone)
		u.tryProvider(ctx, primary, proposal, respCh)
	}()

	headStart := u.PrimaryHeadStart
	if headStart < 0 {
		headStart = 0
	}
	if headStart > 0 {
		select {
		case resp := <-respCh:
			cancel()
			return resp, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(headStart):
		}
	}

	if len(others) == 0 {
		select {
		case resp := <-respCh:
			cancel()
			return resp, nil
		case <-primaryDone:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Fan out to remaining providers (excluding primary).
	var wg sync.WaitGroup
	wg.Add(len(others))
	for _, p := range others {
		provider := p
		go func() {
			defer wg.Done()
			u.tryProvider(ctx, provider, proposal, respCh)
		}()
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		<-primaryDone
		close(doneCh)
	}()

	select {
	case resp := <-respCh:
		cancel()
		return resp, nil
	case <-doneCh:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (u *ProvenanceRoutingUnblinder) tryProvider(ctx context.Context, provider UnblindProvider, proposal *eth2api.VersionedSignedBlindedProposal, respCh chan<- *eth2api.VersionedSignedProposal) {
	attempts := 1 + u.Retries
	if attempts < 1 {
		attempts = 1
	}

	for i := 0; i < attempts; i++ {
		resp, err := provider.UnblindProposal(ctx, &builderapi.UnblindProposalOpts{Proposal: proposal})
		if err == nil && resp != nil && resp.Data != nil {
			select {
			case respCh <- resp.Data:
			default:
			}
			return
		}
		if u.RetryInterval <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(u.RetryInterval):
		}
	}
}
