package executionclient

import (
	"context"
	"fmt"
	"math"
	"math/big"
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

// ExecutionClient represents a client for interacting with Ethereum execution client.
type ExecutionClient struct {
	// mandatory
	nodeAddr        string
	contractAddress ethcommon.Address

	// optional
	logger                      *zap.Logger
	metrics                     metrics
	finalizationOffset          uint64
	connectionTimeout           time.Duration
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration
	logBatchSize                uint64

	// variables
	client atomic.Pointer[ethclient.Client]
	closed chan struct{}
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
func (ec *ExecutionClient) Connect(ctx context.Context) error {
	if ec.client.Load() != nil {
		ec.reconnect(ctx)
		return nil
	}

	if err := ec.connect(ctx); err != nil {
		ec.reconnect(ctx)
	}

	return nil
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
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs []ethtypes.Log, lastBlock uint64, err error) {

	client := ec.client.Load()

	currentBlock, err := client.BlockNumber(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("get current block: %w", err)
	}

	lastBlock = currentBlock - ec.finalizationOffset
	logger := ec.logger.With(
		zap.Uint64("from", fromBlock),
		zap.Uint64("to", lastBlock))

	logger.Info("determined current block number, fetching historical logs",
		zap.Uint64("current_block", currentBlock))

	query := ethereum.FilterQuery{
		Addresses: []ethcommon.Address{ec.contractAddress},
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(lastBlock),
	}

	inLogs := ec.batchedFilterLogs(ctx, client, query)
	for l := range inLogs {
		if l.err != nil {
			return nil, 0, fmt.Errorf("fetch logs error: %w", l.err)
		}
		lastBlock = l.lastBlock
		logs = append(logs, l.logs...)
	}

	logger.Info("fetched historical blocks")
	ec.metrics.LastFetchedBlock(lastBlock)

	return logs, lastBlock, nil
}

type FetchedLogs struct {
	logs      []ethtypes.Log
	lastBlock uint64
	err       error
}

// Calls FilterLogs multiple times and batches results to avoid fetching enormous amount of events
func (ec *ExecutionClient) batchedFilterLogs(ctx context.Context, client *ethclient.Client, query ethereum.FilterQuery) <-chan FetchedLogs {

	logsChan := make(chan FetchedLogs)

	go func() {
		defer close(logsChan)

		q := math.Floor(float64((query.ToBlock.Uint64() - query.FromBlock.Uint64()) / ec.logBatchSize))
		r := (query.ToBlock.Uint64() - query.FromBlock.Uint64()) % ec.logBatchSize

		if r != 0 {
			q++
		}

		for i := 0; i < int(q); i++ {

			var blockFrom uint64
			var blockTo uint64
			
			if i == int(q-1) && r != 0 {
				blockFrom = query.FromBlock.Uint64() + ec.logBatchSize*uint64(i)
				blockTo = blockFrom + r
			} else {
				blockFrom = query.FromBlock.Uint64() + ec.logBatchSize*uint64(i)
				blockTo = blockFrom + ec.logBatchSize
			}

			ec.logger.Info("Fetching :", zap.Uint64("from block", blockFrom), zap.Uint64("to block", blockTo))
			batchOfLogs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
				Addresses: []ethcommon.Address{ec.contractAddress},
				FromBlock: new(big.Int).SetUint64(blockFrom),
				ToBlock:   new(big.Int).SetUint64(blockTo),
			})
			if err != nil {
				ec.logger.Error("log fetching err:", zap.Error(err))
			}
			select {
			case <-ctx.Done():
				ec.logger.Debug("log fetching canceled")
				return

			case <-ec.closed:
				ec.logger.Debug("closed")
				return

			case logsChan <- FetchedLogs{logs: batchOfLogs, lastBlock: blockTo, err: err}:
			}
		}
	}()

	return logsChan
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

	heads := make(chan *ethtypes.Header)

	sub, err := client.SubscribeNewHead(ctx, heads)
	if err != nil {
		return fromBlock, fmt.Errorf("subscribe heads: %w", err)
	}

	for {
		select {
		case err := <-sub.Err():
			return fromBlock, fmt.Errorf("subscription: %w", err)

		case header := <-heads:
			query := ethereum.FilterQuery{
				Addresses: []ethcommon.Address{ec.contractAddress},
				FromBlock: new(big.Int).SetUint64(fromBlock),
				ToBlock:   header.Number,
			}

			// TODO: Instead of FilterLogs it should call a wrapper that calls FilterLogs multiple times and batches results to avoid fetching enormous amount of events.
			newLogs, err := client.FilterLogs(ctx, query)
			if err != nil {
				return fromBlock, fmt.Errorf("fetch logs: %w", err)
			}

			for _, log := range newLogs {
				logs <- log
			}

			fromBlock = query.ToBlock.Uint64()
			ec.logger.Info("last fetched block", fields.BlockNumber(fromBlock))
			ec.metrics.LastFetchedBlock(fromBlock)
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

func(ec *ExecutionClient) Filterer() (*contract.ContractFilterer, error) {
	client := ec.client.Load()
	filterer, err := contract.NewContractFilterer(ethcommon.Address{}, client)
	return filterer, err
}
