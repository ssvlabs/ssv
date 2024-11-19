package slotticker

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/utils/casts"
	"go.uber.org/zap"
)

//go:generate mockgen -package=mocks -destination=./mocks/slotticker.go -source=./slotticker.go

type Provider func() SlotTicker

// SlotTicker provides a way to keep track of Ethereum slots as they change over time.
// Note, the caller is RESPONSIBLE for calling Next method periodically in order for
// SlotTicker to advance forward (to keep ticking) to newer slots.
type SlotTicker interface {
	// Next advances SlotTicker slot number (which keeps track of the "freshest" slot value
	// from Ethereum perspective) potentially jumping several slots ahead. It returns a channel
	// that will relay 1 tick signalling that "freshest" slot has started.
	Next() <-chan time.Time
	// Slot returns the next slot number SlotTicker will tick on.
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
	logger       *zap.Logger
	timer        Timer
	slotDuration time.Duration
	genesisTime  time.Time
	slot         phase0.Slot
}

// New returns a goroutine-free SlotTicker implementation which is not thread-safe.
func New(logger *zap.Logger, cfg Config) *slotTicker {
	return newWithCustomTimer(logger, cfg, NewTimer)
}

func newWithCustomTimer(logger *zap.Logger, cfg Config, timerProvider TimerProvider) *slotTicker {
	timeSinceGenesis := time.Since(cfg.GenesisTime)

	var (
		initialDelay time.Duration
		initialSlot  phase0.Slot
	)
	if timeSinceGenesis < 0 {
		// Genesis time is in the future
		initialDelay = -timeSinceGenesis // Wait until the genesis time
		initialSlot = phase0.Slot(0)     // Start at slot 0
	} else {
		slotsSinceGenesis := timeSinceGenesis / cfg.SlotDuration
		nextSlotStartTime := cfg.GenesisTime.Add((slotsSinceGenesis + 1) * cfg.SlotDuration)
		initialDelay = time.Until(nextSlotStartTime)
		initialSlot = phase0.Slot(slotsSinceGenesis)
	}

	return &slotTicker{
		logger:       logger,
		timer:        timerProvider(initialDelay),
		slotDuration: cfg.SlotDuration,
		genesisTime:  cfg.GenesisTime,
		slot:         initialSlot,
	}
}

// Next implements SlotTicker.Next.
// Note, this method is not thread-safe.
func (s *slotTicker) Next() <-chan time.Time {
	timeSinceGenesis := time.Since(s.genesisTime)
	if timeSinceGenesis < 0 {
		// We are waiting for slotTicker to tick at s.genesisTime (signalling 0th slot start).
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
	if nextSlot <= s.slot {
		// We've already ticked for this slot, so we need to wait for the next one.
		nextSlot = s.slot + 1
		s.logger.Debug("slotTicker: double tick", zap.Uint64("slot", uint64(s.slot)))
	}
	nextSlotStartTime := s.genesisTime.Add(casts.DurationFromUint64(uint64(nextSlot)) * s.slotDuration)
	s.timer.Reset(time.Until(nextSlotStartTime))
	s.slot = nextSlot
	return s.timer.C()
}

// Slot implements SlotTicker.Slot.
// Note, this method is not thread-safe.
func (s *slotTicker) Slot() phase0.Slot {
	return s.slot
}
