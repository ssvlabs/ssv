package streams

import (
	"context"
	"github.com/bloxapp/ssv/network/p2p/discovery"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/libp2p/go-libp2p"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"
)

func TestSyncStream(t *testing.T) {
	hosts := testHosts(t, 3)

	timeout := time.Second

	var wg sync.WaitGroup
	prot := protocol.ID("/protocol")

	hosts[1].SetStreamHandler(prot, func(stream core.Stream) {
		defer wg.Done()
		s := NewTimeoutStream(stream)
		defer s.Close()
		<-time.After(timeout * 3)
		require.Error(t, s.WriteWithTimeout([]byte("xxx"), timeout))
	})

	hosts[2].SetStreamHandler(prot, func(stream core.Stream) {
		defer wg.Done()
		s := NewTimeoutStream(stream)
		defer s.Close()
		require.NoError(t, s.WriteWithTimeout([]byte("xxx"), timeout))
	})

	t.Run("with timeout", func(t *testing.T) {
		wg.Add(1)
		s, err := hosts[0].NewStream(context.Background(), hosts[1].ID(), prot)
		require.NoError(t, err)
		strm := NewTimeoutStream(s)
		byts, err := strm.ReadWithTimeout(timeout)
		require.EqualError(t, err, "i/o deadline reached")
		require.Len(t, byts, 0)
	})

	t.Run("no timeout", func(t *testing.T) {
		wg.Add(1)
		s, err := hosts[0].NewStream(context.Background(), hosts[2].ID(), prot)
		require.NoError(t, err)
		strm := NewTimeoutStream(s)
		byts, err := strm.ReadWithTimeout(timeout)
		require.NoError(t, err)
		require.Len(t, byts, 3)
	})

	wg.Wait()
}

func TestSyncStream_ReadWithoutTimeout(t *testing.T) {
	hosts := testHosts(t, 2)

	prot := protocol.ID("/protocol")
	readByts := threadsafe.Bool()
	hosts[1].SetStreamHandler(prot, func(stream core.Stream) {
		s := NewTimeoutStream(stream)

		// read msg
		buf, err := s.ReadWithTimeout(time.Millisecond * 100)
		require.NoError(t, err)

		require.Len(t, buf, 10)
		readByts.Set(true)
	})

	s, err := hosts[0].NewStream(context.Background(), hosts[1].ID(), prot)
	require.NoError(t, err)
	strm := NewTimeoutStream(s)
	err = strm.WriteWithTimeout(make([]byte, 10), time.Millisecond*100)
	require.NoError(t, err)
	require.NoError(t, strm.CloseWrite())

	time.Sleep(time.Millisecond * 300)
	require.True(t, readByts.Get())
}

func testHosts(t *testing.T, n int) []host.Host {
	ctx := context.Background()

	hosts := make([]host.Host, n)

	for i := 0; i < n; i++ {
		// create 2 peers
		h, err := libp2p.New(ctx,
			libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
		require.NoError(t, err)
		require.NoError(t, discovery.SetupMdnsDiscovery(ctx, zap.L(), h))
		hosts[i] = h
	}

	<-time.After(time.Millisecond * 1500) // important to let nodes reach each other

	return hosts
}
