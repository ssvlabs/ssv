package keys

import (
	crand "crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsatesting"
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

// Helper function to generate a valid private key for tests.
func generateTestPrivateKey(t *testing.T) OperatorPrivateKey {
	t.Helper()

	privKey, err := GeneratePrivateKey()
	require.NoError(t, err, "Failed to generate private key")
	require.NotNil(t, privKey, "Generated private key is nil")

	return privKey
}

// Helper function to get a private key from the test PEM.
func getTestPrivateKeyFromPEM(t *testing.T) OperatorPrivateKey {
	t.Helper()

	privKey, err := PrivateKeyFromString(base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)))
	require.NoError(t, err, "Failed to parse private key")

	return privKey
}

func TestGeneratePrivateKey(t *testing.T) {
	t.Parallel()

	privKey1 := generateTestPrivateKey(t)
	privKey2 := generateTestPrivateKey(t)

	require.NotEqual(t, privKey1, privKey2, "Generated private keys should be different")
}

func TestEncryptDecrypt(t *testing.T) {
	t.Parallel()

	privKey := generateTestPrivateKey(t)
	pubKey := privKey.Public()

	originalText := []byte("test")
	encryptedText, err := pubKey.Encrypt(originalText)
	require.NoError(t, err, "Failed to encrypt text")

	decryptedText, err := privKey.Decrypt(encryptedText)
	require.NoError(t, err, "Failed to decrypt text")
	require.Equal(t, originalText, decryptedText, "Original and decrypted text do not match")
}

func TestPrivateKey_Decrypt_InvalidCiphertext(t *testing.T) {
	t.Parallel()

	privKey := generateTestPrivateKey(t)
	invalidCiphertext := []byte("invalid ciphertext")
	decrypted, err := privKey.Decrypt(invalidCiphertext)

	require.Nil(t, decrypted)
	require.ErrorContains(t, err, "decryption error")
}

func TestSignVerify(t *testing.T) {
	t.Parallel()

	privKey := generateTestPrivateKey(t)
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

func TestSign_Error(t *testing.T) {
	t.Parallel()

	privKey := &privateKey{privKey: &rsa.PrivateKey{
		PublicKey:   rsa.PublicKey{},
		D:           nil,
		Primes:      nil,
		Precomputed: rsa.PrecomputedValues{},
	}}

	_, err := privKey.Sign([]byte("test"))
	require.ErrorContains(t, err, "missing public modulus")
}

func TestBase64Encoding(t *testing.T) {
	t.Parallel()

	privKey := getTestPrivateKeyFromPEM(t)

	encodedPrivKey := privKey.Base64()
	require.NotEmpty(t, encodedPrivKey, "Encoded private key should not be empty")

	pubKey := privKey.Public()
	encodedPubKey, err := pubKey.Base64()

	require.NoError(t, err, "Failed to encode public key")
	require.NotEmpty(t, encodedPubKey, "Encoded public key should not be empty")

	decodedPrivKeyBytes, err := base64.StdEncoding.DecodeString(encodedPrivKey)
	require.NoError(t, err, "Encoded private key should be a valid Base64 string")

	_, err = base64.StdEncoding.DecodeString(encodedPubKey)
	require.NoError(t, err, "Encoded public key should be a valid Base64 string")

	require.Equal(t, privKey.Bytes(), decodedPrivKeyBytes, "Decoded private key bytes do not match original private key")
}

func TestHashing(t *testing.T) {
	t.Parallel()

	t.Run("StorageHash", func(t *testing.T) {
		t.Parallel()

		privKey := getTestPrivateKeyFromPEM(t)
		hash := privKey.StorageHash()
		require.NotEmpty(t, hash, "Storage hash should not be empty")
		require.Equal(t, "eaba9e5320c1ef8023d74103e6b6e9828afce89442f2755cde217c06ccacf74a", hash,
			"Storage hash does not match expected value")
	})

	t.Run("EKMHash", func(t *testing.T) {
		t.Parallel()

		privKey := getTestPrivateKeyFromPEM(t)
		hash := privKey.EKMHash()
		require.NotEmpty(t, hash, "EKM hash should not be empty")
		require.Equal(t, "6db24021c74d4f5784a0c1a6a519f9ffcb3996be5c0a3d9d4a6d8a567f9cc38a", hash,
			"EKM hash does not match expected value")
	})
}

func TestPublicKeyFromString(t *testing.T) {
	t.Parallel()

	pubKey, err := PublicKeyFromString(base64.StdEncoding.EncodeToString([]byte(sampleRSAPublicKey)))

	require.NoError(t, err, "Failed to parse public key")
	require.NotNil(t, pubKey, "Parsed public key is nil")
}

func TestPublicKeyFromString_Errors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		input         string
		expectedError string
		encoder       func([]byte) string
	}{
		{
			name:          "Base64DecodeError",
			input:         "invalid base64",
			expectedError: "illegal base64 data",
			encoder:       func(b []byte) string { return string(b) },
		},
		{
			name:          "PEMError",
			input:         "Not a valid PEM block for RSA public key",
			expectedError: "no PEM block found",
			encoder:       func(b []byte) string { return base64.StdEncoding.EncodeToString(b) },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pubKey, err := PublicKeyFromString(tc.encoder([]byte(tc.input)))
			require.Nil(t, pubKey)
			require.ErrorContains(t, err, tc.expectedError)
		})
	}
}

func TestPrivateKeyFromString_Errors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		input         string
		expectedError string
		encoder       func([]byte) string
	}{
		{
			name:          "Base64DecodeError",
			input:         "invalid base64",
			expectedError: "decode base64:",
			encoder:       func(b []byte) string { return string(b) },
		},
		{
			name:          "PEMError",
			input:         "Not a valid RSA key PEM block",
			expectedError: "pem to private key:",
			encoder:       func(b []byte) string { return base64.StdEncoding.EncodeToString(b) },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			privKey, err := PrivateKeyFromString(tc.encoder([]byte(tc.input)))
			require.Error(t, err)
			require.Nil(t, privKey)
			require.Contains(t, err.Error(), tc.expectedError)
		})
	}
}

func TestPublicKey_Base64_MalformedKey(t *testing.T) {
	t.Parallel()

	invalidPub := &publicKey{pubKey: &rsa.PublicKey{N: nil, E: 0}}

	s, err := invalidPub.Base64()
	require.Error(t, err)
	require.Empty(t, s)
}

func TestGeneratePrivateKey_ReaderError(t *testing.T) {
	origReader := crand.Reader
	defer func() { crand.Reader = origReader }()

	crand.Reader = &failingReader{}

	key, err := GeneratePrivateKey()

	require.Error(t, err)
	require.Nil(t, key)
	require.Contains(t, err.Error(), "failed to read random bytes")
}

type failingReader struct{}

func (r *failingReader) Read([]byte) (n int, err error) {
	return 0, fmt.Errorf("failed to read random bytes")
}
