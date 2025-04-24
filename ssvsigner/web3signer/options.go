package web3signer

import (
	"crypto/tls"
	"time"
)

// Option defines a function that configures a Web3Signer client.
type Option func(*Web3Signer)

// WithRequestTimeout sets the timeout for HTTP requests made by the Web3Signer client.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(w3s *Web3Signer) {
		w3s.httpClient.Timeout = timeout
	}
}

// WithTLS configures TLS for the Web3Signer client.
// This method configures the client with TLS using the provided TLS configuration.
//
// Parameters:
//   - tlsConfig: TLS configuration for the client
//
// Returns an Option that configures the client with TLS.
func WithTLS(tlsConfig *tls.Config) Option {
	return func(w3s *Web3Signer) {
		w3s.applyTLSConfig(tlsConfig)
	}
}
