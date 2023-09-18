package slot_ticker

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// The TTicker interface defines a type which can expose a
// receive-only channel firing slot events.
type TTicker interface {
	C() <-chan phase0.Slot
	Done()
}

// SlotTicker is a special ticker for the beacon chain block.
// The channel emits over the slot interval, and ensures that
// the ticks are in line with the genesis time. This means that
// the duration between the ticks and the genesis time are always a
// multiple of the slot duration.
// In addition, the channel returns the new slot number.
type SlotTicker struct {
	c    chan phase0.Slot
	done chan struct{}
}

// C returns the ticker channel. Call Cancel afterwards to ensure
// that the goroutine exits cleanly.
func (s *SlotTicker) C() <-chan phase0.Slot {
	return s.c
}

// Done should be called to clean up the ticker.
func (s *SlotTicker) Done() {
	go func() {
		s.done <- struct{}{}
	}()
}

// NewSlotTicker starts and returns a new SlotTicker instance.
func NewSlotTicker(genesisTime time.Time, secondsPerSlot uint64) *SlotTicker {
	if genesisTime.IsZero() {
		panic("zero genesis time")
	}
	ticker := &SlotTicker{
		c:    make(chan phase0.Slot),
		done: make(chan struct{}),
	}
	ticker.start(genesisTime, secondsPerSlot, time.Since, time.Until, time.After)
	return ticker
}

func (s *SlotTicker) start(
	genesisTime time.Time,
	secondsPerSlot uint64,
	since, until func(time.Time) time.Duration,
	after func(time.Duration) <-chan time.Time) {

	d := time.Duration(secondsPerSlot) * time.Second

	go func() {
		sinceGenesis := since(genesisTime)

		var nextTickTime time.Time
		var slot phase0.Slot
		if sinceGenesis < d {
			// Handle when the current time is before the genesis time.
			nextTickTime = genesisTime
			slot = 0
		} else {
			nextTick := sinceGenesis.Truncate(d) + d
			nextTickTime = genesisTime.Add(nextTick)
			slot = phase0.Slot(nextTick / d)
		}

		for {
			waitTime := until(nextTickTime)
			select {
			case <-after(waitTime):
				s.c <- slot
				slot++
				nextTickTime = nextTickTime.Add(d)
			case <-s.done:
				return
			}
		}
	}()
}

type SlotTicker2 struct {
	timer        *time.Timer
	slotDuration time.Duration
	genesisTime  time.Time
}

func NewSlotTicker2(slotDuration time.Duration, genesisTime time.Time) *SlotTicker2 {
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

	return &SlotTicker2{
		timer:        time.NewTimer(initialDelay),
		slotDuration: slotDuration,
		genesisTime:  genesisTime,
	}
}

func (s *SlotTicker2) Next() (<-chan time.Time, uint64) {
	timeSinceGenesis := time.Since(s.genesisTime)
	if timeSinceGenesis < 0 {
		return s.timer.C, 0
	}
	slotNumber := uint64(timeSinceGenesis / s.slotDuration)
	nextSlotStartTime := s.genesisTime.Add(time.Duration(slotNumber+1) * s.slotDuration)
	s.timer.Reset(time.Until(nextSlotStartTime))
	return s.timer.C, slotNumber + 1
}
