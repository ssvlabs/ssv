package rsaencryption

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"os"
	"os/exec"
	"testing"

	testingspace "github.com/bloxapp/ssv/utils/rsaencryption/testingspace"
	"github.com/stretchr/testify/require"
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
	var tmpPrivateFile *os.File

	t.Run("CreateTempFile", func(t *testing.T) {
		var err error
		tmpPrivateFile, err = os.CreateTemp("", "private_key.pem")
		require.NoError(t, err)
	})

	defer func() {
		if err := os.Remove(tmpPrivateFile.Name()); err != nil {
			t.Logf("Could not delete temporary file: %v", err)
		}
		if err := os.Remove("encrypted_private_key.pem"); err != nil {
			t.Logf("Could not delete encrypted private key file: %v", err)
		}
	}()

	var generatedPrivateKey *rsa.PrivateKey
	t.Run("GeneratePrivateKey", func(t *testing.T) {
		var err error
		generatedPrivateKey, err = rsa.GenerateKey(rand.Reader, keySize)
		require.NoError(t, err)
	})

	t.Run("EncodePrivateKey", func(t *testing.T) {
		// Create a pem.Block with the private key.
		privDER := x509.MarshalPKCS1PrivateKey(generatedPrivateKey)
		privateBlock := pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: privDER,
		}

		// Encode the pem.Block with the private key into a temporary file.
		err := pem.Encode(tmpPrivateFile, &privateBlock)
		require.NoError(t, err)
		err = tmpPrivateFile.Close()
		require.NoError(t, err)
	})

	t.Run("RunOpenSSL", func(t *testing.T) {
		keystorePassword := "123123123"
		// Encrypt the private key file using OpenSSL.
		passString := "pass:" + keystorePassword
		cmd := exec.Command("openssl", "rsa", "-aes256", "-in", tmpPrivateFile.Name(), "-out", "encrypted_private_key.pem", "-passout", passString)
		err := cmd.Run()
		require.NoError(t, err)
	})

	t.Run("ConvertEncryptedPemToPrivateKey", func(t *testing.T) {
		keystorePassword := "123123123"
		// Read the encrypted private key file.
		pemBytes, err := os.ReadFile("encrypted_private_key.pem")
		require.NoError(t, err)
		hash := sha256.Sum256(pemBytes)
		t.Logf("SHA-256: %s", hex.EncodeToString(hash[:]))

		// Convert encrypted PEM to private key.
		privateKey, err := ConvertEncryptedPemToPrivateKey(pemBytes, keystorePassword)
		require.NoError(t, err)
		require.Equal(t, privateKey, generatedPrivateKey)

		// Test with incorrect password.
		_, err = ConvertEncryptedPemToPrivateKey(pemBytes, keystorePassword+"1")
		require.Error(t, err)

		_, err = ConvertEncryptedPemToPrivateKey(pemBytes, "")
		require.Error(t, err)
	})
}
