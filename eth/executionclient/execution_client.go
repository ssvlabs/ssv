package executionclient

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/utils/tasks"
)

var (
	ErrClosed       = fmt.Errorf("closed")
	ErrNotConnected = fmt.Errorf("not connected")
	ErrBadInput     = fmt.Errorf("bad input")
)

// ExecutionClient represents a client for interacting with Ethereum execution client.
type ExecutionClient struct {
	// mandatory
	nodeAddr        string
	contractAddress ethcommon.Address

	// optional
	logger                      *zap.Logger
	metrics                     metrics
	followDistance              uint64 // TODO: consider reading the finalized checkpoint from consensus layer
	connectionTimeout           time.Duration
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration
	logBatchSize                uint64

	// variables
	client *ethclient.Client
	closed chan struct{}
}

// New creates a new instance of ExecutionClient.
func New(ctx context.Context, nodeAddr string, contractAddr ethcommon.Address, opts ...Option) (*ExecutionClient, error) {
	client := &ExecutionClient{
		nodeAddr:                    nodeAddr,
		contractAddress:             contractAddr,
		logger:                      zap.NewNop(),
		metrics:                     nopMetrics{},
		followDistance:              DefaultFollowDistance,
		connectionTimeout:           DefaultConnectionTimeout,
		reconnectionInitialInterval: DefaultReconnectionInitialInterval,
		reconnectionMaxInterval:     DefaultReconnectionMaxInterval,
		logBatchSize:                DefaultHistoricalLogsBatchSize, // TODO Make batch of logs adaptive depending on "websocket: read limit"
		closed:                      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(client)
	}
	err := client.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to execution client: %w", err)
	}
	return client, nil
}

// Close shuts down ExecutionClient.
func (ec *ExecutionClient) Close() error {
	close(ec.closed)
	ec.client.Close()
	return nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan ethtypes.Log, errors <-chan error, err error) {
	currentBlock, err := ec.client.BlockNumber(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current block: %w", err)
	}
	toBlock := currentBlock - ec.followDistance

	logs, errors = ec.fetchLogsInBatches(ctx, fromBlock, toBlock)
	return
}

// Calls FilterLogs multiple times and batches results to avoid fetching enormous amount of events
func (ec *ExecutionClient) fetchLogsInBatches(ctx context.Context, startBlock, endBlock uint64) (<-chan ethtypes.Log, <-chan error) {
	logs := make(chan ethtypes.Log, defaultLogBuf)
	errors := make(chan error, 1)

	go func() {
		defer close(logs)
		defer close(errors)

		if startBlock > endBlock {
			errors <- ErrBadInput
			return
		}

		logCount := 0

		for fromBlock := startBlock; fromBlock <= endBlock; fromBlock += ec.logBatchSize {
			toBlock := fromBlock + ec.logBatchSize - 1
			if toBlock > endBlock {
				toBlock = endBlock
			}

			batchLogs, err := ec.client.FilterLogs(context.Background(), ethereum.FilterQuery{
				Addresses: []ethcommon.Address{ec.contractAddress},
				FromBlock: new(big.Int).SetUint64(fromBlock),
				ToBlock:   new(big.Int).SetUint64(toBlock),
			})
			if err != nil {
				errors <- err
				return
			}

			if len(batchLogs) != 0 {
				ec.logger.Info("fetched registry events batch",
					fields.FromBlock(fromBlock),
					fields.ToBlock(toBlock),
					zap.Uint64("target", endBlock),
					zap.String("progress", fmt.Sprintf("%.2f%%", float64(toBlock-startBlock+1)/float64(endBlock-startBlock+1)*100)),
					fields.Count(len(batchLogs)),
				)
			}

			select {
			case <-ctx.Done():
				errors <- ctx.Err()
				return

			case <-ec.closed:
				errors <- ErrClosed
				return

			default:
				for _, log := range batchLogs {
					if log.Removed {
						// TODO: test this case
						continue
					}
					logs <- log
					logCount++
				}
			}
		}

		ec.metrics.ExecutionClientLastFetchedBlock(endBlock)
	}()

	return logs, errors
}

// StreamLogs subscribes to events emitted by the contract.
func (ec *ExecutionClient) StreamLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log {
	logs := make(chan ethtypes.Log)

	go func() {
		defer close(logs)

		tries := 0
		for {
			select {
			case <-ctx.Done():
				return

			case <-ec.closed:
				return

			default:
				lastBlock, err := ec.streamLogsToChan(ctx, logs, fromBlock)
				if err != nil {
					tries++
					if tries > 3 {
						ec.logger.Fatal("failed to stream registry events", zap.Error(err))
					}
					ec.logger.Error("failed to stream registry events, reconnecting", zap.Error(err))
					ec.reconnect(ctx)
				}

				fromBlock = lastBlock + 1
			}
		}
	}()

	return logs
}

// IsReady returns if execution client is currently ready: responds to requests and not in the syncing state.
func (ec *ExecutionClient) IsReady(ctx context.Context) (bool, error) {
	if ec.isClosed() {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	sp, err := ec.client.SyncProgress(ctx)
	if err != nil {
		ec.metrics.ExecutionClientFailure()
		return false, err
	}

	if sp != nil {
		ec.metrics.ExecutionClientSyncing()
		return false, nil
	}

	ec.metrics.ExecutionClientReady()

	return true, nil
}

func (ec *ExecutionClient) isClosed() bool {
	select {
	case <-ec.closed:
		return true
	default:
		return false
	}
}

// TODO: consider handling "websocket: read limit exceeded" error and reducing batch size (syncSmartContractsEvents has code for this)
func (ec *ExecutionClient) streamLogsToChan(ctx context.Context, logs chan ethtypes.Log, fromBlock uint64) (lastBlock uint64, err error) {
	heads := make(chan *ethtypes.Header)

	sub, err := ec.client.SubscribeNewHead(ctx, heads)
	if err != nil {
		return fromBlock, fmt.Errorf("subscribe heads: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()

		case err := <-sub.Err():
			return 0, fmt.Errorf("subscription: %w", err)

		case header := <-heads:
			if header.Number.Uint64() < ec.followDistance {
				continue
			}
			toBlock := header.Number.Uint64() - ec.followDistance
			if toBlock < fromBlock {
				continue
			}
			logStream, fetchErrors := ec.fetchLogsInBatches(ctx, fromBlock, toBlock)
			for log := range logStream {
				logs <- log
			}
			if err := <-fetchErrors; err != nil {
				return fromBlock, fmt.Errorf("fetch logs: %w", err)
			}
			fromBlock = toBlock + 1
			ec.metrics.ExecutionClientLastFetchedBlock(fromBlock)
		}
	}
}

// connect connects to Ethereum execution client.
// It must not be called twice in parallel.
func (ec *ExecutionClient) connect(ctx context.Context) error {
	logger := ec.logger.With(fields.Address(ec.nodeAddr))

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	start := time.Now()
	var err error
	ec.client, err = ethclient.DialContext(ctx, ec.nodeAddr)
	if err != nil {
		return err
	}

	logger.Info("connected to execution client", zap.Duration("took", time.Since(start)))
	return nil
}

// reconnect tries to reconnect multiple times with an exponent interval.
// It panics when reconnecting limit is reached.
// It must not be called twice in parallel.
func (ec *ExecutionClient) reconnect(ctx context.Context) {
	logger := ec.logger.With(fields.Address(ec.nodeAddr))

	start := time.Now()
	tasks.ExecWithInterval(func(lastTick time.Duration) (stop bool, cont bool) {
		logger.Info("reconnecting")
		if err := ec.connect(ctx); err != nil {
			if ec.isClosed() {
				return true, false
			}
			// continue until reaching to limit, and then panic as Ethereum execution client connection is required
			if lastTick >= ec.reconnectionMaxInterval {
				logger.Panic("failed to reconnect", zap.Error(err))
			} else {
				logger.Warn("could not reconnect, still trying", zap.Error(err))
			}
			return false, false
		}
		return true, false
	}, ec.reconnectionInitialInterval, ec.reconnectionMaxInterval+(ec.reconnectionInitialInterval))

	logger.Info("reconnected to execution client", zap.Duration("took", time.Since(start)))
}

func (ec *ExecutionClient) Filterer() (*contract.ContractFilterer, error) {
	return contract.NewContractFilterer(ec.contractAddress, ec.client)
}
