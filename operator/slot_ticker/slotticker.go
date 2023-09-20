package slot_ticker

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

//go:generate mockgen -package=mocks -destination=./mocks/slotticker.go -source=./slotticker.go

type SlotTicker interface {
	Next() <-chan time.Time
	Slot() phase0.Slot
}

type ConfigProvider interface {
	SlotDurationSec() time.Duration
	GetGenesisTime() time.Time
}

type SlotTickerConfig struct {
	slotDuration time.Duration
	genesisTime  time.Time
}

func (cfg SlotTickerConfig) SlotDurationSec() time.Duration {
	return cfg.slotDuration
}

func (cfg SlotTickerConfig) GetGenesisTime() time.Time {
	return cfg.genesisTime
}

type slotTicker struct {
	timer        *time.Timer
	slotDuration time.Duration
	genesisTime  time.Time
	slot         phase0.Slot
}

func NewSlotTicker(cfgProvider ConfigProvider) SlotTicker {
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

	return &slotTicker{
		timer:        time.NewTimer(initialDelay),
		slotDuration: slotDuration,
		genesisTime:  genesisTime,
		slot:         0,
	}
}

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
	slotNumber := uint64(timeSinceGenesis / s.slotDuration)
	nextSlotStartTime := s.genesisTime.Add(time.Duration(slotNumber+1) * s.slotDuration)
	s.timer.Reset(time.Until(nextSlotStartTime))
	s.slot = phase0.Slot(slotNumber + 1)
	return s.timer.C
}

func (s *slotTicker) Slot() phase0.Slot {
	return s.slot
}
