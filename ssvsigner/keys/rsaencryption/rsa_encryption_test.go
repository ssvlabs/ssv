package rsaencryption

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsatesting"
)

func TestGenerateKeys(t *testing.T) {
	t.Parallel()

	_, skByte, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)
	require.Equal(t, 2048, sk.N.BitLen())
	require.NoError(t, sk.Validate())
}

func TestDecryptRSA(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		ciphertext    []byte
		expectedError string
		expectedKey   string
	}{
		{
			name:          "Success",
			ciphertext:    nil, // Will be set in the test
			expectedError: "",
			expectedKey:   "626d6a13ae5b1458c310700941764f3841f279f9c8de5f4ba94abd01dc082517",
		},
		{
			name:          "Error",
			ciphertext:    []byte("invalid ciphertext"),
			expectedError: "decryption error",
			expectedKey:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var sk *rsa.PrivateKey
			var err error
			var data []byte

			if tc.ciphertext == nil {
				sk, err = PEMToPrivateKey([]byte(rsatesting.PrivKeyPEM))
				require.NoError(t, err)

				data, err = base64.StdEncoding.DecodeString(rsatesting.PrivKeyBase64)
				require.NoError(t, err)
			} else {
				_, skPem, err := GenerateKeyPairPEM()
				require.NoError(t, err)

				sk, err = PEMToPrivateKey(skPem)
				require.NoError(t, err)

				data = tc.ciphertext
			}

			key, err := Decrypt(sk, data)

			if tc.expectedError != "" {
				require.ErrorContains(t, err, tc.expectedError)
				require.Nil(t, key)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedKey, string(key))
			}
		})
	}
}

func TestPrivateKeyEncoding(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		testFunc func(t *testing.T, sk *rsa.PrivateKey)
	}{
		{
			name: "PrivateKeyToBase64PEM",
			testFunc: func(t *testing.T, sk *rsa.PrivateKey) {
				privKey := PrivateKeyToBase64PEM(sk)
				require.NotNil(t, privKey)
			},
		},
		{
			name: "PrivateKeyToBytes",
			testFunc: func(t *testing.T, sk *rsa.PrivateKey) {
				der := PrivateKeyToBytes(sk)
				require.NotNil(t, der)
				require.Greater(t, len(der), 0)
			},
		},
		{
			name: "PrivateKeyToPEM",
			testFunc: func(t *testing.T, sk *rsa.PrivateKey) {
				b := PrivateKeyToPEM(sk)
				require.NotNil(t, b)
				require.Greater(t, len(b), 1024)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, skByte, err := GenerateKeyPairPEM()
			require.NoError(t, err)

			sk, err := PEMToPrivateKey(skByte)
			require.NoError(t, err)

			tc.testFunc(t, sk)
		})
	}
}

func TestPublicKeyToBase64PEM(t *testing.T) {
	t.Parallel()

	_, skByte, err := GenerateKeyPairPEM()
	require.NoError(t, err)

	sk, err := PEMToPrivateKey(skByte)
	require.NoError(t, err)

	pk, err := PublicKeyToBase64PEM(&sk.PublicKey)
	require.NoError(t, err)
	require.NotNil(t, pk)
}

func TestPEMToPublicKey(t *testing.T) {
	testCases := []struct {
		name          string
		input         []byte
		expectedError string
	}{
		{
			name:          "Success",
			input:         nil, // Will be set in the test
			expectedError: "",
		},
		{
			name: "NoBlock",
			// Not a valid PEM (no "-----BEGIN" line).
			input:         []byte("Not a PEM block"),
			expectedError: "no PEM block found",
		},
		{
			name: "InvalidDER",
			// Syntactically valid PEM block (so pem.Decode is not nil),
			// but the DER bytes inside are wrong. This should trigger the
			// "invalid public key DER: ..." error rather than a PEM or type check error.
			input: []byte(`-----BEGIN RSA PUBLIC KEY-----
VGhpcyBpcyBub3QgdmFsaWQgREVSIGZvciBhIFJTQSBwdWJsaWMga2V5
-----END RSA PUBLIC KEY-----`),
			expectedError: "invalid public key DER:",
		},
		{
			name: "NotRSA",
			// A P-256 ECDSA public key in uncompressed format (PEM PKIX).
			// It will be parsed it as *ecdsa.PublicKey,
			// failing the pub.(*rsa.PublicKey) assertion and triggering
			// the "unknown type of public key" error.
			input: []byte(`-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE49sXVGRUwdDW4f4xoBJvpKtxqe/3
jLvFl/RVaMnDNAPQFCdLjkFmTk0BKTttpYHcP2jBeZXaVZGgKbawXiGm3g==
-----END PUBLIC KEY-----`),
			expectedError: "unknown type of public key",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.input
			if tc.name == "Success" {
				pkPem, _, err := GenerateKeyPairPEM()
				require.NoError(t, err)
				input = pkPem
			}

			pub, err := PEMToPublicKey(input)

			if tc.expectedError == "" {
				require.NoError(t, err)
				require.NotNil(t, pub)
				require.Equal(t, 2048, pub.N.BitLen())
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError)
			}
		})
	}
}

func TestPEMToPrivateKey(t *testing.T) {
	testCases := []struct {
		name          string
		input         []byte
		expectedError string
	}{
		{
			name:          "Success",
			input:         nil, // Will be set to a valid PEM in the test
			expectedError: "",
		},
		{
			name:          "ErrorNoBlock",
			input:         []byte("not PEM"),
			expectedError: "no PEM block found",
		},
		{
			name: "ErrorUnexpectedType",
			// A valid PEM block, but the block type is not "RSA PRIVATE KEY"
			input: []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIPW2dSzFD9GGyNltyj/suRyV + dummy data for your environment
-----END EC PRIVATE KEY-----`),
			expectedError: "unexpected PEM block type",
		},
		{
			name: "ErrorParse",
			// This block claims to be RSA PRIVATE KEY but the DER bytes inside are invalid
			input: []byte(`-----BEGIN RSA PRIVATE KEY-----
dGhpcyBpcyBub3QgdmFsaWQgZW5jb2RlZA==
-----END RSA PRIVATE KEY-----`),
			expectedError: "parse private key",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.input
			if tc.name == "Success" {
				_, skPEM, err := GenerateKeyPairPEM()
				require.NoError(t, err)
				input = skPEM
			}

			sk, err := PEMToPrivateKey(input)

			if tc.expectedError == "" {
				require.NoError(t, err)
				require.NotNil(t, sk)
				require.Equal(t, 2048, sk.N.BitLen())
				require.NoError(t, sk.Validate(), "Expected private key to validate successfully")
			} else {
				require.Error(t, err)
				require.Nil(t, sk)
				require.Contains(t, err.Error(), tc.expectedError)
			}
		})
	}
}

func TestGenerateRSAKeyPairPEM(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		pubPEM, privPEM, err := GenerateKeyPairPEM()
		require.NoError(t, err, "Expected generating RSA key pair to succeed")
		require.NotEmpty(t, pubPEM)
		require.NotEmpty(t, privPEM)

		pubKey, err := PEMToPublicKey(pubPEM)
		require.NoError(t, err)
		require.NotNil(t, pubKey)
		require.Equal(t, 2048, pubKey.N.BitLen())

		privKey, err := PEMToPrivateKey(privPEM)
		require.NoError(t, err)
		require.NotNil(t, privKey)
		require.Equal(t, 2048, privKey.N.BitLen())
		require.NoError(t, privKey.Validate(), "Private key should validate")
	})

	t.Run("Error", func(t *testing.T) {
		origReader := rand.Reader
		defer func() { rand.Reader = origReader }()

		// always fail the random reader
		rand.Reader = &failingReader{}

		pubPEM, privPEM, err := GenerateKeyPairPEM()
		require.Error(t, err)
		require.Contains(t, err.Error(), "generate RSA key:")
		require.Nil(t, pubPEM)
		require.Nil(t, privPEM)
	})
}

func TestHashKeyBytes(t *testing.T) {
	t.Parallel()

	data := []byte("test key")
	hash := HashKeyBytes(data)

	require.Equal(t, "fa2bdca424f01f01ffb48df93acc35d439c7fd331a1a7fba6ac2fd83aa9ab31a", hash)
}

func TestPublicKeyErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		testFunc func(t *testing.T, pubKey *rsa.PublicKey)
	}{
		{
			name: "PublicKeyToPEM_Error",
			testFunc: func(t *testing.T, pubKey *rsa.PublicKey) {
				_, err := PublicKeyToPEM(pubKey)
				require.Error(t, err)
				require.Contains(t, err.Error(), "marshal public key")
			},
		},
		{
			name: "PublicKeyToBase64PEM_Error",
			testFunc: func(t *testing.T, pubKey *rsa.PublicKey) {
				result, err := PublicKeyToBase64PEM(pubKey)
				require.Error(t, err)
				require.Empty(t, result)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			malformedPubKey := &rsa.PublicKey{
				N: nil,
				E: 0,
			}

			tc.testFunc(t, malformedPubKey)
		})
	}
}

type failingReader struct{}

func (r *failingReader) Read([]byte) (n int, err error) {
	return 0, fmt.Errorf("failed to read random bytes")
}
