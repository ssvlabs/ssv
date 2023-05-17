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

// TestHandshakePermissionedFlow tests Handshake() permissioned flow
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
			expectedErr:     errWrongPermissionedMode,
			incomingMessage: td.NodeInfo,
		},
		{
			name:            "non-permissioned node receives permissioned message",
			permissioned:    false,
			expectedErr:     errWrongPermissionedMode,
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
