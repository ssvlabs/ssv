package web3signer

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithRequestTimeout(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		timeout     time.Duration
		expectError bool
	}{
		{
			name:        "valid timeout",
			timeout:     5 * time.Second,
			expectError: false,
		},
		{
			name:        "zero timeout",
			timeout:     0,
			expectError: false,
		},
		{
			name:        "negative timeout",
			timeout:     -1 * time.Second,
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w3s := &Web3Signer{
				httpClient: &http.Client{
					Timeout: DefaultRequestTimeout,
				},
			}

			opt := WithRequestTimeout(tc.timeout)
			err := opt(w3s)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.timeout, w3s.httpClient.Timeout)
			}
		})
	}
}

func TestWithTLS(t *testing.T) {
	t.Parallel()

	cert := tls.Certificate{
		Certificate: [][]byte{[]byte("test-certificate")},
		PrivateKey:  []byte("test-private-key"),
	}

	testCases := []struct {
		name                string
		certificate         tls.Certificate
		trustedFingerprints map[string]string
		expectError         bool
		validateClient      func(t *testing.T, w3s *Web3Signer)
	}{
		{
			name:                "valid certificate with fingerprints",
			certificate:         cert,
			trustedFingerprints: map[string]string{"example.com:443": "aa:bb:cc:dd"},
			expectError:         false,
			validateClient: func(t *testing.T, w3s *Web3Signer) {
				transport, ok := w3s.httpClient.Transport.(*http.Transport)
				require.True(t, ok)
				require.NotNil(t, transport.TLSClientConfig)

				assert.Len(t, transport.TLSClientConfig.Certificates, 1)
				assert.NotNil(t, transport.TLSClientConfig.VerifyConnection)
			},
		},
		{
			name:                "valid certificate without fingerprints",
			certificate:         cert,
			trustedFingerprints: nil,
			expectError:         false,
			validateClient: func(t *testing.T, w3s *Web3Signer) {
				transport, ok := w3s.httpClient.Transport.(*http.Transport)
				require.True(t, ok)
				require.NotNil(t, transport.TLSClientConfig)

				assert.Len(t, transport.TLSClientConfig.Certificates, 1)
				assert.Nil(t, transport.TLSClientConfig.VerifyConnection)
			},
		},
		{
			name:                "no certificate with fingerprints",
			certificate:         tls.Certificate{},
			trustedFingerprints: map[string]string{"example.com:443": "aa:bb:cc:dd"},
			expectError:         false,
			validateClient: func(t *testing.T, w3s *Web3Signer) {
				transport, ok := w3s.httpClient.Transport.(*http.Transport)
				require.True(t, ok)
				require.NotNil(t, transport.TLSClientConfig)

				assert.Empty(t, transport.TLSClientConfig.Certificates)
				assert.NotNil(t, transport.TLSClientConfig.VerifyConnection)
			},
		},
		{
			name:                "no certificate no fingerprints",
			certificate:         tls.Certificate{},
			trustedFingerprints: nil,
			expectError:         false,
			validateClient: func(t *testing.T, w3s *Web3Signer) {
				transport, ok := w3s.httpClient.Transport.(*http.Transport)
				require.True(t, ok)
				require.NotNil(t, transport.TLSClientConfig)

				assert.Empty(t, transport.TLSClientConfig.Certificates)
				assert.Nil(t, transport.TLSClientConfig.VerifyConnection)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w3s := &Web3Signer{
				httpClient: &http.Client{
					Transport: http.DefaultTransport,
				},
			}

			opt := WithTLS(tc.certificate, tc.trustedFingerprints)
			err := opt(w3s)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				tc.validateClient(t, w3s)
			}
		})
	}
}

func TestMultipleOptions(t *testing.T) {
	t.Parallel()

	cert := tls.Certificate{
		Certificate: [][]byte{[]byte("test-certificate")},
		PrivateKey:  []byte("test-private-key"),
	}
	timeout := 30 * time.Second
	fingerprints := map[string]string{"example.com:443": "aa:bb:cc:dd"}

	client, err := New(
		"https://example.com",
		WithRequestTimeout(timeout),
		WithTLS(cert, fingerprints),
	)

	require.NoError(t, err)
	require.NotNil(t, client)

	assert.Equal(t, timeout, client.httpClient.Timeout)

	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.TLSClientConfig)

	assert.Len(t, transport.TLSClientConfig.Certificates, 1)
	assert.NotNil(t, transport.TLSClientConfig.VerifyConnection)
}
