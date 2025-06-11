package utils

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
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
		privKey, err = commons.ECDSAPrivFromInterface(unmarshalledKey)
		if err != nil {
			return nil, err
		}
	} else {
		logger.Info("No private key was provided. Generating a new one...")
		privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to generate 256k1 key")
		}
		privKey, err = commons.ECDSAPrivFromInterface(privInterfaceKey)
		if err != nil {
			return nil, err
		}
	}
	interfacePriv, err := commons.ECDSAPrivToInterface(privKey)
	if err != nil {
		return nil, err
	}

	b, err := interfacePriv.Raw()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to convert private key to interface")
	}
	if privateKey != "" {
		logger.Debug("Using Private Key from config", fields.PrivKey(b), zap.Any("private_key", b))
	} else {
		logger.Debug("Private Key generated", fields.PrivKey(b), zap.Any("private_key", b))
	}

	return privKey, nil
}
