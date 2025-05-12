package keystore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/google/uuid"
	"github.com/herumi/bls-eth-go-binary/bls"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

const (
	keystoreDerivationPath = "m/12381/3600/0/0/0"
)

// DecryptKeystore decrypts a keystore JSON file using the provided password.
func DecryptKeystore(encryptedJSONData []byte, password string) ([]byte, error) {
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("password required for decrypting keystore")
	}

	// Unmarshal the JSON-encoded data
	var data map[string]interface{}
	if err := json.Unmarshal(encryptedJSONData, &data); err != nil {
		return nil, fmt.Errorf("parse JSON data: %w", err)
	}

	// Decrypt the private key using keystorev4
	decryptedBytes, err := keystorev4.New().Decrypt(data, password)
	if err != nil {
		return nil, fmt.Errorf("decrypt private key: %w", err)
	}

	return decryptedBytes, nil
}

// EncryptKeystore encrypts a private key using the provided password, adds in the public key and returns the encrypted keystore JSON data.
func EncryptKeystore(privkey []byte, pubKeyBase64, password string) ([]byte, error) {
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("password required for encrypting keystore")
	}

	// Encrypt the private key using keystorev4
	encryptedKeystoreJSON, err := keystorev4.New().Encrypt(privkey, password)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}

	encryptedKeystoreJSON["pubkey"] = pubKeyBase64

	encryptedData, err := json.Marshal(encryptedKeystoreJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal encrypted keystore: %w", err)
	}

	return encryptedData, nil
}

func LoadOperatorKeystore(encryptedPrivateKeyFile, passwordFile string) (keys.OperatorPrivateKey, error) {
	//nolint: gosec
	encryptedJSON, err := os.ReadFile(encryptedPrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("read keystore file: %w", err)
	}

	//nolint: gosec
	keyStorePassword, err := os.ReadFile(passwordFile)
	if err != nil {
		return nil, fmt.Errorf("read password file: %w", err)
	}

	if len(bytes.TrimSpace(keyStorePassword)) == 0 {
		return nil, errors.New("password file is empty")
	}

	decryptedKeystore, err := DecryptKeystore(encryptedJSON, string(keyStorePassword))
	if err != nil {
		return nil, fmt.Errorf("decrypt operator private key keystore: %w", err)
	}

	operatorPrivKey, err := keys.PrivateKeyFromBytes(decryptedKeystore)
	if err != nil {
		return nil, fmt.Errorf("extract operator private key from keystore: %w", err)
	}

	return operatorPrivKey, nil
}

func GenerateShareKeystore(sharePrivateKey *bls.SecretKey, sharePublicKey phase0.BLSPubKey, passphrase string) (map[string]any, error) {
	keystoreCrypto, err := keystorev4.New().Encrypt(sharePrivateKey.Serialize(), passphrase)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}

	return map[string]any{
		"crypto": keystoreCrypto,
		// web3signer uses "pubkey" in lowercase:
		// - https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Keymanager/operation/KEYMANAGER_IMPORT
		// - https://github.com/Consensys/web3signer/blob/bdf1e72169bf7f6800979b0c765647feea172dc5/core/src/main/java/tech/pegasys/web3signer/core/service/http/handlers/keymanager/imports/ImportKeystoresHandler.java#L234
		"pubkey":  sharePublicKey.String(),
		"version": 4,
		"uuid":    uuid.New().String(),
		"path":    keystoreDerivationPath,
	}, nil
}
