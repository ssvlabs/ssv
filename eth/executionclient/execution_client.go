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

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
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
	IsFinalizedFork(ctx context.Context) bool
	Close() error
}

type SingleClientProvider interface {
	Provider
	SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	streamLogsToChan(ctx context.Context, logCh chan<- BlockLogs, fromBlock uint64) (nextBlockToProcess uint64, err error)
}

var _ Provider = &ExecutionClient{}

// ExecutionClient represents a client for interacting with Ethereum execution client.
type ExecutionClient struct {
	// mandatory
	networkConfig   networkconfig.NetworkConfig
	nodeAddr        string
	contractAddress ethcommon.Address

	// optional
	logger                     *zap.Logger
	connectionTimeout          time.Duration
	healthInvalidationInterval time.Duration
	logBatchSize               uint64
	followDistance             uint64

	syncDistanceTolerance uint64
	syncProgressFn        func(context.Context) (*ethereum.SyncProgress, error)

	// variables
	client         *ethclient.Client
	closed         chan struct{}
	lastSyncedTime atomic.Int64
}

// New creates a new instance of ExecutionClient.
func New(ctx context.Context,
	networkConfig networkconfig.NetworkConfig,
	nodeAddr string,
	contractAddr ethcommon.Address,
	opts ...Option,
) (*ExecutionClient, error) {
	client := &ExecutionClient{
		networkConfig:              networkConfig,
		nodeAddr:                   nodeAddr,
		contractAddress:            contractAddr,
		logger:                     zap.NewNop(),
		connectionTimeout:          DefaultConnectionTimeout,
		healthInvalidationInterval: DefaultHealthInvalidationInterval,
		logBatchSize:               DefaultHistoricalLogsBatchSize, // TODO Make batch of logs adaptive depending on "websocket: read limit"
		followDistance:             DefaultFollowDistance,
		closed:                     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(client)
	}

	client.logger.Info("execution client: connecting", fields.Address(nodeAddr))

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
	var toBlock uint64

	header, err := ec.client.HeaderByNumber(ctx, nil)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_getBlockByNumber"),
			zap.Error(err))
		return nil, nil, fmt.Errorf("failed to get block header: %w", err)
	}

	currentBlock := header.Number.Uint64()
	currentEpoch := ec.epochFromBlockHeader(header)

	if currentEpoch > ec.networkConfig.SSVConfig.Forks.GetFinalityConsensusEpoch() {
		// Post-fork: use finalized block
		toBlock, err = ec.getFinalizedBlock(ctx)
		if err != nil {
			ec.logger.Error(elResponseErrMsg,
				zap.String("method", "eth_getBlockByNumber"),
				zap.String("tag", "finalized"),
				zap.Error(err))
			return nil, nil, err
		}
	} else {
		// Pre-fork: use follow distance
		if currentBlock < ec.followDistance {
			return nil, nil, ErrNothingToSync
		}
		toBlock = currentBlock - ec.followDistance
	}

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
			toBlock := fromBlock + ec.logBatchSize - 1
			if toBlock > endBlock {
				toBlock = endBlock
			}

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

	logs, err := ec.client.FilterLogs(ctx, q)
	if err == nil {
		return logs, nil
	}

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

	ec.logger.Error(elResponseErrMsg,
		zap.String("method", "eth_getLogs"),
		zap.Error(err))

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
				return
			case <-ec.closed:
				return
			default:
				nextBlockToProcess, err := ec.streamLogsToChan(ctx, logs, fromBlock)
				if isInterruptedError(err) {
					// This is a valid way to terminate, no need to log this error.
					return
				}
				// streamLogsToChan should never return without an error, so we treat a nil error as
				// an error by itself.
				if err == nil {
					err = errors.New("streamLogsToChan halted without an error")
				}

				tries++
				if tries > 2 {
					ec.logger.Fatal("failed to stream registry events", zap.Error(err))
				}

				if nextBlockToProcess > fromBlock {
					// Successfully streamed some logs, reset tries.
					tries = 0
				}

				ec.logger.Error("failed to stream registry events, reconnecting", zap.Error(err))
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

// healthy checks if the execution client is currently in a healthy state.
// It performs different checks based on whether the finality fork is active:
//
// Pre-fork (follow distance approach):
// - Verifies the client responds to requests
// - Checks if the sync distance is within the acceptable tolerance
//
// Post-fork (finality approach):
// - Verifies the client responds to requests
// - Checks if the sync distance is within the acceptable tolerance
// - Checks if finalized blocks are available
//
// The method returns nil if the client is healthy, or an error explaining why it's not.
// Error types include:
// - errSyncing: when the client is still synchronizing blocks
// - network errors: when the client doesn't respond
// TODO: update for related stuff (names, etc)
func (ec *ExecutionClient) healthy(ctx context.Context) error {
	if ec.isClosed() {
		return ErrClosed
	}

	// Check if we recently validated health
	lastHealthyTime := time.Unix(ec.lastSyncedTime.Load(), 0)
	if ec.healthInvalidationInterval != 0 && time.Since(lastHealthyTime) <= ec.healthInvalidationInterval {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	// 1. Check if client is reachable
	start := time.Now()
	sp, err := ec.SyncProgress(ctx)
	if err != nil {
		recordExecutionClientStatus(ctx, statusFailure, ec.nodeAddr)
		return err
	}
	recordRequestDuration(ctx, ec.nodeAddr, time.Since(start))

	// 2. Check sync distance
	if sp != nil {
		syncDistance := max(sp.HighestBlock, sp.CurrentBlock) - sp.CurrentBlock
		observability.RecordUint64Value(ctx, syncDistance, syncDistanceGauge.Record,
			metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))

		if syncDistance > ec.syncDistanceTolerance {
			recordExecutionClientStatus(ctx, statusSyncing, ec.nodeAddr)
			return fmt.Errorf("sync distance exceeds tolerance (%d): %w", syncDistance, errSyncing)
		}
	} else {
		syncDistanceGauge.Record(ctx, 0, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))
	}

	// 3. Check finalized block availability (post-fork only)
	header, err := ec.client.HeaderByNumber(ctx, nil)
	if err != nil {
		recordExecutionClientStatus(ctx, statusFailure, ec.nodeAddr)
		return err
	}

	currentEpoch := ec.epochFromBlockHeader(header)

	if currentEpoch > ec.networkConfig.SSVConfig.Forks.GetFinalityConsensusEpoch() {
		_, err := ec.getFinalizedBlock(ctx)
		if err != nil {
			recordExecutionClientStatus(ctx, statusFailure, ec.nodeAddr)
			return err
		}
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
			zap.String("method", "eth_subscribe"),
			zap.String("tag", "logs"),
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

// streamLogsToChan streams ongoing logs from the given block to the given channel.
// streamLogsToChan *always* returns the next block to process.
// TODO: consider handling "websocket: read limit exceeded" error and reducing batch size (syncSmartContractsEvents has code for this)
func (ec *ExecutionClient) streamLogsToChan(ctx context.Context, logCh chan<- BlockLogs, fromBlock uint64) (uint64, error) {
	headersCh := make(chan *ethtypes.Header)

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
	sub, err := ec.client.SubscribeNewHead(ctx, headersCh)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_subscribe"),
			zap.String("tag", "newHeads"),
			zap.Error(err))
		return fromBlock, fmt.Errorf("subscribe heads: %w", err)
	}
	defer sub.Unsubscribe()

	var lastFinalized uint64

	for {
		select {
		case <-ctx.Done():
			return fromBlock, context.Canceled
		case <-ec.closed:
			return fromBlock, ErrClosed
		case subErr := <-sub.Err():
			if subErr == nil {
				return fromBlock, ErrClosed
			}
			return fromBlock, fmt.Errorf("subscription: %w", subErr)
		case header := <-headersCh:
			headerNum := header.Number.Uint64()
			ec.logger.Debug("new head received",
				fields.BlockNumber(headerNum),
				zap.String("head_hash", header.Hash().Hex()),
				zap.String("head_parent_hash", header.ParentHash.Hex()))

			var toBlock uint64

			// Determine target block based on fork state
			currentEpoch := ec.epochFromBlockHeader(header)

			if currentEpoch > ec.networkConfig.SSVConfig.Forks.GetFinalityConsensusEpoch() {
				// Post-fork: use finalized block
				finalizedBlock, err := ec.getFinalizedBlock(ctx)
				if err != nil {
					return fromBlock, err
				}
				toBlock = finalizedBlock

				if toBlock != lastFinalized {
					finalizedHeader, err := ec.client.HeaderByNumber(ctx, new(big.Int).SetUint64(toBlock))
					if err == nil {
						finalizedEpoch := ec.epochFromBlockHeader(finalizedHeader)
						ec.logger.Info("⏱ finalized block changed",
							zap.Uint64("new_finalized", toBlock),
							zap.Uint64("estimated_epoch", uint64(finalizedEpoch)),
							zap.Uint64("previous_finalized", lastFinalized))
					}
					lastFinalized = toBlock
				}
			} else {
				// Pre-fork: follow distance approach
				if headerNum < ec.followDistance {
					continue
				}
				toBlock = headerNum - ec.followDistance
			}

			// Skip if toBlock is less than fromBlock
			if toBlock < fromBlock {
				isUsingFinalized := currentEpoch > ec.networkConfig.SSVConfig.Forks.GetFinalityConsensusEpoch()
				ec.logger.Info("waiting for target block to reach fromBlock",
					fields.FromBlock(fromBlock),
					fields.ToBlock(toBlock),
					zap.Bool("finalized_fork", isUsingFinalized))
				continue
			}

			// Process logs for this block range
			logStream, fetchErrors := ec.fetchLogsInBatches(ctx, fromBlock, toBlock)
			for block := range logStream {
				logCh <- block
				fromBlock = block.BlockNumber + 1
			}

			if err := <-fetchErrors; err != nil {
				// If we get an error while fetching, we return the last block we fetched.
				return fromBlock, fmt.Errorf("fetch logs: %w", err)
			}

			fromBlock = toBlock + 1

			observability.RecordUint64Value(ctx, fromBlock, lastProcessedBlockGauge.Record,
				metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))
		}
	}
}

func (ec *ExecutionClient) Filterer() (*contract.ContractFilterer, error) {
	return contract.NewContractFilterer(ec.contractAddress, ec.client)
}

func (ec *ExecutionClient) ChainID(ctx context.Context) (*big.Int, error) {
	return ec.client.ChainID(ctx)
}

// IsFinalizedFork returns whether finalized blocks should be used instead of follow distance.
// Returns true if we've passed the finality fork epoch threshold.
func (ec *ExecutionClient) IsFinalizedFork(ctx context.Context) bool {
	header, err := ec.client.HeaderByNumber(ctx, nil)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_getBlockByNumber"),
			zap.Error(err))
		return false
	}

	currentEpoch := ec.epochFromBlockHeader(header)

	// Check if we've passed the fork point
	if currentEpoch > ec.networkConfig.SSVConfig.Forks.GetFinalityConsensusEpoch() {
		return true
	}

	return false
}

func (ec *ExecutionClient) getFinalizedBlock(ctx context.Context) (uint64, error) {
	finalizedBlock, err := ec.client.HeaderByNumber(ctx, big.NewInt(rpc.FinalizedBlockNumber.Int64()))
	if err != nil {
		return 0, fmt.Errorf("get finalized block: %w", err)
	}
	return finalizedBlock.Number.Uint64(), nil
}

// epochFromBlockHeader calculates the epoch from a block header
func (ec *ExecutionClient) epochFromBlockHeader(header *ethtypes.Header) phase0.Epoch {
	blockTime := time.Unix(int64(header.Time), 0) // #nosec G115

	slot := ec.networkConfig.EstimatedSlotAtTime(blockTime)
	return ec.networkConfig.EstimatedEpochAtSlot(slot)
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

	logger.Info("connected to execution client", fields.Took(time.Since(start)))
	return nil
}

func (ec *ExecutionClient) isClosed() bool {
	select {
	case <-ec.closed:
		return true
	default:
		return false
	}
}
