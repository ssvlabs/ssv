package executionclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ssvlabs/ssv/eth/contract"
)

var _ Provider = &MultiClient{}

// MultiClient wraps several execution clients and uses the current available one.
//
// There are several scenarios of node outage:
//
// 1) SSV node uses EL1, EL2, CL1, CL2; CL1 uses only EL1, CL2 uses only EL2. EL1 becomes unavailable.
//
// The execution client MultiClient switches to EL2, consensus multi client (another package) remains on CL1.
// CL1 remains available and responds to requests but its syncing distance increases until EL1 is up.
// The consensus multi client cannot determine that it's unhealthy until the sync distance reaches SyncDistanceTolerance,
// but when it does, it switches to CL2 and then SSV node uses EL2 and CL2.
// This case usually causes duty misses approximately for duration of SyncDistanceTolerance.
//
// 2) SSV node uses EL1, EL2, CL1, CL2; CL1 uses EL1 and other available ELs, CL2 uses any available EL.
// EL1 becomes unavailable.
//
// The execution MultiClient switches to EL2, the consensus multi client remains on CL1,
// which should switch its execution client from EL1 to an available one.
// Possible duty misses up to SyncDistanceTolerance duration
//
// 3) SSV node uses EL1, EL2, CL1, CL2; CL1 uses any available EL, CL2 uses any available EL.
// EL1 becomes unavailable.
//
// The execution MultiClient switches to EL2, the consensus multi client remains on CL1,
// which should remain working. This shouldn't cause significant duty misses.
//
// 4) SSV node uses EL1, EL2, CL1, CL2; CL1 uses only EL1, CL2 uses only EL2. EL1 and CL1 become unavailable.
//
// The execution MultiClient switches to EL2, the consensus multi client switches to CL2,
// This shouldn't cause significant duty misses.
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
	chainID         atomic.Pointer[big.Int]
	closed          chan struct{}

	nodeAddrs          []string
	clientsMu          []sync.Mutex           // clientsMu allow for lazy initialization of each client in `clients` slice in thread-safe manner (atomically)
	clients            []SingleClientProvider // nil if not connected
	currentClientIndex atomic.Int64
	lastHealthy        atomic.Int64
}

// NewMulti creates a new instance of MultiClient.
func NewMulti(
	ctx context.Context,
	nodeAddrs []string,
	contractAddr ethcommon.Address,
	opts ...OptionMulti,
) (*MultiClient, error) {
	if len(nodeAddrs) == 0 {
		return nil, fmt.Errorf("no node address provided")
	}

	multiClient := &MultiClient{
		nodeAddrs:                   nodeAddrs,
		clients:                     make([]SingleClientProvider, len(nodeAddrs)), // initialized with nil values (not connected)
		clientsMu:                   make([]sync.Mutex, len(nodeAddrs)),
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

	var connected bool

	var multiErr error
	for clientIndex := range nodeAddrs {
		if err := multiClient.connect(ctx, clientIndex); err != nil {
			multiClient.logger.Error("failed to connect to node",
				zap.String("address", nodeAddrs[clientIndex]),
				zap.Error(err))

			multiErr = errors.Join(multiErr, err)
			continue
		}

		connected = true
	}

	if !connected {
		return nil, fmt.Errorf("no available clients: %w", multiErr)
	}

	return multiClient, nil
}

// getClient gets a client at index.
// If it's nil (which means it's not connected), it attempts to connect to it and store connected client instead of nil.
func (mc *MultiClient) getClient(ctx context.Context, clientIndex int) (SingleClientProvider, error) {
	mc.clientsMu[clientIndex].Lock()
	defer mc.clientsMu[clientIndex].Unlock()

	if mc.clients[clientIndex] == nil {
		if err := mc.connect(ctx, clientIndex); err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
	}

	return mc.clients[clientIndex], nil
}

// connect connects to a client by clientIndex and updates mc.clients[clientIndex] without locks.
// Caller must lock mc.clientsMu[clientIndex].
func (mc *MultiClient) connect(ctx context.Context, clientIndex int) error {
	// ExecutionClient may call Fatal on unsuccessful reconnection attempt.
	// Therefore, we need to override its Fatal behavior to avoid crashing.
	logger := mc.logger.WithOptions(zap.WithFatalHook(zapcore.WriteThenNoop), zap.WithPanicHook(zapcore.WriteThenNoop))

	singleClient, err := New(
		ctx,
		mc.nodeAddrs[clientIndex],
		mc.contractAddress,
		WithLogger(logger),
		WithFollowDistance(mc.followDistance),
		WithConnectionTimeout(mc.connectionTimeout),
		WithReconnectionInitialInterval(mc.reconnectionInitialInterval),
		WithReconnectionMaxInterval(mc.reconnectionMaxInterval),
		WithHealthInvalidationInterval(mc.healthInvalidationInterval),
		WithSyncDistanceTolerance(mc.syncDistanceTolerance),
	)
	if err != nil {
		recordClientInitStatus(ctx, mc.nodeAddrs[clientIndex], false)
		return fmt.Errorf("create single client: %w", err)
	}

	chainID, err := singleClient.ChainID(ctx)
	if err != nil {
		logger.Error("failed to get chain ID",
			zap.String("address", mc.nodeAddrs[clientIndex]),
			zap.Error(err))
		recordClientInitStatus(ctx, mc.nodeAddrs[clientIndex], false)
		return fmt.Errorf("get chain ID: %w", err)
	}

	if expected, err := mc.assertSameChainID(chainID); err != nil {
		logger.Fatal("client returned unexpected chain ID",
			zap.String("expected_chain_id", expected.String()),
			zap.String("checked_chain_id", chainID.String()),
			zap.String("address", mc.nodeAddrs[clientIndex]),
			zap.Error(err),
		)
		recordClientInitStatus(ctx, mc.nodeAddrs[clientIndex], false)
		return err
	}

	mc.clients[clientIndex] = singleClient
	recordClientInitStatus(ctx, mc.nodeAddrs[clientIndex], true)
	return nil
}

// assertSameChainID checks if client has the same chain ID.
// It sets mc.chainID to the chain ID of the first healthy client encountered.
func (mc *MultiClient) assertSameChainID(chainID *big.Int) (*big.Int, error) {
	if chainID == nil {
		return nil, fmt.Errorf("chain ID is nil")
	}

	if mc.chainID.CompareAndSwap(nil, chainID) {
		return chainID, nil
	}

	expected := mc.chainID.Load()
	if expected.Cmp(chainID) != 0 {
		return expected, fmt.Errorf("different chain ID, expected %v, got %v", expected.String(), chainID.String())
	}

	return expected, nil
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

	_, err := mc.call(contextWithMethod(ctx, "FetchHistoricalLogs"), f, len(mc.clients))
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

				_, err := mc.call(contextWithMethod(ctx, "StreamLogs"), f, 0)
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
	if time.Since(time.Unix(mc.lastHealthy.Load(), 0)) < mc.healthInvalidationInterval {
		return nil
	}

	healthyClients := atomic.Bool{}
	healthyCount := atomic.Int64{}
	p := pool.New().WithErrors().WithContext(ctx)

	for i := range mc.clients {
		i := i
		p.Go(func(ctx context.Context) error {
			client, err := mc.getClient(ctx, i)
			if err != nil {
				mc.logger.Warn("client unavailable",
					zap.String("addr", mc.nodeAddrs[i]),
					zap.Error(err))

				return err
			}

			if err := client.Healthy(ctx); err != nil {
				mc.logger.Warn("client is not healthy",
					zap.String("addr", mc.nodeAddrs[i]),
					zap.Error(err))

				return err
			}
			healthyClients.Store(true)
			healthyCount.Add(1)

			return nil
		})
	}
	err := p.Wait()

	// Record the current count of healthy clients and all clients count.
	recordHealthyClientsCount(ctx, healthyCount.Load())
	recordAllClientsCount(ctx, int64(len(mc.clients)))

	if healthyClients.Load() {
		mc.lastHealthy.Store(time.Now().Unix())
		return nil
	}
	return fmt.Errorf("no healthy clients: %w", err)
}

// BlockByNumber retrieves a block by its number.
func (mc *MultiClient) BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.BlockByNumber(ctx, blockNumber)
	}
	res, err := mc.call(contextWithMethod(ctx, "BlockByNumber"), f, len(mc.clients))
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
	res, err := mc.call(contextWithMethod(ctx, "HeaderByNumber"), f, len(mc.clients))
	if err != nil {
		return nil, err
	}

	return res.(*ethtypes.Header), nil
}

func (mc *MultiClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.SubscribeFilterLogs(ctx, q, ch)
	}
	res, err := mc.call(contextWithMethod(ctx, "SubscribeFilterLogs"), f, len(mc.clients))
	if err != nil {
		return nil, err
	}

	return res.(ethereum.Subscription), nil
}

func (mc *MultiClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	f := func(client SingleClientProvider) (any, error) {
		return client.FilterLogs(ctx, q)
	}
	res, err := mc.call(contextWithMethod(ctx, "FilterLogs"), f, len(mc.clients))
	if err != nil {
		return nil, err
	}

	return res.([]ethtypes.Log), nil
}

func (mc *MultiClient) Filterer() (*contract.ContractFilterer, error) {
	return contract.NewContractFilterer(mc.contractAddress, mc)
}

func (mc *MultiClient) ChainID(_ context.Context) (*big.Int, error) {
	return mc.chainID.Load(), nil
}

func (mc *MultiClient) Close() error {
	close(mc.closed)

	var multiErr error
	for i := range mc.clients {
		mc.clientsMu[i].Lock()
		client := mc.clients[i]
		mc.clientsMu[i].Unlock()

		if client != nil {
			if err := client.Close(); err != nil {
				mc.logger.Debug("Failed to close client", zap.String("address", mc.nodeAddrs[i]), zap.Error(err))
				multiErr = errors.Join(multiErr, err)
			}
		}
	}

	return multiErr
}

// call calls f for all clients until it succeeds.
//
// If there's only one client, call just calls f for it preserving old behavior. The maxTries parameter is ignored in this case.
// If maxTries is not 0, it tries all clients in a round-robin logic until the limit is hit,
// and if no client is available then it returns an error.
// If maxTries is 0, it iterates clients forever.
//
// It must be called with maxTries == 0 from StreamLogs because StreamLogs is called once per the node lifetime,
// and it's possible that clients go up and down several times, therefore there should be no limit.
// It must be called with maxTries != 0 from other methods to return an error if all nodes are down.
func (mc *MultiClient) call(ctx context.Context, f func(client SingleClientProvider) (any, error), maxTries int) (any, error) {
	method := methodFromContext(ctx)
	startTime := time.Now()

	if len(mc.clients) == 1 {
		client := mc.clients[0] // no need for mutex because one client is always non-nil
		result, err := f(client)
		recordMultiClientMethodCall(ctx, method, mc.nodeAddrs[0], time.Since(startTime), err)
		return result, err
	}

	// Iterate over the clients in round-robin fashion,
	// starting from the most likely healthy client (currentClientIndex).
	startingIndex := int(mc.currentClientIndex.Load())
	var allErrs error
	// Iterate maxTries times if maxTries != 0. Iterate forever if maxTries == 0
	for i := 0; (maxTries == 0) || (i < maxTries); i++ {
		clientIndex := (startingIndex + i) % len(mc.clients)
		nextClientIndex := (clientIndex + 1) % len(mc.clients) // For logging.

		client, err := mc.getClient(ctx, clientIndex)
		if err != nil {
			mc.logger.Warn("client unavailable, switching to the next client",
				zap.String("addr", mc.nodeAddrs[clientIndex]),
				zap.Error(err))

			allErrs = errors.Join(allErrs, err)
			mc.currentClientIndex.Store(int64(nextClientIndex)) // Advance.
			recordClientSwitch(ctx, mc.nodeAddrs[clientIndex], mc.nodeAddrs[nextClientIndex])
			continue
		}

		logger := mc.logger.With(
			zap.String("addr", mc.nodeAddrs[clientIndex]),
			zap.String("method", method))

		// Make sure this client is healthy. This shouldn't cause too many requests because the result is cached.
		// TODO: Make sure the allowed tolerance doesn't cause issues in log streaming.
		if err := client.Healthy(ctx); err != nil {
			logger.Warn("client is not healthy, switching to the next client",
				zap.String("next_addr", mc.nodeAddrs[nextClientIndex]),
				zap.Error(err))

			allErrs = errors.Join(allErrs, err)
			mc.currentClientIndex.Store(int64(nextClientIndex)) // Advance.
			recordClientSwitch(ctx, mc.nodeAddrs[clientIndex], mc.nodeAddrs[nextClientIndex])
			continue
		}

		v, err := f(client)
		if errors.Is(err, ErrClosed) || errors.Is(err, context.Canceled) {
			logger.Debug("received graceful closure from client", zap.Error(err))
			recordMultiClientMethodCall(ctx, method, mc.nodeAddrs[clientIndex], time.Since(startTime), err)
			return v, err
		}

		if err != nil {
			logger.Error("call failed, switching to the next client",
				zap.String("next_addr", mc.nodeAddrs[nextClientIndex]),
				zap.Error(err))

			allErrs = errors.Join(allErrs, err)
			mc.currentClientIndex.Store(int64(nextClientIndex)) // Advance.
			recordClientSwitch(ctx, mc.nodeAddrs[clientIndex], mc.nodeAddrs[nextClientIndex])
			continue
		}

		// Update currentClientIndex to the successful client.
		mc.currentClientIndex.Store(int64(clientIndex))
		recordMultiClientMethodCall(ctx, method, mc.nodeAddrs[clientIndex], time.Since(startTime), nil)
		return v, nil
	}

	// Record the failure with the last attempted client
	lastClientIndex := (startingIndex + maxTries - 1) % len(mc.clients)
	recordMultiClientMethodCall(ctx, method, mc.nodeAddrs[lastClientIndex], time.Since(startTime), allErrs)
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
