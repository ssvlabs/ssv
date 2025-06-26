package executionclient

import (
	"time"

	"go.uber.org/zap"
)

// Option defines an ExecutionClient configuration option.
type Option func(*ExecutionClient)

// OptionMulti defines a MultiClient configuration option.
type OptionMulti func(client *MultiClient)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(c *ExecutionClient) {
		c.logger = logger.Named("execution_client")
	}
}

// WithLoggerMulti enables logging.
func WithLoggerMulti(logger *zap.Logger) OptionMulti {
	return func(c *MultiClient) {
		c.logger = logger.Named("execution_client_multi")
	}
}

// WithConnectionTimeout sets timeout for network connection to eth1 node.
func WithConnectionTimeout(timeout time.Duration) Option {
	return func(c *ExecutionClient) {
		c.connectionTimeout = timeout
	}
}

// WithConnectionTimeoutMulti sets timeout for network connection to eth1 node.
func WithConnectionTimeoutMulti(timeout time.Duration) OptionMulti {
	return func(c *MultiClient) {
		c.connectionTimeout = timeout
	}
}

// WithHealthInvalidationInterval sets health invalidation interval. 0 disables caching.
func WithHealthInvalidationInterval(interval time.Duration) Option {
	return func(c *ExecutionClient) {
		c.healthInvalidationInterval = interval
	}
}

// WithHealthInvalidationIntervalMulti sets health invalidation interval.
func WithHealthInvalidationIntervalMulti(interval time.Duration) OptionMulti {
	return func(c *MultiClient) {
		c.healthInvalidationInterval = interval
	}
}

// WithLogBatchSize sets log batch size.
func WithLogBatchSize(size uint64) Option {
	return func(c *ExecutionClient) {
		c.logBatchSize = size
	}
}

// WithLogBatchSizeMulti sets log batch size.
func WithLogBatchSizeMulti(size uint64) OptionMulti {
	return func(c *MultiClient) {
		c.logBatchSize = size
	}
}

// WithSyncDistanceTolerance sets the number of blocks that is acceptable to lag behind.
func WithSyncDistanceTolerance(count uint64) Option {
	return func(c *ExecutionClient) {
		c.syncDistanceTolerance = count
	}
}

// WithSyncDistanceToleranceMulti sets the number of blocks that is acceptable to lag behind.
func WithSyncDistanceToleranceMulti(count uint64) OptionMulti {
	return func(c *MultiClient) {
		c.syncDistanceTolerance = count
	}
}

// WithFollowDistance sets the follow distance for pre-fork block processing.
func WithFollowDistance(distance uint64) Option {
	return func(c *ExecutionClient) {
		c.followDistance = distance
	}
}

// WithFollowDistanceMulti sets the follow distance for pre-fork block processing.
func WithFollowDistanceMulti(distance uint64) OptionMulti {
	return func(c *MultiClient) {
		c.followDistance = distance
	}
}
