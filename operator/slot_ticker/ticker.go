package slot_ticker

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

//go:generate mockgen -package=mocks -destination=./mocks/ticker.go -source=./ticker.go

type Ticker interface {
	// Start ticker process
	Start()
	// Subscribe to ticker chan
	Subscribe(subscription chan phase0.Slot) event.Subscription
}

type ticker struct {
	logger       *zap.Logger
	ctx          context.Context
	ethNetwork   beaconprotocol.Network
	genesisEpoch phase0.Epoch

	// chan
	feed *event.Feed
}

// NewTicker returns Ticker struct pointer
func NewTicker(ctx context.Context, logger *zap.Logger, ethNetwork beaconprotocol.Network, genesisEpoch phase0.Epoch) Ticker {
	return &ticker{
		logger:       logger,
		ctx:          ctx,
		ethNetwork:   ethNetwork,
		genesisEpoch: genesisEpoch,
		feed:         &event.Feed{},
	}
}

// Start slot ticker
func (t *ticker) Start() {
	genesisTime := time.Unix(int64(t.ethNetwork.MinGenesisTime()), 0)
	slotTicker := NewSlotTicker(genesisTime, uint64(t.ethNetwork.SlotDurationSec().Seconds()))
	t.listenToTicker(slotTicker.C())
}

// Subscribe will trigger every slot
func (t *ticker) Subscribe(subscription chan phase0.Slot) event.Subscription {
	return t.feed.Subscribe(subscription)
}

// listenToTicker loop over the given slot channel
func (t *ticker) listenToTicker(slots <-chan phase0.Slot) {
	for currentSlot := range slots {
		t.logger.Debug("slot ticker", zap.Uint64("slot", uint64(currentSlot)))
		if !t.genesisEpochEffective() {
			continue
		}
		// notify current slot to channel
		_ = t.feed.Send(currentSlot)
	}
}

func (t *ticker) genesisEpochEffective() bool {
	curSlot := t.ethNetwork.EstimatedCurrentSlot()
	genSlot := t.ethNetwork.GetEpochFirstSlot(t.genesisEpoch)
	if curSlot < genSlot {
		if t.ethNetwork.IsFirstSlotOfEpoch(curSlot) {
			// wait until genesis epoch starts
			curEpoch := t.ethNetwork.EstimatedCurrentEpoch()
			gnsTime := t.ethNetwork.GetSlotStartTime(genSlot)
			t.logger.Info("duties paused, will resume duties on genesis epoch",
				zap.Uint64("genesis_epoch", uint64(t.genesisEpoch)),
				zap.Uint64("current_epoch", uint64(curEpoch)),
				zap.String("genesis_time", gnsTime.Format(time.UnixDate)))
		}
		return false
	}

	return true
}
