package rsaencryption

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	rsatesting "github.com/ssvlabs/ssv/ssvsigner/rsaencryption/testingspace"
)

func TestGenerateKeys(t *testing.T) {
	_, skByte, err := GenerateRSAKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)
	require.Equal(t, 2048, sk.N.BitLen())
	require.NoError(t, sk.Validate())
}

func TestDecryptRSA(t *testing.T) {
	sk, err := PEMToPrivateKey([]byte(rsatesting.SkPem))
	require.NoError(t, err)

	hash, err := base64.StdEncoding.DecodeString(rsatesting.EncryptedKeyBase64)
	require.NoError(t, err)

	key, err := DecryptRSA(sk, hash)
	require.NoError(t, err)
	require.Equal(t, "626d6a13ae5b1458c310700941764f3841f279f9c8de5f4ba94abd01dc082517", string(key))
}

func TestPrivateKeyToBase64PEM(t *testing.T) {
	_, skByte, err := GenerateRSAKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)

	privKey := PrivateKeyToBase64PEM(sk)
	require.NotNil(t, privKey)
}

func TestPublicKeyToBase64PEM(t *testing.T) {
	_, skByte, err := GenerateRSAKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)

	pk, err := PublicKeyToBase64PEM(&sk.PublicKey)
	require.NoError(t, err)
	require.NotNil(t, pk)
}

func TestPrivateKeyToPEM(t *testing.T) {
	_, skByte, err := GenerateRSAKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)

	b := PrivateKeyToPEM(sk)
	require.NotNil(t, b)
	require.Greater(t, len(b), 1024)
}
