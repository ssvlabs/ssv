package slotticker

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/utils/casts"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/slotticker.go -source=./slotticker.go

type Provider func() SlotTicker

type SlotTicker interface {
	Next() <-chan time.Time
	Slot() phase0.Slot
}

type Config struct {
	SlotDuration time.Duration
	GenesisTime  time.Time
}

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
	now := time.Now()
	timeSinceGenesis := now.Sub(cfg.GenesisTime)

	var initialDelay time.Duration
	if timeSinceGenesis < 0 {
		// Genesis time is in the future
		initialDelay = -timeSinceGenesis // Wait until the genesis time
	} else {
		slotsSinceGenesis := timeSinceGenesis / cfg.SlotDuration
		nextSlotStartTime := cfg.GenesisTime.Add((slotsSinceGenesis + 1) * cfg.SlotDuration)
		initialDelay = time.Until(nextSlotStartTime)
	}

	return &slotTicker{
		logger:       logger,
		timer:        timerProvider(initialDelay),
		slotDuration: cfg.SlotDuration,
		genesisTime:  cfg.GenesisTime,
		slot:         0,
	}
}

// Next returns a channel that signals when the next slot should start.
// Note: This function is not thread-safe and should be called in a serialized fashion.
// Make sure no concurrent calls happen, as it can result in unexpected behavior.
func (s *slotTicker) Next() <-chan time.Time {
	timeSinceGenesis := time.Since(s.genesisTime)
	if timeSinceGenesis < 0 {
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
		s.logger.Debug("double tick", zap.Uint64("slot", uint64(s.slot)))
	}
	nextSlotStartTime := s.genesisTime.Add(casts.DurationFromUint64(uint64(nextSlot)) * s.slotDuration)
	s.timer.Reset(time.Until(nextSlotStartTime))
	s.slot = nextSlot
	return s.timer.C()
}

// Slot returns the current slot number.
// Note: Like the Next function, this method is also not thread-safe.
// It should be called in a serialized manner after calling Next.
func (s *slotTicker) Slot() phase0.Slot {
	return s.slot
}
