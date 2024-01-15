package rsaencryption

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"

	testingspace "github.com/bloxapp/ssv/utils/rsaencryption/testingspace"
)

func TestGenerateKeys(t *testing.T) {
	_, skByte, err := GenerateKeys()
	require.NoError(t, err)

	sk, err := ConvertPemToPrivateKey(string(skByte))
	require.NoError(t, err)
	require.Equal(t, 2048, sk.N.BitLen())
	require.NoError(t, sk.Validate())
}

func TestDecodeKey(t *testing.T) {
	sk, err := ConvertPemToPrivateKey(testingspace.SkPem)
	require.NoError(t, err)

	hash, err := base64.StdEncoding.DecodeString(testingspace.EncryptedKeyBase64)
	require.NoError(t, err)

	key, err := DecodeKey(sk, hash)
	require.NoError(t, err)
	require.Equal(t, "626d6a13ae5b1458c310700941764f3841f279f9c8de5f4ba94abd01dc082517", string(key))
}

func TestExtractPrivateKey(t *testing.T) {
	_, skByte, err := GenerateKeys()
	require.NoError(t, err)

	sk, err := ConvertPemToPrivateKey(string(skByte))
	require.NoError(t, err)

	privKey := ExtractPrivateKey(sk)
	require.NotNil(t, privKey)
}

func TestExtractPublicKey(t *testing.T) {
	_, skByte, err := GenerateKeys()
	require.NoError(t, err)

	sk, err := ConvertPemToPrivateKey(string(skByte))
	require.NoError(t, err)

	pk, err := ExtractPublicKey(sk)
	require.NoError(t, err)
	require.NotNil(t, pk)
}

func TestPrivateKeyToByte(t *testing.T) {
	_, skByte, err := GenerateKeys()
	require.NoError(t, err)

	sk, err := ConvertPemToPrivateKey(string(skByte))
	require.NoError(t, err)

	b := PrivateKeyToByte(sk)
	require.NotNil(t, b)
	require.Greater(t, len(b), 1024)
}

func TestConvertEncryptedPemToPrivateKey(t *testing.T) {
	fileName := os.TempDir() + "/encrypted_private_key.json"
	defer func() {
		if err := os.Remove(fileName); err != nil {
			t.Logf("Could not delete encrypted private key file: %v", err)
		}
	}()
	password := "123123123"
	privateKey, err := rsa.GenerateKey(rand.Reader, keySize)
	require.NoError(t, err)

	privDER := x509.MarshalPKCS1PrivateKey(privateKey)

	privateBlock := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	}

	pemBytes := pem.EncodeToMemory(&privateBlock)

	encryptedData, err := keystorev4.New().Encrypt(pemBytes, password)
	require.NoError(t, err)

	encryptedJSON, err := json.Marshal(encryptedData)
	require.NoError(t, err)

	err = os.WriteFile(fileName, encryptedJSON, 0600)
	require.NoError(t, err)

	// Read and decrypt the private key
	encryptedJSON, err = os.ReadFile(fileName)
	require.NoError(t, err)

	key, err := ConvertEncryptedPemToPrivateKey(encryptedJSON, password)
	require.NoError(t, err)
	require.Equal(t, privateKey, key)

	// Convert encrypted PEM to private key.
	_, err = ConvertEncryptedPemToPrivateKey(encryptedJSON, password+"1")
	require.Error(t, err)

	// Test with incorrect password.
	_, err = ConvertEncryptedPemToPrivateKey(pemBytes, "")
	require.Error(t, err)
}
