package keys

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	blsu "github.com/protolambda/bls12-381-util"
	"github.com/ssvlabs/eth2-key-manager/encryptor/keystorev4"
)

func LoadOperatorKeystore(encryptedPrivateKeyFile, passwordFile string) (OperatorPrivateKey, error) {
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

	decryptedKeystore, err := DecryptKeystore(encryptedJSON, string(keyStorePassword))
	if err != nil {
		return nil, fmt.Errorf("could not decrypt operator private key keystore: %w", err)
	}
	operatorPrivKey, err := PrivateKeyFromBytes(decryptedKeystore)
	if err != nil {
		return nil, fmt.Errorf("could not extract operator private key from file: %w", err)
	}

	return operatorPrivKey, nil
}

type Keystore map[string]any

func GenerateShareKeystore(sharePrivateKey []byte, passphrase string) (Keystore, error) {
	sharePrivateKeyBytes, err := hex.DecodeString(strings.TrimPrefix(string(sharePrivateKey), "0x"))
	if err != nil {
		return Keystore{}, fmt.Errorf("could not decode share private key %s: %w", string(sharePrivateKey), err)
	}

	sharePrivateKeyArr := [32]byte(sharePrivateKeyBytes)

	sharePrivBLS := &blsu.SecretKey{}
	if err = sharePrivBLS.Deserialize(&sharePrivateKeyArr); err != nil {
		return Keystore{}, fmt.Errorf("share private key to BLS: %w", err)
	}

	sharePubKey, err := blsu.SkToPk(sharePrivBLS)
	if err != nil {
		return Keystore{}, fmt.Errorf("extract BLS public key: %w", err)
	}

	serializedSharePubKey := sharePubKey.Serialize()
	sharePubKeyHex := "0x" + hex.EncodeToString(serializedSharePubKey[:])

	keystoreCrypto, err := keystorev4.New().Encrypt(sharePrivateKeyBytes, passphrase)
	if err != nil {
		return Keystore{}, fmt.Errorf("encrypt private key: %w", err)
	}

	keystore := Keystore{
		"crypto":  keystoreCrypto,
		"pubkey":  sharePubKeyHex,
		"version": 4,
		"uuid":    uuid.New().String(),
		"path":    "m/12381/3600/0/0/0",
	}

	return keystore, nil
}

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
