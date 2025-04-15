package ssvsigner

import "go.uber.org/zap"

// ClientOption defines a function that configures a Client.
type ClientOption func(*Client)

// WithLogger sets a custom logger for the client.
func WithLogger(logger *zap.Logger) ClientOption {
	return func(client *Client) {
		client.logger = logger
	}
}

// WithClientCert sets the bytes of the client TLS certificate.
func WithClientCert(cert []byte) ClientOption {
	return func(client *Client) {
		client.clientCert = cert
	}
}

// WithClientKey sets the bytes of the client TLS key.
func WithClientKey(key []byte) ClientOption {
	return func(client *Client) {
		client.clientKey = key
	}
}

// WithCACert sets the bytes of the certificate authority TLS certificate.
func WithCACert(cert []byte) ClientOption {
	return func(client *Client) {
		client.caCert = cert
	}
}
