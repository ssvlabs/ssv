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
	return func(s *ExecutionClient) {
		s.logger = logger.Named("execution_client")
	}
}

// WithLoggerMulti enables logging.
func WithLoggerMulti(logger *zap.Logger) OptionMulti {
	return func(s *MultiClient) {
		s.logger = logger.Named("execution_client_multi")
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
func WithConnectionTimeout(timeout time.Duration) Option {
	return func(s *ExecutionClient) {
		s.connectionTimeout = timeout
	}
}

// WithConnectionTimeoutMulti sets timeout for network connection to eth1 node.
func WithConnectionTimeoutMulti(timeout time.Duration) OptionMulti {
	return func(s *MultiClient) {
		s.connectionTimeout = timeout
	}
}

// WithReconnectionInitialInterval sets initial reconnection interval.
func WithReconnectionInitialInterval(interval time.Duration) Option {
	return func(s *ExecutionClient) {
		s.reconnectionInitialInterval = interval
	}
}

// WithReconnectionInitialIntervalMulti sets initial reconnection interval.
func WithReconnectionInitialIntervalMulti(interval time.Duration) OptionMulti {
	return func(s *MultiClient) {
		s.reconnectionInitialInterval = interval
	}
}

// WithReconnectionMaxInterval sets max reconnection interval.
func WithReconnectionMaxInterval(interval time.Duration) Option {
	return func(s *ExecutionClient) {
		s.reconnectionMaxInterval = interval
	}
}

// WithReconnectionMaxIntervalMulti sets max reconnection interval.
func WithReconnectionMaxIntervalMulti(interval time.Duration) OptionMulti {
	return func(s *MultiClient) {
		s.reconnectionMaxInterval = interval
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

// WithHTTPFallback sets the HTTP fallback address for the ExecutionClient configuration.
func WithHTTPFallback(addr string) Option {
	return func(s *ExecutionClient) {
		s.httpFallbackAddr = addr
	}
}

// WithHTTPFallbackMulti sets the HTTP fallback address for the MultiClient configuration.
func WithHTTPFallbackMulti(addr string) OptionMulti {
	return func(s *MultiClient) {
		s.httpFallbackAddr = addr
	}
}

// WithAdaptiveBatch configures an ExecutionClient to use an adaptive batcher.
func WithAdaptiveBatch(cfg BatcherConfig) Option {
	return func(s *ExecutionClient) {
		s.batcher = NewAdaptiveBatcherWithConfig(cfg)
	}
}

// WithAdaptiveBatchMulti configures a MultiClient to use adaptive batchers.
func WithAdaptiveBatchMulti(cfg BatcherConfig) OptionMulti {
	return func(s *MultiClient) {
		s.batcher = NewAdaptiveBatcherWithConfig(cfg)
	}
}
