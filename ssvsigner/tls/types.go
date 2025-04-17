package tls

import (
	"crypto/tls"
)

// ConfigType represents the type of TLS configuration (client or server).
type ConfigType string

const (
	// ClientConfigType represents a client TLS configuration
	ClientConfigType ConfigType = "client"
	// ServerConfigType represents a server TLS configuration
	ServerConfigType ConfigType = "server"
	// MinimumTLSVersion is the minimum TLS version supported
	MinimumTLSVersion = tls.VersionTLS13
)

// ClientConfig defines TLS configuration options for client connections.
type ClientConfig struct {
	// ClientCertFile is the path to the client certificate file
	ClientCertFile string `yaml:"ClientCertFile" env:"CLIENT_CERT_FILE" env-description:"Path to certificate file for TLS connection to SSV Signer"`
	// ClientKeyFile is the path to the client key file
	ClientKeyFile string `yaml:"ClientKeyFile" env:"CLIENT_KEY_FILE" env-description:"Path to key file for TLS connection to SSV Signer"`
	// ClientCACertFile is the path to the CA certificate file
	ClientCACertFile string `yaml:"ClientCACertFile" env:"CLIENT_CA_CERT_FILE" env-description:"Path to CA certificate file for TLS connection to SSV Signer"`
	// ClientInsecureSkipVerify skips certificate verification (not recommended for production)
	ClientInsecureSkipVerify bool `yaml:"ClientInsecureSkipVerify" env:"CLIENT_INSECURE_SKIP_VERIFY" env-description:"Skip TLS certificate verification (not recommended for production)"`
}

// HasConfig returns true if any TLS configuration is provided.
func (c *ClientConfig) HasConfig() bool {
	return c.ClientCertFile != "" ||
		c.ClientKeyFile != "" ||
		c.ClientCACertFile != "" ||
		c.ClientInsecureSkipVerify
}

// ServerConfig contains TLS configuration for the server.
type ServerConfig struct {
	// ServerCACertFile is the certificate authority certificate.
	ServerCACertFile string `yaml:"ServerCACertFile" env:"SERVER_CA_CERT_FILE" env-description:"Path to CA certificate file for client authentication on server"`
	// ServerCertFile is the server certificate.
	ServerCertFile string `yaml:"ServerCertFile" env:"SERVER_CERT_FILE" env-description:"Path to certificate file for server TLS connections"`
	// ServerKeyFile is the server private key.
	ServerKeyFile string `yaml:"ServerKeyFile" env:"SERVER_KEY_FILE" env-description:"Path to key file for server TLS connections"`
	// ServerInsecureSkipVerify skips certificate verification (not recommended for production).
	ServerInsecureSkipVerify bool `yaml:"ServerInsecureSkipVerify" env:"SERVER_INSECURE_SKIP_VERIFY" env-description:"Skip TLS certificate verification for server (not recommended for production)"`
}
