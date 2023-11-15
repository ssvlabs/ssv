package slotticker

import (
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cornelk/hashmap/assert"
	"github.com/stretchr/testify/require"
)

func TestSlotTicker(t *testing.T) {
	const numTicks = 3
	slotDuration := 200 * time.Millisecond
	// Set the genesis time such that we start from slot 1
	genesisTime := time.Now().Truncate(slotDuration).Add(-slotDuration)

	// Calculate the expected starting slot based on genesisTime
	timeSinceGenesis := time.Since(genesisTime)
	expectedSlot := phase0.Slot(timeSinceGenesis/slotDuration) + 1

	ticker := New(Config{slotDuration, genesisTime})

	for i := 0; i < numTicks; i++ {
		<-ticker.Next()
		slot := ticker.Slot()

		require.Equal(t, expectedSlot, slot)
		expectedSlot++
	}
}

func TestTickerInitialization(t *testing.T) {
	slotDuration := 200 * time.Millisecond
	genesisTime := time.Now()
	ticker := New(Config{slotDuration, genesisTime})

	start := time.Now()
	<-ticker.Next()
	slot := ticker.Slot()

	// Allow a small buffer (e.g., 10ms) due to code execution overhead
	buffer := 10 * time.Millisecond

	elapsed := time.Since(start)
	assert.True(t, elapsed+buffer >= slotDuration, "First tick occurred too soon: %v", elapsed.String())
	require.Equal(t, phase0.Slot(1), slot)
}

func TestSlotNumberConsistency(t *testing.T) {
	slotDuration := 200 * time.Millisecond
	genesisTime := time.Now()

	ticker := New(Config{slotDuration, genesisTime})
	var lastSlot phase0.Slot

	for i := 0; i < 10; i++ {
		<-ticker.Next()
		slot := ticker.Slot()

		require.Equal(t, lastSlot+1, slot)
		lastSlot = slot
	}
}

func TestGenesisInFuture(t *testing.T) {
	slotDuration := 200 * time.Millisecond
	genesisTime := time.Now().Add(1 * time.Second) // Setting genesis time 1s in the future

	ticker := New(Config{slotDuration, genesisTime})
	start := time.Now()

	<-ticker.Next()

	// The first tick should occur after the genesis time
	expectedFirstTickDuration := genesisTime.Sub(start)
	actualFirstTickDuration := time.Since(start)

	// Allow a small buffer (e.g., 10ms) due to code execution overhead
	buffer := 10 * time.Millisecond

	assert.True(t, actualFirstTickDuration+buffer >= expectedFirstTickDuration, "First tick occurred too soon. Expected at least: %v, but got: %v", expectedFirstTickDuration.String(), actualFirstTickDuration.String())
}

func TestBoundedDrift(t *testing.T) {
	slotDuration := 20 * time.Millisecond
	genesisTime := time.Now()

	ticker := New(Config{slotDuration, genesisTime})
	ticks := 100

	start := time.Now()
	for i := 0; i < ticks; i++ {
		<-ticker.Next()
	}
	expectedDuration := time.Duration(ticks) * slotDuration
	elapsed := time.Since(start)

	// We'll allow a small buffer for drift, say 1%
	buffer := expectedDuration * 1 / 100
	assert.True(t, elapsed >= expectedDuration-buffer && elapsed <= expectedDuration+buffer, "Drifted too far from expected time. Expected: %v, Actual: %v", expectedDuration.String(), elapsed.String())
}

func TestMultipleSlotTickers(t *testing.T) {
	const (
		numTickers    = 1000
		ticksPerTimer = 3
	)

	slotDuration := 200 * time.Millisecond
	genesisTime := time.Now()

	// Start the clock to time the full execution of all tickers
	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(numTickers)

	for i := 0; i < numTickers; i++ {
		go func() {
			defer wg.Done()
			ticker := New(Config{slotDuration, genesisTime})
			for j := 0; j < ticksPerTimer; j++ {
				<-ticker.Next()
			}
		}()
	}

	wg.Wait()

	// Calculate the total time taken for all tickers to complete their ticks
	elapsed := time.Since(start)
	expectedDuration := slotDuration * ticksPerTimer

	// We'll allow a small buffer for drift, say 5%
	buffer := expectedDuration * 5 / 100
	assert.True(t, elapsed <= expectedDuration+buffer, "Expected all tickers to complete within", expectedDuration.String(), "but took", elapsed.String())
}

func TestSlotSkipping(t *testing.T) {
	const (
		numTicks     = 100
		skipInterval = 10 // Introduce a delay every 10 ticks
		slotDuration = 20 * time.Millisecond
	)

	genesisTime := time.Now()
	ticker := New(Config{slotDuration, genesisTime})

	var lastSlot phase0.Slot
	for i := 1; i <= numTicks; i++ { // Starting loop from 1 for ease of skipInterval check
		select {
		case <-ticker.Next():
			slot := ticker.Slot()

			// Ensure we never receive slots out of order or repeatedly
			require.Equal(t, slot, lastSlot+1, "Expected slot %d to be one more than the last slot %d", slot, lastSlot)
			lastSlot = slot

			// If it's the 10th tick or any multiple thereof
			if i%skipInterval == 0 {
				// Introduce delay to skip a slot
				time.Sleep(slotDuration)

				// Ensure the next slot we receive is exactly 2 slots ahead of the previous slot
				<-ticker.Next()
				slotAfterDelay := ticker.Slot()
				require.Equal(t, lastSlot+2, slotAfterDelay, "Expected to skip a slot after introducing a delay")

				// Update the slot variable to use this new slot for further iterations
				lastSlot = slotAfterDelay
			}

		case <-time.After(2 * slotDuration): // Fail if we don't get a tick within a reasonable time
			t.Fatalf("Did not receive expected tick for iteration %d", i)
		}
	}
}
