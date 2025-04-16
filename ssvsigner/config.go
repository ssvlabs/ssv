package ssvsigner

import (
	"fmt"
	"os"
)

// ClientTLSConfig defines TLS configuration options for client connections
type ClientTLSConfig struct {
	// ClientCertFile is the path to the client certificate file
	ClientCertFile string `yaml:"ClientCertFile" env:"CLIENT_CERT_FILE" env-description:"Path to certificate file for TLS connection to SSV Signer"`
	// ClientKeyFile is the path to the client key file
	ClientKeyFile string `yaml:"ClientKeyFile" env:"CLIENT_KEY_FILE" env-description:"Path to key file for TLS connection to SSV Signer"`
	// ClientCACertFile is the path to the CA certificate file
	ClientCACertFile string `yaml:"ClientCACertFile" env:"CLIENT_CA_CERT_FILE" env-description:"Path to CA certificate file for TLS connection to SSV Signer"`
	// ClientInsecureSkipVerify skips certificate verification (not recommended for production)
	ClientInsecureSkipVerify bool `yaml:"ClientInsecureSkipVerify" env:"CLIENT_INSECURE_SKIP_VERIFY" env-description:"Skip TLS certificate verification (not recommended for production)"`
}

// HasTLSConfig returns true if any TLS configuration is provided
func (c *ClientTLSConfig) HasTLSConfig() bool {
	return c.ClientCertFile != "" ||
		c.ClientKeyFile != "" ||
		c.ClientCACertFile != "" ||
		c.ClientInsecureSkipVerify
}

// LoadTLSOptions loads certificate files from disk and creates ClientOptions for TLS configuration
func LoadTLSOptions(config ClientTLSConfig) ([]ClientOption, error) {
	var options []ClientOption

	// Load client certificate if provided
	if config.ClientCertFile != "" {
		if _, err := os.Stat(config.ClientCertFile); err != nil {
			return nil, fmt.Errorf("client certificate file does not exist: %w", err)
		}

		clientCert, err := os.ReadFile(config.ClientCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read client certificate file: %w", err)
		}
		options = append(options, WithClientCert(clientCert))
	}

	// Load client key if provided
	if config.ClientKeyFile != "" {
		if _, err := os.Stat(config.ClientKeyFile); err != nil {
			return nil, fmt.Errorf("client key file does not exist: %w", err)
		}

		clientKey, err := os.ReadFile(config.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read client key file: %w", err)
		}
		options = append(options, WithClientKey(clientKey))
	}

	// Load CA certificate if provided
	if config.ClientCACertFile != "" {
		if _, err := os.Stat(config.ClientCACertFile); err != nil {
			return nil, fmt.Errorf("CA certificate file does not exist: %w", err)
		}

		caCert, err := os.ReadFile(config.ClientCACertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file: %w", err)
		}
		options = append(options, WithCACert(caCert))
	}

	// Configure insecure skip verify if enabled
	if config.ClientInsecureSkipVerify {
		options = append(options, WithClientInsecureSkipVerify())
	}

	return options, nil
}
