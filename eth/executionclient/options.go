package executionclient

import (
	"time"

	"go.uber.org/zap"
)

// Option defines an ExecutionClient configuration option.
type Option func(*ExecutionClient)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(s *ExecutionClient) {
		s.logger = logger.Named("execution_client")
	}
}

// WithMetrics enables reporting metrics.
func WithMetrics(metrics metrics) Option {
	return func(s *ExecutionClient) {
		s.metrics = metrics
	}
}

// WithFollowDistance sets finalization offset.
// It defines how many blocks in the past the latest block we want to process is.
func WithFollowDistance(offset uint64) Option {
	return func(s *ExecutionClient) {
		s.followDistance = offset
	}
}

// WithConnectionTimeout sets timeout for network connection to eth1 node.
func WithConnectionTimeout(timeout time.Duration) Option {
	return func(s *ExecutionClient) {
		s.connectionTimeout = timeout
	}
}

// WithReconnectionInitialInterval sets initial reconnection interval.
func WithReconnectionInitialInterval(interval time.Duration) Option {
	return func(s *ExecutionClient) {
		s.reconnectionInitialInterval = interval
	}
}

// WithReconnectionMaxInterval sets max reconnection interval.
func WithReconnectionMaxInterval(interval time.Duration) Option {
	return func(s *ExecutionClient) {
		s.reconnectionMaxInterval = interval
	}
}

// WithLogBatchSize sets log batch size.
func WithLogBatchSize(size uint64) Option {
	return func(s *ExecutionClient) {
		s.logBatchSize = size
	}
}
