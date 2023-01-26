package slot_ticker

import (
	"context"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/prysm/async/event"
	"github.com/prysmaticlabs/prysm/time/slots"
	"go.uber.org/zap"
	"time"
)

//go:generate mockgen -package=mocks -destination=./mocks/ticker.go -source=./ticker.go

type Ticker interface {
	// Start ticker process
	Start()
	// Subscribe to ticker chan
	Subscribe(subscription chan types.Slot) event.Subscription
}

type ticker struct {
	logger     *zap.Logger
	ctx        context.Context
	ethNetwork beaconprotocol.Network

	// chan
	feed *event.Feed
}

// NewTicker returns Ticker struct pointer
func NewTicker(ctx context.Context, logger *zap.Logger, ethNetwork beaconprotocol.Network) Ticker {
	return &ticker{
		logger:     logger,
		ctx:        ctx,
		ethNetwork: ethNetwork,
		feed:       &event.Feed{},
	}
}

// Start slot ticker
func (t *ticker) Start() {
	genesisTime := time.Unix(int64(t.ethNetwork.MinGenesisTime()), 0)
	slotTicker := slots.NewSlotTicker(genesisTime, uint64(t.ethNetwork.SlotDurationSec().Seconds()))
	t.listenToTicker(slotTicker.C())
}

// Subscribe will trigger every slot
func (t *ticker) Subscribe(subscription chan types.Slot) event.Subscription {
	return t.feed.Subscribe(subscription)
}

// listenToTicker loop over the given slot channel
func (t *ticker) listenToTicker(slots <-chan types.Slot) {
	for currentSlot := range slots {
		// notify current slot to channel
		count := t.feed.Send(currentSlot)
		t.logger.Debug("slot ticker", zap.Uint64("slot", uint64(currentSlot)), zap.Int("subscribers", count))
	}
}
