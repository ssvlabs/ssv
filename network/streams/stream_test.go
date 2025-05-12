package streams

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/require"
)

func TestStream(t *testing.T) {
	hosts := testHosts(t, 3)

	timeout := time.Second

	var wg sync.WaitGroup
	prot := protocol.ID("/protocol")

	hosts[1].SetStreamHandler(prot, func(stream core.Stream) {
		defer wg.Done()
		s := NewStream(stream)
		defer s.Close()
		<-time.After(timeout * 3)
		// require.Error(t, s.WriteWithTimeout([]byte("xxx"), timeout))
	})

	hosts[2].SetStreamHandler(prot, func(stream core.Stream) {
		defer wg.Done()
		s := NewStream(stream)
		defer s.Close()
		require.NoError(t, s.WriteWithTimeout([]byte("xxx"), timeout))
	})

	t.Run("with timeout", func(t *testing.T) {
		wg.Add(1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s, err := hosts[0].NewStream(ctx, hosts[1].ID(), prot)
		require.NoError(t, err)
		strm := NewStream(s)
		defer strm.Close()
		byts, err := strm.ReadWithTimeout(timeout)
		require.EqualError(t, err, "i/o deadline reached")
		require.Len(t, byts, 0)
	})

	t.Run("no timeout", func(t *testing.T) {
		wg.Add(1)
		ctx, cancel := context.WithTimeout(t.Context(), timeout*2)
		defer cancel()
		s, err := hosts[0].NewStream(ctx, hosts[2].ID(), prot)
		require.NoError(t, err)
		strm := NewStream(s)
		defer strm.Close()
		byts, err := strm.ReadWithTimeout(timeout)
		require.NoError(t, err)
		require.Len(t, byts, 3)
	})

	wg.Wait()
}

func testHosts(t *testing.T, n int) []host.Host {
	ctx := t.Context()

	hosts := make([]host.Host, n)

	for i := 0; i < n; i++ {
		h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
		require.NoError(t, err)
		hosts[i] = h
	}

	for i, current := range hosts {
		for j, h := range hosts {
			if i != j {
				_ = current.Connect(ctx, peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()})
			}
		}
	}

	<-time.After(time.Millisecond * 1500) // let nodes reach each other

	return hosts
}
