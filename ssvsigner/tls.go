package ssvsigner

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// TLSConfigType represents the type of TLS configuration (client or server).
type TLSConfigType string

const (
	// ClientTLSConfigType represents a client TLS configuration
	ClientTLSConfigType TLSConfigType = "client"
	// ServerTLSConfigType represents a server TLS configuration
	ServerTLSConfigType TLSConfigType = "server"
	//minimumTLSVersion is the minimum TLS version supported
	minimumTLSVersion = tls.VersionTLS13
)

// ClientTLSConfig defines TLS configuration options for client connections.
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

// HasTLSConfig returns true if any TLS configuration is provided.
func (c *ClientTLSConfig) HasTLSConfig() bool {
	return c.ClientCertFile != "" ||
		c.ClientKeyFile != "" ||
		c.ClientCACertFile != "" ||
		c.ClientInsecureSkipVerify
}

// ServerTLSConfig contains TLS configuration for the server.
type ServerTLSConfig struct {
	// ServerCACertFile is the certificate authority certificate.
	ServerCACertFile string `yaml:"ServerCACertFile" env:"SERVER_CA_CERT_FILE" env-description:"Path to CA certificate file for client authentication on server"`
	// ServerCertFile is the server certificate.
	ServerCertFile string `yaml:"ServerCertFile" env:"SERVER_CERT_FILE" env-description:"Path to certificate file for server TLS connections"`
	// ServerKeyFile is the server private key.
	ServerKeyFile string `yaml:"ServerKeyFile" env:"SERVER_KEY_FILE" env-description:"Path to key file for server TLS connections"`
	// ServerInsecureSkipVerify skips certificate verification (not recommended for production).
	ServerInsecureSkipVerify bool `yaml:"ServerInsecureSkipVerify" env:"SERVER_INSECURE_SKIP_VERIFY" env-description:"Skip TLS certificate verification for server (not recommended for production)"`
}

// LoadTLSOptions loads certificate files from disk and creates ClientOptions for TLS configuration.
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

// CreateClientTLSConfig creates a TLS configuration for client connections from certificate files.
func CreateClientTLSConfig(config ClientTLSConfig) (*tls.Config, error) {
	return CreateTLSConfigFromFiles(
		ClientTLSConfigType,
		config.ClientCertFile,
		config.ClientKeyFile,
		config.ClientCACertFile,
		config.ClientInsecureSkipVerify,
	)
}

// CreateServerTLSConfig creates a TLS configuration for the server from certificate files.
func CreateServerTLSConfig(config ServerTLSConfig) (*tls.Config, error) {
	return CreateTLSConfigFromFiles(
		ServerTLSConfigType,
		config.ServerCertFile,
		config.ServerKeyFile,
		config.ServerCACertFile,
		config.ServerInsecureSkipVerify,
	)
}

// CreateTLSConfigFromFiles creates a TLS configuration for either client or server from certificate files.
// It handles loading certificates from files, setting up CA certificates, and configuring security settings.
func CreateTLSConfigFromFiles(configType TLSConfigType, certFile, keyFile, caFile string, insecureSkipVerify bool) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         minimumTLSVersion,
		InsecureSkipVerify: insecureSkipVerify,
	}

	// If insecureSkipVerify is true, we should log a warning, but this is handled by the caller

	// Load certificate and key if provided
	if certFile != "" && keyFile != "" {
		cert, err := loadCertificateAndKey(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s certificate and key from files: %w", configType, err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	} else if certFile != "" || keyFile != "" {
		// If only one of certFile or keyFile is provided, return an error
		return nil, fmt.Errorf("%s certificate and key must be provided together", configType)
	}

	// Load CA certificate if provided
	if caFile != "" {
		caCert, err := loadCertFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load CA certificate from file: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA certificate to pool")
		}

		// Set the appropriate field based on the configuration type
		if configType == ClientTLSConfigType {
			tlsConfig.RootCAs = caCertPool
		} else if configType == ServerTLSConfigType {
			tlsConfig.ClientCAs = caCertPool
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}

	return tlsConfig, nil
}

// CreateTLSConfig creates a TLS configuration for either client or server from raw certificate data
// It handles loading certificates directly from byte slices, setting up CA certificates, and configuring security settings
func CreateTLSConfig(configType TLSConfigType, cert, key, caCert []byte, insecureSkipVerify bool) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         minimumTLSVersion,
		InsecureSkipVerify: insecureSkipVerify,
	}

	// If insecureSkipVerify is true, we should log a warning, but this is handled by the caller

	// Load certificate and key if provided
	if len(cert) > 0 && len(key) > 0 {
		tlsCert, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s certificate and key from data: %w", configType, err)
		}
		tlsConfig.Certificates = []tls.Certificate{tlsCert}
	} else if len(cert) > 0 || len(key) > 0 {
		// If only one of cert or key is provided, return an error
		return nil, fmt.Errorf("%s certificate and key must be provided together", configType)
	}

	// Load CA certificate if provided
	if len(caCert) > 0 {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA certificate to pool")
		}

		// Set the appropriate field based on the configuration type
		if configType == ClientTLSConfigType {
			tlsConfig.RootCAs = caCertPool
		} else if configType == ServerTLSConfigType {
			tlsConfig.ClientCAs = caCertPool
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}

	return tlsConfig, nil
}

// loadCertificateAndKey loads a certificate and key from files.
func loadCertificateAndKey(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := loadCertFile(certFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to read certificate file: %w", err)
	}

	key, err := loadCertFile(keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to read key file: %w", err)
	}

	tlsCert, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to load certificate: %w", err)
	}

	return tlsCert, nil
}

// loadCertFile loads a certificate file and returns its contents.
// Returns nil if the path is empty.
func loadCertFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}

	// #nosec G304 - This is a safe file read operation
	cert, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read certificate file %s: %w", path, err)
	}

	return cert, nil
}
