package executionclient

import (
	"sync/atomic"
	"time"
)

// AdaptiveBatcher manages dynamic batch sizing.
type AdaptiveBatcher struct {
	currentSize    atomic.Uint64
	successCount   atomic.Uint32
	lastAdjustTime atomic.Int64
	minSize        uint64
	maxSize        uint64
}

// NewAdaptiveBatcher creates a new adaptive batcher.
func NewAdaptiveBatcher(initial, min, max uint64) *AdaptiveBatcher {
	if min == 0 {
		min = MinBatchSize
	}

	if max == 0 {
		max = MaxBatchSize
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

	ab := &AdaptiveBatcher{
		minSize: min,
		maxSize: max,
	}

	ab.currentSize.Store(initial)
	ab.lastAdjustTime.Store(time.Now().Unix())

	return ab
}

// GetSize returns current batch size.
func (ab *AdaptiveBatcher) GetSize() uint64 {
	return ab.currentSize.Load()
}

// RecordSuccess records a successful batch operation.
func (ab *AdaptiveBatcher) RecordSuccess(duration time.Duration) {
	count := ab.successCount.Add(1)

	if count >= successThreshold && duration < latencyTarget {
		ab.tryIncrease()
		ab.successCount.Store(0)
	}
}

// RecordFailure records a failed batch operation.
func (ab *AdaptiveBatcher) RecordFailure() {
	ab.tryDecrease()
	ab.successCount.Store(0)
}

// Reset resets the batcher to the default size.
func (ab *AdaptiveBatcher) Reset() {
	ab.currentSize.Store(DefaultBatchSize)
	ab.successCount.Store(0)
	ab.lastAdjustTime.Store(time.Now().Unix())
}

// tryIncrease attempts to increase batch size.
func (ab *AdaptiveBatcher) tryIncrease() {
	current := ab.currentSize.Load()
	newSize := uint64(float64(current) * batchIncreaseRatio)

	if newSize > ab.maxSize {
		newSize = ab.maxSize
	}
	if newSize > current {
		ab.currentSize.Store(newSize)
		ab.lastAdjustTime.Store(time.Now().Unix())
	}
}

// tryDecrease attempts to decrease batch size.
func (ab *AdaptiveBatcher) tryDecrease() {
	current := ab.currentSize.Load()
	newSize := uint64(float64(current) * batchDecreaseRatio)

	if newSize < ab.minSize {
		newSize = ab.minSize
	}

	if newSize < current {
		ab.currentSize.Store(newSize)
		ab.lastAdjustTime.Store(time.Now().Unix())
	}
}
