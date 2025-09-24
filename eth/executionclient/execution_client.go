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
	"github.com/ethereum/go-ethereum/rpc"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/metrics"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=executionclient -destination=./mocks.go -source=./execution_client.go

type Provider interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan BlockLogs, errors <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs
	StreamFilterLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log
	StreamFinalizedLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs
	Filterer() (*contract.ContractFilterer, error)
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
	StreamFilterLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log
	StreamFinalizedLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs
	streamLogsToChan(ctx context.Context, logCh chan<- BlockLogs, fromBlock uint64) (nextBlockToProcess uint64, err error)
}

var _ Provider = &ExecutionClient{}

// ExecutionClient represents a client for interacting with Ethereum execution client.
type ExecutionClient struct {
	// mandatory
	nodeAddr        string
	contractAddress ethcommon.Address

	// optional
	logger *zap.Logger
	// followDistance defines an offset into the past from the head block such that the block
	// at this offset will be considered as very likely finalized.
	followDistance             uint64 // TODO: consider reading the finalized checkpoint from consensus layer
	reqTimeout                 time.Duration
	healthInvalidationInterval time.Duration
	logBatchSize               uint64

	syncDistanceTolerance uint64
	// syncProgressFn is a struct-field so it can be overwritten for testing
	syncProgressFn func(context.Context) (*ethereum.SyncProgress, error)

	// variables
	client         *ethClient
	closed         chan struct{}
	lastSyncedTime atomic.Int64
}

// New creates a new instance of ExecutionClient.
func New(ctx context.Context, nodeAddr string, contractAddr ethcommon.Address, opts ...Option) (*ExecutionClient, error) {
	ec := &ExecutionClient{
		nodeAddr:                   nodeAddr,
		contractAddress:            contractAddr,
		logger:                     zap.NewNop(),
		followDistance:             DefaultFollowDistance,
		reqTimeout:                 DefaultReqTimeout,
		healthInvalidationInterval: DefaultHealthInvalidationInterval,
		logBatchSize:               DefaultHistoricalLogsBatchSize, // TODO Make batch of logs adaptive depending on "websocket: read limit"
		closed:                     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(ec)
	}

	ec.logger = ec.logger.With(fields.Address(nodeAddr))

	err := ec.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to execution client: %w", err)
	}

	ec.syncProgressFn = func(ctx context.Context) (*ethereum.SyncProgress, error) {
		sp, err := ec.client.SyncProgress(ctx)
		if err != nil {
			return nil, ec.errSingleClient(fmt.Errorf("fetch sync progress: %w", err), "eth_syncing")
		}
		return sp, nil
	}

	return ec, nil
}

func (ec *ExecutionClient) SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return ec.syncProgressFn(ctx)
}

// Close shuts down ExecutionClient.
func (ec *ExecutionClient) Close() error {
	close(ec.closed)
	ec.client.Close()
	return nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan BlockLogs, errors <-chan error, err error) {
	start := time.Now()
	currentBlock, err := ec.client.BlockNumber(ctx)
	recordRequestDuration(ctx, "BlockNumber", ec.nodeAddr, time.Since(start), err)
	if err != nil {
		return nil, nil, ec.errSingleClient(fmt.Errorf("get current block: %w", err), "eth_blockNumber")
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

// Calls FilterLogs multiple times and batches results to avoid fetching an enormous number of events.
func (ec *ExecutionClient) fetchLogsInBatches(ctx context.Context, startBlock, endBlock uint64) (<-chan BlockLogs, <-chan error) {
	if startBlock > endBlock {
		errCh := make(chan error, 1)
		errCh <- ErrBadInput
		close(errCh)

		return nil, errCh
	}

	logCh := make(chan BlockLogs, defaultLogBuf)
	errCh := make(chan error, 1)

	go func() {
		defer close(logCh)
		defer close(errCh)

		for fromBlock := startBlock; fromBlock <= endBlock; fromBlock += ec.logBatchSize {
			toBlock := min(fromBlock+ec.logBatchSize-1, endBlock)

			start := time.Now()
			query := ethereum.FilterQuery{
				Addresses: []ethcommon.Address{ec.contractAddress},
				FromBlock: new(big.Int).SetUint64(fromBlock),
				ToBlock:   new(big.Int).SetUint64(toBlock),
			}
			results, err := ec.subdivideLogFetch(ctx, query)
			if err != nil {
				errCh <- err
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
				errCh <- ctx.Err()
				return

			case <-ec.closed:
				errCh <- ErrClosed
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
					logCh <- blockLogs
					if blockLogs.BlockNumber > highestBlock {
						highestBlock = blockLogs.BlockNumber
					}
				}
				// Emit an empty BlockLogs to indicate progression to the next block.
				if highestBlock < toBlock {
					logCh <- BlockLogs{BlockNumber: toBlock}
				}
			}
		}
	}()

	return logCh, errCh
}

// subdivideLogFetch handles log fetching with automatic subdivision on query limit errors.
// It first attempts a direct fetch, and if that fails with a query limit error, it subdivides
// the block range and tries again with smaller chunks.
func (ec *ExecutionClient) subdivideLogFetch(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ec.closed:
		return nil, ErrClosed
	default:
	}

	logs, err := ec.FilterLogs(ctx, q)
	if err == nil {
		return logs, nil
	}
	err = ec.errSingleClient(fmt.Errorf("get filtered logs: %w", err), "eth_getLogs")

	if isRPCQueryLimitError(err) {
		if q.FromBlock == nil || q.ToBlock == nil {
			return nil, err
		}

		fromBlock := q.FromBlock.Uint64()
		toBlock := q.ToBlock.Uint64()

		// require at least 2 blocks to subdivide (fromBlock must be less than toBlock)
		if fromBlock >= toBlock {
			return nil, fmt.Errorf("insufficient blocks to subdivide (fromBlock: %d, toBlock: %d): %w", fromBlock, toBlock, err)
		}

		ec.logger.Warn("execution client query limit exceeded, subdividing query",
			zap.String("method", "eth_getLogs"),
			fields.FromBlock(fromBlock),
			fields.ToBlock(toBlock),
			zap.Error(err))

		midBlock := fromBlock + (toBlock-fromBlock)/2

		leftQuery := q
		leftQuery.FromBlock = new(big.Int).SetUint64(fromBlock)
		leftQuery.ToBlock = new(big.Int).SetUint64(midBlock)

		rightQuery := q
		rightQuery.FromBlock = new(big.Int).SetUint64(midBlock + 1)
		rightQuery.ToBlock = new(big.Int).SetUint64(toBlock)

		leftLogs, leftErr := ec.subdivideLogFetch(ctx, leftQuery)
		if leftErr != nil {
			return nil, leftErr
		}

		rightLogs, rightErr := ec.subdivideLogFetch(ctx, rightQuery)
		if rightErr != nil {
			return nil, rightErr
		}

		totalLogs := len(leftLogs) + len(rightLogs)
		combinedLogs := make([]ethtypes.Log, 0, totalLogs)
		combinedLogs = append(combinedLogs, leftLogs...)
		combinedLogs = append(combinedLogs, rightLogs...)

		ec.logger.Info("successfully fetched logs after subdivision",
			fields.FromBlock(fromBlock),
			fields.ToBlock(toBlock),
			zap.Int("total_logs", totalLogs))

		return combinedLogs, nil
	}

	return nil, err
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
				ec.logger.Debug("ExecutionClient StreamLogs context canceled")
				return
			case <-ec.closed:
				ec.logger.Debug("ExecutionClient StreamLogs client closed")
				return
			default:
				ec.logger.Debug("ExecutionClient calling streamLogsToChan", zap.Uint64("from_block", fromBlock))
				nextBlockToProcess, err := ec.streamLogsToChan(ctx, logs, fromBlock)
				ec.logger.Debug("ExecutionClient streamLogsToChan returned",
					zap.Uint64("next_block", nextBlockToProcess),
					zap.Error(err))

				if isInterruptedError(err) {
					// This is a valid way to terminate, no need to log this error.
					ec.logger.Debug("ExecutionClient got interrupted error, exiting normally")
					return
				}
				// streamLogsToChan should never return without an error, so we treat a nil error as
				// an error by itself.
				if err == nil {
					err = errors.New("streamLogsToChan halted without an error")
					ec.logger.Warn("ExecutionClient detected nil error from streamLogsToChan - converting to error", zap.Error(err))
				}

				tries++
				if tries > 2 {
					ec.logger.Fatal("failed to stream registry events", zap.Error(err))
				}
				ec.logger.Error("failed to stream registry events, gonna retry", zap.Error(err))

				if nextBlockToProcess > fromBlock {
					// Successfully streamed some logs, reset tries.
					tries = 0
				}
				fromBlock = nextBlockToProcess
			}
		}
	}()

	return logs
}

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
	start := time.Now()
	sp, err := ec.SyncProgress(ctx)
	recordRequestDuration(ctx, "SyncProgress", ec.nodeAddr, time.Since(start), err)
	if err != nil {
		recordExecutionClientStatus(ctx, statusFailure, ec.nodeAddr)
		return ec.errSingleClient(fmt.Errorf("get sync progress: %w", err), "eth_syncing")
	}

	if sp != nil {
		syncDistance := max(sp.HighestBlock, sp.CurrentBlock) - sp.CurrentBlock
		metrics.RecordUint64Value(ctx, syncDistance, syncDistanceGauge.Record, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))

		// block out of sync distance tolerance
		if syncDistance > ec.syncDistanceTolerance {
			recordExecutionClientStatus(ctx, statusSyncing, ec.nodeAddr)
			return fmt.Errorf("sync distance exceeds tolerance (%d): %w", syncDistance, ErrSyncing)
		}
	} else {
		syncDistanceGauge.Record(ctx, 0, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))
	}

	recordExecutionClientStatus(ctx, statusReady, ec.nodeAddr)
	ec.lastSyncedTime.Store(time.Now().Unix())

	return nil
}

func (ec *ExecutionClient) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
	start := time.Now()
	h, err := ec.client.HeaderByNumber(ctx, blockNumber)
	recordRequestDuration(ctx, "HeaderByNumber", ec.nodeAddr, time.Since(start), err)
	if err != nil {
		return nil, ec.errSingleClient(fmt.Errorf("get header by block number %s: %w", blockNumber, err), "eth_getBlockByNumber")
	}
	return h, nil
}

func (ec *ExecutionClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	start := time.Now()
	logs, err := ec.client.SubscribeFilterLogs(ctx, q, ch)
	recordRequestDuration(ctx, "SubscribeFilterLogs", ec.nodeAddr, time.Since(start), err)
	if err != nil {
		return nil, ec.errSingleClient(fmt.Errorf("subscribe to filtered logs (query=%s): %w", q, err), "logs")
	}
	return logs, nil
}

func (ec *ExecutionClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	start := time.Now()
	logs, err := ec.client.FilterLogs(ctx, q)
	recordRequestDuration(ctx, "FilterLogs", ec.nodeAddr, time.Since(start), err)
	if err != nil {
		return nil, ec.errSingleClient(fmt.Errorf("get filtered logs (query=%s): %w", q, err), "eth_getLogs")
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
// streamLogsToChan *always* returns the next block to process.
// TODO: consider handling "websocket: read limit exceeded" error and reducing batch size (syncSmartContractsEvents has code for this)
func (ec *ExecutionClient) streamLogsToChan(ctx context.Context, logCh chan<- BlockLogs, fromBlock uint64) (uint64, error) {
	ec.logger.Debug("streamLogsToChan started", zap.Uint64("from_block", fromBlock))
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
	// It also allowed us to implement more 'atomic' behavior easier:
	// We can revert the tx if there was an error in processing all the events of a block.
	// So we can restart from this block once everything is good.
	start := time.Now()
	sub, err := ec.client.SubscribeNewHead(ctx, heads)
	recordRequestDuration(ctx, "SubscribeNewHead", ec.nodeAddr, time.Since(start), err)
	if err != nil {
		return fromBlock, ec.errSingleClient(fmt.Errorf("subscribe new head: %w", err), "newHeads")
	}
	defer sub.Unsubscribe()

	ec.logger.Debug("streamLogsToChan entering main loop")
	for {
		select {
		case <-ctx.Done():
			ec.logger.Debug("streamLogsToChan context canceled")
			return fromBlock, context.Canceled

		case <-ec.closed:
			ec.logger.Debug("streamLogsToChan client closed")
			return fromBlock, ErrClosed

		case err := <-sub.Err():
			ec.logger.Debug("streamLogsToChan subscription error", zap.Error(err))
			if err == nil {
				ec.logger.Debug("streamLogsToChan returning ErrClosed due to nil subscription error")
				return fromBlock, ErrClosed
			}
			return fromBlock, fmt.Errorf("subscription: %w", err)

		case header := <-heads:
			ec.logger.Debug("received new head",
				zap.Uint64("header_number", header.Number.Uint64()),
				zap.Uint64("follow_distance", ec.followDistance),
				zap.Uint64("from_block", fromBlock))

			if header.Number.Uint64() < ec.followDistance {
				ec.logger.Debug("skipping head: header number less than follow distance",
					zap.Uint64("header_number", header.Number.Uint64()),
					zap.Uint64("follow_distance", ec.followDistance))
				continue
			}
			toBlock := header.Number.Uint64() - ec.followDistance
			if toBlock < fromBlock {
				ec.logger.Debug("skipping head: toBlock less than fromBlock",
					zap.Uint64("to_block", toBlock),
					zap.Uint64("from_block", fromBlock))
				continue
			}

			ec.logger.Debug("processing blocks range",
				zap.Uint64("from_block", fromBlock),
				zap.Uint64("to_block", toBlock))

			logStream, fetchErrors := ec.fetchLogsInBatches(ctx, fromBlock, toBlock)
			blocksProcessed := 0
			for block := range logStream {
				ec.logger.Debug("streaming block logs",
					zap.Uint64("block", block.BlockNumber),
					zap.Int("log_count", len(block.Logs)))

				select {
				case logCh <- block:
					ec.logger.Debug("streamLogsToChan successfully sent block to channel", zap.Uint64("block", block.BlockNumber))
				case <-ctx.Done():
					ec.logger.Debug("streamLogsToChan context canceled while sending block")
					return fromBlock, context.Canceled
				}
				fromBlock = block.BlockNumber + 1
				blocksProcessed++
			}
			ec.logger.Debug("streamLogsToChan finished processing logStream", zap.Int("blocks_processed", blocksProcessed))

			if err := <-fetchErrors; err != nil {
				// If we get an error while fetching, we return the last block we fetched.
				ec.logger.Debug("streamLogsToChan returning due to fetch error", zap.Error(err))
				return fromBlock, fmt.Errorf("fetch logs: %w", err)
			}

			ec.logger.Debug("completed blocks range processing",
				zap.Int("blocks_processed", blocksProcessed),
				zap.Uint64("next_from_block", toBlock+1))

			fromBlock = toBlock + 1
			metrics.RecordUint64Value(ctx, fromBlock, lastProcessedBlockGauge.Record, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))
		}
	}
}

// connect connects to Ethereum execution client.
func (ec *ExecutionClient) connect(ctx context.Context) error {
	ec.logger.Info("execution client: connecting")

	reqCtx, cancel := context.WithTimeout(ctx, ec.reqTimeout)
	defer cancel()

	reqStart := time.Now()
	c, err := ethclient.DialContext(reqCtx, ec.nodeAddr)
	recordRequestDuration(ctx, "DialContext", ec.nodeAddr, time.Since(reqStart), err)
	if err != nil {
		return ec.errSingleClient(fmt.Errorf("ethclient dial: %w", err), "dial")
	}

	ec.client = newEthClient(c, ec.reqTimeout)

	ec.logger.Info("connected to execution client")

	return nil
}

func (ec *ExecutionClient) Filterer() (*contract.ContractFilterer, error) {
	return contract.NewContractFilterer(ec.contractAddress, ec.client)
}

func (ec *ExecutionClient) ChainID(ctx context.Context) (*big.Int, error) {
	start := time.Now()
	chainID, err := ec.client.ChainID(ctx)
	recordRequestDuration(ctx, "ChainID", ec.nodeAddr, time.Since(start), err)
	if err != nil {
		return nil, ec.errSingleClient(fmt.Errorf("fetch chain ID: %w", err), "eth_chainId")
	}
	return chainID, nil
}

// StreamFilterLogs streams ongoing logs from the given block to the given channel.
// Unlike StreamLogs, this method uses a direct stream subscription from the node.
func (ec *ExecutionClient) StreamFilterLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log {
	// Small buffer to reduce blocking to avoid connection timeouts to the node.
	bufferSize := 100
	logCh := make(chan ethtypes.Log, bufferSize)

	go func() {
		defer close(logCh)

		ec.logger.Debug("starting filter logs subscription",
			zap.Uint64("from_block", fromBlock),
			zap.String("contract_address", ec.contractAddress.Hex()))

		query := ethereum.FilterQuery{
			Addresses: []ethcommon.Address{ec.contractAddress},
			FromBlock: new(big.Int).SetUint64(fromBlock),
		}

		// Create a buffered channel to filter out removed logs
		filterCh := make(chan ethtypes.Log, cap(logCh))
		sub, err := ec.client.SubscribeFilterLogs(ctx, query, filterCh)
		if err != nil {
			ec.logger.Error("failed to subscribe to filter logs", zap.Error(err))
			return
		}
		defer sub.Unsubscribe()

		ec.logger.Debug("filter logs subscription established, waiting for logs")

		for {
			select {
			case <-ctx.Done():
				ec.logger.Debug("filter logs subscription canceled due to context")
				return
			case <-ec.closed:
				ec.logger.Debug("filter logs subscription closed due to client shutdown")
				return
			case log := <-filterCh:
				// Filter out removed logs (indicates blockchain reorganization)
				if log.Removed {
					ec.logger.Warn("filter log is removed (reorg detected)",
						zap.String("block_hash", log.BlockHash.Hex()),
						fields.TxHash(log.TxHash),
						zap.Uint("log_index", log.Index))
					continue
				}
				logCh <- log
			case err := <-sub.Err():
				if err != nil {
					ec.logger.Error("filter logs subscription error", zap.Error(err))
				} else {
					ec.logger.Debug("filter logs subscription closed normally")
				}
				return
			}
		}
	}()

	return logCh
}

// StreamFinalizedLogs streams logs from finalized blocks to detect finalized events.
// This method subscribes to new heads and fetches logs from finalized blocks when they advance.
func (ec *ExecutionClient) StreamFinalizedLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs {
	logCh := make(chan BlockLogs, 10) // Smaller buffer since finalized blocks come slower

	go func() {
		defer close(logCh)

		heads := make(chan *ethtypes.Header)
		sub, err := ec.client.SubscribeNewHead(ctx, heads)
		if err != nil {
			ec.logger.Error("failed to subscribe to new heads for finalized logs", zap.Error(err))
			return
		}
		defer sub.Unsubscribe()

		var lastFinalizedBlock uint64

		for {
			select {
			case <-ctx.Done():
				return
			case <-ec.closed:
				return
			case err := <-sub.Err():
				if err != nil {
					ec.logger.Error("finalized logs subscription error", zap.Error(err))
				}
				return
			case <-heads:
				// Fetch the current finalized block
				finalizedHeader, err := ec.client.HeaderByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
				if err != nil {
					ec.logger.Debug("failed to fetch finalized header", zap.Error(err))
					continue
				}

				finalizedBlockNum := finalizedHeader.Number.Uint64()

				// Skip if finalized block hasn't advanced
				if finalizedBlockNum <= lastFinalizedBlock {
					continue
				}

				// Fetch logs for the newly finalized block
				ec.logger.Debug("fetching logs for finalized block", zap.Uint64("block", finalizedBlockNum))

				// Compute start of range to fetch logs from
				queryFrom := fromBlock + 1
				if lastFinalizedBlock > 0 {
					queryFrom = lastFinalizedBlock + 1
				}
				if queryFrom > finalizedBlockNum {
					queryFrom = finalizedBlockNum
				}

				logStream, errCh := ec.fetchLogsInBatches(ctx, queryFrom, finalizedBlockNum)
				streaming := true
				for streaming {
					select {
					case blockLogs, ok := <-logStream:
						if !ok {
							streaming = false
							continue
						}
						// Only emit logs for the finalized block (or range)
						if blockLogs.BlockNumber > 0 {
							logCh <- blockLogs
						}
					case err, ok := <-errCh:
						if ok && err != nil {
							ec.logger.Error("failed to fetch finalized logs",
								zap.Uint64("block", finalizedBlockNum),
								zap.Error(err))
						}
						streaming = false
					}
				}
				lastFinalizedBlock = finalizedBlockNum
				ec.logger.Debug("emitted finalized logs",
					zap.Uint64("block", finalizedBlockNum))
			}
		}
	}()

	return logCh
}
