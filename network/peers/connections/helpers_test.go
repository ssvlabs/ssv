package connections

import (
	"context"
	"crypto"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/bloxapp/ssv/logging"
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

	senderPublicKeyPem, err := rsaencryption.ExtractPublicKeyPem(senderPrivateKey)
	require.NoError(t, err)

	nodeInfo := &records.NodeInfo{
		ForkVersion: "some-fork",
		NetworkID:   "some-network-id",
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
		SenderPubKeyPem: senderPublicKeyPem,
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
	ns := mock.NodeStates{
		MockNodeState: peers.StateReady,
	}
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
		RegisteredOperatorPublicKeyPEMs: [][]byte{
			senderPublicKeyPem,
		},
	}

	networkPrivateKey, _, err := libp2pcrypto.GenerateKeyPair(libp2pcrypto.ECDSA, 0)
	require.NoError(t, err)

	data, err := sni.Seal(networkPrivateKey)
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
		filters: []HandshakeFilter{
			NetworkIDFilter("some-network-id"),
			SenderRecipientIPsCheckFilter(peerID1),
			SignatureCheckFilter(),
			RegisteredOperatorsFilter(logging.TestLogger(t), nst),
		},
	}

	mockConn := mock.Conn{
		MockPeerID: peerID2,
	}

	return TestData{
		SenderPrivateKey:    senderPrivateKey,
		HandshakeData:       handshakeData,
		HashedHandshakeData: hashedHandshakeData,
		Signature:           signature,
		SenderPeerID:        peerID1,
		RecipientPeerID:     peerID2,
		PrivateKeyPEM:       privateKeyPem,
		SenderPublicKeyPEM:  senderPublicKeyPem,
		Handshaker:          mockHandshaker,
		Conn:                mockConn,
		NetworkPrivateKey:   networkPrivateKey,
		NodeInfo:            nodeInfo,
		SignedNodeInfo:      sni,
	}
}
