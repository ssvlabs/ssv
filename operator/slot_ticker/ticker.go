package slot_ticker

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

//go:generate mockgen -package=mocks -destination=./mocks/ticker.go -source=./ticker.go

type Ticker interface {
	// Start ticker process
	Start(logger *zap.Logger)
	// Subscribe to ticker chan
	Subscribe(subscription chan phase0.Slot) event.Subscription
}

type ticker struct {
	ctx        context.Context
	ethNetwork beaconprotocol.Network

	// chan
	feed *event.Feed
}

// NewTicker returns Ticker struct pointer
func NewTicker(ctx context.Context, ethNetwork beaconprotocol.Network) Ticker {
	return &ticker{
		ctx:        ctx,
		ethNetwork: ethNetwork,
		feed:       &event.Feed{},
	}
}

// Start slot ticker
func (t *ticker) Start(logger *zap.Logger) {
	genesisTime := time.Unix(int64(t.ethNetwork.MinGenesisTime()), 0)
	slotTicker := NewSlotTicker(genesisTime, uint64(t.ethNetwork.SlotDurationSec().Seconds()))
	t.listenToTicker(logger, slotTicker.C())
}

// Subscribe will trigger every slot
func (t *ticker) Subscribe(subscription chan phase0.Slot) event.Subscription {
	return t.feed.Subscribe(subscription)
}

// listenToTicker loop over the given slot channel
func (t *ticker) listenToTicker(logger *zap.Logger, slots <-chan phase0.Slot) {
	for currentSlot := range slots {
		logger.Debug("slot ticker", fields.Slot(currentSlot), zap.Uint64("position", uint64(currentSlot%32+1)))
		if !t.genesisEpochEffective(logger) {
			continue
		}
		// notify current slot to channel
		_ = t.feed.Send(currentSlot)
	}
}

func (t *ticker) genesisEpochEffective(logger *zap.Logger) bool {
	curSlot := t.ethNetwork.EstimatedCurrentSlot()
	genSlot := t.ethNetwork.GetEpochFirstSlot(t.ethNetwork.GenesisEpoch)
	if curSlot < genSlot {
		if t.ethNetwork.IsFirstSlotOfEpoch(curSlot) {
			// wait until genesis epoch starts
			curEpoch := t.ethNetwork.EstimatedCurrentEpoch()
			gnsTime := t.ethNetwork.GetSlotStartTime(genSlot)
			logger.Info("duties paused, will resume duties on genesis epoch",
				zap.Uint64("genesis_epoch", uint64(t.ethNetwork.GenesisEpoch)),
				zap.Uint64("current_epoch", uint64(curEpoch)),
				zap.String("genesis_time", gnsTime.Format(time.UnixDate)))
		}
		return false
	}

	return true
}
