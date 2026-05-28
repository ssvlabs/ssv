package utils

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/libp2p/go-libp2p/core/crypto"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

// ECDSAPrivateKey extracts the ecdsa.PrivateKey from the given string or generate a new key
func ECDSAPrivateKey(logger *zap.Logger, privateKey string) (*ecdsa.PrivateKey, error) {
	var privKey *ecdsa.PrivateKey
	if privateKey != "" {
		dst, err := hex.DecodeString(privateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decode privKey string: %w", err)
		}
		unmarshalledKey, err := crypto.UnmarshalSecp256k1PrivateKey(dst)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal passed privKey: %w", err)
		}
		privKey, err = commons.ECDSAPrivFromInterface(unmarshalledKey)
		if err != nil {
			return nil, err
		}
	} else {
		logger.Info("No private key was provided. Generating a new one...")
		privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("failed to generate 256k1 key: %w", err)
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
		return nil, fmt.Errorf("failed to convert private key to interface: %w", err)
	}
	if privateKey != "" {
		logger.Debug("Using Private Key from config", fields.PrivKey(b), zap.Any("private_key", b))
	} else {
		logger.Debug("Private Key generated", fields.PrivKey(b), zap.Any("private_key", b))
	}

	return privKey, nil
}
