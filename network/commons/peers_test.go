package commons

import (
	"context"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"sync"
	"testing"
	"time"
)

func TestWaitForMinPeers(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)
	net := newNetDiscMock(1 * time.Millisecond)
	wctx := WaitMinPeersCtx{
		Ctx:    ctx,
		Net:    net,
		Logger: logger,
	}
	pk1 := []byte("xxx")
	pk2 := []byte("xxx2")
	net.mut.Lock()
	net.peers[string(pk1)] = []string{"aaa", "bbb", "ccc"}
	net.peers[string(pk2)] = []string{"aaa"}
	net.mut.Unlock()

	t.Run("enough peers", func(t *testing.T) {
		err := WaitForMinPeers(wctx, pk1, 2, 1*time.Millisecond, 3*time.Millisecond, false)
		require.NoError(t, err)
		net.mut.Lock()
		defer net.mut.Unlock()
		require.Equal(t, 0, len(net.operatorsPubKeys))
	})

	t.Run("not enough peers", func(t *testing.T) {
		go func() {
			// push more peers
			time.Sleep(5 * time.Millisecond)
			net.mut.Lock()
			net.peers[string(pk2)] = []string{"aaa", "bbb", "ccc"}
			net.mut.Unlock()
		}()
		wctx.Operators = [][]byte{
			[]byte("op1"), []byte("op2"), []byte("op3"),
		}
		err := WaitForMinPeers(wctx, pk2, 2, 2*time.Millisecond, 8*time.Millisecond, false)
		require.NoError(t, err)
		net.mut.Lock()
		defer net.mut.Unlock()
		require.True(t, len(net.operatorsPubKeys) >= 3)
	})

}

type netDiscMock struct {
	mut              sync.Mutex
	findTimeOut      time.Duration
	peers            map[string][]string
	operatorsPubKeys [][]byte
}

func newNetDiscMock(findTimeOut time.Duration) *netDiscMock {
	return &netDiscMock{
		mut:              sync.Mutex{},
		findTimeOut:      findTimeOut,
		peers:            map[string][]string{},
		operatorsPubKeys: [][]byte{},
	}
}

func (nd *netDiscMock) FindPeers(ctx context.Context, operatorsPubKeys ...[]byte) {
	nd.mut.Lock()
	nd.operatorsPubKeys = append(nd.operatorsPubKeys, operatorsPubKeys...)
	nd.mut.Unlock()
	time.Sleep(nd.findTimeOut)
}

func (nd *netDiscMock) AllPeers(validatorPk []byte) ([]string, error) {
	nd.mut.Lock()
	defer nd.mut.Unlock()

	return nd.peers[string(validatorPk)], nil
}
