package rsaencryption

import (
	"crypto/rsa"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsatesting"
)

func TestGenerateKeys(t *testing.T) {
	_, skByte, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)
	require.Equal(t, 2048, sk.N.BitLen())
	require.NoError(t, sk.Validate())
}

func TestDecryptRSA(t *testing.T) {
	sk, err := PEMToPrivateKey([]byte(rsatesting.PrivKeyPEM))
	require.NoError(t, err)

	data, err := base64.StdEncoding.DecodeString(rsatesting.PrivKeyBase64)
	require.NoError(t, err)

	key, err := Decrypt(sk, data)
	require.NoError(t, err)
	require.Equal(t, "626d6a13ae5b1458c310700941764f3841f279f9c8de5f4ba94abd01dc082517", string(key))
}

func TestDecryptRSA_Error(t *testing.T) {
	_, skPem, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skPem)
	require.NoError(t, err)

	ciphertext := []byte("invalid ciphertext")
	_, err = Decrypt(sk, ciphertext)
	require.ErrorContains(t, err, "decryption error")
}

func TestPrivateKeyToBase64PEM(t *testing.T) {
	_, skByte, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)

	privKey := PrivateKeyToBase64PEM(sk)
	require.NotNil(t, privKey)
}

func TestPrivateKeyToBytes(t *testing.T) {
	_, skPEM, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skPEM)
	require.NoError(t, err)

	der := PrivateKeyToBytes(sk)
	require.NotNil(t, der)
	require.Greater(t, len(der), 0)
}

func TestPublicKeyToBase64PEM(t *testing.T) {
	_, skByte, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)

	pk, err := PublicKeyToBase64PEM(&sk.PublicKey)
	require.NoError(t, err)
	require.NotNil(t, pk)
}

func TestPrivateKeyToPEM(t *testing.T) {
	_, skByte, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)

	b := PrivateKeyToPEM(sk)
	require.NotNil(t, b)
	require.Greater(t, len(b), 1024)
}

func TestPEMToPublicKey_Success(t *testing.T) {
	pkPem, _, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	pub, err := PEMToPublicKey(pkPem)
	require.NoError(t, err)
	require.NotNil(t, pub)
	require.Equal(t, 2048, pub.N.BitLen())
}

func TestPEMToPublicKey_ErrorNoBlock(t *testing.T) {
	// Not a valid PEM (no "-----BEGIN" line).
	notPEM := []byte("Not a PEM block")

	_, err := PEMToPublicKey(notPEM)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no PEM block found")
}

func TestPEMToPublicKey_InvalidDER(t *testing.T) {
	// Syntactically valid PEM block (so pem.Decode is not nil),
	// but the DER bytes inside are wrong. This should trigger the
	// "invalid public key DER: ..." error rather than a PEM or type check error.
	invalidDER := []byte(`-----BEGIN RSA PUBLIC KEY-----
VGhpcyBpcyBub3QgdmFsaWQgREVSIGZvciBhIFJTQSBwdWJsaWMga2V5
-----END RSA PUBLIC KEY-----`)

	_, err := PEMToPublicKey(invalidDER)
	require.Error(t, err)

	require.Contains(t, err.Error(), "invalid public key DER:")
}

func TestPEMToPublicKey_ErrorNotRSA(t *testing.T) {
	// A P-256 ECDSA public key in uncompressed format (PEM PKIX).
	// It will be parsed it as *ecdsa.PublicKey,
	// failing the pub.(*rsa.PublicKey) assertion and triggering
	// the "unknown type of public key" error.
	ecdsaPublicPEM := []byte(`-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE49sXVGRUwdDW4f4xoBJvpKtxqe/3
jLvFl/RVaMnDNAPQFCdLjkFmTk0BKTttpYHcP2jBeZXaVZGgKbawXiGm3g==
-----END PUBLIC KEY-----`)

	_, err := PEMToPublicKey(ecdsaPublicPEM)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown type of public key")
}

func TestPEMToPrivateKey_Success(t *testing.T) {
	_, skPEM, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skPEM)
	require.NoError(t, err)

	require.NotNil(t, sk)
	require.Equal(t, 2048, sk.N.BitLen())
	require.NoError(t, sk.Validate(), "Expected private key to validate successfully")
}

func TestPEMToPrivateKey_ErrorNoBlock(t *testing.T) {
	badData := []byte("not PEM")

	sk, err := PEMToPrivateKey(badData)
	require.Error(t, err)
	require.Nil(t, sk)
	require.Contains(t, err.Error(), "no PEM block found")
}

func TestPEMToPrivateKey_ErrorUnexpectedType(t *testing.T) {
	// A valid PEM block, but the block type is not "RSA PRIVATE KEY"
	ecKey := []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIPW2dSzFD9GGyNltyj/suRyV + dummy data for your environment
-----END EC PRIVATE KEY-----`)

	// We only need the block type mismatch, so the content can be partial or wrong.
	sk, err := PEMToPrivateKey(ecKey)
	require.Error(t, err)
	require.Nil(t, sk)
	require.Contains(t, err.Error(), "unexpected PEM block type")
}

func TestPEMToPrivateKey_ErrorParse(t *testing.T) {
	// This block claims to be RSA PRIVATE KEY but the DER bytes inside are invalid
	invalidPEM := []byte(`-----BEGIN RSA PRIVATE KEY-----
dGhpcyBpcyBub3QgdmFsaWQgZW5jb2RlZA==
-----END RSA PRIVATE KEY-----`)

	sk, err := PEMToPrivateKey(invalidPEM)
	require.Error(t, err)
	require.Nil(t, sk)
	require.Contains(t, err.Error(), "parse private key")
}

func TestGenerateRSAKeyPairPEM_Success(t *testing.T) {
	pubPEM, privPEM, err := GenerateKeyPairPEM()
	require.NoError(t, err, "Expected generating RSA key pair to succeed")
	require.NotEmpty(t, pubPEM)
	require.NotEmpty(t, privPEM)

	// Parse them back to confirm validity
	pubKey, err := PEMToPublicKey(pubPEM)
	require.NoError(t, err)
	require.NotNil(t, pubKey)
	require.Equal(t, 2048, pubKey.N.BitLen())

	privKey, err := PEMToPrivateKey(privPEM)
	require.NoError(t, err)
	require.NotNil(t, privKey)
	require.Equal(t, 2048, privKey.N.BitLen())
	require.NoError(t, privKey.Validate(), "Private key should validate")
}

func TestHashKeyBytes(t *testing.T) {
	data := []byte("test key")
	hash := HashKeyBytes(data)

	require.Equal(t, "fa2bdca424f01f01ffb48df93acc35d439c7fd331a1a7fba6ac2fd83aa9ab31a", hash)
}

func TestPublicKeyToPEM_Error(t *testing.T) {
	malformedPubKey := &rsa.PublicKey{
		N: nil,
		E: 0,
	}

	_, err := PublicKeyToPEM(malformedPubKey)
	require.Error(t, err)
	require.Contains(t, err.Error(), "marshal public key")
}

func TestPublicKeyToBase64PEM_Error(t *testing.T) {
	malformedPubKey := &rsa.PublicKey{
		N: nil,
		E: 0,
	}

	result, err := PublicKeyToBase64PEM(malformedPubKey)
	require.Error(t, err)
	require.Empty(t, result)
}
