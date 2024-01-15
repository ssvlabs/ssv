package connections

import (
	"context"
	"crypto"
	"crypto/rsa"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

type TestData struct {
	NetworkPrivateKey libp2pcrypto.PrivKey
	SenderPrivateKey  *rsa.PrivateKey

	HandshakeData       records.HandshakeData
	HashedHandshakeData []byte
	Signature           []byte

	SenderPeerID    peer.ID
	RecipientPeerID peer.ID

	PrivateKeyPEM            []byte
	SenderBase64PublicKeyPEM string

	Handshaker handshaker
	Conn       mock.Conn

	NodeInfo       *records.NodeInfo
	SignedNodeInfo *records.SignedNodeInfo
}

func getTestingData(t *testing.T) TestData {
	peerID1 := peer.ID("1.1.1.1")
	peerID2 := peer.ID("2.2.2.2")

	_, privateKeyPem, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)

	senderPrivateKey, err := rsaencryption.ConvertPemToPrivateKey(string(privateKeyPem))
	require.NoError(t, err)

	senderPublicKey, err := rsaencryption.ExtractPublicKey(senderPrivateKey)
	require.NoError(t, err)

	nodeInfo := &records.NodeInfo{
		NetworkID: "some-network-id",
		Metadata: &records.NodeMetadata{
			NodeVersion:   "some-node-version",
			OperatorID:    "some-operator-id",
			ExecutionNode: "some-execution-node",
			ConsensusNode: "some-consensus-node",
			Subnets:       "some-subnets",
		},
	}

	handshakeData := records.HandshakeData{
		SenderPeerID:    peerID2,
		RecipientPeerID: peerID1,
		Timestamp:       time.Now(),
		SenderPublicKey: []byte(senderPublicKey),
	}
	hashed := handshakeData.Hash()

	hashedHandshakeData := hashed[:]

	signature, err := rsa.SignPKCS1v15(nil, senderPrivateKey, crypto.SHA256, hashedHandshakeData)
	require.NoError(t, err)

	sni := &records.SignedNodeInfo{
		NodeInfo:      nodeInfo,
		HandshakeData: handshakeData,
		Signature:     signature,
	}

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
	nst := mock.NodeStorage{
		MockGetPrivateKey: senderPrivateKey,
		RegisteredOperatorPublicKeyPEMs: []string{
			senderPublicKey,
		},
	}

	networkPrivateKey, _, err := libp2pcrypto.GenerateKeyPair(libp2pcrypto.ECDSA, 0)
	require.NoError(t, err)

	data, err := nodeInfo.Seal(networkPrivateKey)
	require.NoError(t, err)

	sc := mock.StreamController{
		MockRequest: data,
	}

	mockHandshaker := handshaker{
		ctx:         context.Background(),
		nodeInfos:   nii,
		peerInfos:   ns,
		ids:         ids,
		net:         net,
		nodeStorage: nst,
		streams:     sc,
		filters:     func() []HandshakeFilter { return []HandshakeFilter{} },
	}

	mockConn := mock.Conn{
		MockPeerID: peerID2,
	}

	return TestData{
		SenderPrivateKey:         senderPrivateKey,
		HandshakeData:            handshakeData,
		HashedHandshakeData:      hashedHandshakeData,
		Signature:                signature,
		SenderPeerID:             peerID2,
		RecipientPeerID:          peerID1,
		PrivateKeyPEM:            privateKeyPem,
		SenderBase64PublicKeyPEM: senderPublicKey,
		Handshaker:               mockHandshaker,
		Conn:                     mockConn,
		NetworkPrivateKey:        networkPrivateKey,
		NodeInfo:                 nodeInfo,
		SignedNodeInfo:           sni,
	}
}
