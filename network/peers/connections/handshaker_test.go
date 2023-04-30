package connections

import (
	"context"
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/stretchr/testify/require"
)

// TestHandshake whole handshake flow
// TestHandshake DO NOT CHECK FILERS (filers checks are at another file)
func TestHandshake(t *testing.T) {
	t.Run("regular", func(t *testing.T) {
		td := getTestingData(t)

		require.NoError(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})

	t.Run("wrong NodeInfoIndex", func(t *testing.T) {
		td := getTestingData(t)

		td.Handshaker.nodeInfoIdx = mock.NodeInfoIndex{}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})

	t.Run("wrong NodeStates", func(t *testing.T) {
		td := getTestingData(t)

		td.Handshaker.states = mock.NodeStates{}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})

	t.Run("wrong IDService", func(t *testing.T) {
		td := getTestingData(t)

		ch := make(chan struct{})
		td.Handshaker.ids = mock.IDService{
			MockIdentifyWait: ch,
		}

		var cancel func()
		td.Handshaker.ctx, cancel = context.WithCancel(context.Background())
		cancel()

		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})

	t.Run("wrong Net", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.net = mock.Net{}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})

	t.Run("wrong NodeStorage", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.nodeStorage = mock.NodeStorage{}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})

	t.Run("wrong StreamController", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.streams = mock.StreamController{}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})

	t.Run("wrong Conn", func(t *testing.T) {
		td := getTestingData(t)
		td.Conn = mock.Conn{}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})
}
