package ssvsigner

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"go.uber.org/zap"
)

// ConfigureTLS creates a tls.Config based on provided certificate file paths.
// If certFile and keyFile are provided, it loads the client certificate.
// If caFile is provided, it loads the CA certificate for server verification.
// If caFile is not provided, InsecureSkipVerify is set to true.
func ConfigureTLS(certFile, keyFile, caFile string, logger *zap.Logger) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		// No client certificate provided, return nil config (no TLS) or handle as needed.
		// Depending on requirements, you might want to return an error or a default config.
		// For now, assuming client certs are mandatory for TLS.
		return nil, fmt.Errorf("both TLS certificate file and key file must be provided")
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Load client certificate
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate and key: %w", err)
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	logger.Debug("Loaded client TLS certificate", zap.String("cert_file", certFile), zap.String("key_file", keyFile))

	// Load CA certificate if provided
	if caFile != "" {
		caCertBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCertBytes) {
			return nil, fmt.Errorf("failed to parse CA certificate from PEM")
		}
		tlsConfig.RootCAs = caCertPool
		logger.Debug("Loaded TLS CA certificate", zap.String("ca_file", caFile))
	} else {
		// If no CA cert provided, skip verification (use with caution!)
		logger.Warn("No TLS CA certificate provided, server certificate verification will be skipped (InsecureSkipVerify=true)")
		tlsConfig.InsecureSkipVerify = true
	}

	return tlsConfig, nil
}
