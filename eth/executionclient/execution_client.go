// Package executionclient implements functions for interacting with Ethereum execution clients.
package executionclient

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/metrics"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=executionclient -destination=./mocks.go -source=./execution_client.go

type Provider interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logsCh <-chan BlockLogs, errsCh <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) (logsCh chan BlockLogs)
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
	streamLogsToChan(ctx context.Context, logCh chan<- BlockLogs, fromBlock uint64) (lastBlock uint64, progressed bool, err error)
}

var _ Provider = &ExecutionClient{}

// ExecutionClient represents a client for interacting with Ethereum execution client.
type ExecutionClient struct {
	// mandatory
	nodeAddr        string
	contractAddress ethcommon.Address

	logger *zap.Logger

	reqTimeout    time.Duration
	reqRetryDelay time.Duration

	// followDistance defines an offset into the past from the head block such that the block
	// at this offset will be considered as very likely finalized.
	followDistance uint64 // TODO: consider reading the finalized checkpoint from consensus layer

	syncDistanceTolerance uint64
	// syncProgressFn is a struct-field so it can be overwritten for testing
	syncProgressFn func(context.Context) (*ethereum.SyncProgress, error)

	client *ethClient
	closed chan struct{}

	// healthInvalidationInterval ensures we don't spam EL with health-check type requests too much.
	healthInvalidationInterval time.Duration
	lastHealthyTime            atomic.Int64
}

// New creates a new instance of ExecutionClient.
func New(ctx context.Context, nodeAddr string, contractAddr ethcommon.Address, opts ...Option) (*ExecutionClient, error) {
	ec := &ExecutionClient{
		nodeAddr:                   nodeAddr,
		contractAddress:            contractAddr,
		logger:                     zap.NewNop(),
		reqTimeout:                 DefaultReqTimeout,
		reqRetryDelay:              DefaultReqRetryDelay,
		followDistance:             DefaultFollowDistance,
		healthInvalidationInterval: DefaultHealthInvalidationInterval,
		syncDistanceTolerance:      DefaultSyncDistanceTolerance,
		closed:                     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(ec)
	}

	ec.syncProgressFn = func(ctx context.Context) (*ethereum.SyncProgress, error) {
		sp, err := ec.client.SyncProgress(ctx)
		if err != nil {
			return nil, ec.errSingleClient(fmt.Errorf("fetch sync progress: %w", err), "eth_syncing")
		}
		return sp, nil
	}

	ec.logger = ec.logger.With(fields.Address(nodeAddr))

	ec.logger.Debug("connecting")

	err := ec.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect EL client: %w", err)
	}

	ec.logger.Debug("connected successfully")

	return ec, nil
}

func (ec *ExecutionClient) Address() string {
	return ec.nodeAddr
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
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (
	logsCh <-chan BlockLogs,
	errsCh <-chan error,
	err error,
) {
	start := time.Now()
	currentBlock, err := ec.client.BlockNumber(ctx)
	recordRequest(ctx, ec.logger, "BlockNumber", ec, time.Since(start), err)
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

	logsCh, errsCh = ec.fetchLogsInBatches(ctx, fromBlock, toBlock)
	return
}

// Calls FilterLogs multiple times (in batches) gradually sending the results on logCh to avoid fetching
// an enormous number of events. If an error is encountered, the fetching terminates and the error is sent
// on errCh. Both logCh and errCh are closed upon this function termination.
func (ec *ExecutionClient) fetchLogsInBatches(ctx context.Context, startBlock, endBlock uint64) (logCh chan BlockLogs, errCh chan error) {
	// All errors are buffered so we don't block the execution of this func (waiting on the caller to
	// handle the error before we can continue further) + it provides more flexibility for the caller
	// allowing him to process logCh before continuing to checking the errCh channel.
	errCh = make(chan error, 1) // we send 1 error at most on this channel

	if startBlock > endBlock {
		logCh = make(chan BlockLogs)
		errCh <- ErrBadInput
		close(logCh)
		close(errCh)
		return logCh, errCh // must return a non-nil closed logCh channel to prevent the caller misusing it
	}

	const maxBufferSize = 8 * 1024
	bufferSize := min(endBlock-startBlock+1, maxBufferSize)
	logCh = make(chan BlockLogs, bufferSize)

	go func() {
		defer close(logCh)
		defer close(errCh)

		const logBatchSize = 200 // TODO Make batch of logs adaptive depending on "websocket: read limit"

		for fromBlock := startBlock; fromBlock <= endBlock; fromBlock += logBatchSize {
			toBlock := min(fromBlock+logBatchSize-1, endBlock)

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

// StreamLogs subscribes to events emitted by the Ethereum SSV contract(s) starting at fromBlock.
// It spawns a go-routine that spins in a perpetual retry loop, terminating only on unrecoverable
// interruptions (such as context cancels, client closure, etc.) as defined by isSingleClientInterruptedError
// func. The logsCh is closed once the streaming go-routine terminates. Any errors encountered during
// streaming are logged.
func (ec *ExecutionClient) StreamLogs(ctx context.Context, fromBlock uint64) (logsCh chan BlockLogs) {
	logsCh = make(chan BlockLogs)

	go func() {
		defer close(logsCh)

		attemptDelay := time.Duration(0) // no delay on the 1st attempt

		for {
			select {
			case <-ctx.Done():
				return
			case <-ec.closed:
				return
			case <-time.After(attemptDelay):
				// waited out the delay (precautionary measure to avoid overloading EL with requests)
			}

			lastProcessedBlock, progressed, err := ec.streamLogsToChan(ctx, logsCh, fromBlock)
			if progressed {
				fromBlock = lastProcessedBlock + 1
			}
			if isSingleClientInterruptedError(err) {
				// This is a valid way to finish streamLogsToChan call. We are done with log streaming.
				return
			}
			if err == nil {
				// streamLogsToChan should never return without an error, so we treat a nil error as
				// an error by itself.
				err = fmt.Errorf("streamLogsToChan halted without an error")
			}

			ec.logger.Error("failed to stream registry events, gonna retry", zap.Error(err))
			attemptDelay = ec.reqRetryDelay // any retry attempt should be delayed
		}
	}()

	return logsCh
}

// Healthy returns if execution client is currently healthy: responds to requests and not in the syncing state.
func (ec *ExecutionClient) Healthy(ctx context.Context) error {
	if ec.isClosed() {
		return ErrClosed
	}

	lastHealthyTime := time.Unix(ec.lastHealthyTime.Load(), 0)
	if ec.healthInvalidationInterval != 0 && time.Since(lastHealthyTime) <= ec.healthInvalidationInterval {
		// Synced recently, reuse the result (only if ec.healthInvalidationInterval is set).
		return nil
	}

	start := time.Now()
	sp, err := ec.SyncProgress(ctx)
	recordRequest(ctx, ec.logger, "SyncProgress", ec, time.Since(start), err)
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

	ec.lastHealthyTime.Store(time.Now().Unix())

	return nil
}

func (ec *ExecutionClient) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
	start := time.Now()
	h, err := ec.client.HeaderByNumber(ctx, blockNumber)
	recordRequest(ctx, ec.logger, "HeaderByNumber", ec, time.Since(start), err)
	if err != nil {
		return nil, ec.errSingleClient(fmt.Errorf("get header by block number %s: %w", blockNumber, err), "eth_getBlockByNumber")
	}
	return h, nil
}

func (ec *ExecutionClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	start := time.Now()
	logs, err := ec.client.SubscribeFilterLogs(ctx, q, ch)
	recordRequest(ctx, ec.logger, "SubscribeFilterLogs", ec, time.Since(start), err)
	if err != nil {
		return nil, ec.errSingleClient(fmt.Errorf("subscribe to filtered logs (query=%s): %w", q, err), "logs")
	}
	return logs, nil
}

func (ec *ExecutionClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	start := time.Now()
	logs, err := ec.client.FilterLogs(ctx, q)
	recordRequest(ctx, ec.logger, "FilterLogs", ec, time.Since(start), err)
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

// streamLogsToChan streams ongoing logs from the given block to the given channel. This func blocks forever until
// it returns an error along with the progressed-flag indicating whether or not it was able to stream at least 1 block,
// and, when progressed=true, the last block number that streamed to logCh.
//
// TODO: consider handling "websocket: read limit exceeded" error and reducing batch size (syncSmartContractsEvents has code for this)
func (ec *ExecutionClient) streamLogsToChan(
	ctx context.Context,
	logCh chan<- BlockLogs,
	fromBlock uint64,
) (lastBlock uint64, progressed bool, err error) {
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
	recordRequest(ctx, ec.logger, "SubscribeNewHead", ec, time.Since(start), err)
	if err != nil {
		return 0, false, ec.errSingleClient(fmt.Errorf("subscribe new head: %w", err), "newHeads")
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return lastBlock, progressed, ctx.Err()

		case <-ec.closed:
			return lastBlock, progressed, ErrClosed

		case err := <-sub.Err():
			if err == nil {
				return lastBlock, progressed, fmt.Errorf("subscription error: nil error")
			}
			return lastBlock, progressed, fmt.Errorf("subscription error: %w", err)

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
				logCh <- block
				progressed = true
				lastBlock = block.BlockNumber
			}

			if err := <-fetchErrors; err != nil {
				// If we get an error while fetching, we return the last block we fetched.
				return lastBlock, progressed, fmt.Errorf("fetch logs: %w", err)
			}

			fromBlock = toBlock + 1

			metrics.RecordUint64Value(ctx, fromBlock, lastProcessedBlockGauge.Record, metric.WithAttributes(semconv.ServerAddress(ec.nodeAddr)))

		case <-time.After(2 * time.Minute):
			return lastBlock, progressed, fmt.Errorf("subscription timed out: no block header received in 2m")
		}
	}
}

// connect connects to Ethereum execution client.
func (ec *ExecutionClient) connect(ctx context.Context) error {
	reqCtx, cancel := context.WithTimeout(ctx, ec.reqTimeout)
	defer cancel()
	reqStart := time.Now()
	c, err := ethclient.DialContext(reqCtx, ec.nodeAddr)
	recordRequest(ctx, ec.logger, "DialContext", ec, time.Since(reqStart), err)
	if err != nil {
		return ec.errSingleClient(fmt.Errorf("ethclient dial: %w", err), "dial")
	}

	ec.client = newEthClient(c, ec.reqTimeout)

	return nil
}

func (ec *ExecutionClient) Filterer() (*contract.ContractFilterer, error) {
	return contract.NewContractFilterer(ec.contractAddress, ec.client)
}

func (ec *ExecutionClient) ChainID(ctx context.Context) (*big.Int, error) {
	start := time.Now()
	chainID, err := ec.client.ChainID(ctx)
	recordRequest(ctx, ec.logger, "ChainID", ec, time.Since(start), err)
	if chainID == nil {
		return nil, ec.errSingleClient(fmt.Errorf("chain id response is nil"), "eth_chainId")
	}
	if err != nil {
		return nil, ec.errSingleClient(fmt.Errorf("fetch chain ID: %w", err), "eth_chainId")
	}

	return chainID, nil
}
