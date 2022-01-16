package p2p

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"fmt"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
)

func convertToInterfacePubkey(pubkey *ecdsa.PublicKey) crypto.PubKey {
	typeAssertedKey := crypto.PubKey((*crypto.Secp256k1PublicKey)(pubkey))
	return typeAssertedKey
}

// Adds a private key to the libp2p option if the option was provided.
// If the private key file is missing or cannot be read, or if the
// private key contents cannot be marshaled, an exception is thrown.
func privKeyOption(privkey *ecdsa.PrivateKey) libp2p.Option {
	return func(cfg *libp2p.Config) error {
		//log.Debug("ECDSA private key generated")
		return cfg.Apply(libp2p.Identity(convertToInterfacePrivkey(privkey)))
	}
}

func convertToInterfacePrivkey(privkey *ecdsa.PrivateKey) crypto.PrivKey {
	typeAssertedKey := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(privkey))
	return typeAssertedKey
}

func convertFromInterfacePrivKey(privkey crypto.PrivKey) *ecdsa.PrivateKey {
	typeAssertedKey := (*ecdsa.PrivateKey)(privkey.(*crypto.Secp256k1PrivateKey))
	typeAssertedKey.Curve = gcrypto.S256() // Temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1.
	return typeAssertedKey
}

// operatorID returns sha256 (hex) of the given operator public key
func operatorID(pubkeyHex string) string {
	if len(pubkeyHex) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(pubkeyHex)))
}

func peerToString(peerID peer.ID) string {
	return peer.Encode(peerID)
}

func peerFromString(peerStr string) (peer.ID, error) {
	if len(peerStr) == 0 {
		return "", errors.New("peer ID nil")
	}
	return peer.Decode(peerStr)
}
