package connections

import (
	"context"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/operator/keys"
)

type TestData struct {
	NetworkPrivateKey libp2pcrypto.PrivKey
	SenderPrivateKey  keys.OperatorPrivateKey

	HandshakeData records.HandshakeData
	Signature     []byte

	SenderPeerID    peer.ID
	RecipientPeerID peer.ID

	SenderBase64PublicKeyPEM string

	Handshaker handshaker
	Conn       mock.Conn

	NodeInfo *records.NodeInfo
}

func getTestingData(t *testing.T) TestData {
	peerID1 := peer.ID("1.1.1.1")
	peerID2 := peer.ID("2.2.2.2")

	privateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	senderPublicKey, err := privateKey.Public().Base64()
	require.NoError(t, err)

	nodeInfo := &records.NodeInfo{
		NetworkID: "some-network-id",
		Metadata: &records.NodeMetadata{
			NodeVersion:   "some-node-version",
			ExecutionNode: "some-execution-node",
			ConsensusNode: "some-consensus-node",
			Subnets:       "some-subnets",
		},
	}

	handshakeData := records.HandshakeData{
		SenderPeerID:    peerID2,
		RecipientPeerID: peerID1,
		Timestamp:       time.Now(),
		SenderPublicKey: senderPublicKey,
	}

	signature, err := privateKey.Sign(handshakeData.Encode())
	require.NoError(t, err)

	nii := mock.NodeInfoIndex{
		MockNodeInfo:   nil,
		MockSelfSealed: []byte("something"),
	}
	ns := peers.NewPeerInfoIndex()
	ch := make(chan struct{})
	close(ch)
	ids := mock.IDService{
		MockIdentifyWait: ch,
	}
	ps := mock.Peerstore{
		ExistingPIDs:               []peer.ID{peerID2},
		MockFirstSupportedProtocol: "I support handshake protocol",
	}
	net := mock.Net{
		MockPeerstore: ps,
	}

	networkPrivateKey, _, err := libp2pcrypto.GenerateKeyPair(libp2pcrypto.ECDSA, 0)
	require.NoError(t, err)

	data, err := nodeInfo.Seal(networkPrivateKey)
	require.NoError(t, err)

	sc := mock.StreamController{
		MockRequest: data,
	}

	mockHandshaker := handshaker{
		ctx:       context.Background(),
		nodeInfos: nii,
		peerInfos: ns,
		ids:       ids,
		net:       net,
		streams:   sc,
		filters:   func() []HandshakeFilter { return []HandshakeFilter{} },
	}

	mockConn := mock.Conn{
		MockPeerID: peerID2,
	}

	return TestData{
		SenderPrivateKey:         privateKey,
		HandshakeData:            handshakeData,
		Signature:                signature,
		SenderPeerID:             peerID2,
		RecipientPeerID:          peerID1,
		SenderBase64PublicKeyPEM: string(senderPublicKey),
		Handshaker:               mockHandshaker,
		Conn:                     mockConn,
		NetworkPrivateKey:        networkPrivateKey,
		NodeInfo:                 nodeInfo,
	}
}
