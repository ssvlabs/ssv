package executionclient

import (
	"context"
	"fmt"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/ethereum/go-ethereum/rpc"
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

// WithFinalizedBlocksSubscription setting up a subscription for beacon sync channel to be consumed in streamLogsToChan
func WithFinalizedBlocksSubscription(
	ctx context.Context,
	subscribe func(ctx context.Context, finalizedBlocks chan<- *eth2apiv1.FinalizedCheckpointEvent) error,
) Option {
	return func(s *ExecutionClient) {
		if s.finalizedCheckpointFeed == nil {
			s.finalizedCheckpointFeed = make(chan *eth2apiv1.FinalizedCheckpointEvent)
		}
		if err := subscribe(ctx, s.finalizedCheckpointFeed); err != nil {
			panic(fmt.Errorf("can't setup dependencies for exec client: %w", err))
		}
	}
}

// WithFinalizedCheckpointsFork sets the height for fork switch to handling only finalized blocks
func WithFinalizedCheckpointsFork(
	finalizedCheckpointForkActivationHeight uint64,
) Option {
	return func(s *ExecutionClient) {
		s.finalizedCheckpointActivationHeight = finalizedCheckpointForkActivationHeight
	}
}

// WithCustomGetHeaderArg sets the exact
func WithCustomGetHeaderArg(
	arg rpc.BlockNumber,
) Option {
	return func(s *ExecutionClient) {
		s.rpcGetHeaderArg = arg
	}
}
