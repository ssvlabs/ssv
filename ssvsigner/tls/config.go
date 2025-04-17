package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

// CreateConfigFromFiles creates a TLS configuration for either client or server from certificate files.
// It handles loading certificates from files, setting up CA certificates, and configuring security settings.
func CreateConfigFromFiles(configType ConfigType, certFile, keyFile, caFile string, insecureSkipVerify bool) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         MinimumTLSVersion,
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
		if configType == ClientConfigType {
			tlsConfig.RootCAs = caCertPool
		} else if configType == ServerConfigType {
			tlsConfig.ClientCAs = caCertPool
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}

	return tlsConfig, nil
}

// CreateConfig creates a TLS configuration for either client or server from raw certificate data
// It handles loading certificates directly from byte slices, setting up CA certificates, and configuring security settings
func CreateConfig(configType ConfigType, cert, key, caCert []byte, insecureSkipVerify bool) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         MinimumTLSVersion,
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
		if configType == ClientConfigType {
			tlsConfig.RootCAs = caCertPool
		} else if configType == ServerConfigType {
			tlsConfig.ClientCAs = caCertPool
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}

	return tlsConfig, nil
}
