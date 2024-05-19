package rsaencryption

import (
	"encoding/base64"
	"testing"

	testingspace "github.com/ssvlabs/ssv/utils/rsaencryption/testingspace"
	"github.com/stretchr/testify/require"
)

func TestGenerateKeys(t *testing.T) {
	_, skByte, err := GenerateKeys()
	require.NoError(t, err)

	sk, err := PemToPrivateKey(skByte)
	require.NoError(t, err)
	require.Equal(t, 2048, sk.N.BitLen())
	require.NoError(t, sk.Validate())
}

func TestDecodeKey(t *testing.T) {
	sk, err := PemToPrivateKey([]byte(testingspace.SkPem))
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

	sk, err := PemToPrivateKey(skByte)
	require.NoError(t, err)

	privKey := ExtractPrivateKey(sk)
	require.NotNil(t, privKey)
}

func TestExtractPublicKey(t *testing.T) {
	_, skByte, err := GenerateKeys()
	require.NoError(t, err)

	sk, err := PemToPrivateKey(skByte)
	require.NoError(t, err)

	pk, err := ExtractPublicKey(&sk.PublicKey)
	require.NoError(t, err)
	require.NotNil(t, pk)
}

func TestPrivateKeyToByte(t *testing.T) {
	_, skByte, err := GenerateKeys()
	require.NoError(t, err)

	sk, err := PemToPrivateKey(skByte)
	require.NoError(t, err)

	b := PrivateKeyToByte(sk)
	require.NotNil(t, b)
	require.Greater(t, len(b), 1024)
}
