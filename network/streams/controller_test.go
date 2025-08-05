package streams

import (
	"bytes"
	"testing"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability/log"
)

func TestStreamCtrl(t *testing.T) {
	hosts := testHosts(t, 3)

	prot := protocol.ID("/test/protocol")

	logger := log.TestLogger(t)
	ctrl0 := NewStreamController(t.Context(), hosts[0], time.Second, time.Second)
	ctrl1 := NewStreamController(t.Context(), hosts[1], time.Second, time.Second)

	t.Run("handle request", func(t *testing.T) {
		hosts[0].SetStreamHandler(prot, func(stream libp2pnetwork.Stream) {
			msg, res, done, err := ctrl0.HandleStream(logger, stream)
			defer done()
			require.NoError(t, err)
			require.NotNil(t, msg)
			resp, err := dummyMsg().Encode()
			require.NoError(t, err)
			require.NoError(t, res(resp))
		})
		d, err := dummyMsg().Encode()
		require.NoError(t, err)
		res, err := ctrl1.Request(logger, hosts[0].ID(), prot, d)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.True(t, bytes.Equal(res, d))
	})

	t.Run("request deadline", func(t *testing.T) {
		timeout := time.Millisecond * 10
		ctrl0.(*streamCtrl).readWriteTimeout = timeout
		hosts[1].SetStreamHandler(prot, func(stream libp2pnetwork.Stream) {
			msg, s, done, err := ctrl0.HandleStream(logger, stream)
			done()
			require.NoError(t, err)
			require.NotNil(t, msg)
			require.NotNil(t, s)
			<-time.After(timeout + time.Millisecond)
		})
		d, err := dummyMsg().Encode()
		require.NoError(t, err)
		res, err := ctrl0.Request(logger, hosts[0].ID(), prot, d)
		require.Error(t, err)
		require.Nil(t, res)
	})

}

func dummyMsg() *spectypes.SSVMessage {
	return &spectypes.SSVMessage{Data: []byte("dummy")}
}
