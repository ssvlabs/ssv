// Package executionclient implements functions for interacting with Ethereum execution clients.
package executionclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/utils/tasks"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=executionclient -destination=./mocks.go -source=./execution_client.go

type Provider interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan BlockLogs, errors <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs
	Filterer() (*contract.ContractFilterer, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*ethtypes.Block, error)
	HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error)
	ChainID(ctx context.Context) (*big.Int, error)
	Healthy(ctx context.Context) error
	SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error)
	Close() error
}

type SingleClientProvider interface {
	Provider
	SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	streamLogsToChan(ctx context.Context, logs chan<- BlockLogs, fromBlock uint64) (lastBlock uint64, err error)
}

var _ Provider = &ExecutionClient{}

var (
	ErrClosed        = fmt.Errorf("closed")
	ErrNotConnected  = fmt.Errorf("not connected")
	ErrBadInput      = fmt.Errorf("bad input")
	ErrNothingToSync = errors.New("nothing to sync")
)

const elResponseErrMsg = "Execution client returned an error"

// ExecutionClient represents a client for interacting with Ethereum execution client.
type ExecutionClient struct {
	// mandatory
	nodeAddr        string
	contractAddress ethcommon.Address

	// optional
	logger *zap.Logger
	// followDistance defines an offset into the past from the head block such that the block
	// at this offset will be considered as very likely finalized.
	followDistance              uint64 // TODO: consider reading the finalized checkpoint from consensus layer
	connectionTimeout           time.Duration
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration
	healthInvalidationInterval  time.Duration
	logBatchSize                uint64

	syncDistanceTolerance uint64
	syncProgressFn        func(context.Context) (*ethereum.SyncProgress, error)

	// variables
	client         *ethclient.Client
	closed         chan struct{}
	lastSyncedTime atomic.Int64
}

// New creates a new instance of ExecutionClient.
func New(ctx context.Context, nodeAddr string, contractAddr ethcommon.Address, opts ...Option) (*ExecutionClient, error) {
	client := &ExecutionClient{
		nodeAddr:                    nodeAddr,
		contractAddress:             contractAddr,
		logger:                      zap.NewNop(),
		followDistance:              DefaultFollowDistance,
		connectionTimeout:           DefaultConnectionTimeout,
		reconnectionInitialInterval: DefaultReconnectionInitialInterval,
		reconnectionMaxInterval:     DefaultReconnectionMaxInterval,
		healthInvalidationInterval:  DefaultHealthInvalidationInterval,
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

	client.syncProgressFn = client.syncProgress

	return client, nil
}

// TODO: add comments about SyncProgress, syncProgress, syncProgressFn
func (ec *ExecutionClient) SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return ec.syncProgressFn(ctx)
}

func (ec *ExecutionClient) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return ec.client.SyncProgress(ctx)
}

// Close shuts down ExecutionClient.
func (ec *ExecutionClient) Close() error {
	close(ec.closed)
	ec.client.Close()
	return nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan BlockLogs, errors <-chan error, err error) {
	currentBlock, err := ec.client.BlockNumber(ctx)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_blockNumber"),
			zap.Error(err))
		return nil, nil, fmt.Errorf("failed to get current block: %w", err)
	}
	if currentBlock < ec.followDistance {
		return nil, nil, ErrNothingToSync
	}
	toBlock := currentBlock - ec.followDistance
	if toBlock < fromBlock {
		return nil, nil, ErrNothingToSync
	}

	logs, errors = ec.fetchLogsInBatches(ctx, fromBlock, toBlock)
	return
}

// Calls FilterLogs multiple times and batches results to avoid fetching enormous amount of events
func (ec *ExecutionClient) fetchLogsInBatches(ctx context.Context, startBlock, endBlock uint64) (<-chan BlockLogs, <-chan error) {
	logs := make(chan BlockLogs, defaultLogBuf)
	errors := make(chan error, 1)

	go func() {
		defer close(logs)
		defer close(errors)

		if startBlock > endBlock {
			errors <- ErrBadInput
			return
		}

		for fromBlock := startBlock; fromBlock <= endBlock; fromBlock += ec.logBatchSize {
			toBlock := fromBlock + ec.logBatchSize - 1
			if toBlock > endBlock {
				toBlock = endBlock
			}

			start := time.Now()
			results, err := ec.client.FilterLogs(ctx, ethereum.FilterQuery{
				Addresses: []ethcommon.Address{ec.contractAddress},
				FromBlock: new(big.Int).SetUint64(fromBlock),
				ToBlock:   new(big.Int).SetUint64(toBlock),
			})
			if err != nil {
				ec.logger.Error(elResponseErrMsg,
					zap.String("method", "eth_getLogs"),
					zap.Error(err))
				errors <- err
				return
			}

			ec.logger.Info("fetched registry events",
				fields.FromBlock(fromBlock),
				fields.ToBlock(toBlock),
				zap.Uint64("target_block", endBlock),
				zap.String("progress", fmt.Sprintf("%.2f%%", float64(toBlock-startBlock+1)/float64(endBlock-startBlock+1)*100)),
				zap.Int("events", len(results)),
				fields.Took(time.Since(start)),
			)

			select {
			case <-ctx.Done():
				errors <- ctx.Err()
				return

			case <-ec.closed:
				errors <- ErrClosed
				return

			default:
				validLogs := make([]ethtypes.Log, 0, len(results))
				for _, log := range results {
					if log.Removed {
						// This shouldn't happen unless there was a reorg!
						ec.logger.Warn("log is removed",
							zap.String("block_hash", log.BlockHash.Hex()),
							fields.TxHash(log.TxHash),
							zap.Uint("log_index", log.Index))
						continue
					}
					validLogs = append(validLogs, log)
				}
				var highestBlock uint64
				for _, blockLogs := range PackLogs(validLogs) {
					logs <- blockLogs
					if blockLogs.BlockNumber > highestBlock {
						highestBlock = blockLogs.BlockNumber
					}
				}
				// Emit empty block logs to indicate that we have advanced to this block.
				if highestBlock < toBlock {
					logs <- BlockLogs{BlockNumber: toBlock}
				}
			}
		}
	}()

	return logs, errors
}

// StreamLogs subscribes to events emitted by the contract.
func (ec *ExecutionClient) StreamLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs {
	logs := make(chan BlockLogs)

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
				if errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
					// Closed gracefully.
					return
				}

				// streamLogsToChan should never return without an error,
				// so we treat a nil error as an error by itself.
				if err == nil {
					err = errors.New("streamLogsToChan halted without an error")
				}

				tries++
				if tries > 2 {
					ec.logger.Fatal("failed to stream registry events", zap.Error(err))
				}
				if lastBlock > fromBlock {
					// Successfully streamed some logs, reset tries.
					tries = 0
				}

				ec.logger.Error("failed to stream registry events, reconnecting", zap.Error(err))
				ec.reconnect(ctx) // TODO: ethclient implements reconnection, consider removing this logic after thorough testing
				fromBlock = lastBlock + 1
			}
		}
	}()

	return logs
}

var errSyncing = fmt.Errorf("syncing")

// Healthy returns if execution client is currently healthy: responds to requests and not in the syncing state.
func (ec *ExecutionClient) Healthy(ctx context.Context) error {
	if ec.isClosed() {
		return ErrClosed
	}

	lastHealthyTime := time.Unix(ec.lastSyncedTime.Load(), 0)
	if ec.healthInvalidationInterval != 0 && time.Since(lastHealthyTime) <= ec.healthInvalidationInterval {
		// Synced recently, reuse the result (only if ec.healthInvalidationInterval is set).
		return nil
	}

	return ec.healthy(ctx)
}

func (ec *ExecutionClient) healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	start := time.Now()
	sp, err := ec.SyncProgress(ctx)
	if err != nil {
		recordExecutionClientStatus(ctx, statusFailure, ec.nodeAddr)
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_syncing"),
			zap.Error(err))
		return err
	}
	recordRequestDuration(ctx, ec.nodeAddr, time.Since(start))

	if sp != nil {
		syncDistance := max(sp.HighestBlock, sp.CurrentBlock) - sp.CurrentBlock
		observability.RecordUint64Value(ctx, syncDistance, syncDistanceGauge.Record, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))

		// block out of sync distance tolerance
		if syncDistance > ec.syncDistanceTolerance {
			recordExecutionClientStatus(ctx, statusSyncing, ec.nodeAddr)
			return fmt.Errorf("sync distance exceeds tolerance (%d): %w", syncDistance, errSyncing)
		}
	} else {
		syncDistanceGauge.Record(ctx, 0, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))
	}

	recordExecutionClientStatus(ctx, statusReady, ec.nodeAddr)
	ec.lastSyncedTime.Store(time.Now().Unix())

	return nil
}

func (ec *ExecutionClient) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
	b, err := ec.client.BlockByNumber(ctx, blockNumber)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_getBlockByNumber"),
			zap.Error(err))
		return nil, err
	}

	return b, nil
}

func (ec *ExecutionClient) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
	h, err := ec.client.HeaderByNumber(ctx, blockNumber)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_getBlockByNumber"),
			zap.Error(err))
		return nil, err
	}

	return h, nil
}

func (ec *ExecutionClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	logs, err := ec.client.SubscribeFilterLogs(ctx, q, ch)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "EthSubscribe"),
			zap.Error(err))
		return nil, err
	}

	return logs, nil
}

func (ec *ExecutionClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	logs, err := ec.client.FilterLogs(ctx, q)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_getLogs"),
			zap.Error(err))
		return nil, err
	}

	return logs, nil
}

func (ec *ExecutionClient) isClosed() bool {
	select {
	case <-ec.closed:
		return true
	default:
		return false
	}
}

// streamLogsToChan streams ongoing logs from the given block to the given channel.
// streamLogsToChan *always* returns the last block it fetched, even if it errored.
// TODO: consider handling "websocket: read limit exceeded" error and reducing batch size (syncSmartContractsEvents has code for this)
func (ec *ExecutionClient) streamLogsToChan(ctx context.Context, logs chan<- BlockLogs, fromBlock uint64) (lastBlock uint64, err error) {
	heads := make(chan *ethtypes.Header)

	// Generally, execution client can stream logs using SubscribeFilterLogs, but we chose to use SubscribeNewHead + FilterLogs.
	//
	// We must receive all events as they determine the state of the ssv network, so a discrepancy can result in slashing.
	// Therefore, we must be sure that we don't miss any log while streaming.
	//
	// With SubscribeFilterLogs we cannot specify the block we subscribe from, it always starts at the highest.
	// So with streaming we had some bugs because of missing blocks:
	// - first sync history from genesis to block 100, but then stream sometimes starts late at 102 (missed 101)
	// - inevitably miss blocks during any stream connection interruptions (such as EL restarts)
	//
	// Thus, we decided not to rely on log streaming and use SubscribeNewHead + FilterLogs.
	//
	// It also allowed us to implement more 'atomic' behaviour easier:
	// We can revert the tx if there was an error in processing all the events of a block.
	// So we can restart from this block once everything is good.
	sub, err := ec.client.SubscribeNewHead(ctx, heads)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("operation", "SubscribeNewHead"),
			zap.Error(err))
		return fromBlock, fmt.Errorf("subscribe heads: %w", err)
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return fromBlock, context.Canceled

		case <-ec.closed:
			return fromBlock, ErrClosed

		case err := <-sub.Err():
			if err == nil {
				return fromBlock, ErrClosed
			}
			return fromBlock, fmt.Errorf("subscription: %w", err)

		case header := <-heads:
			if header.Number.Uint64() < ec.followDistance {
				continue
			}
			toBlock := header.Number.Uint64() - ec.followDistance
			if toBlock < fromBlock {
				continue
			}
			logStream, fetchErrors := ec.fetchLogsInBatches(ctx, fromBlock, toBlock)
			for block := range logStream {
				logs <- block
				lastBlock = block.BlockNumber
			}
			if err := <-fetchErrors; err != nil {
				// If we get an error while fetching, we return the last block we fetched.
				return lastBlock, fmt.Errorf("fetch logs: %w", err)
			}
			fromBlock = toBlock + 1
			observability.RecordUint64Value(ctx, fromBlock, lastProcessedBlockGauge.Record, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))
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
	client, err := ethclient.DialContext(ctx, ec.nodeAddr)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("operation", "DialContext"),
			zap.Error(err))
		return err
	}
	ec.client = client

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

func (ec *ExecutionClient) ChainID(ctx context.Context) (*big.Int, error) {
	return ec.client.ChainID(ctx)
}
