package executionclient

import (
	"sync/atomic"
	"time"
)

// BatcherConfig contains configuration for adaptive batcher.
type BatcherConfig struct {
	InitialSize uint64
	MinSize     uint64
	MaxSize     uint64

	// Scaling ratios (as percentages)
	IncreaseRatio uint64 // e.g., 150 for 50% increase
	DecreaseRatio uint64 // e.g., 70 for 30% decrease

	// Scaling triggers
	SuccessThreshold uint32
	LatencyTarget    time.Duration
}

// DefaultBatcherConfig returns default configuration.
func DefaultBatcherConfig() BatcherConfig {
	return BatcherConfig{
		InitialSize:      DefaultBatchSize,
		MinSize:          DefaultMinBatchSize,
		MaxSize:          DefaultMaxBatchSize,
		IncreaseRatio:    DefaultIncreaseRatio,
		DecreaseRatio:    DefaultDecreaseRatio,
		SuccessThreshold: DefaultSuccessThreshold,
		LatencyTarget:    DefaultLatencyTarget,
	}
}

// AdaptiveBatcher dynamically adjusts batch sizes based on operation performance.
// Thread-safe for concurrent use.
type AdaptiveBatcher struct {
	config BatcherConfig

	size           atomic.Uint64
	successCount   atomic.Uint32
	lastAdjustTime atomic.Int64
}

// NewAdaptiveBatcher creates a new adaptive batcher with specified size limits.
func NewAdaptiveBatcher(initial, min, max uint64) *AdaptiveBatcher {
	if min == 0 {
		min = DefaultMinBatchSize
	}
	if max == 0 {
		max = DefaultMaxBatchSize
	}
	if initial == 0 {
		initial = DefaultBatchSize
	}

	if initial < min {
		initial = min
	}
	if initial > max {
		initial = max
	}

	// Use default configuration with custom sizes
	cfg := DefaultBatcherConfig()
	cfg.InitialSize = initial
	cfg.MinSize = min
	cfg.MaxSize = max

	ab := &AdaptiveBatcher{
		config: cfg,
	}

	ab.size.Store(initial)
	ab.lastAdjustTime.Store(time.Now().UnixNano())

	return ab
}

// NewAdaptiveBatcherWithConfig creates a new batcher with custom configuration.
// Use this if you need to customize increase/decrease ratios, thresholds, etc.
func NewAdaptiveBatcherWithConfig(cfg BatcherConfig) *AdaptiveBatcher {
	if cfg.MinSize == 0 {
		cfg.MinSize = DefaultMinBatchSize
	}
	if cfg.MaxSize == 0 {
		cfg.MaxSize = DefaultMaxBatchSize
	}
	if cfg.InitialSize == 0 {
		cfg.InitialSize = DefaultBatchSize
	}
	if cfg.IncreaseRatio == 0 {
		cfg.IncreaseRatio = DefaultIncreaseRatio
	}
	if cfg.DecreaseRatio == 0 {
		cfg.DecreaseRatio = DefaultDecreaseRatio
	}
	if cfg.SuccessThreshold == 0 {
		cfg.SuccessThreshold = DefaultSuccessThreshold
	}
	if cfg.LatencyTarget == 0 {
		cfg.LatencyTarget = DefaultLatencyTarget
	}

	if cfg.InitialSize < cfg.MinSize {
		cfg.InitialSize = cfg.MinSize
	}
	if cfg.InitialSize > cfg.MaxSize {
		cfg.InitialSize = cfg.MaxSize
	}

	if cfg.IncreaseRatio <= 100 {
		cfg.IncreaseRatio = DefaultIncreaseRatio
	}
	if cfg.DecreaseRatio >= 100 || cfg.DecreaseRatio == 0 {
		cfg.DecreaseRatio = DefaultDecreaseRatio
	}

	ab := &AdaptiveBatcher{
		config: cfg,
	}

	ab.size.Store(cfg.InitialSize)
	ab.lastAdjustTime.Store(time.Now().UnixNano())

	return ab
}

// GetSize returns the current batch size.
func (ab *AdaptiveBatcher) GetSize() uint64 {
	return ab.size.Load()
}

// RecordSuccess records a successful operation with its latency.
func (ab *AdaptiveBatcher) RecordSuccess(latency time.Duration) {
	count := ab.successCount.Add(1)

	if count >= ab.config.SuccessThreshold && latency < ab.config.LatencyTarget {
		ab.increase()
		ab.successCount.Store(0)
	}
}

// RecordFailure records a failed operation.
func (ab *AdaptiveBatcher) RecordFailure() {
	ab.decrease()
	ab.successCount.Store(0)
}

// Reset resets the batcher to initial configuration.
func (ab *AdaptiveBatcher) Reset() {
	ab.size.Store(ab.config.InitialSize)
	ab.successCount.Store(0)
	ab.lastAdjustTime.Store(time.Now().UnixNano())
}

// Config returns the current configuration.
func (ab *AdaptiveBatcher) Config() BatcherConfig {
	return ab.config
}

// increase the batch size according to configured ratio.
func (ab *AdaptiveBatcher) increase() {
	current := ab.size.Load()

	// Calculate increase: new = current * ratio / 100
	newSize := (current * ab.config.IncreaseRatio) / 100

	if newSize == current {
		newSize = current + 1
	}

	if newSize < current {
		newSize = ab.config.MaxSize
	}

	if newSize > ab.config.MaxSize {
		newSize = ab.config.MaxSize
	}

	if newSize != current {
		ab.size.Store(newSize)
		ab.lastAdjustTime.Store(time.Now().UnixNano())
	}
}

// decrease the batch size according to configured ratio.
func (ab *AdaptiveBatcher) decrease() {
	current := ab.size.Load()

	// Calculate decrease: new = current * ratio / 100
	newSize := (current * ab.config.DecreaseRatio) / 100

	if newSize == current && current > ab.config.MinSize {
		newSize = current - 1
	}

	if newSize < ab.config.MinSize {
		newSize = ab.config.MinSize
	}

	if newSize != current {
		ab.size.Store(newSize)
		ab.lastAdjustTime.Store(time.Now().UnixNano())
	}
}
