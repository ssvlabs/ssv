package tls

import (
	"crypto/tls"
)

// CreateServerConfig creates a TLS configuration for the server from certificate files.
func CreateServerConfig(config ServerConfig) (*tls.Config, error) {
	return CreateConfigFromFiles(
		ServerConfigType,
		config.ServerCertFile,
		config.ServerKeyFile,
		config.ServerCACertFile,
		config.ServerInsecureSkipVerify,
	)
}
