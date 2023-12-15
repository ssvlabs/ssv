// Package executionclient implements functions for interacting with Ethereum execution clients.
package executionclient

import (
	"context"
	"errors"
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
	ErrClosed        = fmt.Errorf("closed")
	ErrNotConnected  = fmt.Errorf("not connected")
	ErrBadInput      = fmt.Errorf("bad input")
	ErrNothingToSync = errors.New("nothing to sync")
)

// ExecutionClient represents a client for interacting with Ethereum execution client.
type ExecutionClient struct {
	// mandatory
	nodeAddr        string
	finalizedBlocks chan uint64
	contractAddress ethcommon.Address

	// optional
	logger                      *zap.Logger
	metrics                     metrics
	connectionTimeout           time.Duration
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration
	logBatchSize                uint64

	// variables
	client *ethclient.Client
	closed chan struct{}
}

// New creates a new instance of ExecutionClient.
func New(
	ctx context.Context,
	nodeAddr string,
	contractAddr ethcommon.Address,
	opts ...Option,
) (*ExecutionClient, error) {
	client := &ExecutionClient{
		nodeAddr:                    nodeAddr,
		finalizedBlocks:             make(chan uint64),
		contractAddress:             contractAddr,
		logger:                      zap.NewNop(),
		metrics:                     nopMetrics{},
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
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan BlockLogs, errors <-chan error, err error) {
	// #TODO refactor this to fetch last finalized one
	currentBlock, err := ec.client.BlockNumber(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current block: %w", err)
	}
	toBlock := currentBlock
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

		ec.metrics.ExecutionClientLastFetchedBlock(endBlock)
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

// Healthy returns if execution client is currently healthy: responds to requests and not in the syncing state.
func (ec *ExecutionClient) Healthy(ctx context.Context) error {
	if ec.isClosed() {
		return ErrClosed
	}

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	sp, err := ec.client.SyncProgress(ctx)
	if err != nil {
		ec.metrics.ExecutionClientFailure()
		return err
	}

	if sp != nil {
		ec.metrics.ExecutionClientSyncing()
		return fmt.Errorf("syncing")
	}

	ec.metrics.ExecutionClientReady()

	return nil
}

func (ec *ExecutionClient) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
	return ec.client.BlockByNumber(ctx, blockNumber)
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
	for {
		select {
		case <-ctx.Done():
			return fromBlock, context.Canceled

		case <-ec.closed:
			return fromBlock, ErrClosed

		case toBlock := <-ec.finalizedBlocks:
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
