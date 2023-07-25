package executionclient

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
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
	finalizationOffset          uint64 // TODO: consider reading the finalized checkpoint from consensus layer
	connectionTimeout           time.Duration
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration
	logBatchSize                uint64

	// variables
	client    atomic.Pointer[ethclient.Client]
	connectMu sync.Mutex
	closed    chan struct{}
}

// New creates a new instance of ExecutionClient.
func New(nodeAddr string, contractAddr ethcommon.Address, opts ...Option) *ExecutionClient {
	client := &ExecutionClient{
		nodeAddr:                    nodeAddr,
		contractAddress:             contractAddr,
		logger:                      zap.NewNop(),
		metrics:                     nopMetrics{},
		finalizationOffset:          DefaultFinalizationOffset,
		connectionTimeout:           DefaultConnectionTimeout,
		reconnectionInitialInterval: DefaultReconnectionInitialInterval,
		reconnectionMaxInterval:     DefaultReconnectionMaxInterval,
		logBatchSize:                DefaultHistoricalLogsBatchSize, // TODO Make batch of logs adaptive depending on "websocket: read limit"
		closed:                      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// Connect connects to Ethereum execution client.
func (ec *ExecutionClient) Connect(ctx context.Context) {
	ec.connectMu.Lock()
	defer ec.connectMu.Unlock()

	if ec.client.Load() != nil {
		ec.reconnect(ctx)
		return
	}

	if err := ec.connect(ctx); err != nil {
		ec.reconnect(ctx)
	}
}

// Close shuts down ExecutionClient.
func (ec *ExecutionClient) Close() error {
	close(ec.closed)

	if client := ec.client.Load(); client != nil {
		client.Close()
	}

	return nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logCh <-chan ethtypes.Log, fetchErrCh <-chan error, err error) {
	client := ec.client.Load()
	if client == nil {
		return nil, nil, ErrNotConnected
	}

	currentBlock, err := client.BlockNumber(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get current block: %w", err)
	}

	lastBlock := currentBlock - ec.finalizationOffset
	logger := ec.logger.With(
		zap.Uint64("from", fromBlock),
		zap.Uint64("to", lastBlock))

	logger.Info("fetching registry events in batches",
		zap.Uint64("current_block", currentBlock))

	logStream, fetchErrors := ec.fetchLogsInBatches(ctx, fromBlock, lastBlock)
	return logStream, fetchErrors, nil
}

// Calls FilterLogs multiple times and batches results to avoid fetching enormous amount of events
func (ec *ExecutionClient) fetchLogsInBatches(ctx context.Context, startBlock, endBlock uint64) (<-chan ethtypes.Log, <-chan error) {
	logCh := make(chan ethtypes.Log, defaultLogBuf)
	fetchErrCh := make(chan error, 1)

	go func() {
		defer close(logCh)
		defer close(fetchErrCh)

		if startBlock > endBlock {
			fetchErrCh <- ErrBadInput
			return
		}

		logCount := 0

		for fromBlock := startBlock; fromBlock <= endBlock; fromBlock += ec.logBatchSize {
			toBlock := fromBlock + ec.logBatchSize - 1
			if toBlock > endBlock {
				toBlock = endBlock
			}

			client := ec.client.Load()
			if client == nil {
				fetchErrCh <- ErrNotConnected
				return
			}

			batchLogs, err := client.FilterLogs(context.Background(), ethereum.FilterQuery{
				Addresses: []ethcommon.Address{ec.contractAddress},
				FromBlock: new(big.Int).SetUint64(fromBlock),
				ToBlock:   new(big.Int).SetUint64(toBlock),
			})
			if err != nil {
				ec.logger.Error("failed to fetch events batch", zap.Error(err))
				fetchErrCh <- err
				return
			}

			ec.logger.Info("fetched events in batch",
				zap.Uint64("from", fromBlock),
				zap.Uint64("to", toBlock),
				zap.Uint64("target", endBlock),
				zap.String("progress", fmt.Sprintf("%.2f%%", float64(fromBlock-startBlock)/float64(endBlock-startBlock)*100)),
				fields.Count(len(batchLogs)),
			)

			select {
			case <-ctx.Done():
				ec.logger.Debug("batched log fetching canceled")
				fetchErrCh <- ctx.Err()
				return

			case <-ec.closed:
				ec.logger.Debug("closed")
				fetchErrCh <- ErrClosed
				return

			default:
				ec.logger.Debug("fetched log batch",
					zap.Uint64("from", fromBlock),
					zap.Uint64("to", toBlock),
					fields.Count(len(batchLogs)),
				)
				for _, log := range batchLogs {
					if log.Removed {
						// TODO: test this case
						continue
					}
					logCh <- log
					logCount++
				}
			}
		}

		ec.logger.Debug("fetching registry events in batches completed",
			zap.Uint64("from", startBlock),
			zap.Uint64("to", endBlock),
			zap.String("progress", "100%"),
			fields.Count(logCount),
		)

		ec.metrics.ExecutionClientLastFetchedBlock(endBlock)
	}()

	return logCh, fetchErrCh
}

// StreamLogs subscribes to events emitted by the contract.
func (ec *ExecutionClient) StreamLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log {
	logs := make(chan ethtypes.Log)

	go func() {
		defer close(logs)

		for {
			select {
			case <-ctx.Done():
				ec.logger.Debug("log streaming canceled")
				return

			case <-ec.closed:
				ec.logger.Debug("closed")
				return

			default:
				lastBlock, err := ec.streamLogsToChan(ctx, logs, fromBlock)
				if err != nil {
					ec.logger.Error("log streaming failed", zap.Error(err))
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

	client := ec.client.Load()
	if client == nil {
		return false, ErrNotConnected
	}

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	sp, err := client.SyncProgress(ctx)
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

// HealthCheck is left for compatibility, TODO: consider removing
func (ec *ExecutionClient) HealthCheck() []string {
	ready, err := ec.IsReady(context.Background())
	if err != nil {
		return []string{err.Error()}
	}

	if !ready {
		return []string{"syncing"}
	}

	return []string{}
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
	client := ec.client.Load()
	if client == nil {
		return 0, ErrNotConnected
	}

	heads := make(chan *ethtypes.Header)

	sub, err := client.SubscribeNewHead(ctx, heads)
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
			query := ethereum.FilterQuery{
				Addresses: []ethcommon.Address{ec.contractAddress},
				FromBlock: new(big.Int).SetUint64(fromBlock),
				ToBlock:   header.Number,
			}

			logStream, fetchErrors := ec.fetchLogsInBatches(ctx, fromBlock, header.Number.Uint64())

			for log := range logStream {
				logs <- log
			}

			err = <-fetchErrors

			if err != nil {
				return fromBlock, fmt.Errorf("fetch logs: %w", err)
			}
			fromBlock = query.ToBlock.Uint64()
			ec.logger.Info("last fetched block", fields.BlockNumber(fromBlock))
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

	logger.Info("connecting", zap.Duration("timeout", ec.connectionTimeout))

	client, err := ethclient.DialContext(ctx, ec.nodeAddr)
	if err != nil {
		logger.Error("connection failed", zap.Error(err))
		return err
	}

	logger.Info("connected")
	ec.client.Store(client)

	return nil
}

// reconnect tries to reconnect multiple times with an exponent interval.
// It panics when reconnecting limit is reached.
// It must not be called twice in parallel.
func (ec *ExecutionClient) reconnect(ctx context.Context) {
	logger := ec.logger.With(fields.Address(ec.nodeAddr))

	if cl := ec.client.Load(); cl != nil {
		cl.Close()
	}

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

	logger.Info("reconnected")
}

func (ec *ExecutionClient) Filterer() (*contract.ContractFilterer, error) {
	return contract.NewContractFilterer(ec.contractAddress, ec.client.Load())
}
