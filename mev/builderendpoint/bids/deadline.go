package bids

import (
	"context"
	"sync"
	"time"

	builderclient "github.com/attestantio/go-builder-client"
	builderapi "github.com/attestantio/go-builder-client/api"
	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
)

// DeadlineStrategy polls each relay for bids until a deadline (relative to slot start), keeping the best bid by value.
type DeadlineStrategy struct {
	Deadline time.Duration
	BidGap   time.Duration

	// MinValue is an optional minimum bid value filter.
	// If set, bids with value < MinValue are ignored.
	MinValue *uint256.Int
}

type result struct {
	provider string
	bid      *builderspec.VersionedSignedBuilderBid
}

func (s DeadlineStrategy) BestBid(
	ctx context.Context,
	slotStart time.Time,
	providers []builderclient.BuilderBidProvider,
	slot phase0.Slot,
	parentHash phase0.Hash32,
	pubkey phase0.BLSPubKey,
) (*builderspec.VersionedSignedBuilderBid, string, error) {
	if len(providers) == 0 {
		return nil, "", nil
	}

	if s.BidGap <= 0 {
		s.BidGap = 50 * time.Millisecond
	}
	if s.Deadline <= 0 {
		s.Deadline = 750 * time.Millisecond
	}

	deadlineTime := slotStart.Add(s.Deadline)
	ctx, cancel := context.WithDeadline(ctx, deadlineTime)
	defer cancel()

	resCh := make(chan result, len(providers))

	var wg sync.WaitGroup
	wg.Add(len(providers))
	for _, p := range providers {
		provider := p
		go func() {
			defer wg.Done()
			s.pollProvider(ctx, deadlineTime, provider, slot, parentHash, pubkey, resCh)
		}()
	}

	// Close channel when all providers exit.
	go func() {
		wg.Wait()
		close(resCh)
	}()

	var best *builderspec.VersionedSignedBuilderBid
	var bestProvider string
	for res := range resCh {
		if res.bid == nil || res.bid.IsEmpty() {
			continue
		}

		value, err := res.bid.Value()
		if err != nil {
			continue
		}
		if s.MinValue != nil && value.Cmp(s.MinValue) < 0 {
			continue
		}

		if best == nil {
			best = res.bid
			bestProvider = res.provider
			continue
		}
		bestValue, err := best.Value()
		if err != nil {
			best = res.bid
			bestProvider = res.provider
			continue
		}
		if value.Cmp(bestValue) > 0 {
			best = res.bid
			bestProvider = res.provider
		}
	}

	return best, bestProvider, nil
}

func (s DeadlineStrategy) pollProvider(
	ctx context.Context,
	deadline time.Time,
	provider builderclient.BuilderBidProvider,
	slot phase0.Slot,
	parentHash phase0.Hash32,
	pubkey phase0.BLSPubKey,
	resCh chan<- result,
) {
	for {
		bid, err := fetchBid(ctx, provider, slot, parentHash, pubkey)
		if err == nil {
			resCh <- result{provider: provider.Address(), bid: bid}
		}

		if time.Until(deadline) <= s.BidGap {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(s.BidGap):
		}
	}
}

func fetchBid(
	ctx context.Context,
	provider builderclient.BuilderBidProvider,
	slot phase0.Slot,
	parentHash phase0.Hash32,
	pubkey phase0.BLSPubKey,
) (*builderspec.VersionedSignedBuilderBid, error) {
	resp, err := provider.BuilderBid(ctx, &builderapi.BuilderBidOpts{
		Slot:       slot,
		ParentHash: parentHash,
		PubKey:     pubkey,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Data == nil {
		return nil, nil
	}
	return resp.Data, nil
}
