package dutyfetcher

import (
	"context"
	"fmt"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

type Fetcher struct {
	logger              *zap.Logger
	closed              chan struct{}
	beaconNode          beacon.BeaconNode
	activeIndicesGetter activeIndicesGetter
	ticker              chan phase0.Slot
	proposer            *ttlcache.Cache[phase0.Epoch, map[phase0.ValidatorIndex]*eth2apiv1.ProposerDuty]
	syncCommittee       *ttlcache.Cache[phase0.Epoch, map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty]
}

// Option defines EventHandler configuration option.
type Option func(*Fetcher)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(f *Fetcher) {
		f.logger = logger.Named(logging.NameDutyFetcher)
	}
}

type activeIndicesGetter interface {
	ActiveValidatorIndices(epoch phase0.Epoch) []phase0.ValidatorIndex
}

func New(
	beaconNode beacon.BeaconNode,
	slotTicker slot_ticker.Ticker,
	activeIndicesGetter activeIndicesGetter,
	opts ...Option,
) *Fetcher {
	tickerChan := make(chan phase0.Slot, 32)
	slotTicker.Subscribe(tickerChan)

	f := &Fetcher{
		logger:              zap.NewNop(),
		closed:              make(chan struct{}),
		beaconNode:          beaconNode,
		ticker:              tickerChan,
		activeIndicesGetter: activeIndicesGetter,
		proposer:            ttlcache.New[phase0.Epoch, map[phase0.ValidatorIndex]*eth2apiv1.ProposerDuty](),
		syncCommittee:       ttlcache.New[phase0.Epoch, map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty](),
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

func (f *Fetcher) Start(ctx context.Context) {
	go f.proposer.Start()
	go f.syncCommittee.Start()

	slotsPerEpoch := phase0.Slot(f.beaconNode.GetBeaconNetwork().SlotsPerEpoch())

	firstRun := true
	go func() {
		for {
			select {
			case <-f.closed:
				return

			case <-ctx.Done():
				return

			case slot := <-f.ticker:
				epochForSlot := f.beaconNode.GetBeaconNetwork().EstimatedEpochAtSlot(slot)

				if firstRun || slot%slotsPerEpoch >= slotsPerEpoch/2 {
					for epoch := epochForSlot - 1; epoch <= epochForSlot+1; epoch++ {
						if err := f.fetchEpoch(ctx, epoch); err != nil {
							f.logger.Warn("failed to fetch epoch duties: %w", fields.Epoch(epoch), zap.Error(err))
						}
					}

					firstRun = false
				}
			}
		}
	}()
}

func (f *Fetcher) ProposerDuty(slot phase0.Slot, validatorIndex phase0.ValidatorIndex) *eth2apiv1.ProposerDuty {
	epoch := f.beaconNode.GetBeaconNetwork().EstimatedEpochAtSlot(slot)

	duties := f.proposer.Get(epoch)
	if duties == nil {
		return nil
	}

	duty, ok := duties.Value()[validatorIndex]
	if !ok {
		return nil
	}

	return duty
}

func (f *Fetcher) SyncCommitteeDuty(slot phase0.Slot, validatorIndex phase0.ValidatorIndex) *eth2apiv1.SyncCommitteeDuty {
	epoch := f.beaconNode.GetBeaconNetwork().EstimatedEpochAtSlot(slot)

	duties := f.syncCommittee.Get(epoch)
	if duties == nil {
		return nil
	}

	duty, ok := duties.Value()[validatorIndex]
	if !ok {
		return nil
	}

	return duty
}

func (f *Fetcher) fetchEpoch(ctx context.Context, epoch phase0.Epoch) error {
	indices := f.activeIndicesGetter.ActiveValidatorIndices(epoch)
	if len(indices) == 0 {
		return nil
	}

	ttl := f.beaconNode.GetBeaconNetwork().SlotDurationSec() * time.Duration(f.beaconNode.GetBeaconNetwork().SlotsPerEpoch()) * 3

	if f.proposer.Get(epoch) == nil {
		proposerDuties, err := f.beaconNode.ProposerDuties(ctx, epoch, indices)
		if err != nil {
			return fmt.Errorf("proposer: %w", err)
		}

		duties := make(map[phase0.ValidatorIndex]*eth2apiv1.ProposerDuty)
		for _, duty := range proposerDuties {
			if duty != nil {
				duties[duty.ValidatorIndex] = duty
			}
		}
		f.proposer.Set(epoch, duties, ttl)
	}

	if f.syncCommittee.Get(epoch) == nil {
		syncCommitteeDuties, err := f.beaconNode.SyncCommitteeDuties(ctx, epoch, indices)
		if err != nil {
			return fmt.Errorf("sync committee: %w", err)
		}

		duties := make(map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty)
		for _, duty := range syncCommitteeDuties {
			if duty != nil {
				duties[duty.ValidatorIndex] = duty
			}
		}
		f.syncCommittee.Set(epoch, duties, ttl)
	}

	return nil
}

func (f *Fetcher) Stop() {
	close(f.closed)
	f.proposer.Stop()
	f.syncCommittee.Stop()
}
