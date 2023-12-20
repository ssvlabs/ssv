package executionclient

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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

// WithFinalizedBlocksSubscription setting up a subscription for beacon sync channel to be consumed in streamLogsToChan
func WithFinalizedBlocksSubscription(
	ctx context.Context,
	subscribe func(ctx context.Context, finalizedBlocks chan<- uint64) error,
) Option {
	return func(s *ExecutionClient) {
		if s.blocksChan == nil {
			s.blocksChan = make(chan uint64)
		}
		if err := subscribe(ctx, s.blocksChan); err != nil {
			panic(fmt.Errorf("can't setup dependencies for exec client: %w", err))
		}
	}
}

// WithFinalizedCheckpointsFork sets the exact
func WithFinalizedCheckpointsFork(
	finalizedCheckpointActivationSlot phase0.Slot,
) Option {
	return func(s *ExecutionClient) {
		s.finalizedCheckpointActivationSlot = uint64(finalizedCheckpointActivationSlot)
	}
}
