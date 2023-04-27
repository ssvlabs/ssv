package connections

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

var (
	SenderPrivateKey *rsa.PrivateKey

	HandshakeData       records.HandshakeData
	HashedHandshakeData []byte
	Signature           []byte

	SenderPeerID    = peer.ID("1.1.1.1")
	RecipientPeerID = peer.ID("2.2.2.2")

	PrivateKeyPEM      []byte
	SenderPublicKeyPEM []byte
)

func init() {
	var err error
	_, PrivateKeyPEM, err = rsaencryption.GenerateKeys()
	if err != nil {
		panic(err)
	}

	SenderPrivateKey, err = rsaencryption.ConvertPemToPrivateKey(string(PrivateKeyPEM))
	if err != nil {
		panic(err)
	}

	SenderPublicKeyPEM, err = rsaencryption.ExtractPublicKeyPem(SenderPrivateKey)
	if err != nil {
		panic(err)
	}

	HandshakeData = records.HandshakeData{
		SenderPeerID:    SenderPeerID,
		RecipientPeerID: RecipientPeerID,
		Timestamp:       time.Now(),
		SenderPubKeyPem: SenderPublicKeyPEM,
	}
	hashed := HandshakeData.Hash()

	HashedHandshakeData = hashed[:]

	Signature, err = rsa.SignPKCS1v15(rand.Reader, SenderPrivateKey, crypto.SHA256, HashedHandshakeData)
	if err != nil {
		panic(err)
	}
}

func TestTest(t *testing.T) {
	pk, err := rsaencryption.ConvertPemToPrivateKey(string(PrivateKeyPEM))
	require.NoError(t, err)
	require.NotNil(t, pk)
}

func TestNetworkIDFilter(t *testing.T) {
	f := NetworkIDFilter("xxx")

	ok, err := f("", &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "xxx",
		},
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f("", &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "bbb",
		},
	})
	require.Error(t, err)
	require.False(t, ok)
}

func TestSenderRecipientIPsCheckFilter(t *testing.T) {
	f := SenderRecipientIPsCheckFilter(RecipientPeerID)

	ok, err := f(SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    SenderPeerID,
			RecipientPeerID: RecipientPeerID,
		},
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f(SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    "wrong sender",
			RecipientPeerID: RecipientPeerID,
		},
	})
	require.Error(t, err)
	require.False(t, ok)

	ok, err = f(SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    SenderPeerID,
			RecipientPeerID: "wrong recipient",
		},
	})
	require.Error(t, err)
	require.False(t, ok)
}

func TestSignatureCheckFFilter(t *testing.T) {
	f := SignatureCheckFilter()

	ok, err := f("", &records.SignedNodeInfo{
		HandshakeData: HandshakeData,
		Signature:     Signature,
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{}, //wrong handshake data
		Signature:     Signature,
	})
	require.Error(t, err)
	require.False(t, ok)

	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: HandshakeData,
		Signature:     []byte("wrong signature"),
	})
	require.Error(t, err)
	require.False(t, ok)

	wrongTimestamp := HandshakeData
	wrongTimestamp.Timestamp = wrongTimestamp.Timestamp.Add(-2 * AllowedDifference)
	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: wrongTimestamp,
		Signature:     Signature,
	})
	require.Error(t, err)
	require.False(t, ok)
}

func TestRegisteredOperatorsFilter(t *testing.T) {
	f := RegisteredOperatorsFilter(logging.TestLogger(t), MockStorage{
		PrivateKey: SenderPrivateKey,
	})

	ok, err := f("", &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID: SenderPeerID,
		},
	})
	require.NoError(t, err)
	require.True(t, ok)
}
