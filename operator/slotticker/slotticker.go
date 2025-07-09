package slotticker

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/utils/casts"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/slotticker.go -source=./slotticker.go

type Provider func() SlotTicker

// SlotTicker provides a way to keep track of Ethereum slots as they change over time.
// Note, the caller is RESPONSIBLE for calling Next method periodically in order for
// SlotTicker to advance forward (to keep ticking) to newer slots.
type SlotTicker interface {
	// Next returns a channel that will relay 1 tick signaling that "freshest" slot has started.
	// It advances slot number SlotTicker keeps track of (potentially jumping several slots ahead)
	// and returns a channel that will signal once the time corresponding to that "freshest" slot
	// comes.
	Next() <-chan time.Time
	// Slot returns the slot number that corresponds to Next.
	Slot() phase0.Slot
}

type Config struct {
	SlotDuration time.Duration
	GenesisTime  time.Time
}

// slotTicker implements SlotTicker.
// Note, this implementation is NOT thread-safe, hence all of its methods should be called
// in a serialized fashion (concurrent calls can result in unexpected behavior).
type slotTicker struct {
	logger *zap.Logger
	timer  Timer
	// slotDuration is blockchain slot duration (e.g. 12s for Ethereum mainnet).
	slotDuration time.Duration
	// genesisTime is blockchain genesis time (time that corresponds to the very first initial slot).
	genesisTime time.Time
	// nextSlot is the next slot number slotTicker will tick on.
	nextSlot phase0.Slot
}

// New returns a goroutine-free SlotTicker implementation which is not thread-safe.
func New(logger *zap.Logger, cfg Config) *slotTicker {
	return newWithCustomTimer(logger, cfg, NewTimer)
}

func newWithCustomTimer(logger *zap.Logger, cfg Config, timerProvider TimerProvider) *slotTicker {
	timeSinceGenesis := time.Since(cfg.GenesisTime)

	var (
		initialDelay time.Duration
		initialSlot  int64
	)
	if timeSinceGenesis < 0 {
		initialSlot = 0                  // start at slot 0
		initialDelay = -timeSinceGenesis // wait until the genesis slot comes
	} else {
		initialSlot = int64(timeSinceGenesis / cfg.SlotDuration) // start at genesis slot
		initialDelay = 0                                         // no need to wait since we are at or past genesis slot already
	}

	return &slotTicker{
		logger:       logger.Named("slot_ticker"),
		timer:        timerProvider(initialDelay),
		slotDuration: cfg.SlotDuration,
		genesisTime:  cfg.GenesisTime,
		nextSlot:     phase0.Slot(initialSlot), //nolint: gosec
	}
}

// Next implements SlotTicker.Next.
// Note, this method is not thread-safe.
func (s *slotTicker) Next() <-chan time.Time {
	timeSinceGenesis := time.Since(s.genesisTime)
	if timeSinceGenesis < 0 {
		// we are waiting for slotTicker to tick at s.genesisTime (signaling 0th slot start)
		return s.timer.C()
	}
	if !s.timer.Stop() {
		// try to drain the channel, but don't block if there's no value
		select {
		case <-s.timer.C():
		default:
		}
	}
	nextSlot := phase0.Slot(timeSinceGenesis/s.slotDuration) + 1 // #nosec G115
	if nextSlot <= s.nextSlot {
		// we've already ticked for this slot, so we'll try to tick for the next one
		s.logger.Warn(
			"double tick",
			zap.Uint64("nextSlot", uint64(nextSlot)),
			zap.Uint64("s.nextSlot", uint64(s.nextSlot)),
		)
		nextSlot = s.nextSlot + 1
	}
	nextSlotStartTime := s.genesisTime.Add(casts.DurationFromUint64(uint64(nextSlot)) * s.slotDuration)
	s.timer.Reset(time.Until(nextSlotStartTime))
	s.nextSlot = nextSlot
	return s.timer.C()
}

// Slot implements SlotTicker.Slot.
// Note, this method is not thread-safe.
func (s *slotTicker) Slot() phase0.Slot {
	return s.nextSlot
}
