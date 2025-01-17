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
	"go.uber.org/zap/zapcore"

	"github.com/ssvlabs/ssv/eth/contract"
)

var _ Provider = &MultiClient{}

type MultiClient struct {
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

	logger := multiClient.logger.WithOptions(zap.WithFatalHook(zapcore.WriteThenNoop))

	for _, nodeAddr := range nodeAddrs {
		singleClient, err := New(
			ctx,
			nodeAddr,
			contractAddr,
			WithLogger(logger),
			WithFollowDistance(multiClient.followDistance),
			WithConnectionTimeout(multiClient.connectionTimeout),
			WithReconnectionInitialInterval(multiClient.reconnectionInitialInterval),
			WithReconnectionMaxInterval(multiClient.reconnectionMaxInterval),
			WithHealthInvalidationInterval(multiClient.healthInvalidationInterval),
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
	errCh := make(chan error, 1)

	var singleLogsCh <-chan BlockLogs
	var singleErrCh <-chan error
	var nothingToSyncCount atomic.Int64

	// Update healthiness of all nodes and make sure at least one of them is available.
	if err := ec.Healthy(ctx); err != nil {
		ec.logger.Fatal("no healthy clients", zap.Error(err))
	}

	// Find a client that's able to provide the requested data.
	startFetchingFunc := func(client SingleClientProvider) (any, error) {
		cl, ce, err := client.FetchHistoricalLogs(ctx, fromBlock)
		if err != nil {
			if errors.Is(err, ErrNothingToSync) {
				nothingToSyncCount.Add(1)
			}
			// Consider ErrNothingToSync as an error to make sure that other nodes return ErrNothingToSync too.
			// If they don't, it means that the current node is out of sync.
			// Therefore, we keep count of how many ErrNothingToSync we saw.
			return nil, err
		}

		singleLogsCh = cl
		singleErrCh = ce
		return nil, nil
	}

	if _, err := ec.call(contextWithMethod(ctx, "FetchHistoricalLogs [start fetching]"), startFetchingFunc); err != nil {
		if int(nothingToSyncCount.Load()) == len(ec.clients) {
			// All clients returned ErrNothingToSync
			return nil, nil, ErrNothingToSync
		}
		return nil, nil, err
	}

	go func() {
		defer func() {
			close(logsCh)
			close(errCh)
		}()

		var needInitChans atomic.Bool

		// Getting historical logs may take significant amount of time.
		// The current client may fail in the middle of the process,
		// and also some clients may become available again, therefore we need to try other clients on error.
		// Since singleLogsCh and singleErrCh are initialized by the active client,
		// we need to re-initialize them if we try other clients.
		processLogsFunc := func(client SingleClientProvider) (any, error) {
			if needInitChans.Load() {
				if _, err := startFetchingFunc(client); err != nil {
					return nil, err
				}
			} else {
				needInitChans.Store(true)
			}

			var lastBlock uint64

		processLogsLabel:
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ec.closed:
					return nil, fmt.Errorf("client closed")
				case log, ok := <-singleLogsCh:
					if !ok {
						// Underlying channel is closed -> no more logs.
						break processLogsLabel
					}
					logsCh <- log
					lastBlock = max(lastBlock, log.BlockNumber)
				}
			}

		processErrsLabel:
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ec.closed:
					return nil, fmt.Errorf("client closed")
				case err, ok := <-singleErrCh:
					if !ok {
						// If the error channel closed, treat that as no more logs or success.
						break processErrsLabel
					}
					fromBlock = max(fromBlock, lastBlock+1)
					return nil, err // Try another client
				}
			}

			return nil, nil
		}

		if _, err := ec.call(contextWithMethod(ctx, "FetchHistoricalLogs [process logs]"), processLogsFunc); err != nil {
			errCh <- err
		}
	}()

	return logsCh, errCh, nil
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
				// Update healthiness of all nodes and make sure at least one of them is available.
				if err := ec.Healthy(ctx); err != nil {
					ec.logger.Fatal("no healthy clients", zap.Error(err))
				}

				f := func(client SingleClientProvider) (any, error) {
					lastBlock, err := client.streamLogsToChan(ctx, logs, fromBlock)
					if errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
						return lastBlock, err
					}
					if err != nil {
						// fromBlock's value in the outer scope is updated here, so this function needs to be a closure
						fromBlock = max(fromBlock, lastBlock+1)
						return nil, err
					}

					return nil, nil
				}

				_, err := ec.call(contextWithMethod(ctx, "StreamLogs"), f)
				if err != nil && !errors.Is(err, ErrClosed) && !errors.Is(err, context.Canceled) {
					// NOTE: There are unit tests that trigger Fatal and override its behavior.
					// Therefore, the code must call `return` afterward.
					ec.logger.Fatal("failed to stream registry events", zap.Error(err))
				}
				return
			}
		}
	}()

	return logs
}

// Healthy returns if execution client is currently healthy: responds to requests and not in the syncing state.
func (ec *MultiClient) Healthy(ctx context.Context) error {
	var atLeastOneHealthy atomic.Bool
	var errs error
	var errsMu sync.Mutex

	var wg sync.WaitGroup
	// We need to fetch all clients to update the internal state
	for i, client := range ec.clients {
		wg.Add(1)
		go func(i int, client SingleClientProvider) {
			defer wg.Done()

			if err := client.Healthy(ctx); err != nil {
				ec.logger.Warn("client is not healthy",
					zap.String("addr", ec.nodeAddrs[i]),
					zap.Error(err))

				errsMu.Lock()
				errs = errors.Join(errs, err)
				errsMu.Unlock()
			} else {
				ec.logger.Debug("client is healthy",
					zap.String("addr", ec.nodeAddrs[i]))

				atLeastOneHealthy.Store(true)
			}
		}(i, client)
	}
	wg.Wait()

	if !atLeastOneHealthy.Load() {
		return errs
	}

	return nil
}

// BlockByNumber retrieves a block by its number.
func (ec *MultiClient) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.BlockByNumber(ctx, blockNumber)
	}
	res, err := ec.call(contextWithMethod(ctx, "BlockByNumber"), f)
	if err != nil {
		return nil, err
	}

	return res.(*ethtypes.Block), nil
}

// HeaderByNumber retrieves a block header by its number.
func (ec *MultiClient) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.HeaderByNumber(ctx, blockNumber)
	}
	res, err := ec.call(contextWithMethod(ctx, "HeaderByNumber"), f)
	if err != nil {
		return nil, err
	}

	return res.(*ethtypes.Header), nil
}

func (ec *MultiClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.SubscribeFilterLogs(ctx, q, ch)
	}
	res, err := ec.call(contextWithMethod(ctx, "SubscribeFilterLogs"), f)
	if err != nil {
		return nil, err
	}

	return res.(ethereum.Subscription), nil
}

func (ec *MultiClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.FilterLogs(ctx, q)
	}
	res, err := ec.call(contextWithMethod(ctx, "FilterLogs"), f)
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
	if len(ec.clients) == 1 {
		return f(ec.clients[0])
	}

	for i := 0; i < len(ec.clients); i++ {
		ec.currClientMu.Lock()
		currentIdx := ec.currClientIdx
		ec.currClientMu.Unlock()

		client := ec.clients[currentIdx]

		ec.logger.Debug("checking client healthiness",
			zap.Int("index", currentIdx),
			zap.String("method", methodFromContext(ctx)),
			zap.String("addr", ec.nodeAddrs[currentIdx]))

		// Make sure this client is healthy, this shouldn't cause too many requests because the result is cached.
		// TODO: Make sure the allowed tolerance doesn't cause issues in log streaming.
		if err := client.Healthy(ctx); err != nil {
			ec.logger.Warn("client is not healthy, using another one",
				zap.Int("index", currentIdx),
				zap.String("method", methodFromContext(ctx)),
				zap.String("addr", ec.nodeAddrs[currentIdx]))

			ec.useNextClient(ctx, currentIdx, client)

			continue
		}

		ec.logger.Debug("calling client",
			zap.Int("index", currentIdx),
			zap.String("method", methodFromContext(ctx)),
			zap.String("addr", ec.nodeAddrs[currentIdx]))

		v, err := f(client)
		if errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
			ec.logger.Debug("received graceful closure from client", zap.Error(err))
			return v, err
		}

		if err != nil {
			ec.logger.Error("call failed, trying another client",
				zap.String("method", methodFromContext(ctx)),
				zap.String("addr", ec.nodeAddrs[currentIdx]),
				zap.Error(err))

			ec.useNextClient(ctx, currentIdx, client)

			continue
		}

		ec.logger.Debug("call succeeded, returning value",
			zap.String("method", methodFromContext(ctx)),
			zap.String("addr", ec.nodeAddrs[currentIdx]))
		return v, nil
	}

	return nil, fmt.Errorf("all clients returned an error")
}

func (ec *MultiClient) useNextClient(ctx context.Context, currentIdx int, client SingleClientProvider) {
	ec.currClientMu.Lock()
	idx := ec.currClientIdx
	// The index might have already changed if a parallel request failed.
	if idx == currentIdx {
		ec.currClientIdx = (ec.currClientIdx + 1) % len(ec.clients)
	}
	ec.currClientMu.Unlock()

	go client.reconnect(ctx)
}

type methodContextKey struct{}

func contextWithMethod(ctx context.Context, method string) context.Context {
	return context.WithValue(ctx, methodContextKey{}, method)
}

func methodFromContext(ctx context.Context) string {
	v, ok := ctx.Value(methodContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}
