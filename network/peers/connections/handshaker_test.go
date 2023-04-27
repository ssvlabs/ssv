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

// TestHandshake tests whole handshake flow
// TestHandshake DO NOT CHECK SEAL/CONSUME FLOW AND FILERS (these checks are in another files)
func TestHandshake(t *testing.T) {
	prepareTestingData()

	nii := mock.NodeInfoIndex{
		MockNodeInfo:   &records.NodeInfo{},
		MockSelfSealed: nil,
	}
	ns := mock.NodeStates{
		MockNodeState: peers.StateReady,
	}
	ch := make(chan struct{})
	close(ch)
	ids := mock.IDService{
		MockIdentifyWait: ch,
	}
	ps := mock.Peerstore{
		MockFirstSupportedProtocol: "I support handshake protocol",
	}
	net := mock.Net{
		MockPeerstore: ps,
	}
	nst := mock.NodeStorage{
		MockGetPrivateKey: SenderPrivateKey,
	}
	sc := mock.StreamController{
		MockRequest: []byte{},
	}

	h := handshaker{
		ctx:         context.Background(),
		nodeInfoIdx: nii,
		states:      ns,
		ids:         ids,
		net:         net,
		nodeStorage: nst,
		streams:     sc,
		filters:     []HandshakeFilter{},
	}
	c := mock.Conn{
		MockPeerID: RecipientPeerID,
	}

	require.NoError(t, h.Handshake(logging.TestLogger(t), c))
}
