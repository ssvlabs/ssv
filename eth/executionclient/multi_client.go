package executionclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/observability/log/fields"
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
	followDistance             uint64 // TODO: consider reading the finalized checkpoint from consensus layer
	connectionTimeout          time.Duration
	healthInvalidationInterval time.Duration
	logBatchSize               uint64
	syncDistanceTolerance      uint64

	contractAddress ethcommon.Address
	chainID         atomic.Pointer[big.Int]
	closed          chan struct{}

	clientAddrs        []string
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
		clientAddrs:       nodeAddrs,
		clients:           make([]SingleClientProvider, len(nodeAddrs)), // initialized with nil values (not connected)
		clientsMu:         make([]sync.Mutex, len(nodeAddrs)),
		contractAddress:   contractAddr,
		logger:            zap.NewNop(),
		followDistance:    DefaultFollowDistance,
		connectionTimeout: DefaultConnectionTimeout,
		logBatchSize:      DefaultHistoricalLogsBatchSize,
	}

	for _, opt := range opts {
		opt(multiClient)
	}

	multiClient.logger.Info("execution client: connecting (multi client)", fields.Addresses(nodeAddrs))

	var connected bool

	var multiErr error
	for clientIndex, clientAddr := range nodeAddrs {
		c, err := multiClient.connect(ctx, clientAddr)
		if err != nil {
			multiErr = errors.Join(multiErr, fmt.Errorf("connect EL client %s error: %w", clientAddr, err))
			continue
		}
		multiClient.clients[clientIndex] = c
		connected = true
	}

	if !connected {
		return nil, fmt.Errorf("couldn't connect even one of EL clients: %w", multiErr)
	}

	return multiClient, nil
}

// getClient gets a client at index. If it's nil (which means it's not connected), it attempts to connect to it and
// store connected client instead of nil. Even though we try to connect all the EL clients in NewMulti some of those
// clients might not connect successfully at the time, this lazy initialization implemented by getClient remedies
// that (essentially serving as a clutch re-connect mechanism).
func (mc *MultiClient) getClient(ctx context.Context, clientIndex int) (SingleClientProvider, error) {
	mc.clientsMu[clientIndex].Lock()
	defer mc.clientsMu[clientIndex].Unlock()

	if mc.clients[clientIndex] == nil {
		clientAddr := mc.clientAddrs[clientIndex]
		c, err := mc.connect(ctx, clientAddr)
		if err != nil {
			return nil, fmt.Errorf("connect EL client: %w", err)
		}
		mc.clients[clientIndex] = c
	}

	return mc.clients[clientIndex], nil
}

// connect connects to a client by clientIndex and updates mc.clients[clientIndex] without locks.
// Caller must lock mc.clientsMu[clientIndex].
func (mc *MultiClient) connect(ctx context.Context, clientAddr string) (*ExecutionClient, error) {
	singleClient, err := New(
		ctx,
		clientAddr,
		mc.contractAddress,
		WithLogger(mc.logger),
		WithFollowDistance(mc.followDistance),
		WithConnectionTimeout(mc.connectionTimeout),
		WithHealthInvalidationInterval(mc.healthInvalidationInterval),
		WithSyncDistanceTolerance(mc.syncDistanceTolerance),
	)
	if err != nil {
		recordClientInitStatus(ctx, clientAddr, false)
		return nil, fmt.Errorf("create single client: %w", err)
	}

	chainID, err := singleClient.ChainID(ctx)
	if err != nil {
		recordClientInitStatus(ctx, clientAddr, false)
		return nil, fmt.Errorf("get chain ID: %w", err)
	}

	expectedChainID, ok := mc.assertSameChainID(chainID)
	if !ok {
		recordClientInitStatus(ctx, clientAddr, false)
		return nil, fmt.Errorf("client responded with unexpected chain ID, want: %s, got: %s", expectedChainID.String(), chainID.String())
	}

	recordClientInitStatus(ctx, clientAddr, true)

	return singleClient, nil
}

// assertSameChainID checks if client has the same chain ID.
// It sets mc.chainID to the chain ID of the first healthy client encountered.
func (mc *MultiClient) assertSameChainID(chainID *big.Int) (*big.Int, bool) {
	if mc.chainID.CompareAndSwap(nil, chainID) {
		return chainID, true
	}

	expected := mc.chainID.Load()
	if expected.Cmp(chainID) != 0 {
		return expected, false
	}

	return expected, true
}

// FetchHistoricalLogs retrieves historical logs emitted by the Ethereum SSV contract(s) starting at fromBlock.
// It delegates the FetchHistoricalLogs call to clients MultiClient is configured with until one of them manages
// to serve it without an error.
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

// StreamLogs subscribes to events emitted by the Ethereum SSV contract(s) starting at fromBlock.
// It spawns a go-routine that spins in a perpetual retry loop, terminating only on unrecoverable
// interruptions (such as context cancels, client closure, etc.) as defined by isMultiClientInterruptedError func.
// Any errors encountered during log-streaming are relayed on errorsCh channel. Both logsCh and errorsCh
// are closed once the streaming go-routine terminates.
func (mc *MultiClient) StreamLogs(ctx context.Context, fromBlock uint64) (logsCh chan BlockLogs, errCh chan error) {
	logsCh = make(chan BlockLogs)
	// All errors are buffered, so we don't block the execution of this func (waiting on the caller to
	// handle the error before we can continue further).
	errCh = make(chan error, 1)

	go func() {
		defer close(logsCh)
		defer close(errCh)

		const maxTries = math.MaxUint64 // infinitely many
		tries := uint64(0)

		for {
			select {
			case <-ctx.Done():
				return
			case <-mc.closed:
				return
			default:
				// fromBlock is defined in the outer scope (from f's func perspective), but we have to modify it
				// inside the f func because in case of failover to another client - we want to keep progressing
				// from the latest block (not re-read the whole feed from the initial value of fromBlock). Note,
				// f is called sequentially by a single go-routine, hence there is no need to synchronize concurrent
				// access to fromBlock variable.
				f := func(client SingleClientProvider) (any, error) {
					lastProcessedBlock, progressed, err := client.streamLogsToChan(ctx, logsCh, fromBlock)
					if progressed {
						fromBlock = lastProcessedBlock + 1
					}
					if isSingleClientInterruptedError(err) {
						// This is a valid way to finish streamLogsToChan call. Propagate error to let the caller know.
						return nil, err
					}
					if err == nil {
						// streamLogsToChan should never return without an error, so we treat a nil error as
						// an error by itself.
						err = fmt.Errorf("streamLogsToChan halted without an error")
					}
					return nil, err
				}
				_, err := mc.call(contextWithMethod(ctx, "StreamLogs"), f)
				if isMultiClientInterruptedError(err) {
					// We are done with log streaming.
					return
				}

				tries++
				if tries > maxTries {
					errCh <- fmt.Errorf("failed to stream registry events even after %d retries: %w", tries, err)
					return
				}
				mc.logger.Error("failed to stream registry events, gonna retry", zap.Error(err))
			}
		}
	}()

	return logsCh, errCh
}

// Healthy returns whether MultiClient has at least 1 healthy EL client (a client that responds to requests and
// not is in the syncing state).
func (mc *MultiClient) Healthy(ctx context.Context) error {
	if time.Since(time.Unix(mc.lastHealthy.Load(), 0)) < mc.healthInvalidationInterval {
		return nil
	}

	healthyClients := atomic.Bool{}
	healthyCount := atomic.Int64{}
	p := pool.New().WithErrors().WithContext(ctx)

	for i := range mc.clients {
		p.Go(func(ctx context.Context) error {
			client, err := mc.getClient(ctx, i)
			if err != nil {
				mc.logger.Warn("client unavailable",
					zap.String("addr", mc.clientAddrs[i]),
					zap.Error(err))
				return err
			}

			if err := client.Healthy(ctx); err != nil {
				mc.logger.Warn("client is not healthy",
					zap.String("addr", mc.clientAddrs[i]),
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
				mc.logger.Debug("Failed to close client", zap.String("address", mc.clientAddrs[i]), zap.Error(err))
				multiErr = errors.Join(multiErr, err)
			}
		}
	}

	return multiErr
}

// call calls f until it succeeds using MultiClient clients, starting with the most likely healthy client
// (the one at currentClientIndex) iterating over the client list in round-robin fashion.
func (mc *MultiClient) call(ctx context.Context, f func(client SingleClientProvider) (any, error)) (any, error) {
	method := routeNameFromContext(ctx)
	startTime := time.Now()

	clientsTotal := len(mc.clients)

	// Iterate over the clients in round-robin fashion, starting from the most likely healthy client (currentClientIndex).
	startingIndex := int(mc.currentClientIndex.Load())
	var allErrs error
	for i := 0; i < clientsTotal; i++ {
		clientIndex := (startingIndex + i) % clientsTotal
		nextClientIndex := (clientIndex + 1) % clientsTotal // For logging.

		client, err := mc.getClient(ctx, clientIndex)
		if err != nil {
			mc.logger.Warn("client unavailable, switching to the next client",
				zap.String("addr", mc.clientAddrs[clientIndex]),
				zap.Error(err),
			)
			allErrs = errors.Join(allErrs, err)
			mc.currentClientIndex.Store(int64(nextClientIndex)) // Advance.
			recordClientSwitch(ctx, mc.clientAddrs[clientIndex], mc.clientAddrs[nextClientIndex])
			continue
		}

		logger := mc.logger.With(zap.String("addr", mc.clientAddrs[clientIndex]), zap.String("method", method))

		// Make sure this client is healthy. This shouldn't cause too many requests because the result is cached.
		// TODO: Make sure the allowed tolerance doesn't cause issues in log streaming.
		if err := client.Healthy(ctx); err != nil {
			logger.Warn("client is not healthy, switching to the next client",
				zap.String("next_addr", mc.clientAddrs[nextClientIndex]),
				zap.Error(err),
			)
			allErrs = errors.Join(allErrs, err)
			mc.currentClientIndex.Store(int64(nextClientIndex)) // Advance.
			recordClientSwitch(ctx, mc.clientAddrs[clientIndex], mc.clientAddrs[nextClientIndex])
			continue
		}

		v, err := f(client)
		if isMultiClientInterruptedError(err) {
			recordMultiClientMethodCall(ctx, method, mc.clientAddrs[clientIndex], time.Since(startTime), err)
			return v, err
		}
		if err != nil {
			logger.Error("call failed, switching to the next client",
				zap.String("next_addr", mc.clientAddrs[nextClientIndex]),
				zap.Error(err),
			)
			allErrs = errors.Join(allErrs, err)
			mc.currentClientIndex.Store(int64(nextClientIndex)) // Advance.
			recordClientSwitch(ctx, mc.clientAddrs[clientIndex], mc.clientAddrs[nextClientIndex])
			continue
		}

		// Update currentClientIndex to the successful client.
		mc.currentClientIndex.Store(int64(clientIndex))
		recordMultiClientMethodCall(ctx, method, mc.clientAddrs[clientIndex], time.Since(startTime), nil)
		return v, nil
	}

	// Record the failure with the last attempted client
	lastClientIndex := (startingIndex + clientsTotal - 1) % clientsTotal
	recordMultiClientMethodCall(ctx, method, mc.clientAddrs[lastClientIndex], time.Since(startTime), allErrs)
	return nil, fmt.Errorf("all clients failed: %w", allErrs)
}

type routeNameContextKey struct{}

func contextWithMethod(ctx context.Context, method string) context.Context {
	return context.WithValue(ctx, routeNameContextKey{}, method)
}

func routeNameFromContext(ctx context.Context) string {
	v, ok := ctx.Value(routeNameContextKey{}).(string)
	if !ok {
		return ""
	}
	return v
}
