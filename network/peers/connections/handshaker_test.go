package connections

import (
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
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
		require.ErrorContains(t, pi.LastHandshakeError, "failed requesting node info")
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

	t.Run("filtered peer", func(t *testing.T) {
		td := getTestingData(t)

		beforeHandshake := time.Now()
		td.Handshaker.filters = func() []HandshakeFilter {
			return []HandshakeFilter{
				func(senderID peer.ID, nodeInfo records.AnyNodeInfo) error {
					return fmt.Errorf("peer filtered")
				},
			}
		}
		require.Error(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))

		pi := td.Handshaker.peerInfos.PeerInfo(td.SenderPeerID)
		require.NotNil(t, pi)
		require.True(t, pi.LastHandshake.After(beforeHandshake) && pi.LastHandshake.Before(time.Now()))
		require.ErrorContains(t, pi.LastHandshakeError, "failed verifying their node info: peer filtered")

		// Test that happy flow works correctly after failing prior handshake.
		beforeHandshake = time.Now()
		td.Handshaker.filters = func() []HandshakeFilter {
			return []HandshakeFilter{}
		}
		require.NoError(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn))

		pi = td.Handshaker.peerInfos.PeerInfo(td.SenderPeerID)
		require.NotNil(t, pi)
		require.True(t, pi.LastHandshake.After(beforeHandshake) && pi.LastHandshake.Before(time.Now()))
		require.Nil(t, pi.LastHandshakeError)
	})
}
