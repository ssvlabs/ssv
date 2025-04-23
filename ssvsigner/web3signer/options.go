package web3signer

import (
	"crypto/tls"
	"time"

	ssvsignertls "github.com/ssvlabs/ssv/ssvsigner/tls"
)

// Option defines a function that configures a Web3Signer client.
type Option func(*Web3Signer) error

// WithRequestTimeout sets the timeout for HTTP requests made by the Web3Signer client.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(w3s *Web3Signer) error {
		w3s.httpClient.Timeout = timeout

		return nil
	}
}

// WithTLS configures TLS for the Web3Signer client.
// This method configures the client with TLS using the provided certificate and trusted fingerprints.
//
// Parameters:
//   - certificate: client certificate for mutual TLS authentication
//   - trustedFingerprints: map of hostname:port strings to SHA-256 certificate fingerprints
//     (optional, can be nil if certificate pinning is not required)
//
// Returns a ClientOption that configures the client with TLS.
func WithTLS(certificate tls.Certificate, trustedFingerprints map[string]string) Option {
	return func(client *Web3Signer) error {
		tlsConfig, err := ssvsignertls.LoadClientConfig(certificate, trustedFingerprints)
		if err != nil {
			return err
		}

		client.applyTLSConfig(tlsConfig)

		return nil
	}
}
