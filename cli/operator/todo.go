package operator

import (
	"encoding/base64"
	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/operator/keystore"
	"go.uber.org/zap"
	"os"
)

func operatorPrivateKey(logger *zap.Logger) (keys.OperatorPrivateKey, string) {
	if cfg.KeyStore.PrivateKeyFile != "" {
		// nolint: gosec
		encryptedJSON, err := os.ReadFile(cfg.KeyStore.PrivateKeyFile)
		if err != nil {
			logger.Fatal("could not read PEM file", zap.Error(err))
		}

		// nolint: gosec
		keyStorePassword, err := os.ReadFile(cfg.KeyStore.PasswordFile)
		if err != nil {
			logger.Fatal("could not read password file", zap.Error(err))
		}

		decryptedKeystore, err := keystore.DecryptKeystore(encryptedJSON, string(keyStorePassword))
		if err != nil {
			logger.Fatal("could not decrypt operator private key keystore", zap.Error(err))
		}
		operatorPrivKey, err := keys.PrivateKeyFromBytes(decryptedKeystore)
		if err != nil {
			logger.Fatal("could not extract operator private key from file", zap.Error(err))
		}
		operatorPrivKeyText := base64.StdEncoding.EncodeToString(decryptedKeystore)

		return operatorPrivKey, operatorPrivKeyText
	}

	operatorPrivKey, err := keys.PrivateKeyFromString(cfg.OperatorPrivateKey)
	if err != nil {
		logger.Fatal("could not decode operator private key", zap.Error(err))
	}
	operatorPrivKeyText := cfg.OperatorPrivateKey

	return operatorPrivKey, operatorPrivKeyText
}
