package unblinder

import (
	"context"
	"sync"
	"time"

	builderapi "github.com/attestantio/go-builder-client/api"
	eth2api "github.com/attestantio/go-eth2-client/api"
	"github.com/pkg/errors"
)

// FanoutUnblinder calls multiple relays in parallel and returns the first successful unblinded proposal.
//
// This intentionally follows a "first success wins" model (like vouch) for MVP, and does not require
// provenance (that comes later with header caching).
type FanoutUnblinder struct {
	Providers []UnblindProvider

	// Retries is the number of retries per provider (in addition to the first attempt).
	// Keep this low and bounded by timeouts.
	Retries int

	RetryInterval time.Duration
}

// UnblindProvider is the minimal interface required for unblinding.
// `builderclient.UnblindedProposalProvider` satisfies this.
type UnblindProvider interface {
	Address() string
	UnblindProposal(ctx context.Context, opts *builderapi.UnblindProposalOpts) (*builderapi.Response[*eth2api.VersionedSignedProposal], error)
}

func (u *FanoutUnblinder) UnblindBlock(ctx context.Context, block *eth2api.VersionedSignedBlindedBeaconBlock) (*eth2api.VersionedSignedProposal, error) {
	start := time.Now()
	requestCtx := ctx

	if u == nil {
		return nil, nil
	}
	if len(u.Providers) == 0 {
		return nil, nil
	}
	if block == nil {
		err := errors.New("nil blinded beacon block")
		recordUnblind(ctx, unblindModeFanout, unblindResultError, time.Since(start))
		return nil, err
	}

	proposal := toSignedBlindedProposal(block)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	respCh := make(chan *eth2api.VersionedSignedProposal, 1)
	doneCh := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(len(u.Providers))

	for _, provider := range u.Providers {
		p := provider
		go func() {
			defer wg.Done()
			resp := unblindWithRetries(ctx, p, proposal, u.Retries, u.RetryInterval)
			if resp == nil {
				return
			}
			select {
			case respCh <- resp:
			default:
			}
		}()
	}

	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case resp := <-respCh:
		cancel()
		recordUnblind(requestCtx, unblindModeFanout, unblindResultSuccess, time.Since(start))
		return resp, nil
	case <-doneCh:
		recordUnblind(requestCtx, unblindModeFanout, unblindResultNoPayload, time.Since(start))
		return nil, nil
	case <-requestCtx.Done():
		recordUnblind(requestCtx, unblindModeFanout, unblindResultError, time.Since(start))
		return nil, requestCtx.Err()
	}
}

func toSignedBlindedProposal(block *eth2api.VersionedSignedBlindedBeaconBlock) *eth2api.VersionedSignedBlindedProposal {
	if block == nil {
		return nil
	}
	return &eth2api.VersionedSignedBlindedProposal{
		Version:   block.Version,
		Bellatrix: block.Bellatrix,
		Capella:   block.Capella,
		Deneb:     block.Deneb,
		Electra:   block.Electra,
		Fulu:      block.Fulu,
	}
}
