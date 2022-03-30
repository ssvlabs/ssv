package discovery

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"fmt"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p-core/crypto"
)

func fromInterfacePrivKey(privkey crypto.PrivKey) *ecdsa.PrivateKey {
	typeAssertedKey := (*ecdsa.PrivateKey)(privkey.(*crypto.Secp256k1PrivateKey))
	typeAssertedKey.Curve = gcrypto.S256() // Temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1.
	return typeAssertedKey
}

func fromInterfacePubKey(pubKey crypto.PubKey) *ecdsa.PublicKey {
	pk := (*ecdsa.PublicKey)(pubKey.(*crypto.Secp256k1PublicKey))
	pk.Curve = gcrypto.S256() // Temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1.
	return pk
}

// operatorID returns sha256 (hex) of the given operator public key
func operatorID(pubkeyHex string) string {
	if len(pubkeyHex) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(pubkeyHex)))
}
