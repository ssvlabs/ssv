package rsaencryption

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
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
	keystorePassword := "123123123"
	generatedPrivateKey, err := rsa.GenerateKey(rand.Reader, keySize)
	require.NoError(t, err)
	privDER := x509.MarshalPKCS1PrivateKey(generatedPrivateKey)

	block, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", privDER, []byte(keystorePassword), x509.PEMCipherAES256)
	require.NoError(t, err)

	var pemData bytes.Buffer
	err = pem.Encode(&pemData, block)
	require.NoError(t, err)

	// Now pemData.String() contains your PEM data as a string
	pemString := pemData.String()

	privateKey, err := ConvertEncryptedPemToPrivateKey(pemString, keystorePassword)
	require.NoError(t, err)
	require.Equal(t, privateKey, generatedPrivateKey)

	// fails positive
	_, err = ConvertEncryptedPemToPrivateKey(pemString, keystorePassword+"1")
	require.Error(t, err)

	_, err = ConvertEncryptedPemToPrivateKey(pemString)
	require.Error(t, err)
}
