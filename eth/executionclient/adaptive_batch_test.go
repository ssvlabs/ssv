package executionclient

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAdaptiveBatcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		initial  uint64
		min      uint64
		max      uint64
		expected uint64
	}{
		{
			name:     "default values",
			initial:  0,
			min:      0,
			max:      0,
			expected: DefaultBatchSize,
		},
		{
			name:     "custom values",
			initial:  1000,
			min:      500,
			max:      1500,
			expected: 1000,
		},
		{
			name:     "initial below min",
			initial:  100,
			min:      500,
			max:      1500,
			expected: 500,
		},
		{
			name:     "initial above max",
			initial:  2000,
			min:      500,
			max:      1500,
			expected: 1500,
		},
		{
			name:     "zero min uses default",
			initial:  300,
			min:      0,
			max:      1000,
			expected: 300,
		},
		{
			name:     "zero max uses default",
			initial:  1500,
			min:      100,
			max:      0,
			expected: 1500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ab := NewAdaptiveBatcher(tt.initial, tt.min, tt.max)
			require.NotNil(t, ab)
			assert.Equal(t, tt.expected, ab.GetSize())

			if tt.min == 0 {
				assert.Equal(t, uint64(MinBatchSize), ab.minSize)
			} else {
				assert.Equal(t, tt.min, ab.minSize)
			}

			if tt.max == 0 {
				assert.Equal(t, uint64(MaxBatchSize), ab.maxSize)
			} else {
				assert.Equal(t, tt.max, ab.maxSize)
			}
		})
	}
}

func TestAdaptiveBatcher_RecordSuccess(t *testing.T) {
	t.Parallel()

	t.Run("increases size after threshold successes with low latency", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		initialSize := ab.GetSize()

		for i := 0; i < successThreshold; i++ {
			ab.RecordSuccess(100 * time.Millisecond)
		}

		newSize := ab.GetSize()
		expectedSize := uint64(float64(initialSize) * batchIncreaseRatio)
		assert.Equal(t, expectedSize, newSize)
		assert.Greater(t, newSize, initialSize)
	})

	t.Run("does not increase size with high latency", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		initialSize := ab.GetSize()

		for i := 0; i < successThreshold; i++ {
			ab.RecordSuccess(600 * time.Millisecond)
		}

		assert.Equal(t, initialSize, ab.GetSize())
	})

	t.Run("resets counter after increase", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)

		for i := 0; i < successThreshold; i++ {
			ab.RecordSuccess(100 * time.Millisecond)
		}

		firstIncrease := ab.GetSize()

		for i := 0; i < successThreshold-1; i++ {
			ab.RecordSuccess(100 * time.Millisecond)
		}
		assert.Equal(t, firstIncrease, ab.GetSize())

		ab.RecordSuccess(100 * time.Millisecond)
		assert.Greater(t, ab.GetSize(), firstIncrease)
	})

	t.Run("respects max size", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1900, 100, 2000)

		for i := 0; i < successThreshold; i++ {
			ab.RecordSuccess(100 * time.Millisecond)
		}

		assert.Equal(t, uint64(2000), ab.GetSize())
	})
}

func TestAdaptiveBatcher_RecordFailure(t *testing.T) {
	t.Parallel()

	t.Run("decreases size on failure", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		initialSize := ab.GetSize()

		ab.RecordFailure()

		newSize := ab.GetSize()
		expectedSize := uint64(float64(initialSize) * batchDecreaseRatio)
		assert.Equal(t, expectedSize, newSize)
		assert.Less(t, newSize, initialSize)
	})

	t.Run("respects min size", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(210, 200, 2000)

		ab.RecordFailure()
		assert.Equal(t, uint64(200), ab.GetSize())

		ab.RecordFailure()
		assert.Equal(t, uint64(200), ab.GetSize())
	})

	t.Run("resets success counter", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)

		for i := 0; i < successThreshold-1; i++ {
			ab.RecordSuccess(100 * time.Millisecond)
		}

		ab.RecordFailure()

		for i := 0; i < successThreshold-1; i++ {
			ab.RecordSuccess(100 * time.Millisecond)
		}

		assert.Less(t, ab.GetSize(), uint64(1000))
	})
}

func TestAdaptiveBatcher_Reset(t *testing.T) {
	t.Parallel()

	ab := NewAdaptiveBatcher(1000, 100, 2000)

	for i := 0; i < successThreshold; i++ {
		ab.RecordSuccess(100 * time.Millisecond)
	}
	assert.NotEqual(t, uint64(DefaultBatchSize), ab.GetSize())

	ab.Reset()
	assert.Equal(t, uint64(DefaultBatchSize), ab.GetSize())
	assert.Equal(t, uint32(0), ab.successCount.Load())
}

func TestAdaptiveBatcher_ConcurrentOperations(t *testing.T) {
	t.Parallel()

	ab := NewAdaptiveBatcher(1000, 100, 2000)

	const goroutines = 10
	const operations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				ab.RecordSuccess(100 * time.Millisecond)
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				ab.RecordFailure()
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				size := ab.GetSize()
				assert.GreaterOrEqual(t, size, uint64(100))
				assert.LessOrEqual(t, size, uint64(2000))
			}
		}()
	}

	wg.Wait()

	finalSize := ab.GetSize()
	assert.GreaterOrEqual(t, finalSize, uint64(100))
	assert.LessOrEqual(t, finalSize, uint64(2000))
}

func TestAdaptiveBatcher_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("handles max uint64 calculations", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(^uint64(0)-1000, 100, ^uint64(0))

		for i := 0; i < successThreshold; i++ {
			ab.RecordSuccess(100 * time.Millisecond)
		}

		assert.Equal(t, ^uint64(0), ab.GetSize())
	})

	t.Run("alternating success and failure", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		initialSize := ab.GetSize()

		for i := 0; i < 10; i++ {
			if i%2 == 0 {
				ab.RecordSuccess(100 * time.Millisecond)
			} else {
				ab.RecordFailure()
			}
		}

		assert.Less(t, ab.GetSize(), initialSize)
	})

	t.Run("exact threshold boundary", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		initialSize := ab.GetSize()

		for i := 0; i < successThreshold-1; i++ {
			ab.RecordSuccess(100 * time.Millisecond)
		}
		assert.Equal(t, initialSize, ab.GetSize())

		ab.RecordSuccess(100 * time.Millisecond)
		assert.Greater(t, ab.GetSize(), initialSize)
	})

	t.Run("lastAdjustTime_updates", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)

		ab.lastAdjustTime.Store(time.Now().Unix() - 10)
		initialTime := ab.lastAdjustTime.Load()

		ab.RecordFailure()

		newTime := ab.lastAdjustTime.Load()
		assert.Greater(t, newTime, initialTime)
	})
}
