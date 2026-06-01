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

// WithQueryAddr sets the HTTP endpoint used for request/response calls
// (eth_getLogs, eth_blockNumber, etc.) while subscriptions stay on the
// subscription transport. Empty string keeps the legacy single-transport
// behavior. See ethClient godoc and hyperledger/besu#9848 for context.
func WithQueryAddr(addr string) Option {
	return func(s *ExecutionClient) {
		s.queryAddr = addr
	}
}

// WithLoggerMulti enables logging.
func WithLoggerMulti(logger *zap.Logger) OptionMulti {
	return func(s *MultiClient) {
		s.logger = logger.Named(log.NameExecutionClientMulti)
	}
}

// WithQueryAddrsMulti sets the per-client HTTP query endpoints. The slice is
// paired positionally with the subscription addrs passed to NewMulti — entry i
// is the HTTP endpoint for subscribe addr i. Empty strings (or a shorter slice)
// fall back to single-transport for those clients. See WithQueryAddr.
func WithQueryAddrsMulti(addrs []string) OptionMulti {
	return func(s *MultiClient) {
		s.queryAddrs = addrs
	}
}

// WithReqTimeout sets timeout for RPC requests to eth1 node.
// The timeout must be positive, otherwise the default value will be used.
func WithReqTimeout(timeout time.Duration) Option {
	return func(s *ExecutionClient) {
		if timeout > 0 {
			s.reqTimeout = timeout
		}
	}
}

// WithReqTimeoutMulti sets timeout for RPC requests to eth1 node.
// The timeout must be positive, otherwise the default value will be used.
func WithReqTimeoutMulti(timeout time.Duration) OptionMulti {
	return func(s *MultiClient) {
		if timeout > 0 {
			s.reqTimeout = timeout
		}
	}
}

// WithHealthInvalidationInterval sets health invalidation interval. 0 disables caching.
func WithHealthInvalidationInterval(interval time.Duration) Option {
	return func(s *ExecutionClient) {
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
