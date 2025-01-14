package executionclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNewMulti(t *testing.T) {
	t.Run("no node addresses", func(t *testing.T) {
		ctx := context.Background()

		mc, err := NewMulti(ctx, []string{}, ethcommon.Address{})

		require.Nil(t, mc, "MultiClient should be nil on error")
		require.Error(t, err, "expected an error due to no node addresses")
		require.Contains(t, err.Error(), "no node address provided")
	})

	t.Run("error creating single client", func(t *testing.T) {
		ctx := context.Background()
		addr := "invalid-addr"
		addresses := []string{addr}

		mc, err := NewMulti(ctx, addresses, ethcommon.Address{})

		require.Nil(t, mc, "MultiClient should be nil on error")
		require.Error(t, err)
		require.Contains(t, err.Error(), "create single client")
	})

	t.Run("chain ID mismatch", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient1 := NewMockSingleClientProvider(ctrl)
		mockClient2 := NewMockSingleClientProvider(ctrl)

		mockClient1.
			EXPECT().
			ChainID(gomock.Any()).
			Return(big.NewInt(1), nil).
			AnyTimes()

		mockClient2.
			EXPECT().
			ChainID(gomock.Any()).
			Return(big.NewInt(2), nil).
			AnyTimes()

		mc := &MultiClient{
			nodeAddrs: []string{"mock1", "mock2"},
			clients:   []SingleClientProvider{mockClient1, mockClient2},
			logger:    zap.NewNop(),
		}

		same, err := mc.assertSameChainIDs(context.Background())

		require.NoError(t, err)
		require.False(t, same)
	})
}

func TestMultiClient_assertSameChainIDs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		ChainID(gomock.Any()).
		Return(big.NewInt(5), nil).
		Times(1)

	mockClient2.
		EXPECT().
		ChainID(gomock.Any()).
		Return(big.NewInt(5), nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1", "mock2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		logger:    zap.NewNop(),
	}

	same, err := mc.assertSameChainIDs(context.Background())
	require.NoError(t, err)
	require.True(t, same, "expected chain IDs to match")

	chainID, err := mc.ChainID(context.Background())
	require.NoError(t, err)
	require.NotNil(t, chainID)
	require.Equal(t, int64(5), chainID.Int64())
}

func TestMultiClient_FetchHistoricalLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
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

	mc := &MultiClient{
		nodeAddrs: []string{"mockaddr"},
		clients:   []SingleClientProvider{mockClient},
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

func TestMultiClient_FetchHistoricalLogs_MultiClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		FetchHistoricalLogs(gomock.Any(), uint64(100)).
		Return((<-chan BlockLogs)(nil), (<-chan error)(nil), errors.New("fetch error")).
		Times(1)

	mockClient1.
		EXPECT().
		reconnect(gomock.Any()).
		Do(func(ctx context.Context) {}).
		Times(1)

	client2Ready := make(chan struct{})

	logCh := make(chan BlockLogs, 1)
	errCh := make(chan error, 1)

	mockClient2.
		EXPECT().
		FetchHistoricalLogs(gomock.Any(), uint64(100)).
		DoAndReturn(func(ctx context.Context, fromBlock uint64) (<-chan BlockLogs, <-chan error, error) {
			close(client2Ready)
			return logCh, errCh, nil
		}).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logs, errs, err := mc.FetchHistoricalLogs(ctx, 100)
	require.NoError(t, err)
	require.NotNil(t, logs)
	require.NotNil(t, errs)

	<-client2Ready

	logCh <- BlockLogs{BlockNumber: 100}
	logCh <- BlockLogs{BlockNumber: 101}
	close(logCh)
	close(errCh)

	select {
	case blk1, ok1 := <-logs:
		require.True(t, ok1, "expected to receive the first log from channel")
		require.Equal(t, uint64(100), blk1.BlockNumber)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected to receive the first log from channel")
	}

	select {
	case blk2, ok2 := <-logs:
		require.True(t, ok2, "expected to receive the second log from channel")
		require.Equal(t, uint64(101), blk2.BlockNumber)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected to receive the second log from channel")
	}

	_, open := <-logs
	require.False(t, open, "logs channel should be closed after all logs are received")

	errVal, openErr := <-errs
	require.False(t, openErr, "errors channel should be closed")
	require.Nil(t, errVal, "expected no errors")
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

func TestMultiClient_StreamLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	mockClient1.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
		DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
			out <- BlockLogs{BlockNumber: 200}
			out <- BlockLogs{BlockNumber: 201}
			return 201, nil // Success
		}).
		Times(1)

	mockClient2.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(202)). // fromBlock=202
		Return(uint64(202), nil).                                  // Should not be called
		Times(0)

	hook := &fatalHook{}

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	var wg sync.WaitGroup
	wg.Add(2) // Expecting two logs: 200, 201

	var receivedLogs []BlockLogs
	var mu sync.Mutex

	go func() {
		for blk := range logsCh {
			mu.Lock()
			receivedLogs = append(receivedLogs, blk)
			mu.Unlock()
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient := NewMockSingleClientProvider(ctrl)

	mockClient.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
		DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
			out <- BlockLogs{BlockNumber: 200}
			out <- BlockLogs{BlockNumber: 201}
			return 201, nil
		}).
		Times(1)

	hook := &fatalHook{}

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1"},
		clients:   []SingleClientProvider{mockClient},
		logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	var wg sync.WaitGroup
	wg.Add(2) // Expecting two logs: 200, 201

	var receivedLogs []BlockLogs
	var mu sync.Mutex

	go func() {
		for blk := range logsCh {
			mu.Lock()
			receivedLogs = append(receivedLogs, blk)
			mu.Unlock()
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

func TestMultiClient_StreamLogs_Failover(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	gomock.InOrder(
		// First client: mockClient1 with fromBlock=200
		mockClient1.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				out <- BlockLogs{BlockNumber: 200}
				out <- BlockLogs{BlockNumber: 201}
				return 201, errors.New("network error") // Triggers failover
			}).
			Times(1),

		mockClient1.
			EXPECT().
			reconnect(gomock.Any()).
			Do(func(ctx context.Context) {}).
			Times(1),

		// Second client: mockClient2 with fromBlock=202
		mockClient2.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(202)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				out <- BlockLogs{BlockNumber: 202}
				return 202, ErrClosed // Reference exported ErrClosed
			}).
			Times(1),
	)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockClient2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	var wg sync.WaitGroup
	wg.Add(3) // Expecting three logs: 200, 201, 202

	var receivedLogs []BlockLogs
	var mu sync.Mutex

	go func() {
		for blk := range logsCh {
			mu.Lock()
			receivedLogs = append(receivedLogs, blk)
			mu.Unlock()
			wg.Done()
		}
	}()

	wg.Wait()

	require.Len(t, receivedLogs, 3, "expected to receive three logs")
	require.Equal(t, uint64(200), receivedLogs[0].BlockNumber)
	require.Equal(t, uint64(201), receivedLogs[1].BlockNumber)
	require.Equal(t, uint64(202), receivedLogs[2].BlockNumber)
}

func TestMultiClient_StreamLogs_AllClientsFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)

	// Setup both clients to fail
	mockClient1.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
		DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
			out <- BlockLogs{BlockNumber: 200}
			return 200, errors.New("network error") // Triggers failover
		}).
		Times(1)

	mockClient1.
		EXPECT().
		reconnect(gomock.Any()).
		Do(func(ctx context.Context) {}).
		Times(1)

	mockClient2.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(201)). // Updated fromBlock to 201
		DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
			out <- BlockLogs{BlockNumber: 201}
			return 201, errors.New("network error") // All clients failed
		}).
		Times(1)

	mockClient2.
		EXPECT().
		reconnect(gomock.Any()).
		Do(func(ctx context.Context) {}).
		Times(1)

	hook := &fatalHook{}

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	var wg sync.WaitGroup
	wg.Add(2) // Expecting two logs: 200, 201

	var receivedLogs []BlockLogs
	var mu sync.Mutex

	go func() {
		for blk := range logsCh {
			mu.Lock()
			receivedLogs = append(receivedLogs, blk)
			mu.Unlock()
			wg.Done()
		}
	}()

	wg.Wait()

	require.Len(t, receivedLogs, 2, "expected to receive two logs")
	require.Equal(t, uint64(200), receivedLogs[0].BlockNumber)
	require.Equal(t, uint64(201), receivedLogs[1].BlockNumber)

	_, open := <-logsCh
	require.False(t, open, "logs channel should be closed after all logs are received")

	// Make sure Fatal was called due to all clients failing
	require.True(t, hook.called, "expected Fatal to be called due to all clients failing")
}

func TestMultiClient_StreamLogs_SameFromBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
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

	// Setup mockClient2 to not be called
	mockClient2.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient1 := NewMockSingleClientProvider(ctrl)
	mockClient2 := NewMockSingleClientProvider(ctrl)
	mockClient3 := NewMockSingleClientProvider(ctrl)

	gomock.InOrder(
		// Setup mockClient1 to fail with fromBlock=200
		mockClient1.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			Return(uint64(0), errors.New("network error")).
			Times(1),

		mockClient1.
			EXPECT().
			reconnect(gomock.Any()).
			Do(func(ctx context.Context) {}).
			Times(1),

		// Setup mockClient2 to fail with fromBlock=200
		mockClient2.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			Return(uint64(0), errors.New("network error")).
			Times(1),

		mockClient2.
			EXPECT().
			reconnect(gomock.Any()).
			Do(func(ctx context.Context) {}).
			Times(1),

		// Setup mockClient3 to handle fromBlock=200
		mockClient3.
			EXPECT().
			streamLogsToChan(gomock.Any(), gomock.Any(), uint64(200)).
			DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
				out <- BlockLogs{BlockNumber: 200}
				return 200, nil
			}).
			Times(1),
	)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2", "mockNode3"},
		clients:   []SingleClientProvider{mockClient1, mockClient2, mockClient3},
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
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	err := mc.Healthy(context.Background())
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

	mockClient1.
		EXPECT().
		reconnect(gomock.Any()).
		Do(func(ctx context.Context) {}).
		Times(1)

	mockClient2.
		EXPECT().
		Healthy(gomock.Any()).
		Return(nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mockNode1", "mockNode2"},
		clients:   []SingleClientProvider{mockClient1, mockClient2},
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	err := mc.Healthy(context.Background())
	require.NoError(t, err, "expected all clients to be healthy")
}

func TestMultiClient_BlockByNumber(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := NewMockSingleClientProvider(ctrl)

	mockClient.
		EXPECT().
		BlockByNumber(gomock.Any(), big.NewInt(1234)).
		Return(&ethtypes.Block{}, nil).
		Times(1)

	mc := &MultiClient{
		nodeAddrs: []string{"mock1"},
		clients:   []SingleClientProvider{mockClient},
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	blk, err := mc.BlockByNumber(context.Background(), big.NewInt(1234))
	require.NoError(t, err)
	require.NotNil(t, blk)
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
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	subscription, err := mc.SubscribeFilterLogs(context.Background(), query, logCh)
	require.NoError(t, err)
	require.Equal(t, sub, subscription)
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
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	logs, err := mc.FilterLogs(context.Background(), query)
	require.NoError(t, err)
	require.Equal(t, expectedLogs, logs)
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

func TestMultiClient_ChainID(t *testing.T) {
	mc := &MultiClient{
		chainID: big.NewInt(5),
		logger:  zap.NewNop(),
		closed:  make(chan struct{}),
	}

	cid, err := mc.ChainID(context.Background())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(5), cid)
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

	// Setup clients to close successfully or with an error
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
		logger:    zap.NewNop(),
		closed:    make(chan struct{}),
	}

	err := mc.Close()
	// Should combine errors if multiple close calls fail.
	require.Error(t, err)
	require.Contains(t, err.Error(), "close error")
}

// dummySubscription is a stub implementing ethereum.Subscription.
type dummySubscription struct{}

func (d *dummySubscription) Unsubscribe() {}
func (d *dummySubscription) Err() <-chan error {
	return make(chan error)
}
