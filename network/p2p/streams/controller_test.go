package streams

import (
	"bytes"
	"context"
	"github.com/bloxapp/ssv/network"
	v0 "github.com/bloxapp/ssv/network/forks/v0"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestStreamCtrl(t *testing.T) {
	hosts := testHosts(t, 3)

	prot := protocol.ID("/test/protocol")
	identifier := []byte("xxx")

	ctrl0 := NewStreamController(context.Background(), zaptest.NewLogger(t), hosts[0], v0.New(), time.Second)
	ctrl1 := NewStreamController(context.Background(), zaptest.NewLogger(t), hosts[1], v0.New(), time.Second)

	t.Run("sanity", func(t *testing.T) {
		hosts[0].SetStreamHandler(prot, func(stream libp2pnetwork.Stream) {
			msg, s, err := ctrl0.HandleStream(stream)
			require.NoError(t, err)
			require.NotNil(t, msg)
			resp := dummyMsg()
			resp.SyncMessage.Lambda = identifier
			resp.StreamID = s.ID()
			require.NoError(t, ctrl0.Respond(resp))
		})
		res, err := ctrl1.Request(hosts[0].ID(), prot, dummyMsg())
		require.NoError(t, err)
		require.NotNil(t, res)
		require.True(t, bytes.Equal(res.SyncMessage.Lambda, identifier))
	})

	t.Run("with deadline", func(t *testing.T) {
		timeout := time.Millisecond * 10
		ctrl0.(*streamCtrl).requestTimeout = timeout
		hosts[1].SetStreamHandler(prot, func(stream libp2pnetwork.Stream) {
			msg, s, err := ctrl0.HandleStream(stream)
			require.NoError(t, err)
			require.NotNil(t, msg)
			require.NotNil(t, s)
			<-time.After(timeout + time.Millisecond)
		})
		res, err := ctrl0.Request(hosts[0].ID(), prot, dummyMsg())
		require.Error(t, err)
		require.Nil(t, res)
	})

}

func dummyMsg() *network.Message {
	return &network.Message{SyncMessage: &network.SyncMessage{}}
}
