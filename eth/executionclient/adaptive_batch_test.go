package executionclient

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatcherConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultBatcherConfig()

	assert.Equal(t, uint64(DefaultBatchSize), cfg.InitialSize)
	assert.Equal(t, uint64(DefaultMinBatchSize), cfg.MinSize)
	assert.Equal(t, uint64(DefaultMaxBatchSize), cfg.MaxSize)
	assert.Equal(t, uint64(DefaultIncreaseRatio), cfg.IncreaseRatio)
	assert.Equal(t, uint64(DefaultDecreaseRatio), cfg.DecreaseRatio)
	assert.Equal(t, uint32(DefaultHighLogsThreshold), cfg.HighLogsThreshold)
}

func TestNewAdaptiveBatcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		initial     uint64
		min         uint64
		max         uint64
		wantInitial uint64
		wantMin     uint64
		wantMax     uint64
	}{
		{
			name:        "all defaults",
			initial:     0,
			min:         0,
			max:         0,
			wantInitial: DefaultBatchSize,
			wantMin:     DefaultMinBatchSize,
			wantMax:     DefaultMaxBatchSize,
		},
		{
			name:        "custom values",
			initial:     1000,
			min:         500,
			max:         1500,
			wantInitial: 1000,
			wantMin:     500,
			wantMax:     1500,
		},
		{
			name:        "initial below min",
			initial:     100,
			min:         500,
			max:         1500,
			wantInitial: 500, // Clamped to min
			wantMin:     500,
			wantMax:     1500,
		},
		{
			name:        "initial above max",
			initial:     2000,
			min:         500,
			max:         1500,
			wantInitial: 1500, // Clamped to max
			wantMin:     500,
			wantMax:     1500,
		},
		{
			name:        "zero min uses default",
			initial:     300,
			min:         0,
			max:         1000,
			wantInitial: 300,
			wantMin:     DefaultMinBatchSize,
			wantMax:     1000,
		},
		{
			name:        "zero max uses default",
			initial:     1500,
			min:         100,
			max:         0,
			wantInitial: 1500,
			wantMin:     100,
			wantMax:     DefaultMaxBatchSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ab := NewAdaptiveBatcher(tt.initial, tt.min, tt.max)
			require.NotNil(t, ab)
			assert.Equal(t, tt.wantInitial, ab.GetSize())
			assert.Equal(t, tt.wantMin, ab.config.MinSize)
			assert.Equal(t, tt.wantMax, ab.config.MaxSize)

			// Should use default ratios
			assert.Equal(t, uint64(150), ab.config.IncreaseRatio)
			assert.Equal(t, uint64(70), ab.config.DecreaseRatio)
		})
	}
}

func TestNewAdaptiveBatcherWithConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         BatcherConfig
		wantInitial uint64
		wantMin     uint64
		wantMax     uint64
	}{
		{
			name:        "default config",
			cfg:         DefaultBatcherConfig(),
			wantInitial: 500,
			wantMin:     200,
			wantMax:     2000,
		},
		{
			name: "custom config",
			cfg: BatcherConfig{
				InitialSize:   1000,
				MinSize:       500,
				MaxSize:       1500,
				IncreaseRatio: 200, // 100% increase
				DecreaseRatio: 50,  // 50% decrease
			},
			wantInitial: 1000,
			wantMin:     500,
			wantMax:     1500,
		},
		{
			name: "initial below min",
			cfg: BatcherConfig{
				InitialSize: 100,
				MinSize:     500,
				MaxSize:     1500,
			},
			wantInitial: 500, // Clamped to min
		},
		{
			name: "initial above max",
			cfg: BatcherConfig{
				InitialSize: 2000,
				MinSize:     500,
				MaxSize:     1500,
			},
			wantInitial: 1500, // Clamped to max
		},
		{
			name:        "all zeros use defaults",
			cfg:         BatcherConfig{},
			wantInitial: DefaultBatchSize,
			wantMin:     DefaultMinBatchSize,
			wantMax:     DefaultMaxBatchSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ab := NewAdaptiveBatcherWithConfig(tt.cfg)
			require.NotNil(t, ab)
			assert.Equal(t, tt.wantInitial, ab.GetSize())

			// Check config was properly set
			if tt.wantMin != 0 {
				assert.Equal(t, tt.wantMin, ab.config.MinSize)
			}
			if tt.wantMax != 0 {
				assert.Equal(t, tt.wantMax, ab.config.MaxSize)
			}
		})
	}
}

func TestAdaptiveBatcher_RecordResult(t *testing.T) {
	t.Parallel()

	t.Run("zero logs increases batch size", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		ab.RecordResult(0)

		// Default: 150% = 1.5x = 1500
		assert.Equal(t, uint64(1500), ab.GetSize())
	})

	t.Run("high log count decreases batch size", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		ab.RecordResult(1500) // Above DefaultHighLogsThreshold (1000)

		// Default: 70% = 0.7x = 700
		assert.Equal(t, uint64(700), ab.GetSize())
	})

	t.Run("moderate log count no change", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		ab.RecordResult(500) // Below HighLogsThreshold

		// No change
		assert.Equal(t, uint64(1000), ab.GetSize())
	})

	t.Run("custom high logs threshold", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultBatcherConfig()
		cfg.InitialSize = 1000
		cfg.HighLogsThreshold = 500 // Custom threshold
		ab := NewAdaptiveBatcherWithConfig(cfg)

		// Below threshold - no change
		ab.RecordResult(400)
		assert.Equal(t, uint64(1000), ab.GetSize())

		// Above threshold - decrease
		ab.RecordResult(600)
		assert.Equal(t, uint64(700), ab.GetSize())
	})

	t.Run("respects max size when increasing", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1900, 100, 2000)
		ab.RecordResult(0) // Zero logs

		assert.Equal(t, uint64(2000), ab.GetSize()) // Capped at max
	})

	t.Run("respects min size when decreasing", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(250, 200, 2000)
		ab.RecordResult(1500) // High log count

		// 250 * 70 / 100 = 175, but minimum is 200, so should be 200
		assert.Equal(t, uint64(200), ab.GetSize())
	})

	t.Run("custom ratios", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultBatcherConfig()
		cfg.InitialSize = 1000
		cfg.IncreaseRatio = 200 // 100% increase (double)
		cfg.DecreaseRatio = 50  // 50% (halve)
		ab := NewAdaptiveBatcherWithConfig(cfg)

		// Zero logs - increase
		ab.RecordResult(0)
		assert.Equal(t, uint64(2000), ab.GetSize())

		// Reset and test decrease
		ab.Reset()
		ab.RecordResult(1500) // High log count
		assert.Equal(t, uint64(500), ab.GetSize())
	})
}

func TestAdaptiveBatcher_Reset(t *testing.T) {
	t.Parallel()

	cfg := DefaultBatcherConfig()
	cfg.InitialSize = 1000
	ab := NewAdaptiveBatcherWithConfig(cfg)

	// Change state
	ab.RecordResult(0) // Zero logs to increase size
	assert.Equal(t, uint64(1500), ab.GetSize())

	// Reset
	ab.Reset()
	assert.Equal(t, uint64(1000), ab.GetSize()) // Back to initial
}

func TestAdaptiveBatcher_Config(t *testing.T) {
	t.Parallel()

	cfg := BatcherConfig{
		InitialSize:       1000,
		MinSize:           100,
		MaxSize:           2000,
		IncreaseRatio:     175,
		DecreaseRatio:     60,
		HighLogsThreshold: 800,
	}

	ab := NewAdaptiveBatcherWithConfig(cfg)
	returnedCfg := ab.Config()

	assert.Equal(t, cfg.InitialSize, returnedCfg.InitialSize)
	assert.Equal(t, cfg.MinSize, returnedCfg.MinSize)
	assert.Equal(t, cfg.MaxSize, returnedCfg.MaxSize)
	assert.Equal(t, cfg.IncreaseRatio, returnedCfg.IncreaseRatio)
	assert.Equal(t, cfg.DecreaseRatio, returnedCfg.DecreaseRatio)
	assert.Equal(t, cfg.HighLogsThreshold, returnedCfg.HighLogsThreshold)
}

func TestAdaptiveBatcher_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("invalid increase ratio", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultBatcherConfig()
		cfg.IncreaseRatio = 90 // Less than 100%
		ab := NewAdaptiveBatcherWithConfig(cfg)

		// Should use default
		assert.Equal(t, uint64(DefaultIncreaseRatio), ab.config.IncreaseRatio)
	})

	t.Run("invalid decrease ratio", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultBatcherConfig()
		cfg.DecreaseRatio = 110 // More than 100%
		ab := NewAdaptiveBatcherWithConfig(cfg)

		// Should use default
		assert.Equal(t, uint64(DefaultDecreaseRatio), ab.config.DecreaseRatio)
	})

	t.Run("overflow protection", func(t *testing.T) {
		t.Parallel()

		maxUint := ^uint64(0)
		cfg := DefaultBatcherConfig()
		cfg.InitialSize = maxUint - 1000
		cfg.MaxSize = maxUint
		cfg.MinSize = 100
		ab := NewAdaptiveBatcherWithConfig(cfg)

		ab.RecordResult(0) // Zero logs to trigger increase

		assert.Equal(t, maxUint, ab.GetSize())
	})
}

func TestAdaptiveBatcher_Concurrent(t *testing.T) {
	t.Parallel()

	ab := NewAdaptiveBatcher(1000, 100, 2000)

	const workers = 10
	const opsPerWorker = 100

	var wg sync.WaitGroup
	wg.Add(workers * 3)

	// Zero log workers (increase batch)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				ab.RecordResult(0)
			}
		}()
	}

	// High log workers (decrease batch)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				ab.RecordResult(1500)
			}
		}()
	}

	// Reader workers
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				size := ab.GetSize()
				assert.GreaterOrEqual(t, size, uint64(100))
				assert.LessOrEqual(t, size, uint64(2000))
			}
		}()
	}

	wg.Wait()

	// Final size should be within bounds
	finalSize := ab.GetSize()
	assert.GreaterOrEqual(t, finalSize, uint64(100))
	assert.LessOrEqual(t, finalSize, uint64(2000))
}

func TestAdaptiveBatcher_Calculations(t *testing.T) {
	t.Parallel()

	t.Run("increase calculations", func(t *testing.T) {
		testCases := []struct {
			initial  uint64
			ratio    uint64
			expected uint64
		}{
			{1000, 150, 1500}, // 50% increase
			{1000, 200, 2000}, // 100% increase
			{1000, 110, 1100}, // 10% increase
			{100, 125, 125},   // 25% increase
			{10, 300, 30},     // 200% increase
		}

		for _, tc := range testCases {
			cfg := DefaultBatcherConfig()
			cfg.InitialSize = tc.initial
			cfg.IncreaseRatio = tc.ratio
			cfg.MinSize = 1
			cfg.MaxSize = 10000
			ab := NewAdaptiveBatcherWithConfig(cfg)

			ab.RecordResult(0) // Zero logs to trigger increase

			assert.Equal(t, tc.expected, ab.GetSize())
		}
	})

	t.Run("decrease calculations", func(t *testing.T) {
		testCases := []struct {
			initial  uint64
			ratio    uint64
			expected uint64
		}{
			{1000, 70, 700}, // 30% decrease
			{1000, 50, 500}, // 50% decrease
			{1000, 90, 900}, // 10% decrease
			{100, 25, 25},   // 75% decrease
			{10, 10, 1},     // 90% decrease
		}

		for _, tc := range testCases {
			cfg := DefaultBatcherConfig()
			cfg.InitialSize = tc.initial
			cfg.DecreaseRatio = tc.ratio
			cfg.MinSize = 1
			cfg.MaxSize = 10000
			ab := NewAdaptiveBatcherWithConfig(cfg)

			ab.RecordResult(1500) // High log count to trigger decrease

			assert.Equal(t, tc.expected, ab.GetSize())
		}
	})
}

func TestAdaptiveBatcher_RecordFailure(t *testing.T) {
	t.Parallel()

	t.Run("default decrease ratio", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(1000, 100, 2000)
		ab.RecordFailure()

		// Default: 70% = 0.7x = 700
		assert.Equal(t, uint64(700), ab.GetSize())
	})

	t.Run("custom decrease ratio", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultBatcherConfig()
		cfg.InitialSize = 1000
		cfg.DecreaseRatio = 50 // 50% (halve)
		ab := NewAdaptiveBatcherWithConfig(cfg)

		ab.RecordFailure()

		// 50% = 0.5x = 500
		assert.Equal(t, uint64(500), ab.GetSize())
	})

	t.Run("respects min size", func(t *testing.T) {
		t.Parallel()

		ab := NewAdaptiveBatcher(210, 200, 2000)

		ab.RecordFailure()
		assert.Equal(t, uint64(200), ab.GetSize())

		// Second failure stays at min
		ab.RecordFailure()
		assert.Equal(t, uint64(200), ab.GetSize())
	})
}
