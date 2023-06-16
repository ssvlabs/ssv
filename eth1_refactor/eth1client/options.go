package eth1client

import (
	"time"

	"go.uber.org/zap"
)

// Option defines an Eth1Client configuration option.
type Option func(*Eth1Client)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(s *Eth1Client) {
		s.logger = logger.Named("eth1_client")
	}
}

// WithMetrics enables reporting metrics.
func WithMetrics(metrics metrics) Option {
	return func(s *Eth1Client) {
		s.metrics = metrics
	}
}

// WithFinalizationOffset sets finalization offset.
// It defines how many blocks in the past the latest block we want to process is.
func WithFinalizationOffset(offset uint64) Option {
	return func(s *Eth1Client) {
		s.finalizationOffset = offset
	}
}

// WithConnectionTimeout sets timeout for network connection to eth1 node.
func WithConnectionTimeout(timeout time.Duration) Option {
	return func(s *Eth1Client) {
		s.connectionTimeout = timeout
	}
}

// WithReconnectionInitialInterval sets initial reconnection interval.
func WithReconnectionInitialInterval(interval time.Duration) Option {
	return func(s *Eth1Client) {
		s.reconnectionInitialInterval = interval
	}
}

// WithReconnectionMaxInterval sets max reconnection interval.
func WithReconnectionMaxInterval(interval time.Duration) Option {
	return func(s *Eth1Client) {
		s.reconnectionMaxInterval = interval
	}
}
