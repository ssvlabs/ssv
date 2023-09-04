package records

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"reflect"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/utils/rsaencryption"
)

func TestSignedNodeInfo_Seal_Consume(t *testing.T) {
	nodeInfo := &NodeInfo{
		NetworkID: "testnet",
		Metadata: &NodeMetadata{
			NodeVersion:   "v0.1.12",
			ExecutionNode: "geth/x",
			ConsensusNode: "prysm/x",
			OperatorID:    "xxx",
			Subnets:       "some-subnets",
		},
	}

	_, senderPrivateKeyPem, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)

	senderPrivateKey, err := rsaencryption.ConvertPemToPrivateKey(string(senderPrivateKeyPem))
	require.NoError(t, err)

	senderBase64PublicKeyPem, err := rsaencryption.ExtractPublicKey(senderPrivateKey)
	require.NoError(t, err)

	handshakeData := HandshakeData{
		SenderPeerID:    peer.ID("1.1.1.1"),
		RecipientPeerID: peer.ID("2.2.2.2"),
		Timestamp:       time.Now().Round(time.Second),
		SenderPublicKey: []byte(senderBase64PublicKeyPem),
	}
	hashed := handshakeData.Hash()

	hashedHandshakeData := hashed[:]

	signature, err := rsa.SignPKCS1v15(nil, senderPrivateKey, crypto.SHA256, hashedHandshakeData)
	require.NoError(t, err)

	sni := &SignedNodeInfo{
		NodeInfo:      nodeInfo,
		HandshakeData: handshakeData,
		Signature:     signature,
	}

	netKey, _, err := libp2pcrypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)

	data, err := sni.Seal(netKey)
	require.NoError(t, err)

	parsedRec := &SignedNodeInfo{}
	require.NoError(t, parsedRec.Consume(data))

	require.True(t, reflect.DeepEqual(sni, parsedRec))
}

func TestSignedNodeInfo_Marshal_Unmarshal(t *testing.T) {
	nodeInfo := &NodeInfo{
		NetworkID: "testnet",
		Metadata: &NodeMetadata{
			NodeVersion:   "v0.1.12",
			ExecutionNode: "geth/x",
			ConsensusNode: "prysm/x",
			OperatorID:    "xxx",
			Subnets:       "some-subnets",
		},
	}

	_, senderPrivateKeyPem, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)

	senderPrivateKey, err := rsaencryption.ConvertPemToPrivateKey(string(senderPrivateKeyPem))
	require.NoError(t, err)

	senderBase64PublicKeyPem, err := rsaencryption.ExtractPublicKey(senderPrivateKey)
	require.NoError(t, err)

	handshakeData := HandshakeData{
		SenderPeerID:    peer.ID("1.1.1.1"),
		RecipientPeerID: peer.ID("2.2.2.2"),
		Timestamp:       time.Now().Round(time.Second),
		SenderPublicKey: []byte(senderBase64PublicKeyPem),
	}
	hashed := handshakeData.Hash()

	hashedHandshakeData := hashed[:]

	signature, err := rsa.SignPKCS1v15(nil, senderPrivateKey, crypto.SHA256, hashedHandshakeData)
	require.NoError(t, err)

	sni := &SignedNodeInfo{
		NodeInfo:      nodeInfo,
		HandshakeData: handshakeData,
		Signature:     signature,
	}

	data, err := sni.MarshalRecord()
	require.NoError(t, err)

	parsedRec := &SignedNodeInfo{}
	require.NoError(t, parsedRec.UnmarshalRecord(data))

	require.True(t, reflect.DeepEqual(sni, parsedRec))
}
