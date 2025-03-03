package keystore

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/herumi/bls-eth-go-binary/bls"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"

	"github.com/ssvlabs/ssv/operator/keys"
)

// DecryptKeystore decrypts a keystore JSON file using the provided password.
func DecryptKeystore(encryptedJSONData []byte, password string) ([]byte, error) {
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("Password required for decrypting keystore")
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
		return nil, fmt.Errorf("Password required for encrypting keystore")
	}

	// Encrypt the private key using keystorev4
	encryptedKeystoreJSON, err := keystorev4.New().Encrypt(privkey, password)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}

	encryptedKeystoreJSON["pubKey"] = pubKeyBase64

	encryptedData, err := json.Marshal(encryptedKeystoreJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal encrypted keystore: %w", err)
	}

	return encryptedData, nil
}

func LoadOperatorKeystore(encryptedPrivateKeyFile, passwordFile string) (keys.OperatorPrivateKey, error) {
	// nolint: gosec
	encryptedJSON, err := os.ReadFile(encryptedPrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("could not read PEM file: %w", err)
	}

	// nolint: gosec
	keyStorePassword, err := os.ReadFile(passwordFile)
	if err != nil {
		return nil, fmt.Errorf("could not read password file: %w", err)
	}

	if len(bytes.TrimSpace(keyStorePassword)) == 0 {
		return nil, fmt.Errorf("password file is empty")
	}

	decryptedKeystore, err := DecryptKeystore(encryptedJSON, string(keyStorePassword))
	if err != nil {
		return nil, fmt.Errorf("could not decrypt operator private key keystore: %w", err)
	}
	operatorPrivKey, err := keys.PrivateKeyFromBytes(decryptedKeystore)
	if err != nil {
		return nil, fmt.Errorf("could not extract operator private key from file: %w", err)
	}

	return operatorPrivKey, nil
}

type Keystore map[string]any

func GenerateShareKeystore(sharePrivateKey, sharePublicKey []byte, passphrase string) (Keystore, error) {
	keystoreCrypto, err := keystorev4.New().Encrypt(sharePrivateKey, passphrase)
	if err != nil {
		return Keystore{}, fmt.Errorf("encrypt private key: %w", err)
	}

	sharePrivBLS := &bls.SecretKey{}
	if err = sharePrivBLS.Deserialize(sharePrivateKey); err != nil {
		return Keystore{}, fmt.Errorf("share private key to BLS: %w", err)
	}

	keystoreData := Keystore{
		"crypto":  keystoreCrypto,
		"pubkey":  "0x" + hex.EncodeToString(sharePublicKey),
		"version": 4,
		"uuid":    uuid.New().String(),
		"path":    "m/12381/3600/0/0/0",
	}

	return keystoreData, nil
}
