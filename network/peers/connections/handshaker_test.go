package connections

import (
	"context"
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/stretchr/testify/require"
)

// TestHandshake whole handshake flow
// TestHandshake DO NOT CHECK FILERS (filers checks are at filters_test.go)
func TestHandshakeBaseFlow(t *testing.T) {
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

		nii := mock.NodeInfoIndex{
			MockNodeInfo: &records.NodeInfo{},
		}

		td.Handshaker.nodeInfoIdx = nii

		td.Handshaker.states = mock.NodeStates{
			MockNodeState: peers.StatePruned,
		}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})

	t.Run("wrong IDService", func(t *testing.T) {
		td := getTestingData(t)

		ch := make(chan struct{})
		td.Handshaker.ids = mock.IDService{
			MockIdentifyWait: ch,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		td.Handshaker.ctx = ctx
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
}

func TestHandshakePermissionedFlow(t *testing.T) {
	t.Run("Permissioned", func(t *testing.T) {
		td := getTestingData(t)

		td.Handshaker.Permissioned = true

		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))

		data, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)

		td.Handshaker.streams = mock.StreamController{
			MockRequest: data,
		}

		require.NoError(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})
}
