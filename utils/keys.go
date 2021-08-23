package utils

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p-core/crypto"
	"go.uber.org/zap"
)

// ECDSAPrivateKey extracts the ecdsa.PrivateKey from the given string or generate a new key
func ECDSAPrivateKey(logger *zap.Logger, privateKey string) *ecdsa.PrivateKey {
	var privKey *ecdsa.PrivateKey
	if privateKey != "" {
		dst, err := hex.DecodeString(privateKey)
		if err != nil {
			panic(err)
		}
		unmarshalledKey, err := crypto.UnmarshalSecp256k1PrivateKey(dst)
		if err != nil {
			panic(err)
		}
		privKey = (*ecdsa.PrivateKey)(unmarshalledKey.(*crypto.Secp256k1PrivateKey))
	} else {
		privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
		if err != nil {
			panic(err)
		}
		privKey = (*ecdsa.PrivateKey)(privInterfaceKey.(*crypto.Secp256k1PrivateKey))
		logger.Warn("No private key was provided. Using default/random private key")
		b, err := privInterfaceKey.Raw()
		if err != nil {
			panic(err)
		}
		logger.Debug("Private Key generated", zap.ByteString("private-key", b))
	}
	privKey.Curve = gcrypto.S256()

	return privKey
}