package executionclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNewMulti(t *testing.T) {
	t.Run("no node addresses", func(t *testing.T) {
		ctx := t.Context()

		mc, err := NewMulti(ctx, []string{}, ethcommon.Address{})

		require.Nil(t, mc, "MultiClient should be nil on error")
		require.Error(t, err, "expected an error due to no node addresses")
		require.Contains(t, err.Error(), "no node address provided")
	})

	t.Run("error creating single client", func(t *testing.T) {
		ctx := t.Context()
		addr := "invalid-addr"
		addresses := []string{addr}

		mc, err := NewMulti(ctx, addresses, ethcommon.Address{})

		require.Nil(t, mc, "MultiClient should be nil on error")
		require.Error(t, err)
		require.Contains(t, err.Error(), "create single client")
	})
}

func TestNewMulti_WithOptions(t *testing.T) {
	ctx := t.Context()

	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node().RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := httpToWebSocketURL(httpsrv.URL)

	addresses := []string{addr}
	contractAddr := ethcommon.HexToAddress("0x1234")

	customLogger := zap.NewExample()
	const customFollowDistance = uint64(10)
	const customTimeout = 100 * time.Millisecond
	const customReconnectionInterval = 10 * time.Millisecond
	const customReconnectionMaxInterval = 1 * time.Second
	const customHealthInvalidationInterval = 50 * time.Millisecond
	const customLogBatchSize = 11
	const customSyncDistanceTolerance = 12

	mc, err := NewMulti(
		ctx,
		addresses,
		contractAddr,
		WithLoggerMulti(customLogger),
		WithFollowDistanceMulti(customFollowDistance),
		WithConnectionTimeoutMulti(customTimeout),
		WithReconnectionInitialIntervalMulti(customReconnectionInterval),
		WithReconnectionMaxIntervalMulti(customReconnectionMaxInterval),
		WithHealthInvalidationIntervalMulti(customHealthInvalidationInterval),
		WithLogBatchSizeMulti(customLogBatchSize),
		WithSyncDistanceToleranceMulti(customSyncDistanceTolerance),
	)
	require.NoError(t, err)
	require.NotNil(t, mc)
	require.Equal(t, customLogger.Named("execution_client_multi"), mc.logger)
	require.EqualValues(t, customFollowDistance, mc.followDistance)
	require.EqualValues(t, customTimeout, mc.connectionTimeout)
	require.EqualValues(t, customReconnectionInterval, mc.reconnectionInitialInterval)
	require.EqualValues(t, customReconnectionMaxInterval, mc.reconnectionMaxInterval)
	require.EqualValues(t, customHealthInvalidationInterval, mc.healthInvalidationInterval)
	require.EqualValues(t, customLogBatchSize, mc.logBatchSize)
	require.EqualValues(t, customSyncDistanceTolerance, mc.syncDistanceTolerance)
}

func TestMultiClient_assertSameChainIDs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mc := &MultiClient{
		logger: zap.NewNop(),
	}

	expected, err := mc.assertSameChainID(big.NewInt(5))
	require.NoError(t, err)
	require.EqualValues(t, 5, expected.Uint64())
	expected, err = mc.assertSameChainID(big.NewInt(5))
	require.NoError(t, err)
	require.EqualValues(t, 5, expected.Uint64())

	chainID, err := mc.ChainID(t.Context())
	require.NoError(t, err)
	require.NotNil(t, chainID)
	require.Equal(t, int64(5), chainID.Int64())
}

func TestMultiClient_assertSameChainIDs_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mc := &MultiClient{
		logger: zap.NewNop(),
		closed: make(chan struct{}),
	}

	expected, err := mc.assertSameChainID(big.NewInt(5))
	require.NoError(t, err)
	require.EqualValues(t, 5, expected.Uint64())
	expected, err = mc.assertSameChainID(big.NewInt(6))
	require.Error(t, err)
	require.EqualValues(t, 5, expected.Uint64())

	chainID, err := mc.ChainID(t.Context())
	require.NoError(t, err)
	require.NotNil(t, chainID)
	require.Equal(t, int64(5), chainID.Int64())
}

func TestMultiClient_FetchHistoricalLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mockClient := NewMockSingleClientProvider(ctrl)

	logCh := make(chan BlockLogs, 1)
	errCh := make(chan error, 1)

	mockClient.
		EXPECT().
		FetchHistoricalLogs(gomock.Any(), uint64(100)).
		DoAndReturn(func(ctx context.Context, fromBlock uint64) (<-chan BlockLogs, <-chan error, error) {
			go func() {
				logCh <- BlockLogs{BlockNumber: 100}
				close(logCh)
				close(errCh)
			}()
			return logCh, errCh, nil
		}).
		Times(1)

	mockClient.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	mc := &MultiClient{
		nodeAddrs: []string{"mockaddr"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logs, errs, err := mc.FetchHistoricalLogs(ctx, 100)
	require.NoError(t, err)
	require.NotNil(t, logs)
	require.NotNil(t, errs)

	firstLog, ok1 := <-logs
	require.True(t, ok1, "expected to receive the first log from channel")
	require.Equal(t, uint64(100), firstLog.BlockNumber)

	_, open := <-logs
	require.False(t, open, "logs channel should be closed once done")

	errVal, openErr := <-errs
	require.False(t, openErr, "errors channel should be closed")
	require.Nil(t, errVal, "expected no errors")
}

func TestMultiClient_FetchHistoricalLogs_AllClientsNothingToSync(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		FetchHistoricalLogs(gomock.Any(), uint64(100)).
		Return((<-chan BlockLogs)(nil), (<-chan error)(nil), ErrNothingToSync).
		Times(1)

	mockClient2.
		EXPECT().
		FetchHistoricalLogs(gomock.Any(), uint64(100)).
		Return((<-chan BlockLogs)(nil), (<-chan error)(nil), ErrNothingToSync).
		Times(1)

	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		AnyTimes()

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		AnyTimes()

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logs, errs, err := mc.FetchHistoricalLogs(ctx, 100)
	require.Error(t, err)
	require.ErrorContains(t, err, ErrNothingToSync.Error())
	require.Nil(t, logs)
	require.Nil(t, errs)
}

func TestMultiClient_FetchHistoricalLogs_MixedErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		FetchHistoricalLogs(gomock.Any(), uint64(100)).
		Return((<-chan BlockLogs)(nil), (<-chan error)(nil), fmt.Errorf("unexpected error")).
		Times(1)

	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		AnyTimes()

	mockClient2.
		EXPECT().
		FetchHistoricalLogs(gomock.Any(), uint64(100)).
		Return((<-chan BlockLogs)(nil), (<-chan error)(nil), ErrNothingToSync).
		Times(1)

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		AnyTimes()

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logs, errs, err := mc.FetchHistoricalLogs(ctx, 100)
	require.Error(t, err)
	require.ErrorContains(t, err, "all clients failed")
	require.Nil(t, logs)
	require.Nil(t, errs)
}

func TestMultiClient_StreamLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
		DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
			out <- BlockLogs{BlockNumber: 200}
			out <- BlockLogs{BlockNumber: 201}
			return 202, nil // Success
		}).
		Times(1)

	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	mockClient2.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(202)). // fromBlock=202
		Return(uint64(202), nil).                                  // Should not be called
		Times(0)

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	hook := &fatalHook{}

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	var receivedLogs []BlockLogs
	var wg sync.WaitGroup
	wg.Add(2) // Expecting two logs: 200, 201
	go func() {
		for blk := range logsCh {
			receivedLogs = append(receivedLogs, blk)
			wg.Done()
		}
	}()
	wg.Wait()
	require.Len(t, receivedLogs, 2, "expected to receive two logs")
	require.Equal(t, uint64(200), receivedLogs[0].BlockNumber)
	require.Equal(t, uint64(201), receivedLogs[1].BlockNumber)

	_, open := <-logsCh
	require.False(t, open, "logs channel should be closed after all logs are received")

	// Make sure Fatal was not called since the first client succeeded
	require.False(t, hook.called, "did not expect Fatal to be called")
}

func TestMultiClient_StreamLogs_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mockClient := NewMockSingleClientProvider(ctrl)

	mockClient.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
		DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
			out <- BlockLogs{BlockNumber: 200}
			out <- BlockLogs{BlockNumber: 201}
			return 202, nil
		}).
		Times(1)

	mockClient.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	hook := &fatalHook{}

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	var receivedLogs []BlockLogs
	var wg sync.WaitGroup
	wg.Add(2) // Expecting two logs: 200, 201
	go func() {
		for blk := range logsCh {
			receivedLogs = append(receivedLogs, blk)
			wg.Done()
		}
	}()
	wg.Wait()
	require.Len(t, receivedLogs, 2, "expected to receive two logs")
	require.Equal(t, uint64(200), receivedLogs[0].BlockNumber)
	require.Equal(t, uint64(201), receivedLogs[1].BlockNumber)

	_, open := <-logsCh
	require.False(t, open, "logs channel should be closed after all logs are received")

	// Make sure Fatal was not called since the client succeeded
	require.False(t, hook.called, "did not expect Fatal to be called")
}

func TestMultiClient_StreamLogs_Interrupted(t *testing.T) {
	t.Run("ErrClosed", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		mockClient1 := NewMockSingleClientProvider(ctrl)
		mockClient2 := NewMockSingleClientProvider(ctrl)

		// Setup mockClient1 to return interrupt-error
		mockClient1.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				return 0, ErrClosed
			}).
			Times(1)

		mockClient1.
			EXPECT().
			Healthy(gomock.Any()).
			DoAndReturn(func(ctx context.Context) error {
				return nil
			}).
			AnyTimes()

		// Setup mockClient2 to not be called
		mockClient2.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), gomock.Any()).
			Times(0)

		mockClient2.
			EXPECT().
			Healthy(gomock.Any()).
			DoAndReturn(func(ctx context.Context) error {
				return nil
			}).
			AnyTimes()

		hook := &fatalHook{}

		mc := &MultiClient{
			nodeAddrs: []string{"mockNode1", "mockClient2"},
			clients:   []SingleClientProvider{mockClient1, mockClient2},
			clientsMu: make([]sync.Mutex, 2),
			logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
			closed:    make(chan struct{}),
		}

		logsCh := mc.StreamLogs(ctx, 200)
		// Make sure logsCh is closed
		for range logsCh {
			require.FailNow(t, "expected to receive no logs")
		}
		// Make sure Fatal was not called since the client succeeded
		require.False(t, hook.called, "did not expect Fatal to be called")
	})
	t.Run("rpc.ErrClientQuit", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		mockClient1 := NewMockSingleClientProvider(ctrl)
		mockClient2 := NewMockSingleClientProvider(ctrl)

		// Setup mockClient1 to return interrupt-error
		mockClient1.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				return 0, rpc.ErrClientQuit
			}).
			Times(1)

		mockClient1.
			EXPECT().
			Healthy(gomock.Any()).
			DoAndReturn(func(ctx context.Context) error {
				return nil
			}).
			AnyTimes()

		// Setup mockClient2 to not be called
		mockClient2.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), gomock.Any()).
			Times(0)

		mockClient2.
			EXPECT().
			Healthy(gomock.Any()).
			DoAndReturn(func(ctx context.Context) error {
				return nil
			}).
			AnyTimes()

		hook := &fatalHook{}

		mc := &MultiClient{
			nodeAddrs: []string{"mockNode1", "mockClient2"},
			clients:   []SingleClientProvider{mockClient1, mockClient2},
			clientsMu: make([]sync.Mutex, 2),
			logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
			closed:    make(chan struct{}),
		}

		logsCh := mc.StreamLogs(ctx, 200)
		// Make sure logsCh is closed
		for range logsCh {
			require.FailNow(t, "expected to receive no logs")
		}
		// Make sure Fatal was not called since the client succeeded
		require.False(t, hook.called, "did not expect Fatal to be called")
	})
	t.Run("context.Canceled", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		mockClient1 := NewMockSingleClientProvider(ctrl)
		mockClient2 := NewMockSingleClientProvider(ctrl)

		// Setup mockClient1 to return interrupt-error
		mockClient1.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				return 0, context.Canceled
			}).
			Times(1)

		mockClient1.
			EXPECT().
			Healthy(gomock.Any()).
			DoAndReturn(func(ctx context.Context) error {
				return nil
			}).
			AnyTimes()

		// Setup mockClient2 to not be called
		mockClient2.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), gomock.Any()).
			Times(0)

		mockClient2.
			EXPECT().
			Healthy(gomock.Any()).
			DoAndReturn(func(ctx context.Context) error {
				return nil
			}).
			AnyTimes()

		hook := &fatalHook{}

		mc := &MultiClient{
			nodeAddrs: []string{"mockNode1", "mockClient2"},
			clients:   []SingleClientProvider{mockClient1, mockClient2},
			clientsMu: make([]sync.Mutex, 2),
			logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
			closed:    make(chan struct{}),
		}

		logsCh := mc.StreamLogs(ctx, 200)
		// Make sure logsCh is closed
		for range logsCh {
			require.FailNow(t, "expected to receive no logs")
		}
		// Make sure Fatal was not called since the client succeeded
		require.False(t, hook.called, "did not expect Fatal to be called")
	})
}

func TestMultiClient_StreamLogs_Failover(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	gomock.InOrder(
		// First client: mockClient1 with fromBlock=200
		mockClient1.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				out <- BlockLogs{BlockNumber: 200}
				out <- BlockLogs{BlockNumber: 201}
				return 202, errors.New("network error") // Triggers failover
			}).
			Times(1),

		// Second client: mockClient2 with fromBlock=202
		mockClient2.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(202)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				out <- BlockLogs{BlockNumber: 202}
				return 203, nil
			}).
			Times(1),
	)

	hook := &fatalHook{}

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockClient2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	var receivedLogs []BlockLogs
	var wg sync.WaitGroup
	wg.Add(3) // Expecting three logs: 200, 201, 202
	go func() {
		for blk := range logsCh {
			receivedLogs = append(receivedLogs, blk)
			wg.Done()
		}
	}()
	wg.Wait()

	require.Len(t, receivedLogs, 3, "expected to receive three logs")
	require.Equal(t, uint64(200), receivedLogs[0].BlockNumber)
	require.Equal(t, uint64(201), receivedLogs[1].BlockNumber)
	require.Equal(t, uint64(202), receivedLogs[2].BlockNumber)

	_, open := <-logsCh
	require.False(t, open, "logs channel should be closed after all logs are received")

	// Make sure Fatal was not called since the client succeeded
	require.False(t, hook.called, "did not expect Fatal to be called")
}

func TestMultiClient_StreamLogs_SameFromBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	// Setup mockClient1 to return lastBlock=200 without errors
	mockClient1.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
		DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
			out <- BlockLogs{BlockNumber: 200}
			return 200, nil
		}).
		Times(1)

	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	// Setup mockClient2 to not be called
	mockClient2.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	blk1, ok1 := <-logsCh
	require.True(t, ok1, "expected to receive the first log from channel")
	require.Equal(t, uint64(200), blk1.BlockNumber)

	_, open := <-logsCh
	require.False(t, open, "logs channel should be closed after all logs are received")
}

func TestMultiClient_StreamLogs_MultipleFailoverAttempts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)
	mockClient3 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	mockClient3.
		EXPECT().
		Healthy(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			return nil
		}).
		AnyTimes()

	gomock.InOrder(
		// Setup mockClient1 to fail with fromBlock=200
		mockClient1.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			Return(uint64(200), errors.New("network error")).
			Times(1),

		// Setup mockClient2 to fail with fromBlock=200
		mockClient2.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			Return(uint64(200), errors.New("network error")).
			Times(1),

		// Setup mockClient3 to handle fromBlock=200
		mockClient3.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				out <- BlockLogs{BlockNumber: 200}
				return 201, nil
			}).
			Times(1),
	)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2", "mockNode3"},
		clients:   []SingleClientProvider{mockClient1, mockClient2, mockClient3},
		clientsMu: make([]sync.Mutex, 3),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	blk1, ok1 := <-logsCh
	require.True(t, ok1, "expected to receive the first log from channel")
	require.Equal(t, uint64(200), blk1.BlockNumber)

	_, open := <-logsCh
	require.False(t, open, "logs channel should be closed after all logs are received")
}

func TestMultiClient_Healthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)

	mockClient.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	err := mc.Healthy(t.Context())
	require.NoError(t, err)
}

func TestMultiClient_Healthy_MultiClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	// Setup only one client to be healthy to iterate all of them
	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		Return(fmt.Errorf("not healthy")).
		Times(1)

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	err := mc.Healthy(t.Context())
	require.NoError(t, err, "expected all clients to be healthy")
}

func TestMultiClient_Healthy_AllClientsUnhealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		Return(fmt.Errorf("client1 unhealthy")).
		Times(1)

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		Return(fmt.Errorf("client2 unhealthy")).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	err := mc.Healthy(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "client1 unhealthy")
	require.Contains(t, err.Error(), "client2 unhealthy")
}

func TestMultiClient_HeaderByNumber(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)

	mockClient.
		EXPECT().
		HeaderByNumber(gomock.Any(), big.NewInt(1234)).
		Return(&ethtypes.Header{}, nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	blk, err := mc.HeaderByNumber(t.Context(), big.NewInt(1234))
	require.NoError(t, err)
	require.NotNil(t, blk)
}

func TestMultiClient_HeaderByNumber_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)

	mockClient.
		EXPECT().
		HeaderByNumber(gomock.Any(), big.NewInt(1234)).
		Return((*ethtypes.Header)(nil), fmt.Errorf("header not found")).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	blk, err := mc.HeaderByNumber(t.Context(), big.NewInt(1234))
	require.Error(t, err)
	require.Nil(t, blk)
	require.Contains(t, err.Error(), "header not found")
}

func TestMultiClient_SubscribeFilterLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)

	sub := &dummySubscription{}

	query := ethereum.FilterQuery{
		Addresses: []ethcommon.Address{ethcommon.HexToAddress("0x1234")},
		FromBlock: big.NewInt(100),
		ToBlock:   big.NewInt(200),
	}

	logCh := make(chan ethtypes.Log)

	mockClient.
		EXPECT().
		SubscribeFilterLogs(gomock.Any(), query, logCh).
		Return(sub, nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	subscription, err := mc.SubscribeFilterLogs(t.Context(), query, logCh)
	require.NoError(t, err)
	require.Equal(t, sub, subscription)
}

func TestMultiClient_SubscribeFilterLogs_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)

	query := ethereum.FilterQuery{
		Addresses: []ethcommon.Address{ethcommon.HexToAddress("0x1234")},
		FromBlock: big.NewInt(100),
		ToBlock:   big.NewInt(200),
	}

	logCh := make(chan ethtypes.Log)

	mockClient.
		EXPECT().
		SubscribeFilterLogs(gomock.Any(), query, logCh).
		Return((ethereum.Subscription)(nil), fmt.Errorf("subscription error")).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	subscription, err := mc.SubscribeFilterLogs(t.Context(), query, logCh)
	require.Error(t, err)
	require.Nil(t, subscription)
	require.Contains(t, err.Error(), "subscription error")
}

func TestMultiClient_FilterLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)
	query := ethereum.FilterQuery{Addresses: []ethcommon.Address{ethcommon.HexToAddress("0x1234")}}

	expectedLogs := []ethtypes.Log{
		{Address: ethcommon.HexToAddress("0x1234")},
	}

	mockClient.
		EXPECT().
		FilterLogs(gomock.Any(), query).
		Return(expectedLogs, nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logs, err := mc.FilterLogs(t.Context(), query)
	require.NoError(t, err)
	require.Equal(t, expectedLogs, logs)
}

func TestMultiClient_FilterLogs_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)
	query := ethereum.FilterQuery{Addresses: []ethcommon.Address{ethcommon.HexToAddress("0x1234")}}

	mockClient.
		EXPECT().
		FilterLogs(gomock.Any(), query).
		Return(nil, fmt.Errorf("filtering error")).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logs, err := mc.FilterLogs(t.Context(), query)
	require.Error(t, err)
	require.Nil(t, logs)
	require.Contains(t, err.Error(), "filtering error")
}

func TestMultiClient_Filterer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mc := &MultiClient{
		contractAddress: ethcommon.HexToAddress("0x1234"),
		logger:          zap.NewNop(),
		closed:          make(chan struct{}),
	}

	filterer, err := mc.Filterer()
	require.NoError(t, err)
	require.NotNil(t, filterer)
}

func TestMultiClient_Filterer_Integration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	contractAddr := ethcommon.HexToAddress("0x1234")
	mc := &MultiClient{
		contractAddress: contractAddr,
		logger:          zap.NewNop(),
		closed:          make(chan struct{}),
	}

	filterer, err := mc.Filterer()
	require.NoError(t, err)
	require.NotNil(t, filterer)

	const event = `{
			"address": "0x4b133c68a084b8a88f72edcd7944b69c8d545f03",
			"topics": [
			  "0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5",
			  "0x00000000000000000000000077fc6e8b24a623725d935bc88057098d0bca6eb3"
			],
			"data": "0x000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000001a0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000030000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000017c4071580000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000532d024bb0158000000000000000000000000000000000000000000000000000000000000000000400000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000300000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000030b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000520823f86dfcae5771e9b847c07ed6dda49211274db079f3397b3b06ab7291cebd688f63cb5648ca535c4ec4f9e2b89a48515a035dc24f4738ada942b72d32f75a7e72ac8a8c39d73a6fd633d0f58cadc4618dc70c8160cab5573d541c88a6aebada0acbbeacd49f2931c8c0c548d1b0f69cd468c803ec3fe06bddf08186ae3b64e0b5f1762feb141a06e71c828cedd3c878a08a40fd84d3a0449308c458fd324e67eb6df89e28bf6c304a1e71dcb8c9823b85c2dcca82a980cffb994da194e70b487d02db1ff328b6a8d5f6b519ffc1b524753ce8ed56d4ec1a3cdddc3b24f427b22caa351a1fa0d9f523bd279756ec38080c4850691c1f520dee49a9f4267c0a7a53c7818d165681c9a615a391ee5e4cc9c0e51725c1a92f7e92a393afe4f8f3f35503f241288822a1d721f5c164ea7f85d2b43b638678593d670e79c17a0e1398ac6bbd3ef7ccbdf67e38f6d058ffc5319280868c9a44529b1a4ea7f73792680e67693c502ad9053935edc312c48a94a0d18c71c18af8eb46ae8979d30f176969063430c14ef18a51b3dca4f8775551f4e1fc6a651ee909fc4f7b872c041514a01b4d88b972be86960e3472419ef1577c92af61d572e4e07b32bb38a0e52a1c75f03e1cee80d053b97e3e238c022521c48c6c1dc0f0def8fa0472d7669095b0e9304e63af8b5a9928d9fcf4de166029d88891d10ae6abafe150cc6e9aa6464d76064b16a19b09dad4556dffa580d14cd6755fa2274022abbc917eb7a50f296a153781742c2f101cf280b7f095bf443d51be4dcdd114804fb40ba496c16a3c3a3e82d8645833e51c22cebb78ce4b18b6b9eb3b480f5478b3ed97b5a93b705f41d05ed8423f424c5d05317c4e6e53e954570a46027361f7f3f18a581860720dac25afc00f4378a35439fd860ccbc0f0586ae6cf44d53f828faf77c1949bfe58c428de7263d48b1f7ecbb24a25c6abc11aa6105fe41a9a1f608c6895d808e8ca805efd306ec8774201a63e7d9220e031c5e8abdde49f5d56590637a5234b4b35a20875d5e0a5b06c2834dae47dd50633c371ef1071ea3d79a8ce727c2e83d3fae85a596112404875e847c743ddde50bc13b5d661f558e4d02f29b972188418d2f875d0603abbe9ab5c1fc19ae80636d9aa3a6c80be21b2970b84aa4659244424f943b3a99c8ec73304bcc8fc51519f1655ad6f75954af3cb238e946ac50aa365fd6538a7190b6e64b320f822a0010e92e1e4f3d773d25c4e29b3d590e75b4ebfecd6c059df2060f44344dda27f2f794aeb3dfcde62c7b24b80ff95ff1246d05805a12028d9840316f6b8368b60d2a824cc14b02d25a46b689e4519dd9963b5786ff9fa0c695fbdff455499a8f6cc88261b498e90223c0abca38dfe188eef0ac4680f6ac172fdf6b4a343cb1f090a8ce427ef4c745a2408f9ffe67c5b8eb17e7cc2e5851db5f5c75c0658afea00dc1552caf7ef745d2e5e057ccd3b177de22d989fe97bcac17471d0e8ee330a6d9788c6927b1991e784ec61deef91afad21895718e3fa751a782cf66c5911e3f2148dffb01e7e09dcbce8053e060df505f1121202017b34010ddbf02e63b40b8e88a73ac75eb239c401f136b255aded2201de167c9b6ee140de2d307712c8db8e958c5bab3de27d6a40e0b1211ccb634ca9204ce1bda71064f3bee1546f97979c9ec07cd5b4cb5befd7cf8ac930ad74111f381c72d18d3cf1aefef073630cc7bfef722650023032d6493fea494b3b01c95790b08609c9c039a1849fbe47042a29e98ce92f87641647db7d610357c087de95218ea357284828925c21ff7685f01f1b0e5ebadef7d1e763c64ee06d29f4ded10075d39",
			"blockNumber": "0x89EBFF",
			"transactionHash": "0x921a3f836fb873a40aa4f83097e52b69225334c49674dc262b2bb90d27e3a801"
		}`

	var log ethtypes.Log
	require.NoError(t, json.Unmarshal([]byte(event), &log))

	logs, err := filterer.ParseValidatorAdded(log)
	require.NoError(t, err)
	require.NotNil(t, logs)
}

func TestMultiClient_ChainID(t *testing.T) {
	mc := &MultiClient{
		logger: zap.NewNop(),
		closed: make(chan struct{}),
	}
	mc.chainID.Store(big.NewInt(5))

	cid, err := mc.ChainID(t.Context())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(5), cid)
}

func TestMultiClient_ChainID_NotSet(t *testing.T) {
	mc := &MultiClient{
		logger: zap.NewNop(),
		closed: make(chan struct{}),
	}

	cid, err := mc.ChainID(t.Context())
	require.NoError(t, err)
	require.Nil(t, cid, "expected ChainID to be nil when not set")
}

func TestMultiClient_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		Close().
		Return(nil).
		Times(1)

	mockClient2.
		EXPECT().
		Close().
		Return(errors.New("close error")).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1", "mock2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	err := mc.Close()
	// Should combine errors if multiple close calls fail.
	require.Error(t, err)
	require.Contains(t, err.Error(), "close error")
}

func TestMultiClient_Close_MultiClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)
	mockClient3 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		Close().
		Return(nil).
		Times(1)

	mockClient2.
		EXPECT().
		Close().
		Return(errors.New("close error")).
		Times(1)

	mockClient3.
		EXPECT().
		Close().
		Return(nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2", "mockNode3"},
		clients:   []SingleClientProvider{mockClient1, mockClient2, mockClient3},
		clientsMu: make([]sync.Mutex, 3),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	err := mc.Close()
	// Should combine errors if multiple close calls fail.
	require.Error(t, err)
	require.Contains(t, err.Error(), "close error")
}

func TestMultiClient_Call_Concurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)

	mockClient.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		AnyTimes()

	mockClient.
		EXPECT().
		HeaderByNumber(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, num *big.Int) (*ethtypes.Header, error) {
			return &ethtypes.Header{}, nil
		}).
		Times(10)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		clientsMu: make([]sync.Mutex, 1),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(10)

	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_, err := mc.HeaderByNumber(t.Context(), big.NewInt(1234))
			require.NoError(t, err)
		}()
	}

	wg.Wait()
}

func TestMultiClient_Call_AllClientsFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		Times(1)

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		Times(1)

	mockClient1.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(uint64(0), fmt.Errorf("streaming error")).
		Times(1)

	mockClient2.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(uint64(201), fmt.Errorf("another streaming error")).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		clientsMu: make([]sync.Mutex, 2),
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	// Define a simple function to simulate a call
	f := func(client SingleClientProvider) (any, error) {
		return client.streamLogsToChan(context.TODO(), nil, 200)
	}

	_, err := mc.call(t.Context(), f, len(mc.clients))
	require.Error(t, err)
	require.Contains(t, err.Error(), "all clients failed")
}

type fatalHook struct {
	mu     sync.Mutex
	called bool
}

func (hook *fatalHook) OnWrite(*zapcore.CheckedEntry, []zapcore.Field) {
	hook.mu.Lock()
	defer hook.mu.Unlock()
	hook.called = true
}

// dummySubscription is a stub implementing ethereum.Subscription.
type dummySubscription struct{}

func (d *dummySubscription) Unsubscribe() {}
func (d *dummySubscription) Err() <-chan error {
	return make(chan error)
}
