package keys_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/operator/keys"
	rsatesting "github.com/ssvlabs/ssv/utils/rsaencryption/testingspace"
)

const sampleRSAPublicKey = `
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEArVzXJ1Xm3YIY8QYs2MFL
O/FY8M5BZ5GtCgFVdAkhDY2S3n6Q0X8gY9K+9YiQ6ZrLGfrbhUQ9D8q2JY9KZpQ1
X3sMfJ3TYIjdbq6KUZ0C8fLIft8E0qPMIYlGjjbYKjLC3MBq3Md0K9V7jW7NAjIe
A5CjGHlTlI5n8YUZBQhp2zKDHOFThq4Mh8BiWC5LdiJF1F4fW2JzruBHZMGxK4EX
E3y7OUL8IkYI3RFm7L4yx1M2FAhkQdqBP5LjCObTbk27R8nW5g4pvlrf9GPpDaV9
UH3pIsH5oiLqSi6q5Y4yAgL1MVzF3eeZ5kPVwLzopY6B4KjP2Lvb9Kbw5tz4gjx2
QwIDAQAB
-----END PUBLIC KEY-----
`

func TestGeneratePrivateKey(t *testing.T) {
	privKey1, err := keys.GeneratePrivateKey()
	require.NoError(t, err, "Failed to generate private key 1")
	require.NotNil(t, privKey1, "Generated private key 1 is nil")

	privKey2, err := keys.GeneratePrivateKey()
	require.NoError(t, err, "Failed to generate private key 2")
	require.NotNil(t, privKey2, "Generated private key 2 is nil")

	require.NotEqual(t, privKey1, privKey2, "Generated private keys 1 and 2 are same")
}

func TestEncryptDecrypt(t *testing.T) {
	privKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err, "Failed to generate private key")

	pubKey := privKey.Public()

	originalText := []byte("test")
	encryptedText, err := pubKey.Encrypt(originalText)
	require.NoError(t, err, "Failed to encrypt text")

	decryptedText, err := privKey.Decrypt(encryptedText)
	require.NoError(t, err, "Failed to decrypt text")
	require.Equal(t, originalText, decryptedText, "Original and decrypted text do not match")
}

func TestSignVerify(t *testing.T) {
	privKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err, "Failed to generate private key")

	pubKey := privKey.Public()

	dataToSign := []byte("test1")
	signature, err := privKey.Sign(dataToSign)
	require.NoError(t, err, "Failed to sign data")

	err = pubKey.Verify(dataToSign, signature)
	require.NoError(t, err, "Failed to verify signature")

	alteredData := []byte("test2")
	err = pubKey.Verify(alteredData, signature)
	require.Error(t, err, "Verification should fail for altered data")
}

func TestBase64Encoding(t *testing.T) {
	privKey, err := keys.PrivateKeyFromString(base64.StdEncoding.EncodeToString([]byte(rsatesting.SkPem)))
	require.NoError(t, err, "Failed to parse private key")

	encodedPrivKey := privKey.Base64()
	require.NotEmpty(t, encodedPrivKey, "Encoded private key should not be empty")

	pubKey := privKey.Public()
	encodedPubKey, err := pubKey.Base64()
	require.NoError(t, err, "Failed to encode public key")
	require.NotEmpty(t, encodedPubKey, "Encoded public key should not be empty")

	decodedPrivKeyBytes, err := base64.StdEncoding.DecodeString(string(encodedPrivKey))
	require.NoError(t, err, "Encoded private key should be a valid Base64 string")

	_, err = base64.StdEncoding.DecodeString(string(encodedPubKey))
	require.NoError(t, err, "Encoded public key should be a valid Base64 string")

	require.Equal(t, privKey.Bytes(), decodedPrivKeyBytes, "Decoded private key bytes do not match original private key")
}

func TestStorageHash(t *testing.T) {
	privKey, err := keys.PrivateKeyFromString(base64.StdEncoding.EncodeToString([]byte(rsatesting.SkPem)))
	require.NoError(t, err, "Failed to parse private key")

	hash, err := privKey.StorageHash()
	require.NoError(t, err, "Failed to compute storage hash")
	require.NotEmpty(t, hash, "Storage hash should not be empty")
	require.Equal(t, "eaba9e5320c1ef8023d74103e6b6e9828afce89442f2755cde217c06ccacf74a", hash, "Storage hash does not match expected value")
}

func TestEKMHash(t *testing.T) {
	privKey, err := keys.PrivateKeyFromString(base64.StdEncoding.EncodeToString([]byte(rsatesting.SkPem)))
	require.NoError(t, err, "Failed to parse private key")

	hash, err := privKey.EKMHash()
	require.NoError(t, err, "Failed to compute EKM hash")
	require.NotEmpty(t, hash, "EKM hash should not be empty")
	require.Equal(t, "6db24021c74d4f5784a0c1a6a519f9ffcb3996be5c0a3d9d4a6d8a567f9cc38a", hash, "EKM hash does not match expected value")
}

func TestPublicKeyFromString(t *testing.T) {
	pubKey, err := keys.PublicKeyFromString(base64.StdEncoding.EncodeToString([]byte(sampleRSAPublicKey)))
	require.NoError(t, err, "Failed to parse public key")
	require.NotNil(t, pubKey, "Parsed public key is nil")
}
