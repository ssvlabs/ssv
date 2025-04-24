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
		name    string
		timeout time.Duration
	}{
		{
			name:    "valid timeout",
			timeout: 5 * time.Second,
		},
		{
			name:    "zero timeout",
			timeout: 0,
		},
		{
			name:    "negative timeout",
			timeout: -1 * time.Second,
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
			opt(w3s)

			assert.Equal(t, tc.timeout, w3s.httpClient.Timeout)
		})
	}
}

func TestWithTLS(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		tlsConfig      *tls.Config
		validateClient func(t *testing.T, w3s *Web3Signer)
	}{
		{
			name: "with certificates",
			tlsConfig: &tls.Config{
				Certificates: []tls.Certificate{
					{
						Certificate: [][]byte{[]byte("test-certificate")},
						PrivateKey:  []byte("test-private-key"),
					},
				},
			},
			validateClient: func(t *testing.T, w3s *Web3Signer) {
				transport, ok := w3s.httpClient.Transport.(*http.Transport)
				require.True(t, ok)
				require.NotNil(t, transport.TLSClientConfig)

				assert.Len(t, transport.TLSClientConfig.Certificates, 1)
			},
		},
		{
			name:      "nil config",
			tlsConfig: nil,
			validateClient: func(t *testing.T, w3s *Web3Signer) {
				transport, ok := w3s.httpClient.Transport.(*http.Transport)
				require.True(t, ok)
				require.Nil(t, transport.TLSClientConfig)
			},
		},
		{
			name:      "empty config",
			tlsConfig: &tls.Config{},
			validateClient: func(t *testing.T, w3s *Web3Signer) {
				transport, ok := w3s.httpClient.Transport.(*http.Transport)
				require.True(t, ok)
				require.NotNil(t, transport.TLSClientConfig)

				assert.Empty(t, transport.TLSClientConfig.Certificates)
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

			opt := WithTLS(tc.tlsConfig)
			opt(w3s)

			tc.validateClient(t, w3s)
		})
	}
}

func TestMultipleOptions(t *testing.T) {
	t.Parallel()

	timeout := 30 * time.Second
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{[]byte("test-certificate")},
				PrivateKey:  []byte("test-private-key"),
			},
		},
	}

	client := New(
		"https://example.com",
		WithRequestTimeout(timeout),
		WithTLS(tlsConfig),
	)

	require.NotNil(t, client)

	assert.Equal(t, timeout, client.httpClient.Timeout)

	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.TLSClientConfig)

	assert.Len(t, transport.TLSClientConfig.Certificates, 1)
}
