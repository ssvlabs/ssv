package connections

import (
	"context"
	"crypto"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

type TestData struct {
	NetworkPrivateKey libp2pcrypto.PrivKey
	SenderPrivateKey  *rsa.PrivateKey

	HandshakeData       records.HandshakeData
	HashedHandshakeData []byte
	Signature           []byte

	SenderPeerID    peer.ID
	RecipientPeerID peer.ID

	PrivateKeyPEM      []byte
	SenderPublicKeyPEM []byte

	Handshaker handshaker
	Conn       mock.Conn
}

func getTestingData(t *testing.T) TestData {
	senderPeerID := peer.ID("senderPeerID")
	recipientPeerID := peer.ID("recipientPeerID")

	_, privateKeyPem, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)

	senderPrivateKey, err := rsaencryption.ConvertPemToPrivateKey(string(privateKeyPem))
	require.NoError(t, err)

	senderPublicKeyPem, err := rsaencryption.ExtractPublicKeyPem(senderPrivateKey)
	require.NoError(t, err)

	handshakeData := records.HandshakeData{
		SenderPeerID:    senderPeerID,
		RecipientPeerID: recipientPeerID,
		Timestamp:       time.Now(),
		SenderPubKeyPem: senderPublicKeyPem,
	}
	hashed := handshakeData.Hash()

	hashedHandshakeData := hashed[:]

	signature, err := rsa.SignPKCS1v15(nil, senderPrivateKey, crypto.SHA256, hashedHandshakeData)
	require.NoError(t, err)

	nii := mock.NodeInfoIndex{
		MockNodeInfo:   nil,
		MockSelfSealed: []byte("something"),
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
		ExistingPIDs:               []peer.ID{recipientPeerID},
		MockFirstSupportedProtocol: "I support handshake protocol",
	}
	net := mock.Net{
		MockPeerstore: ps,
	}
	nst := mock.NodeStorage{
		MockGetPrivateKey: senderPrivateKey,
	}

	networkPrivateKey, _, err := libp2pcrypto.GenerateKeyPair(libp2pcrypto.ECDSA, 0)
	require.NoError(t, err)

	ni := &records.NodeInfo{}
	data, err := ni.Seal(networkPrivateKey, handshakeData, signature)
	require.NoError(t, err)

	sc := mock.StreamController{
		MockRequest: data,
	}

	mockHandshaker := handshaker{
		ctx:         context.Background(),
		nodeInfoIdx: nii,
		states:      ns,
		ids:         ids,
		net:         net,
		nodeStorage: nst,
		streams:     sc,
		filters:     []HandshakeFilter{},
	}

	mockConn := mock.Conn{
		MockPeerID: recipientPeerID,
	}

	return TestData{
		SenderPrivateKey:    senderPrivateKey,
		HandshakeData:       handshakeData,
		HashedHandshakeData: hashedHandshakeData,
		Signature:           signature,
		SenderPeerID:        senderPeerID,
		RecipientPeerID:     recipientPeerID,
		PrivateKeyPEM:       privateKeyPem,
		SenderPublicKeyPEM:  senderPublicKeyPem,
		Handshaker:          mockHandshaker,
		Conn:                mockConn,
		NetworkPrivateKey:   networkPrivateKey,
	}
}
