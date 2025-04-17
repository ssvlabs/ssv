package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

const (
	minimumTLSVersion = tls.VersionTLS13
)

// LoadTLSConfig loads cert/key and optional CA bundle.
// If requireClientCert is true, the CA bundle is used for client‐auth mutual TLS.
func LoadTLSConfig(certPath, keyPath, caPath string, requireClientCert bool) (*tls.Config, error) {
	cfg := &tls.Config{}
	cfg.MinVersion = minimumTLSVersion

	// server/client cert
	if certPath != "" && keyPath != "" {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("load cert/key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	// CA bundle
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("failed to append CA certs")
		}
		if requireClientCert {
			cfg.ClientCAs = pool
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			cfg.RootCAs = pool
		}
	}

	return cfg, nil
}
