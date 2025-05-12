package connections

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/network/peers/connections/mock"
)

// TestHandshakeTestData is a test for testing data and mocks
func TestHandshakeTestData(t *testing.T) {
	t.Run("happy flow", func(t *testing.T) {
		td := getTestingData(t)

		beforeHandshake := time.Now()
		require.NoError(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))

		pi := td.Handshaker.peerInfos.PeerInfo(td.SenderPeerID)
		require.NotNil(t, pi)
		require.True(t, pi.LastHandshake.After(beforeHandshake) && pi.LastHandshake.Before(time.Now()))
		require.Nil(t, pi.LastHandshakeError)
	})

	t.Run("wrong NodeInfoIndex", func(t *testing.T) {
		td := getTestingData(t)

		beforeHandshake := time.Now()
		td.Handshaker.nodeInfos = mock.NodeInfoIndex{}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))

		pi := td.Handshaker.peerInfos.PeerInfo(td.SenderPeerID)
		require.NotNil(t, pi)
		require.True(t, pi.LastHandshake.After(beforeHandshake) && pi.LastHandshake.Before(time.Now()))
		require.Error(t, pi.LastHandshakeError)
	})

	t.Run("wrong StreamController", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.streams = mock.StreamController{}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))
	})
}
