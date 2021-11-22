package utils

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ECDSAPrivateKey extracts the ecdsa.PrivateKey from the given string or generate a new key
func ECDSAPrivateKey(logger *zap.Logger, privateKey string) (*ecdsa.PrivateKey, error) {
	var privKey *ecdsa.PrivateKey
	if privateKey != "" {
		dst, err := hex.DecodeString(privateKey)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to decode privKey string")
		}
		unmarshalledKey, err := crypto.UnmarshalSecp256k1PrivateKey(dst)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to unmarshal passed privKey")
		}
		privKey = (*ecdsa.PrivateKey)(unmarshalledKey.(*crypto.Secp256k1PrivateKey))
	} else {
		privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to generate 256k1 key")
		}
		privKey = (*ecdsa.PrivateKey)(privInterfaceKey.(*crypto.Secp256k1PrivateKey))
		logger.Warn("No private key was provided. Using default/random private key")
	}
	privKey.Curve = gcrypto.S256()

	interfacePriv := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(privKey))
	b, err := interfacePriv.Raw()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to convert private key to interface")
	}
	if privateKey != "" {
		logger.Debug("Using Private Key from config", zap.String("private-key encoded", hex.EncodeToString(b)), zap.Any("private-key", b))
	} else {
		logger.Debug("Private Key generated", zap.String("private-key encoded", hex.EncodeToString(b)), zap.Any("private-key", b))
	}

	return privKey, nil
}
