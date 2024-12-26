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
)

var (
	ErrClosed        = fmt.Errorf("closed")
	ErrNotConnected  = fmt.Errorf("not connected")
	ErrBadInput      = fmt.Errorf("bad input")
	ErrNothingToSync = errors.New("nothing to sync")
)

const elResponseErrMsg = "Execution client returned an error"

// ExecutionClient represents a client for interacting with Ethereum execution clients.
type ExecutionClient struct {
	// mandatory
	nodeAddrs       []string
	contractAddress ethcommon.Address

	// optional
	logger *zap.Logger
	// followDistance defines an offset into the past from the head block such that the block
	// at this offset will be considered as very likely finalized.
	followDistance              uint64
	connectionTimeout           time.Duration
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration
	logBatchSize                uint64

	syncDistanceTolerance uint64
	syncProgressFn        func(context.Context, *ManagedClient) (*ethereum.SyncProgress, error)

	// variables
	clientsMu   sync.RWMutex
	clients     []*ManagedClient
	closed      chan struct{}
	closingOnce sync.Once
}

// New creates a new instance of ExecutionClient.
func New(ctx context.Context, nodeAddr string, contractAddr ethcommon.Address, opts ...Option) (*ExecutionClient, error) {
	addrList := strings.Split(nodeAddr, ";")
	if len(addrList) == 0 {
		return nil, fmt.Errorf("no node address provided")
	}

	client := &ExecutionClient{
		nodeAddrs:                   addrList,
		contractAddress:             contractAddr,
		logger:                      zap.NewNop(),
		followDistance:              DefaultFollowDistance,
		connectionTimeout:           DefaultConnectionTimeout,
		reconnectionInitialInterval: DefaultReconnectionInitialInterval,
		reconnectionMaxInterval:     DefaultReconnectionMaxInterval,
		logBatchSize:                DefaultHistoricalLogsBatchSize,
		closed:                      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(client)
	}

	// Initialize ManagedClients
	for _, addr := range client.nodeAddrs {
		mc, err := NewManagedClient(ctx, addr, client.logger, client.reconnectionInitialInterval, client.reconnectionMaxInterval)
		if err != nil {
			client.logger.Error("Failed to initialize ManagedClient", zap.String("address", addr), zap.Error(err))
			// Continue initializing other clients
		}
		client.clients = append(client.clients, mc)
	}

	if len(client.clients) == 0 {
		return nil, fmt.Errorf("no execution clients could be initialized")
	}

	same, err := client.assertSameChainIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("assert same chain IDs: %w", err)
	}
	if !same {
		return nil, fmt.Errorf("execution clients' chain IDs are not same")
	}

	client.syncProgressFn = client.syncProgress

	return client, nil
}

// assertSameChainIDs checks if all healthy clients have the same chain ID.
// It sets firstChainID to the chain ID of the first healthy client encountered.
func (ec *ExecutionClient) assertSameChainIDs(ctx context.Context) (bool, error) {
	ec.clientsMu.RLock()
	defer ec.clientsMu.RUnlock()

	var firstChainID *big.Int
	for _, client := range ec.clients {
		c := client.getClient()
		if c == nil {
			ec.logger.Warn("Skipping unhealthy client", zap.String("address", client.addr))
			continue // Skip unhealthy clients
		}
		chainID, err := c.ChainID(ctx)
		if err != nil {
			ec.logger.Error("Failed to get chain ID", zap.String("address", client.addr), zap.Error(err))
			return false, err
		}
		if firstChainID == nil {
			firstChainID = chainID
			continue
		}
		if firstChainID.Cmp(chainID) != 0 {
			ec.logger.Warn("Chain ID mismatch",
				zap.String("first_chain_id", firstChainID.String()),
				zap.String("current_chain_id", chainID.String()),
				zap.String("address", client.addr))
			return false, nil
		}
	}

	if firstChainID == nil {
		return false, fmt.Errorf("no healthy clients to determine chain ID")
	}

	return true, nil
}

// syncProgress retrieves the sync progress using a healthy client.
func (ec *ExecutionClient) syncProgress(ctx context.Context, mc *ManagedClient) (*ethereum.SyncProgress, error) {
	client := mc.getClient()
	if client == nil {
		return nil, ErrNotConnected
	}
	sp, err := client.SyncProgress(ctx)
	if err != nil {
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_syncing"),
			zap.Error(err))
		return nil, err
	}
	return sp, nil
}

// Close shuts down ExecutionClient.
func (ec *ExecutionClient) Close() error {
	ec.closingOnce.Do(func() {
		close(ec.closed)
		ec.clientsMu.RLock()
		defer ec.clientsMu.RUnlock()
		for _, client := range ec.clients {
			client.Close()
		}
	})
	return nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
func (ec *ExecutionClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan BlockLogs, errors <-chan error, err error) {
	res, err := ec.doCall(ctx, func(ctx context.Context, mc *ManagedClient) (any, error) {
		return mc.getClient().BlockNumber(ctx)
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

// fetchLogsInBatches calls FilterLogs multiple times and batches results to avoid fetching enormous amount of events.
func (ec *ExecutionClient) fetchLogsInBatches(ctx context.Context, startBlock, endBlock uint64) (<-chan BlockLogs, <-chan error) {
	logsCh := make(chan BlockLogs, defaultLogBuf)
	errorsCh := make(chan error, 1)

	go func() {
		defer close(logsCh)
		defer close(errorsCh)

		if startBlock > endBlock {
			errorsCh <- ErrBadInput
			return
		}

		for fromBlock := startBlock; fromBlock <= endBlock; fromBlock += ec.logBatchSize {
			toBlock := fromBlock + ec.logBatchSize - 1
			if toBlock > endBlock {
				toBlock = endBlock
			}

			start := time.Now()
			res, err := ec.doCall(ctx, func(ctx context.Context, mc *ManagedClient) (any, error) {
				return mc.getClient().FilterLogs(ctx, ethereum.FilterQuery{
					Addresses: []ethcommon.Address{ec.contractAddress},
					FromBlock: new(big.Int).SetUint64(fromBlock),
					ToBlock:   new(big.Int).SetUint64(toBlock),
				})
			})
			if err != nil {
				ec.logger.Error(elResponseErrMsg,
					zap.String("method", "eth_getLogs"),
					zap.Error(err))
				errorsCh <- err
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
				errorsCh <- ctx.Err()
				return

			case <-ec.closed:
				errorsCh <- ErrClosed
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
					logsCh <- BlockLogs{BlockNumber: toBlock}
				} else {
					for _, blockLogs := range PackLogs(validLogs) {
						logsCh <- blockLogs
					}
				}
			}
		}
	}()

	return logsCh, errorsCh
}

// StreamLogs subscribes to events emitted by the contract.
func (ec *ExecutionClient) StreamLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs {
	logs := make(chan BlockLogs)
	go func() {
		defer close(logs)
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

				ec.logger.Error("failed to stream registry events, reconnecting", zap.Error(err))
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

	mc, _, err := ec.getHealthyClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	start := time.Now()
	sp, err := ec.syncProgressFn(ctx, mc)
	if err != nil {
		recordExecutionClientStatus(ctx, statusFailure, mc.addr)
		ec.logger.Error(elResponseErrMsg,
			zap.String("method", "eth_syncing"),
			zap.Error(err))
		return err
	}
	recordRequestDuration(ctx, mc.addr, time.Since(start))

	if sp != nil {
		recordExecutionClientStatus(ctx, statusSyncing, mc.addr)

		syncDistance := max(sp.HighestBlock, sp.CurrentBlock) - sp.CurrentBlock

		observability.RecordUint64Value(ctx, syncDistance, syncDistanceGauge.Record, metric.WithAttributes(semconv.ServerAddress(mc.addr)))

		// block out of sync distance tolerance
		if syncDistance > ec.syncDistanceTolerance {
			return fmt.Errorf("sync distance exceeds tolerance (%d): %w", syncDistance, errSyncing)
		}
	}

	recordExecutionClientStatus(ctx, statusReady, mc.addr)

	syncDistanceGauge.Record(ctx, 0, metric.WithAttributes(semconv.ServerAddress(mc.addr)))

	return nil
}

// BlockByNumber retrieves a block by its number.
func (ec *ExecutionClient) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
	res, err := ec.doCall(ctx, func(ctx context.Context, mc *ManagedClient) (any, error) {
		return mc.getClient().BlockByNumber(ctx, blockNumber)
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

// isClosed checks if the ExecutionClient is closed.
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
func (ec *ExecutionClient) streamLogsToChan(ctx context.Context, logs chan<- BlockLogs, fromBlock uint64) (lastBlock uint64, err error) {
	mc, client, err := ec.getHealthyClient()
	if err != nil {
		return fromBlock, err
	}

	heads := make(chan *ethtypes.Header)

	sub, err := client.SubscribeNewHead(ctx, heads)
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
			observability.RecordUint64Value(ctx, fromBlock, lastProcessedBlockGauge.Record, metric.WithAttributes(semconv.ServerAddress(mc.addr)))
		}
	}
}

// getHealthyClient selects the first healthy ManagedClient and returns it along with its ethclient.Client.
func (ec *ExecutionClient) getHealthyClient() (*ManagedClient, *ethclient.Client, error) {
	ec.clientsMu.RLock()
	defer ec.clientsMu.RUnlock()

	for _, mc := range ec.clients {
		if mc.isHealthy() {
			client := mc.getClient()
			if client != nil {
				return mc, client, nil
			}
		}
	}
	return nil, nil, ErrNotConnected
}

// Filterer returns a contract filterer using the first healthy client.
func (ec *ExecutionClient) Filterer() (*contract.ContractFilterer, error) {
	_, client, err := ec.getHealthyClient()
	if err != nil {
		return nil, err
	}
	return contract.NewContractFilterer(ec.contractAddress, client)
}

type callFunc func(ctx context.Context, mc *ManagedClient) (any, error)

// doCall carries out a call on the healthy clients until one succeeds.
func (ec *ExecutionClient) doCall(ctx context.Context, call callFunc) (any, error) {
	ec.clientsMu.RLock()
	clients := make([]*ManagedClient, len(ec.clients))
	copy(clients, ec.clients)
	ec.clientsMu.RUnlock()

	var errs error

	for _, mc := range clients {
		if !mc.isHealthy() {
			continue
		}
		client := mc.getClient()
		if client == nil {
			continue
		}
		res, err := call(ctx, mc)
		if err != nil {
			ec.logger.Warn("Execution client returned an error. Marking as unhealthy.",
				zap.String("address", mc.addr),
				zap.Error(err))
			mc.disconnect()
			errs = errors.Join(errs, err)
			continue
		}
		return res, nil
	}

	if errs != nil {
		return nil, fmt.Errorf("no clients available: %w", errs)
	}
	return nil, ErrNotConnected
}
