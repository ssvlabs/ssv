package executionclient

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAdaptiveBatcher(t *testing.T) {
	t.Parallel()

	ab := NewAdaptiveBatcher()
	require.NotNil(t, ab)
	assert.Equal(t, uint64(500), ab.GetSize()) // defaultInitialBatchSize
}

func TestAdaptiveBatcher_OnEmptyResult(t *testing.T) {
	t.Parallel()

	ab := NewAdaptiveBatcher()
	initialSize := ab.GetSize() // 500

	ab.OnEmptyResult()

	// Should increase by growth factor: 500 * 125 / 100 = 625
	assert.Equal(t, uint64(625), ab.GetSize())
	assert.Greater(t, ab.GetSize(), initialSize)
}

func TestAdaptiveBatcher_OnHighLogCount(t *testing.T) {
	t.Parallel()

	t.Run("low log count no change", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher()
		initialSize := ab.GetSize()

		ab.OnHighLogCount(500) // Below highLogsThreshold (1000)

		// No change
		assert.Equal(t, initialSize, ab.GetSize())
	})

	t.Run("high log count decreases batch size", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher()
		initialSize := ab.GetSize() // 500

		ab.OnHighLogCount(1500) // Above highLogsThreshold (1000)

		// Should decrease by shrink factor: 500 * 80 / 100 = 400
		assert.Equal(t, uint64(400), ab.GetSize())
		assert.Less(t, ab.GetSize(), initialSize)
	})

	t.Run("exactly at threshold no change", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher()
		initialSize := ab.GetSize()

		ab.OnHighLogCount(1000) // Exactly at the highLogsThreshold
		assert.Equal(t, initialSize, ab.GetSize())
	})
}

func TestAdaptiveBatcher_OnQueryLimitError(t *testing.T) {
	t.Parallel()

	ab := NewAdaptiveBatcher()
	initialSize := ab.GetSize() // 500

	ab.OnQueryLimitError()

	// Should decrease aggressively by query limit factor: 500 * 50 / 100 = 250
	assert.Equal(t, uint64(250), ab.GetSize())
	assert.Less(t, ab.GetSize(), initialSize)
}

func TestAdaptiveBatcher_Reset(t *testing.T) {
	t.Parallel()

	ab := NewAdaptiveBatcher()

	// Change size
	ab.OnEmptyResult() // Increase
	assert.NotEqual(t, uint64(500), ab.GetSize())

	// Reset
	ab.Reset()
	assert.Equal(t, uint64(500), ab.GetSize()) // Back to defaultInitialBatchSize
}

func TestAdaptiveBatcher_Bounds(t *testing.T) {
	t.Parallel()

	t.Run("respects maximum", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher()

		// Repeatedly increase to hit max
		for i := 0; i < 10; i++ {
			ab.OnEmptyResult()
		}

		assert.Equal(t, uint64(2000), ab.GetSize()) // defaultMaxBatchSize
	})

	t.Run("respects minimum", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher()

		// Repeatedly decrease to hit min
		for i := 0; i < 10; i++ {
			ab.OnQueryLimitError()
		}

		assert.Equal(t, uint64(200), ab.GetSize())
	})
}

func TestAdaptiveBatcher_CalculationEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("small size increase ensures at least 1", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher()
		// Set to a size where factor calculation would result in the same size
		ab.size.Store(201) // 201 * 125 / 100 = 251.25 -> 251

		ab.OnEmptyResult()

		assert.Equal(t, uint64(251), ab.GetSize())
	})

	t.Run("small size decrease ensures at least 1 reduction", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher()
		// Set to a size where factor calculation would result in the same size
		ab.size.Store(250) // 250 * 80 / 100 = 200 (at minimum)

		ab.OnHighLogCount(1500)

		assert.Equal(t, uint64(200), ab.GetSize()) // At minimum
	})

	t.Run("handles overflow in increase", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher()
		// Set to a very large size that would overflow
		maxUint64 := ^uint64(0)
		ab.size.Store(maxUint64 - 100)

		ab.OnEmptyResult()

		// Should cap at max size, not overflow
		assert.Equal(t, uint64(2000), ab.GetSize())
	})
}

func TestAdaptiveBatcher_Concurrent(t *testing.T) {
	t.Parallel()

	ab := NewAdaptiveBatcher()

	const workers = 10
	const opsPerWorker = 100

	var wg sync.WaitGroup
	wg.Add(workers * 4)

	// Empty result workers (increase)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				ab.OnEmptyResult()
			}
		}()
	}

	// High log workers (decrease)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				ab.OnHighLogCount(1500)
			}
		}()
	}

	// Query limit workers (aggressive decrease)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				ab.OnQueryLimitError()
			}
		}()
	}

	// Reader workers
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				size := ab.GetSize()
				assert.GreaterOrEqual(t, size, uint64(200))
				assert.LessOrEqual(t, size, uint64(2000))
			}
		}()
	}

	wg.Wait()

	finalSize := ab.GetSize()
	assert.GreaterOrEqual(t, finalSize, uint64(200))
	assert.LessOrEqual(t, finalSize, uint64(2000))
}

func TestAdaptiveBatcher_FactorCalculations(t *testing.T) {
	t.Parallel()

	t.Run("growth factor calculations", func(t *testing.T) {
		testCases := []struct {
			initial  uint64
			expected uint64
		}{
			{1000, 1250}, // 1000 * 125 / 100 = 1250
			{100, 125},   // 100 * 125 / 100 = 125
			{200, 250},   // 200 * 125 / 100 = 250
			{333, 416},   // 333 * 125 / 100 = 416.25 -> 416
		}

		for _, tc := range testCases {
			ab := NewAdaptiveBatcher()
			ab.size.Store(tc.initial)

			ab.OnEmptyResult()

			assert.Equal(t, tc.expected, ab.GetSize())
		}
	})

	t.Run("shrink factor calculations", func(t *testing.T) {
		testCases := []struct {
			initial  uint64
			expected uint64
		}{
			{1000, 800}, // 1000 * 80 / 100 = 800
			{500, 400},  // 500 * 80 / 100 = 400
			{300, 240},  // 300 * 80 / 100 = 240
		}

		for _, tc := range testCases {
			ab := NewAdaptiveBatcher()
			ab.size.Store(tc.initial)

			ab.OnHighLogCount(1500)

			assert.Equal(t, tc.expected, ab.GetSize())
		}
	})

	t.Run("query limit factor calculations", func(t *testing.T) {
		testCases := []struct {
			initial  uint64
			expected uint64
		}{
			{1000, 500}, // 1000 * 50 / 100 = 500
			{500, 250},  // 500 * 50 / 100 = 250
			{400, 200},  // 400 * 50 / 100 = 200
		}

		for _, tc := range testCases {
			ab := NewAdaptiveBatcher()
			ab.size.Store(tc.initial)

			ab.OnQueryLimitError()

			assert.Equal(t, tc.expected, ab.GetSize())
		}
	})
}
