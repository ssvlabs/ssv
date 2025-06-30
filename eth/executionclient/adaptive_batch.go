package executionclient

import (
	"sync/atomic"
)

// Internal constants for adaptive batching - not exposed to configuration
const (
	// defaultInitialBatchSize is the initial number of blocks processed per batch.
	defaultInitialBatchSize = 500

	// defaultMinBatchSize is the minimum allowed batch size.
	defaultMinBatchSize = 200

	// defaultMaxBatchSize is the maximum allowed batch size.
	defaultMaxBatchSize = 2000

	// growthFactor is the multiplication factor for batch size increases.
	growthFactor = 125 // 25% increase (125% = 1.25x)

	// shrinkFactor is the multiplication factor for batch size decreases on high log counts.
	shrinkFactor = 80 // 20% decrease (80% = 0.8x)

	// queryLimitFactor is the multiplication factor for batch size decreases on query limit errors.
	queryLimitFactor = 50 // 50% decrease on query errors (50% = 0.5x)

	// highLogsThreshold is the log count that triggers batch size reduction.
	highLogsThreshold = 1000
)

// AdaptiveBatcher dynamically adjusts batch sizes based on operation performance.
// This is an internal optimization mechanism not exposed to operators.
// Thread-safe for concurrent use.
type AdaptiveBatcher struct {
	size atomic.Uint64
}

// NewAdaptiveBatcher creates a new adaptive batcher with default internal settings.
func NewAdaptiveBatcher() *AdaptiveBatcher {
	ab := &AdaptiveBatcher{}
	ab.size.Store(defaultInitialBatchSize)
	return ab
}

// GetSize returns the current batch size.
func (ab *AdaptiveBatcher) GetSize() uint64 {
	return ab.size.Load()
}

// OnEmptyResult adjusts batch size when no logs are returned.
// Increases batch size since we can handle more blocks.
func (ab *AdaptiveBatcher) OnEmptyResult() {
	ab.increaseByFactor(growthFactor)
}

// OnHighLogCount adjusts batch size based on the number of logs returned.
// Decreases batch size if the log count exceeds the threshold.
func (ab *AdaptiveBatcher) OnHighLogCount(logCount int) {
	if logCount > highLogsThreshold {
		ab.decreaseByFactor(shrinkFactor)
	}
}

// OnQueryLimitError adjusts batch size when RPC or WS query limits are exceeded.
// Aggressively decreases batch size for limit-related errors.
func (ab *AdaptiveBatcher) OnQueryLimitError() {
	ab.decreaseByFactor(queryLimitFactor)
}

// Reset resets the batcher to the initial batch size.
func (ab *AdaptiveBatcher) Reset() {
	ab.size.Store(defaultInitialBatchSize)
}

// increaseByFactor increases the batch size by the given factor (as percentage).
func (ab *AdaptiveBatcher) increaseByFactor(factor uint64) {
	current := ab.size.Load()

	// Calculate increase: new = current * factor / 100
	newSize := (current * factor) / 100

	// Ensure at least 1 block increase if calculation would result in the same size
	if newSize == current {
		newSize = current + 1
	}

	// Handle overflow protection
	if newSize < current {
		newSize = defaultMaxBatchSize
	}

	// Cap at maximum
	if newSize > defaultMaxBatchSize {
		newSize = defaultMaxBatchSize
	}

	if newSize != current {
		ab.size.Store(newSize)
	}
}

// decreaseByFactor decreases the batch size by the given factor (as percentage).
func (ab *AdaptiveBatcher) decreaseByFactor(factor uint64) {
	current := ab.size.Load()

	// Calculate decrease: new = current * factor / 100
	newSize := (current * factor) / 100

	// Ensure at least 1 block decrease if calculation would result in the same size
	if newSize == current && current > defaultMinBatchSize {
		newSize = current - 1
	}

	// Cap at minimum
	if newSize < defaultMinBatchSize {
		newSize = defaultMinBatchSize
	}

	if newSize != current {
		ab.size.Store(newSize)
	}
}
