package executionclient

import (
	"context"
	"errors"
	"math/big"
	"testing"

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
		Return((<-chan BlockLogs)(logCh), (<-chan error)(errCh), nil).
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

	go func() {
		logCh <- BlockLogs{BlockNumber: 100}
		close(logCh)
		close(errCh)
	}()

	firstLog := <-logs
	require.Equal(t, uint64(100), firstLog.BlockNumber)

	_, open := <-logs
	require.False(t, open, "logs channel should be closed once done")

	errVal, open := <-errs
	require.False(t, open)
	require.Nil(t, errVal)
}

type fatalHook struct {
	called bool
}

func (hook *fatalHook) OnWrite(*zapcore.CheckedEntry, []zapcore.Field) {
	hook.called = true
}

func TestMultiClient_StreamLogs(t *testing.T) {
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
			// Return lastBlock=201 + an error to trigger reconnect
			return 201, errors.New("network error")
		}).
		Times(1)

	// TODO: uncomment when reconnect is implemented
	//mockClient.
	//	EXPECT().
	//	reconnect(gomock.Any()).
	//	Times(1)

	mockClient.
		EXPECT().
		streamLogsToChan(gomock.Any(), gomock.Any(), uint64(202)).
		DoAndReturn(func(_ context.Context, out chan<- BlockLogs, fromBlock uint64) (uint64, error) {
			// Return gracefully
			return 202, ErrClosed
		}).
		Times(1)

	hook := &fatalHook{}
	mc := &MultiClient{
		nodeAddrs: []string{"mockNode"},
		clients:   []SingleClientProvider{mockClient},
		logger:    zap.NewNop().WithOptions(zap.WithFatalHook(hook)),
		closed:    make(chan struct{}),
	}

	logsCh := mc.StreamLogs(ctx, 200)

	blk1 := <-logsCh
	require.Equal(t, uint64(200), blk1.BlockNumber)

	blk2 := <-logsCh
	require.Equal(t, uint64(201), blk2.BlockNumber)

	// Next read should end quickly once the second call returns ErrClosed
	_, more := <-logsCh
	require.False(t, more, "channel should be closed after ErrClosed from streamLogsToChan")

	require.True(t, hook.called)
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

// dummySubscription is a stub implementing ethereum.Subscription.
type dummySubscription struct{}

func (d *dummySubscription) Unsubscribe() {}
func (d *dummySubscription) Err() <-chan error {
	return make(chan error)
}
