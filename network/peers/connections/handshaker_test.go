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

// TestHandshakePermissionedFlow tests Handshake() Permissioned flow
func TestHandshakePermissionedFlow(t *testing.T) {
	td := getTestingData(t)

	type test struct {
		name            string
		permissioned    func() bool
		expectedErr     error
		incomingMessage records.AnyNodeInfo
	}

	testCases := []test{
		{
			name:            "non-permissioned happy flow",
			permissioned:    func() bool { return false },
			expectedErr:     nil,
			incomingMessage: td.NodeInfo,
		},
		{
			name:            "permissioned happy flow",
			permissioned:    func() bool { return true },
			expectedErr:     nil,
			incomingMessage: td.SignedNodeInfo,
		},
		{
			name:            "permissioned node receives non-permissioned message",
			permissioned:    func() bool { return true },
			expectedErr:     errConsumingMessage,
			incomingMessage: td.NodeInfo,
		},
		{
			name:            "non-permissioned node receives permissioned message",
			permissioned:    func() bool { return false },
			expectedErr:     errConsumingMessage,
			incomingMessage: td.SignedNodeInfo,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			td := getTestingData(t)
			td.Handshaker.Permissioned = tc.permissioned

			sealedIncomingMessage, err := tc.incomingMessage.Seal(td.NetworkPrivateKey)
			require.NoError(t, err)

			td.Handshaker.streams = mock.StreamController{
				MockRequest: sealedIncomingMessage,
			}

			require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), tc.expectedErr)
		})
	}
}

// TestHandshakeProcessIncomingNodeInfoFlow tests Handshake() incoming node info flow
func TestHandshakeProcessIncomingNodeInfoFlow(t *testing.T) {
	t.Run("not pass through filters", func(t *testing.T) {
		td := getTestingData(t)

		td.Handshaker.filters = func() []HandshakeFilter {
			return []HandshakeFilter{SenderRecipientIPsCheckFilter("some-id")}
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), errPeerWasFiltered)
	})

	t.Run("pass through operators filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = func() bool { return true }
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		senderPublicKey, err := td.SenderPrivateKey.Public().Base64()
		require.NoError(t, err)
		storgmock := mock.NodeStorage{RegisteredOperatorPublicKeyPEMs: []string{string(senderPublicKey)}}
		td.Handshaker.filters = func() []HandshakeFilter {
			return []HandshakeFilter{RegisteredOperatorsFilter(storgmock, []string{})}
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), nil)
	})

	t.Run(" pass through operators filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = func() bool { return true }
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)

		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		senderPublicKey, err := td.SenderPrivateKey.Public().Base64()
		require.NoError(t, err)

		storgmock := mock.NodeStorage{RegisteredOperatorPublicKeyPEMs: []string{}}

		td.Handshaker.filters = func() []HandshakeFilter {
			return []HandshakeFilter{RegisteredOperatorsFilter(storgmock, []string{string(senderPublicKey)})}
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), nil)
	})

	t.Run("not pass through operators filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = func() bool { return true }
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		storgmock := mock.NodeStorage{RegisteredOperatorPublicKeyPEMs: []string{}}
		td.Handshaker.filters = func() []HandshakeFilter {
			return []HandshakeFilter{RegisteredOperatorsFilter(storgmock, []string{})}
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), errPeerWasFiltered)
	})

	t.Run(" pass through peers filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = func() bool { return true }
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		td.Handshaker.filters = func() []HandshakeFilter {
			return []HandshakeFilter{SenderRecipientIPsCheckFilter(td.RecipientPeerID)}
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), nil)
	})

	t.Run("not pass through peers filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = func() bool { return true }
		td.Conn.MockPeerID = "otherpeer"
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		td.Handshaker.filters = func() []HandshakeFilter {
			return []HandshakeFilter{SenderRecipientIPsCheckFilter(td.RecipientPeerID)}
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), errPeerWasFiltered)
	})
}
