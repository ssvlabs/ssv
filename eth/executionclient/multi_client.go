package executionclient

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sourcegraph/conc/pool"
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
	chainID         *big.Int
	closed          chan struct{}

	nodeAddrs          []string
	clients            []SingleClientProvider
	currentClientIndex atomic.Int64
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
func (mc *MultiClient) assertSameChainIDs(ctx context.Context) (bool, error) {
	for i, client := range mc.clients {
		addr := mc.nodeAddrs[i]

		chainID, err := client.ChainID(ctx)
		if err != nil {
			mc.logger.Error("failed to get chain ID", zap.String("address", addr), zap.Error(err))
			return false, fmt.Errorf("get chain ID: %w", err)
		}
		if mc.chainID == nil {
			mc.chainID = chainID
			continue
		}
		if mc.chainID.Cmp(chainID) != 0 {
			mc.logger.Error("chain ID mismatch",
				zap.String("observed_chain_id", mc.chainID.String()),
				zap.String("checked_chain_id", chainID.String()),
				zap.String("address", addr))
			return false, nil
		}
	}

	return true, nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
// It calls FetchHistoricalLogs of all clients until a no-error result.
// It doesn't handle errors in the error channel to simplify logic.
// In this case, caller should call Panic/Fatal to restart the node.
func (mc *MultiClient) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (<-chan BlockLogs, <-chan error, error) {
	var logCh <-chan BlockLogs
	var errCh <-chan error

	f := func(client SingleClientProvider) (any, error) {
		singleLogsCh, singleErrCh, err := client.FetchHistoricalLogs(ctx, fromBlock)
		if err != nil {
			return nil, err
		}

		logCh = singleLogsCh
		errCh = singleErrCh
		return nil, nil
	}

	_, err := mc.call(contextWithMethod(ctx, "FetchHistoricalLogs"), f)
	if err != nil {
		return nil, nil, err
	}

	return logCh, errCh, nil
}

// StreamLogs subscribes to events emitted by the contract.
// NOTE: StreamLogs spawns a goroutine which calls os.Exit(1) if no client is available.
func (mc *MultiClient) StreamLogs(ctx context.Context, fromBlock uint64) <-chan BlockLogs {
	logs := make(chan BlockLogs)

	go func() {
		defer close(logs)
		for {
			select {
			case <-ctx.Done():
				return
			case <-mc.closed:
				return
			default:
				// Update healthyCh of all nodes and make sure at least one of them is available.
				if err := mc.Healthy(ctx); err != nil {
					mc.logger.Fatal("no healthy clients", zap.Error(err))
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

				_, err := mc.call(contextWithMethod(ctx, "StreamLogs"), f)
				if err != nil && !errors.Is(err, ErrClosed) && !errors.Is(err, context.Canceled) {
					// NOTE: There are unit tests that trigger Fatal and override its behavior.
					// Therefore, the code must call `return` afterward.
					mc.logger.Fatal("failed to stream registry events", zap.Error(err))
				}
				return
			}
		}
	}()

	return logs
}

// Healthy returns if execution client is currently healthy: responds to requests and not in the syncing state.
func (mc *MultiClient) Healthy(ctx context.Context) error {
	atLeastOne := atomic.Bool{}
	p := pool.New().WithErrors()
	for i, client := range mc.clients {
		p.Go(func() error {
			err := client.Healthy(ctx)
			if err != nil {
				mc.logger.Warn("client is not healthy",
					zap.String("addr", mc.nodeAddrs[i]),
					zap.Error(err))
				return err
			}
			atLeastOne.Store(true)
			return nil
		})
	}
	err := p.Wait()
	if atLeastOne.Load() {
		return nil
	}
	return fmt.Errorf("no healthy clients: %w", err)
}

// BlockByNumber retrieves a block by its number.
func (mc *MultiClient) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.BlockByNumber(ctx, blockNumber)
	}
	res, err := mc.call(contextWithMethod(ctx, "BlockByNumber"), f)
	if err != nil {
		return nil, err
	}

	return res.(*ethtypes.Block), nil
}

// HeaderByNumber retrieves a block header by its number.
func (mc *MultiClient) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.HeaderByNumber(ctx, blockNumber)
	}
	res, err := mc.call(contextWithMethod(ctx, "HeaderByNumber"), f)
	if err != nil {
		return nil, err
	}

	return res.(*ethtypes.Header), nil
}

func (mc *MultiClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.SubscribeFilterLogs(ctx, q, ch)
	}
	res, err := mc.call(contextWithMethod(ctx, "SubscribeFilterLogs"), f)
	if err != nil {
		return nil, err
	}

	return res.(ethereum.Subscription), nil
}

func (mc *MultiClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.FilterLogs(ctx, q)
	}
	res, err := mc.call(contextWithMethod(ctx, "FilterLogs"), f)
	if err != nil {
		return nil, err
	}

	return res.([]ethtypes.Log), nil
}

func (mc *MultiClient) Filterer() (*contract.ContractFilterer, error) {
	return contract.NewContractFilterer(mc.contractAddress, mc)
}

func (mc *MultiClient) ChainID(_ context.Context) (*big.Int, error) {
	return mc.chainID, nil
}

func (mc *MultiClient) Close() error {
	close(mc.closed)

	var multiErr error
	for i, client := range mc.clients {
		if err := client.Close(); err != nil {
			mc.logger.Debug("Failed to close client", zap.String("address", mc.nodeAddrs[i]), zap.Error(err))
			multiErr = errors.Join(multiErr, err)
		}
	}

	return multiErr
}

func (mc *MultiClient) call(ctx context.Context, f func(client SingleClientProvider) (any, error)) (any, error) {
	if len(mc.clients) == 1 {
		return f(mc.clients[0])
	}

	var allErrs error
	clientIndex := int(mc.currentClientIndex.Load())

	for i := 0; i < len(mc.clients); i++ {
		// Get the next client in round-robin fashion.
		clientIndex := (clientIndex + i) % len(mc.clients)
		nextClientIndex := (clientIndex + 1) % len(mc.clients) // For logging.
		client := mc.clients[clientIndex]

		logger := mc.logger.With(
			zap.String("addr", mc.nodeAddrs[clientIndex]),
			zap.String("method", methodFromContext(ctx)))

		// Make sure this client is healthy, this shouldn't cause too many requests because the result is cached.
		// TODO: Make sure the allowed tolerance doesn't cause issues in log streaming.
		if err := client.Healthy(ctx); err != nil {
			logger.Warn("client is not healthy, switching to next client",
				zap.String("next_addr", mc.nodeAddrs[nextClientIndex]),
				zap.Error(err))
			allErrs = errors.Join(allErrs, err)
			mc.currentClientIndex.Store(int64(clientIndex))
			continue
		}

		logger.Debug("calling client")

		v, err := f(client)
		if errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
			mc.logger.Debug("received graceful closure from client", zap.Error(err))
			return v, err
		}

		if err != nil {
			mc.logger.Error("call failed, trying another client",
				zap.String("next_addr", mc.nodeAddrs[nextClientIndex]),
				zap.Error(err))

			allErrs = errors.Join(allErrs, err)
			mc.currentClientIndex.Store(int64(clientIndex))
			continue
		}

		mc.currentClientIndex.Store(int64(clientIndex))
		return v, nil
	}

	return nil, fmt.Errorf("all clients failed: %w", allErrs)
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
