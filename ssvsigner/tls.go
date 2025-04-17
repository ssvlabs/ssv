package ssvsigner

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// TLSConfigType represents the type of TLS configuration (client or server)
type TLSConfigType string

const (
	// ClientTLSConfigType represents a client TLS configuration
	ClientTLSConfigType TLSConfigType = "client"
	// ServerTLSConfigType represents a server TLS configuration
	ServerTLSConfigType TLSConfigType = "server"
	//minimumTLSVersion is the minimum TLS version supported
	minimumTLSVersion = tls.VersionTLS13
)

// CreateClientTLSConfig creates a TLS configuration for client connections from certificate files
func CreateClientTLSConfig(config ClientTLSConfig) (*tls.Config, error) {
	return CreateTLSConfigFromFiles(
		ClientTLSConfigType,
		config.ClientCertFile,
		config.ClientKeyFile,
		config.ClientCACertFile,
		config.ClientInsecureSkipVerify,
	)
}

// CreateServerTLSConfig creates a TLS configuration for the server from certificate files
func CreateServerTLSConfig(config ServerTLSConfig) (*tls.Config, error) {
	return CreateTLSConfigFromFiles(
		ServerTLSConfigType,
		config.ServerCertFile,
		config.ServerKeyFile,
		config.ServerCACertFile,
		config.ServerInsecureSkipVerify,
	)
}

// CreateTLSConfigFromFiles creates a TLS configuration for either client or server from certificate files
// It handles loading certificates from files, setting up CA certificates, and configuring security settings
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

// loadCertificateAndKey loads a certificate and key from files
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

// loadCertFile loads a certificate file
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
