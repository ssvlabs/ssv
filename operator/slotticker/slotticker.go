package slotticker

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

//go:generate mockgen -package=mocks -destination=./mocks/slotticker.go -source=./slotticker.go

type Provider func() SlotTicker

type SlotTicker interface {
	Next() <-chan time.Time
	Slot() phase0.Slot
}

type ConfigProvider interface {
	SlotDurationSec() time.Duration
	GetGenesisTime() time.Time
}

type Config struct {
	slotDuration time.Duration
	genesisTime  time.Time
}

func (cfg Config) SlotDurationSec() time.Duration {
	return cfg.slotDuration
}

func (cfg Config) GetGenesisTime() time.Time {
	return cfg.genesisTime
}

type TimerProvider interface {
	NewTimer(d time.Duration) *time.Timer
}

// realTimerProvider is an implementation of TimerProvider that uses real timers.
type realTimerProvider struct{}

func (rtp *realTimerProvider) NewTimer(d time.Duration) *time.Timer {
	return time.NewTimer(d)
}

type slotTicker struct {
	logger       *zap.Logger
	timer        *time.Timer
	slotDuration time.Duration
	genesisTime  time.Time
	slot         phase0.Slot
}

// New returns a goroutine-free SlotTicker implementation which is not thread-safe.
func New(logger *zap.Logger, cfgProvider ConfigProvider) *slotTicker {
	return NewWithCustomTimer(logger, cfgProvider, &realTimerProvider{})
}

func NewWithCustomTimer(logger *zap.Logger, cfgProvider ConfigProvider, timerProvider TimerProvider) *slotTicker {
	genesisTime := cfgProvider.GetGenesisTime()
	slotDuration := cfgProvider.SlotDurationSec()

	now := time.Now()
	timeSinceGenesis := now.Sub(genesisTime)

	var initialDelay time.Duration
	if timeSinceGenesis < 0 {
		// Genesis time is in the future
		initialDelay = -timeSinceGenesis // Wait until the genesis time
	} else {
		slotsSinceGenesis := timeSinceGenesis / slotDuration
		nextSlotStartTime := genesisTime.Add((slotsSinceGenesis + 1) * slotDuration)
		initialDelay = time.Until(nextSlotStartTime)
	}

	if timerProvider == nil {
		timerProvider = &realTimerProvider{}
	}

	return &slotTicker{
		logger:       logger,
		timer:        timerProvider.NewTimer(initialDelay),
		slotDuration: slotDuration,
		genesisTime:  genesisTime,
		slot:         0,
	}
}

// Next returns a channel that signals when the next slot should start.
// Note: This function is not thread-safe and should be called in a serialized fashion.
// Make sure no concurrent calls happen, as it can result in unexpected behavior.
func (s *slotTicker) Next() <-chan time.Time {
	timeSinceGenesis := time.Since(s.genesisTime)
	if timeSinceGenesis < 0 {
		return s.timer.C
	}
	if !s.timer.Stop() {
		// try to drain the channel, but don't block if there's no value
		select {
		case <-s.timer.C:
		default:
		}
	}
	nextSlot := phase0.Slot(timeSinceGenesis/s.slotDuration) + 1
	if nextSlot <= s.slot {
		// We've already ticked for this slot, so we need to wait for the next one.
		nextSlot = s.slot
		s.logger.Warn("double tick", zap.Uint64("slot", uint64(nextSlot)))
	}
	nextSlotStartTime := s.genesisTime.Add(time.Duration(nextSlot) * s.slotDuration)
	s.timer.Reset(time.Until(nextSlotStartTime))
	s.slot = nextSlot
	return s.timer.C
}

// Slot returns the current slot number.
// Note: Like the Next function, this method is also not thread-safe.
// It should be called in a serialized manner after calling Next.
func (s *slotTicker) Slot() phase0.Slot {
	return s.slot
}
