package connections

import (
	"crypto"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

type TestData struct {
	SenderPrivateKey *rsa.PrivateKey

	HandshakeData       records.HandshakeData
	HashedHandshakeData []byte
	Signature           []byte

	SenderPeerID    peer.ID
	RecipientPeerID peer.ID

	PrivateKeyPEM      []byte
	SenderPublicKeyPEM []byte
}

func getTestingData(t *testing.T) TestData {
	senderPeerID := peer.ID("1.1.1.1")
	recipientPeerID := peer.ID("2.2.2.2")

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

	return TestData{
		SenderPrivateKey:    senderPrivateKey,
		HandshakeData:       handshakeData,
		HashedHandshakeData: hashedHandshakeData,
		Signature:           signature,
		SenderPeerID:        senderPeerID,
		RecipientPeerID:     recipientPeerID,
		PrivateKeyPEM:       privateKeyPem,
		SenderPublicKeyPEM:  senderPublicKeyPem,
	}
}
