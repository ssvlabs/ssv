package executionclient

import (
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
)

// Option defines an ExecutionClient configuration option.
type Option func(*ExecutionClient)

// OptionMulti defines a MultiClient configuration option.
type OptionMulti func(client *MultiClient)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(s *ExecutionClient) {
		s.logger = logger.Named(log.NameExecutionClient)
	}
}

// WithLoggerMulti enables logging.
func WithLoggerMulti(logger *zap.Logger) OptionMulti {
	return func(s *MultiClient) {
		s.logger = logger.Named(log.NameExecutionClientMulti)
	}
}

// WithFollowDistance sets finalization offset (a block at this offset into the past
// from the head block will be considered as very likely finalized).
func WithFollowDistance(offset uint64) Option {
	return func(s *ExecutionClient) {
		s.followDistance = offset
	}
}

// WithFollowDistanceMulti sets finalization offset (a block at this offset into the past
// from the head block will be considered as very likely finalized).
func WithFollowDistanceMulti(offset uint64) OptionMulti {
	return func(s *MultiClient) {
		s.followDistance = offset
	}
}

// WithConnectionTimeout sets timeout for network connection to eth1 node.
// The timeout must be positive, otherwise the default value will be used.
func WithConnectionTimeout(timeout time.Duration) Option {
	return func(s *ExecutionClient) {
		s.connectionTimeout = timeout
	}
}

// WithConnectionTimeoutMulti sets timeout for network connection to eth1 node.
// The timeout must be positive, otherwise the default value will be used.
func WithConnectionTimeoutMulti(timeout time.Duration) OptionMulti {
	return func(s *MultiClient) {
		s.connectionTimeout = timeout
	}
}

// WithHealthInvalidationInterval sets health invalidation interval. 0 disables caching.
func WithHealthInvalidationInterval(interval time.Duration) Option {
	return func(s *ExecutionClient) {
		s.healthInvalidationInterval = interval
	}
}

// WithHealthInvalidationIntervalMulti sets health invalidation interval.
func WithHealthInvalidationIntervalMulti(interval time.Duration) OptionMulti {
	return func(s *MultiClient) {
		s.healthInvalidationInterval = interval
	}
}

// WithSyncDistanceTolerance sets the number of blocks that is acceptable to lag behind.
func WithSyncDistanceTolerance(count uint64) Option {
	return func(s *ExecutionClient) {
		s.syncDistanceTolerance = count
	}
}

// WithSyncDistanceToleranceMulti sets the number of blocks that is acceptable to lag behind.
func WithSyncDistanceToleranceMulti(count uint64) OptionMulti {
	return func(s *MultiClient) {
		s.syncDistanceTolerance = count
	}
}
