package rsaencryption

import (
	testingspace "github.com/bloxapp/ssv/utils/rsaencryption/testingspace"
	"github.com/stretchr/testify/require"
	"testing"
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
	key, err := DecodeKey(sk, testingspace.EncryptedKeyBase64)
	require.NoError(t, err)
	require.Equal(t, "626d6a13ae5b1458c310700941764f3841f279f9c8de5f4ba94abd01dc082517", key)
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