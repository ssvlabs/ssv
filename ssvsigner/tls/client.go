package tls

import (
	"crypto/tls"
)

// CreateClientConfig creates a TLS configuration for client connections from certificate files.
func CreateClientConfig(config ClientConfig) (*tls.Config, error) {
	return CreateConfigFromFiles(
		ClientConfigType,
		config.ClientCertFile,
		config.ClientKeyFile,
		config.ClientCACertFile,
		config.ClientInsecureSkipVerify,
	)
}
