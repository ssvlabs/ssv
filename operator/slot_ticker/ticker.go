package slot_ticker

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/networkconfig"
)

//go:generate mockgen -package=mocks -destination=./mocks/ticker.go -source=./ticker.go

type Ticker interface {
	// Start ticker process
	Start(logger *zap.Logger)
	// Subscribe to ticker chan
	Subscribe(subscription chan phase0.Slot) event.Subscription
}

type ticker struct {
	ctx     context.Context
	network networkconfig.NetworkConfig

	// chan
	feed *event.Feed
}

// NewTicker returns Ticker struct pointer
func NewTicker(ctx context.Context, network networkconfig.NetworkConfig) Ticker {
	return &ticker{
		ctx:     ctx,
		network: network,
		feed:    &event.Feed{},
	}
}

// Start slot ticker
func (t *ticker) Start(logger *zap.Logger) {
	genesisTime := time.Unix(int64(t.network.Beacon.MinGenesisTime()), 0)
	slotTicker := NewSlotTicker(genesisTime, uint64(t.network.SlotDurationSec().Seconds()))
	t.listenToTicker(logger, slotTicker.C())
}

// Subscribe will trigger every slot
func (t *ticker) Subscribe(subscription chan phase0.Slot) event.Subscription {
	return t.feed.Subscribe(subscription)
}

// listenToTicker loop over the given slot channel
func (t *ticker) listenToTicker(logger *zap.Logger, slots <-chan phase0.Slot) {
	for currentSlot := range slots {
		currentEpoch := t.network.Beacon.EstimatedEpochAtSlot(currentSlot)
		buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, currentSlot%32+1)
		logger.Debug("📅 slot ticker", zap.String("epoch_slot_seq", buildStr))
		if !t.genesisEpochEffective(logger) {
			continue
		}
		// notify current slot to channel
		_ = t.feed.Send(currentSlot)
	}
}

func (t *ticker) genesisEpochEffective(logger *zap.Logger) bool {
	curSlot := t.network.Beacon.EstimatedCurrentSlot()
	genSlot := t.network.Beacon.GetEpochFirstSlot(t.network.GenesisEpoch)
	if curSlot < genSlot {
		if t.network.Beacon.IsFirstSlotOfEpoch(curSlot) {
			// wait until genesis epoch starts
			curEpoch := t.network.Beacon.EstimatedCurrentEpoch()
			gnsTime := t.network.Beacon.GetSlotStartTime(genSlot)
			logger.Info("duties paused, will resume duties on genesis epoch",
				zap.Uint64("genesis_epoch", uint64(t.network.GenesisEpoch)),
				zap.Uint64("current_epoch", uint64(curEpoch)),
				zap.String("genesis_time", gnsTime.Format(time.UnixDate)))
		}
		return false
	}

	return true
}
