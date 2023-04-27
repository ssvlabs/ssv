package connections

import (
	"crypto"
	"crypto/rsa"
	"time"

	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/libp2p/go-libp2p/core/peer"
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

func prepareTestingData() {
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

	Signature, err = rsa.SignPKCS1v15(nil, SenderPrivateKey, crypto.SHA256, HashedHandshakeData)
	if err != nil {
		panic(err)
	}
}
