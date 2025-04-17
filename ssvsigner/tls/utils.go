package tls

import (
	"crypto/tls"
	"fmt"
	"os"
)

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

// LoadCertificatesFromFiles loads certificate from the specified files.
func LoadCertificatesFromFiles(certFile, keyFile, caCertFile string) (cert, key, caCert []byte, err error) {
	// Check if client cert and key are provided
	if (certFile != "" || keyFile != "") && (certFile == "" || keyFile == "") {
		return nil, nil, nil, fmt.Errorf("both client certificate and key files must be provided")
	}

	if certFile != "" {
		// Check if files exist
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			return nil, nil, nil, fmt.Errorf("client certificate file does not exist: %s", certFile)
		}
		// Read client cert
		cert, err = os.ReadFile(certFile)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to read client certificate: %w", err)
		}
	}

	if keyFile != "" {
		// Check if files exist
		if _, err := os.Stat(keyFile); os.IsNotExist(err) {
			return nil, nil, nil, fmt.Errorf("client key file does not exist: %s", keyFile)
		}
		// Read client key
		key, err = os.ReadFile(keyFile)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to read client key: %w", err)
		}
	}

	if caCertFile != "" {
		// Check if CA cert file exists
		if _, err := os.Stat(caCertFile); os.IsNotExist(err) {
			return nil, nil, nil, fmt.Errorf("CA certificate file does not exist: %s", caCertFile)
		}
		// Read CA cert
		caCert, err = os.ReadFile(caCertFile)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
	}

	return cert, key, caCert, nil
}
