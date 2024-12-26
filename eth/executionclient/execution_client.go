// Package executionclient implements functions for interacting with Ethereum execution clients.
package executionclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
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
	logBatchSize                uint64

	syncDistanceTolerance uint64
	syncProgressFn        func(context.Context) (*ethereum.SyncProgress, error)

	// variables
	currentClientIndexMu sync.Mutex
	currentClientIndex   int
	clients              []*ethclient.Client
	closed               chan struct{}
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

	same, err := assertSameChainIDs(ctx, client.clients...)
	if err != nil {
		return nil, fmt.Errorf("assert same chain IDs: %w", err)
	}
	if !same {
		return nil, fmt.Errorf("execution clients' chain IDs are not same")
	}

	client.syncProgressFn = client.syncProgress

	return client, nil
}

// assertSameChainIDs should receive a non-empty list
func assertSameChainIDs(ctx context.Context, clients ...*ethclient.Client) (bool, error) {
	firstChainID, err := clients[0].ChainID(ctx)
	if err != nil {
		return false, fmt.Errorf("get first chain ID: %w", err)
	}

	for _, client := range clients[1:] {
		clientChainID, err := client.ChainID(ctx)
		if err != nil {
			return false, fmt.Errorf("get client chain ID: %w", err)
		}

		if firstChainID.Cmp(clientChainID) != 0 {
			return false, nil
		}
	}

	return true, nil
}

func (ec *ExecutionClient) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	res, err := ec.doCall(ctx, func(ctx context.Context, client *ethclient.Client) (any, error) {
		return client.SyncProgress(ctx)
	})
	if err != nil {
		return nil, err
	}

	return res.(*ethereum.SyncProgress), nil
}

// Close shuts down ExecutionClient.
func (ec *ExecutionClient) Close() error {
	close(ec.closed)
	for _, client := range ec.clients {
		client.Close()
	}
	return nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan BlockLogs, errors <-chan error, err error) {
	res, err := ec.doCall(ctx, func(ctx context.Context, client *ethclient.Client) (any, error) {
		return client.BlockNumber(ctx)
	})
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_blockNumber"),
			zap.Error(err))
		return nil, nil, fmt.Errorf("failed to get current block: %w", err)
	}
	currentBlock := res.(uint64)

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
			res, err := ec.doCall(ctx, func(ctx context.Context, client *ethclient.Client) (any, error) {
				return client.FilterLogs(ctx, ethereum.FilterQuery{
					Addresses: []ethcommon.Address{ec.contractAddress},
					FromBlock: new(big.Int).SetUint64(fromBlock),
					ToBlock:   new(big.Int).SetUint64(toBlock),
				})
			})
			if err != nil {
				ec.logger.Error(elResponseErrMsg,
					zap.String("method", "eth_getLogs"),
					zap.Error(err))
				errors <- err
				return
			}
			results := res.([]ethtypes.Log)

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
				if len(validLogs) == 0 {
					// Emit empty block logs to indicate that we have advanced to this block.
					logs <- BlockLogs{BlockNumber: toBlock}
				} else {
					for _, blockLogs := range PackLogs(validLogs) {
						logs <- blockLogs
					}
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
				ec.reconnect(ctx)
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

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	start := time.Now()
	sp, err := ec.syncProgressFn(ctx)
	if err != nil {
		recordExecutionClientStatus(ctx, statusFailure, ec.nodeAddr)
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_syncing"),
			zap.Error(err))
		return err
	}
	recordRequestDuration(ctx, ec.nodeAddr, time.Since(start))

	if sp != nil {
		recordExecutionClientStatus(ctx, statusSyncing, ec.nodeAddr)

		syncDistance := max(sp.HighestBlock, sp.CurrentBlock) - sp.CurrentBlock

		observability.RecordUint64Value(ctx, syncDistance, syncDistanceGauge.Record, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))

		// block out of sync distance tolerance
		if syncDistance > ec.syncDistanceTolerance {
			return fmt.Errorf("sync distance exceeds tolerance (%d): %w", syncDistance, errSyncing)
		}
	}

	recordExecutionClientStatus(ctx, statusReady, ec.nodeAddr)

	syncDistanceGauge.Record(ctx, 0, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))

	return nil
}

func (ec *ExecutionClient) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
	res, err := ec.doCall(ctx, func(ctx context.Context, client *ethclient.Client) (any, error) {
		return client.BlockByNumber(ctx, blockNumber)
	})
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_getBlockByNumber"),
			zap.Error(err))
		return nil, err
	}
	b := res.(*ethtypes.Block)

	return b, nil
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

	res, err := ec.doCall(ctx, func(ctx context.Context, client *ethclient.Client) (any, error) {
		return client.SubscribeNewHead(ctx, heads)
	})
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("operation", "SubscribeNewHead"),
			zap.Error(err))
		return fromBlock, fmt.Errorf("subscribe heads: %w", err)
	}
	sub := res.(ethereum.Subscription)

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

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	addrList := strings.Split(ec.nodeAddr, ";") // TODO: temporary using ; as separator because , is used as separator by deployment bot
	if len(addrList) == 0 {
		return fmt.Errorf("no node address provided")
	}

	var clients []*ethclient.Client

	for _, addr := range addrList {
		logger := ec.logger.With(fields.Address(addr))
		start := time.Now()
		client, err := ethclient.DialContext(ctx, addr)
		if err != nil {
			logger.Error(elResponseErrMsg,
				zap.String("operation", "DialContext"),
				zap.Error(err))
			return err
		}

		logger.Info("connected to execution client",
			zap.Duration("took", time.Since(start)))

		clients = append(clients, client)
	}

	ec.clients = clients

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
		if err := ec.connect(ctx); err != nil { // TODO: handle case if some nodes are down
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
	return contract.NewContractFilterer(ec.contractAddress, ec.clients[0]) // TODO: Do we need more complex logic?
}

type callFunc func(ctx context.Context, client *ethclient.Client) (any, error)

// doCall carries out a call on the active clients in turn until one succeeds.
func (ec *ExecutionClient) doCall(ctx context.Context, call callFunc) (any, error) {
	if len(ec.clients) == 1 {
		return call(ctx, ec.clients[0])
	}

	var errs error

	for i := 0; i < len(ec.clients); i++ {
		ec.currentClientIndexMu.Lock()
		currentIndex := ec.currentClientIndex
		ec.currentClientIndexMu.Unlock()

		res, err := call(ctx, ec.clients[currentIndex])
		if err != nil {
			failedClientIndex := currentIndex

			ec.currentClientIndexMu.Lock()
			if ec.currentClientIndex == failedClientIndex { // it might have already been changed by a parallel request
				ec.currentClientIndex = (ec.currentClientIndex + 1) % len(ec.clients)
			}
			newClientIndex := ec.currentClientIndex
			ec.currentClientIndexMu.Unlock()

			ec.logger.Warn("Execution client returned an error. Trying to use the next available one",
				zap.Int("failed_client_index", failedClientIndex),
				zap.Int("new_client_index", newClientIndex),
				zap.Error(err))

			errs = errors.Join(errs, err)

			continue
		}

		return res, nil
	}

	return nil, fmt.Errorf("no clients available: %w", errs)
}
