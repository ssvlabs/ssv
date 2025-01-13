package executionclient

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/contract"
)

var _ Provider = &MultiClient{}

type SingleClientProvider interface {
	Provider
	SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	connect(ctx context.Context) error
	reconnect(ctx context.Context)
	streamLogsToChan(ctx context.Context, logs chan<- BlockLogs, fromBlock uint64) (lastBlock uint64, err error)
}

type MultiClient struct {
	// optional
	logger *zap.Logger
	// followDistance defines an offset into the past from the head block such that the block
	// at this offset will be considered as very likely finalized.
	followDistance              uint64 // TODO: consider reading the finalized checkpoint from consensus layer
	connectionTimeout           time.Duration
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration
	logBatchSize                uint64
	syncDistanceTolerance       uint64

	contractAddress ethcommon.Address
	nodeAddrs       []string
	clients         []SingleClientProvider
	chainID         *big.Int
	currClientMu    sync.Mutex
	currClientIdx   int
	closed          chan struct{}
}

// NewMulti creates a new instance of MultiClient.
func NewMulti(ctx context.Context, nodeAddrs []string, contractAddr ethcommon.Address, opts ...OptionMulti) (*MultiClient, error) {
	if len(nodeAddrs) == 0 {
		return nil, fmt.Errorf("no node address provided")
	}

	multiClient := &MultiClient{
		nodeAddrs:                   nodeAddrs,
		contractAddress:             contractAddr,
		logger:                      zap.NewNop(),
		followDistance:              DefaultFollowDistance,
		connectionTimeout:           DefaultConnectionTimeout,
		reconnectionInitialInterval: DefaultReconnectionInitialInterval,
		reconnectionMaxInterval:     DefaultReconnectionMaxInterval,
		logBatchSize:                DefaultHistoricalLogsBatchSize,
	}

	for _, opt := range opts {
		opt(multiClient)
	}

	for _, nodeAddr := range nodeAddrs {
		singleClient, err := New(
			ctx,
			nodeAddr,
			contractAddr,
			WithLogger(multiClient.logger),
			WithFollowDistance(multiClient.followDistance),
			WithConnectionTimeout(multiClient.connectionTimeout),
			WithReconnectionInitialInterval(multiClient.reconnectionInitialInterval),
			WithReconnectionMaxInterval(multiClient.reconnectionMaxInterval),
			WithSyncDistanceTolerance(multiClient.syncDistanceTolerance),
		)
		if err != nil {
			return nil, fmt.Errorf("create single client: %w", err)
		}

		multiClient.clients = append(multiClient.clients, singleClient)
	}

	same, err := multiClient.assertSameChainIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("assert same chain IDs: %w", err)
	}
	if !same {
		return nil, fmt.Errorf("execution clients' chain IDs are not same")
	}

	return multiClient, nil
}

// assertSameChainIDs checks if all healthy clients have the same chain ID.
// It sets firstChainID to the chain ID of the first healthy client encountered.
func (ec *MultiClient) assertSameChainIDs(ctx context.Context) (bool, error) {
	for i, client := range ec.clients {
		addr := ec.nodeAddrs[i]

		chainID, err := client.ChainID(ctx)
		if err != nil {
			ec.logger.Error("failed to get chain ID", zap.String("address", addr), zap.Error(err))
			return false, fmt.Errorf("get chain ID: %w", err)
		}
		if ec.chainID == nil {
			ec.chainID = chainID
			continue
		}
		if ec.chainID.Cmp(chainID) != 0 {
			ec.logger.Error("chain ID mismatch",
				zap.String("observed_chain_id", ec.chainID.String()),
				zap.String("checked_chain_id", chainID.String()),
				zap.String("address", addr))
			return false, nil
		}
	}

	return true, nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
func (ec *MultiClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (<-chan BlockLogs, <-chan error, error) {
	logsCh := make(chan BlockLogs, defaultLogBuf)
	errorsCh := make(chan error, 1)
	defer func() {
		close(logsCh)
		close(errorsCh)
	}()

	var nothingToSyncCount atomic.Int64

	f := func(client SingleClientProvider) (any, error) {
		innerLogs, innerErrors, innerErr := client.FetchHistoricalLogs(ctx, fromBlock)
		if innerErr != nil {
			if errors.Is(innerErr, ErrNothingToSync) {
				nothingToSyncCount.Add(1)
			}
			// Consider ErrNothingToSync as an error to make sure that other nodes return ErrNothingToSync too.
			// If they don't, it means that the current node is out of sync.
			// Therefore, we keep count of how many ErrNothingToSync we saw.
			return nil, innerErr
		}

		ec.logger.Info("waiting for client to fetch historical logs",
			zap.Uint64("from_block", fromBlock),
		)

		// Wait for the goroutine FetchHistoricalLogs to finish.
		// TODO: Make the logic asynchronous as the caller of FetchHistoricalLogs doesn't expect it to block.
		// This blocks until the underlying client closes its errors channel or sends an error.
		// The ExecutionClient code does defer-close, so it should never block forever.
		processingErr := <-innerErrors

		ec.logger.Info("client finished to fetch historical logs",
			zap.Uint64("from_block", fromBlock),
			zap.Error(processingErr),
		)

		if processingErr != nil {
			// If there has been an error during the process, copy all fetched logs, the error and update fromBlock,
			// then try another client.
			var lastBlock uint64
			for innerLog := range innerLogs {
				logsCh <- innerLog
				lastBlock = max(lastBlock, innerLog.BlockNumber)
			}
			fromBlock = lastBlock + 1 // TODO: make sure it will handle reorgs correctly

			errorsCh = make(chan error, defaultLogBuf)
			errorsCh <- processingErr
			close(errorsCh)

			// Return the error so that ec.call(...) tries the next client
			return nil, processingErr
		}

		for innerLog := range innerLogs {
			logsCh <- innerLog
		}
		// Clear the error if the client fetches the data successfully
		errorsCh = make(chan error, defaultLogBuf)
		close(errorsCh)
		return nil, nil
	}

	if _, err := ec.call(ctx, f); err != nil {
		if int(nothingToSyncCount.Load()) == len(ec.clients) {
			// All clients returned ErrNothingToSync
			return nil, nil, ErrNothingToSync
		}
		return nil, nil, err
	}

	return logsCh, errorsCh, nil
}

// StreamLogs subscribes to events emitted by the contract.
func (ec *MultiClient) StreamLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs {
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
				f := func(client SingleClientProvider) (any, error) {
					lastBlock, err := client.streamLogsToChan(ctx, logs, fromBlock)
					if errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
						// Closed gracefully.
						return lastBlock, err
					}

					// streamLogsToChan should never return without an error,
					// so we treat a nil error as an error by itself.
					if err == nil {
						err = errors.New("streamLogsToChan halted without an error")
					}
					if lastBlock <= fromBlock {
						err = errors.New("no new logs")
					}

					fromBlock = lastBlock + 1

					// Proceed with the next client
					return nil, err
				}

				_, err := ec.call(ctx, f)
				if errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
					// Closed gracefully.
					return
				}
				if err != nil {
					ec.logger.Fatal("failed to stream registry events", zap.Error(err))
				}
			}
		}
	}()

	return logs
}

// Healthy returns if execution client is currently healthy: responds to requests and not in the syncing state.
func (ec *MultiClient) Healthy(ctx context.Context) error {
	f := func(client SingleClientProvider) (any, error) {
		return nil, client.Healthy(ctx)
	}

	if _, err := ec.call(ctx, f); err != nil {
		return err
	}

	return nil
}

// BlockByNumber retrieves a block by its number.
func (ec *MultiClient) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.BlockByNumber(ctx, blockNumber)
	}
	res, err := ec.call(ctx, f)
	if err != nil {
		return nil, err
	}

	return res.(*ethtypes.Block), nil
}

func (ec *MultiClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.SubscribeFilterLogs(ctx, q, ch)
	}
	res, err := ec.call(ctx, f)
	if err != nil {
		return nil, err
	}

	return res.(ethereum.Subscription), nil
}

func (ec *MultiClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.FilterLogs(ctx, q)
	}
	res, err := ec.call(ctx, f)
	if err != nil {
		return nil, err
	}

	return res.([]ethtypes.Log), nil
}

func (ec *MultiClient) Filterer() (*contract.ContractFilterer, error) {
	return contract.NewContractFilterer(ec.contractAddress, ec)
}

func (ec *MultiClient) ChainID(_ context.Context) (*big.Int, error) {
	return ec.chainID, nil
}

func (ec *MultiClient) Close() error {
	close(ec.closed)

	var multiErr error
	for i, client := range ec.clients {
		if err := client.Close(); err != nil {
			ec.logger.Debug("Failed to close client", zap.String("address", ec.nodeAddrs[i]), zap.Error(err))
			multiErr = errors.Join(multiErr, err)
		}
	}

	return multiErr
}

func (ec *MultiClient) call(ctx context.Context, f func(client SingleClientProvider) (any, error)) (any, error) {
	for range len(ec.clients) {
		ec.currClientMu.Lock()
		currentIdx := ec.currClientIdx
		client := ec.clients[currentIdx]
		ec.currClientMu.Unlock()

		v, err := f(client)
		if errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
			// Closed gracefully.
			return v, nil
		}

		if err != nil {
			// TODO: log

			ec.currClientMu.Lock()
			idx := ec.currClientIdx
			// The index might have already changed if a parallel request failed.
			if idx == currentIdx {
				ec.currClientIdx = (ec.currClientIdx + 1) % len(ec.clients)
			}
			ec.currClientMu.Unlock()

			//client.reconnect(ctx) // TODO: implement reconnecting

			continue
		}

		return v, nil
	}

	return nil, fmt.Errorf("all clients returned an error")
}
