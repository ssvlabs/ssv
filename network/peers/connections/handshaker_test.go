package connections

import (
	"context"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/stretchr/testify/require"
)

// TestHandshakeTestData is a test for testing data and mocks
func TestHandshakeTestData(t *testing.T) {
	t.Run("happy flow", func(t *testing.T) {
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

// TestHandshakePeerIsKnownFlow tests Handshake() PeerIsKnown flow
func TestHandshakePeerIsKnownFlow(t *testing.T) {
	type test struct {
		name        string
		state       peers.NodeState
		expectedErr error
	}

	testCases := []test{
		{
			name:        "peer is known and indexing",
			state:       peers.StateIndexing,
			expectedErr: errHandshakeInProcess,
		},
		{
			name:        "peer is known and pruned",
			state:       peers.StatePruned,
			expectedErr: errPeerPruned,
		},
		{
			name:        "peer is known and ready",
			state:       peers.StateReady,
			expectedErr: nil,
		},
	}

	for _, tc := range testCases {
		td := getTestingData(t)

		td.Handshaker.nodeInfoIdx = mock.NodeInfoIndex{
			MockNodeInfo: td.NodeInfo,
		}

		td.Handshaker.states = mock.NodeStates{
			MockNodeState: tc.state,
		}

		td.Handshaker.net = mock.Net{ // it needs to fail on a next row after isPeerKnow check
			MockPeerstore: mock.Peerstore{
				MockFirstSupportedProtocol: "",
			},
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), tc.expectedErr)
	}
}

// TestHandshakePermissionedFlow tests Handshake() Permissioned flow
func TestHandshakePermissionedFlow(t *testing.T) {
	td := getTestingData(t)

	type test struct {
		name            string
		permissioned    bool
		expectedErr     error
		incomingMessage records.AnyNodeInfo
	}

	testCases := []test{
		{
			name:            "non-permissioned happy flow",
			permissioned:    false,
			expectedErr:     nil,
			incomingMessage: td.NodeInfo,
		},
		{
			name:            "permissioned happy flow",
			permissioned:    true,
			expectedErr:     nil,
			incomingMessage: td.SignedNodeInfo,
		},
		{
			name:            "permissioned node receives non-permissioned message",
			permissioned:    true,
			expectedErr:     errConsumingMessage,
			incomingMessage: td.NodeInfo,
		},
		{
			name:            "non-permissioned node receives permissioned message",
			permissioned:    false,
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

		td.Handshaker.filters = []HandshakeFilter{
			SenderRecipientIPsCheckFilter("some-id"),
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), errPeerWasFiltered)
	})

	t.Run("pass through operators filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = true
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		senderPublicKey, err := rsaencryption.ExtractPublicKey(td.SenderPrivateKey)
		require.NoError(t, err)
		storgmock := mock.NodeStorage{RegisteredOperatorPublicKeyPEMs: []string{senderPublicKey}}
		td.Handshaker.filters = []HandshakeFilter{
			RegisteredOperatorsFilter(logging.TestLogger(t), storgmock, []string{}),
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), nil)
	})

	t.Run(" pass through operators filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = true
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}
		senderPublicKey, err := rsaencryption.ExtractPublicKey(td.SenderPrivateKey)
		require.NoError(t, err)

		storgmock := mock.NodeStorage{RegisteredOperatorPublicKeyPEMs: []string{}}

		td.Handshaker.filters = []HandshakeFilter{
			RegisteredOperatorsFilter(logging.TestLogger(t), storgmock, []string{senderPublicKey}),
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), nil)
	})

	t.Run("not pass through operators filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = true
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		storgmock := mock.NodeStorage{RegisteredOperatorPublicKeyPEMs: []string{}}
		td.Handshaker.filters = []HandshakeFilter{
			RegisteredOperatorsFilter(logging.TestLogger(t), storgmock, []string{}),
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), errPeerWasFiltered)
	})

	t.Run(" pass through peers filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = true
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		td.Handshaker.filters = []HandshakeFilter{
			SenderRecipientIPsCheckFilter(td.RecipientPeerID),
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), nil)
	})

	t.Run("not pass through peers filter", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.Permissioned = true
		td.Conn.MockPeerID = "otherpeer"
		sealedIncomingMessage, err := td.SignedNodeInfo.Seal(td.NetworkPrivateKey)
		require.NoError(t, err)
		td.Handshaker.streams = mock.StreamController{MockRequest: sealedIncomingMessage}

		td.Handshaker.filters = []HandshakeFilter{
			SenderRecipientIPsCheckFilter(td.RecipientPeerID),
		}

		require.ErrorIs(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn), errPeerWasFiltered)
	})

	t.Run("error add node info", func(t *testing.T) {
		td := getTestingData(t)

		td.Handshaker.nodeInfoIdx = mock.NodeInfoIndex{
			MockNodeInfo:          nil,
			MockSelfSealed:        []byte("something"),
			MockAddNodeInfoResult: false,
		}

		require.Equal(t, td.Handshaker.Handshake(logging.TestLogger(t), td.Conn).Error(), "AddNodeInfo error")
	})
}
