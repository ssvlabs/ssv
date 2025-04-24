package tls

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/pkcs12"
)

// MinTLSVersion is the minimum TLS version supported
const MinTLSVersion = tls.VersionTLS13

// Config defines all TLS-related configuration options.
// This approach matches Web3Signer's TLS configuration options.
// https://docs.web3signer.consensys.io/how-to/configure-tls
type Config struct {
	// Server TLS configuration (for incoming connections to SSV Signer)
	ServerKeystoreFile         string
	ServerKeystorePasswordFile string
	ServerKnownClientsFile     string

	// Client TLS configuration (for connecting to Web3Signer)
	ClientKeystoreFile         string
	ClientKeystorePasswordFile string
	ClientServerCertFile       string
}

// LoadClientConfig creates a client TLS configuration based on the provided parameters.
// It configures certificate for mutual TLS and certificate fingerprint verification for additional security.
// This matches Web3Signer's approach to TLS connection verification.
//
// Configuration scenarios:
// 1. No certificate, no fingerprints - Basic TLS with standard certificate validation
// 2. Certificate only - Client provides identity to server (mutual TLS)
// 3. Fingerprints only - Server identity strictly verified against expected fingerprints
// 4. Certificate and fingerprints - Full mutual TLS with fingerprint verification (most secure)
//
// Parameters:
// - certificate: client certificate to present to the server (can be empty)
// - trustedFingerprints: map of server host:port to expected certificate fingerprints (can be nil)
//
// Returns a properly configured tls.Config object or an error if the configuration is invalid.
func LoadClientConfig(certificate tls.Certificate, trustedFingerprints map[string]string) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: MinTLSVersion,
	}

	// Add a certificate if provided
	if certificate.Certificate != nil {
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}

	// Set up certificate verification if fingerprints provided
	if len(trustedFingerprints) > 0 {
		// Keep track of verified connections using context
		tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("no server certificate provided")
			}

			cert := state.PeerCertificates[0]

			// Calculate fingerprint of the certificate
			fingerprint := sha256.Sum256(cert.Raw)
			fingerprintHex := strings.ToLower(hex.EncodeToString(fingerprint[:]))

			// Get the hostname from the state
			host := state.ServerName

			// If empty, try to get from the certificate
			if host == "" && len(cert.DNSNames) > 0 {
				host = cert.DNSNames[0]
			} else if host == "" {
				host = cert.Subject.CommonName
			}

			// Check if the fingerprint matches the expected fingerprint for this host
			expectedFingerprint, ok := trustedFingerprints[host]
			if ok {
				// Clean up the expected fingerprint (remove colons, convert to lowercase)
				expectedFingerprint = parseFingerprint(expectedFingerprint)
				if expectedFingerprint == fingerprintHex {
					return nil
				}
				return fmt.Errorf("server certificate fingerprint mismatch for %s: expected %s, got %s",
					host,
					formatFingerprintWithColons(expectedFingerprint),
					formatFingerprintWithColons(fingerprintHex))
			}

			return fmt.Errorf("server certificate fingerprint not trusted: %s", formatFingerprintWithColons(fingerprintHex))
		}
	}

	return tlsConfig, nil
}

// LoadServerConfig creates a server TLS configuration based on the provided parameters.
// It configures server certificates and certificate verification.
// This matches Web3Signer's approach to TLS authentication for server connections.
//
// Configuration scenarios:
// 1. Certificate only - Basic TLS, server presents identity, no client verification
// 2. Certificate and fingerprints - Mutual TLS with client verification against fingerprints
//
// Security notes:
// - Server certificate is always required for TLS servers
// - When fingerprints are provided, clients must present certificates
// - Client certificates are verified against the known clients fingerprints
// - Client authentication is based on certificate common name and its fingerprint
//
// Parameters:
// - certificate: server certificate to present to clients (required)
// - trustedFingerprints: map of client common names to expected certificate fingerprints (optional)
//
// Returns a properly configured tls.Config object or an error if the configuration is invalid.
func LoadServerConfig(certificate tls.Certificate, trustedFingerprints map[string]string) (*tls.Config, error) {
	// Validate inputs
	if certificate.Certificate == nil {
		return nil, fmt.Errorf("server certificate is required")
	}

	tlsConfig := &tls.Config{
		MinVersion:   MinTLSVersion,
		Certificates: []tls.Certificate{certificate},
	}

	// Configure client authentication based on fingerprints
	if len(trustedFingerprints) > 0 {
		tlsConfig.ClientAuth = tls.RequireAnyClientCert

		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no client certificate provided")
			}

			// Parse the certificate from raw data
			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return fmt.Errorf("failed to parse client certificate: %w", err)
			}

			// Get the client's common name from the certificate
			clientName := cert.Subject.CommonName
			if clientName == "" {
				return fmt.Errorf("client certificate has no common name")
			}

			// Calculate the fingerprint of the certificate
			fingerprint := sha256.Sum256(cert.Raw)
			fingerprintHex := strings.ToLower(hex.EncodeToString(fingerprint[:]))

			// Check if the client name is in our trusted fingerprints map
			if expectedFingerprint, ok := trustedFingerprints[clientName]; ok {
				// Clean up the expected fingerprint (remove colons, convert to lowercase)
				expectedFingerprint = parseFingerprint(expectedFingerprint)
				if expectedFingerprint == fingerprintHex {
					return nil
				}
				return fmt.Errorf("client certificate fingerprint mismatch for %s: expected %s, got %s",
					clientName,
					formatFingerprintWithColons(expectedFingerprint),
					formatFingerprintWithColons(fingerprintHex))
			}

			return fmt.Errorf("client certificate common name not in known clients list: %s", clientName)
		}
	} else {
		// If no trusted fingerprints, don't require client certificate
		tlsConfig.ClientAuth = tls.NoClientCert
	}

	return tlsConfig, nil
}

// LoadServerTLS loads the server TLS configuration from the provided config.
// Returns certificates and trusted fingerprints for server configuration.
func (c *Config) LoadServerTLS() (tls.Certificate, map[string]string, error) {
	var (
		certificate  tls.Certificate
		fingerprints map[string]string
		err          error
	)

	// Load certificate if provided
	if c.ServerKeystoreFile != "" {
		password, err := c.getServerKeystorePassword()
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("get server keystore password: %w", err)
		}

		cert, err := loadKeystoreCertificate(c.ServerKeystoreFile, password)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load server certificate: %w", err)
		}
		certificate = cert
	}

	// Load known clients if provided
	if c.ServerKnownClientsFile != "" {
		fingerprints, err = loadFingerprintsFile(c.ServerKnownClientsFile)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load known clients: %w", err)
		}
	}

	return certificate, fingerprints, nil
}

// LoadClientTLS loads the client TLS configuration from the provided config.
// This is a simplified interface that wraps LoadClientConfig for easy configuration.
// Returns certificate and trusted fingerprints for client configuration.
func (c *Config) LoadClientTLS() (tls.Certificate, map[string]string, error) {
	var (
		certificate  tls.Certificate
		fingerprints map[string]string
	)

	// Load client certificate if provided
	if c.ClientKeystoreFile != "" {
		password, err := c.getClientKeystorePassword()
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("get client keystore password: %w", err)
		}

		cert, err := loadKeystoreCertificate(c.ClientKeystoreFile, password)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load client certificate: %w", err)
		}
		certificate = cert
	}

	// Load trusted server certificate if provided
	if c.ClientServerCertFile != "" {
		serverCert, err := loadServerCertificate(c.ClientServerCertFile)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load trusted server certificate: %w", err)
		}

		// Calculate fingerprint from the server certificate to use for verification
		fingerprint := sha256.Sum256(serverCert.Raw)
		fingerprintHex := strings.ToLower(hex.EncodeToString(fingerprint[:]))

		// Use the certificate's common name or first DNS name as the host identifier
		hostID := serverCert.Subject.CommonName
		if hostID == "" && len(serverCert.DNSNames) > 0 {
			hostID = serverCert.DNSNames[0]
		}

		if hostID == "" {
			return tls.Certificate{}, nil, fmt.Errorf("server certificate has no common name or DNS names")
		}

		fingerprints = map[string]string{
			hostID: fingerprintHex,
		}
	}

	return certificate, fingerprints, nil
}

// LoadClientConfigForSSV combines the functionality of LoadClientTLS and LoadClientConfig.
// It's a convenience function that creates a ready-to-use tls.Config object
// for client connections directly from the configuration options.
//
// This is an optimization that avoids creating intermediate data structures
// when both the certificate and fingerprints are just passed to LoadClientConfig.
func (c *Config) LoadClientConfigForSSV() (*tls.Config, error) {
	if c.ClientKeystoreFile == "" && c.ClientServerCertFile == "" {
		// No TLS configuration needed
		return &tls.Config{
			MinVersion: MinTLSVersion,
		}, nil
	}

	// Load client certificate if provided
	var certificate tls.Certificate
	if c.ClientKeystoreFile != "" {
		password, err := c.getClientKeystorePassword()
		if err != nil {
			return nil, fmt.Errorf("get client keystore password: %w", err)
		}

		cert, err := loadKeystoreCertificate(c.ClientKeystoreFile, password)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		certificate = cert
	}

	// Load trusted server certificate and convert to fingerprints if provided
	var trustedFingerprints map[string]string
	if c.ClientServerCertFile != "" {
		serverCert, err := loadServerCertificate(c.ClientServerCertFile)
		if err != nil {
			return nil, fmt.Errorf("load trusted server certificate: %w", err)
		}

		// Calculate fingerprint from the server certificate to use for verification
		fingerprint := sha256.Sum256(serverCert.Raw)
		fingerprintHex := strings.ToLower(hex.EncodeToString(fingerprint[:]))

		// Use the certificate's common name or first DNS name as the host identifier
		hostID := serverCert.Subject.CommonName
		if hostID == "" && len(serverCert.DNSNames) > 0 {
			hostID = serverCert.DNSNames[0]
		}

		if hostID == "" {
			return nil, fmt.Errorf("server certificate has no common name or DNS names")
		}

		trustedFingerprints = map[string]string{
			hostID: fingerprintHex,
		}
	}

	// Create the final TLS configuration using LoadClientConfig
	return LoadClientConfig(certificate, trustedFingerprints)
}

// ValidateServerTLS validates the server TLS configuration.
// It verifies that TLS configuration is consistent based on multiple valid use cases.
func (c *Config) ValidateServerTLS() error {
	// Case 1: No server TLS config provided - TLS is optional
	if c.ServerKeystoreFile == "" && c.ServerKnownClientsFile == "" {
		return nil
	}

	// Case 2: Server keystore file provided but no password file
	if c.ServerKeystoreFile != "" && c.ServerKeystorePasswordFile == "" {
		return fmt.Errorf("server keystore password file is required when using a server keystore file")
	}

	// Case 3: Only known clients file provided without server certificate
	// This is invalid because a server must have a certificate to accept TLS connections
	if c.ServerKeystoreFile == "" && c.ServerKnownClientsFile != "" {
		return fmt.Errorf("server keystore file is required when specifying known clients file")
	}

	// Case 4: Server keystore and password file provided (with or without known clients)
	// This is a valid configuration
	return nil
}

// ValidateClientTLS validates the client TLS configuration.
// It verifies that TLS configuration is consistent based on multiple valid use cases.
func (c *Config) ValidateClientTLS() error {
	// Case 1: No client TLS config provided - TLS is optional
	if c.ClientKeystoreFile == "" && c.ClientServerCertFile == "" {
		return nil
	}

	// Case 2: Only trusted server certificate file provided without client certificate
	// This is valid - the client can verify server certificates without presenting its own
	if c.ClientKeystoreFile == "" && c.ClientServerCertFile != "" {
		return nil
	}

	// Case 3: Client keystore file provided but no password file
	if c.ClientKeystoreFile != "" && c.ClientKeystorePasswordFile == "" {
		return fmt.Errorf("client keystore password file is required when using a client keystore file")
	}

	// Case 4: Client keystore and password file provided (with or without server certificate)
	// This is a valid configuration for mutual TLS
	return nil
}

// getServerKeystorePassword returns the server keystore password by loading it from the password file if specified.
func (c *Config) getServerKeystorePassword() (string, error) {
	if c.ServerKeystorePasswordFile != "" {
		return loadPasswordFromFile(c.ServerKeystorePasswordFile)
	}

	return "", fmt.Errorf("neither server keystore password nor password file provided")
}

// getClientKeystorePassword returns the client keystore password by loading it from the password file if specified.
func (c *Config) getClientKeystorePassword() (string, error) {
	if c.ClientKeystorePasswordFile != "" {
		return loadPasswordFromFile(c.ClientKeystorePasswordFile)
	}

	return "", fmt.Errorf("neither client keystore password nor password file provided")
}

// loadPasswordFromFile loads a password from a file.
// The password is expected to be the first line of the file.
func loadPasswordFromFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read password file: %w", err)
	}

	password := strings.TrimSpace(string(data))
	return password, nil
}

// loadKeystoreCertificate loads a certificate from a keystore file.
// The keystore file is expected to be in PKCS12 format, matching Web3Signer's approach.
// Note: While Web3Signer handles PKCS12 decoding internally via command-line parameters,
// this implementation explicitly decodes the keystore to extract certificates and keys.
//
// Parameters:
// - keystoreFile: path to the keystore file
// - password: password to decrypt the keystore
//
// Returns the loaded certificate or an error if loading fails.
func loadKeystoreCertificate(keystoreFile, password string) (tls.Certificate, error) {
	p12Data, err := os.ReadFile(keystoreFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read keystore file: %w", err)
	}

	// Decode the P12/PFX data to get the private key and certificate
	privateKey, certificate, err := pkcs12.Decode(p12Data, password)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("decode PKCS12 keystore: %w", err)
	}

	if certificate == nil {
		return tls.Certificate{}, fmt.Errorf("no certificate found in keystore")
	}

	certChain := [][]byte{certificate.Raw}

	tlsCert := tls.Certificate{
		Certificate: certChain,
		PrivateKey:  privateKey,
		Leaf:        certificate,
	}

	return tlsCert, nil
}

// loadFingerprintsFile loads a file with lines in the format: <id> <fingerprint>
// This generic function can load both known clients and known servers fingerprint files.
// The format matches Web3Signer's fingerprint files format exactly.
//
// Parameters:
// - filePath: a path to the fingerprint file
//
// Returns a map of IDs to fingerprints or an error if loading fails.
func loadFingerprintsFile(filePath string) (map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open fingerprints file: %w", err)
	}
	defer file.Close()

	fingerprints := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format at line %d: expected '<id> <fingerprint>'", lineNum)
		}

		id := parts[0]
		fingerprint := parts[1]

		// Standardize a fingerprint format (remove colons, convert to lowercase)
		fingerprint = parseFingerprint(fingerprint)

		fingerprints[id] = fingerprint
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read fingerprints file: %w", err)
	}

	return fingerprints, nil
}

// formatFingerprintWithColons formats a fingerprint string with colons between each byte.
// This is useful for display purposes as many tools show fingerprints in this format.
//
// Parameters:
// - fingerprint: the fingerprint as a hexadecimal string without separators
//
// Returns the fingerprint with colons inserted between each byte.
func formatFingerprintWithColons(fingerprint string) string {
	fingerprint = strings.ReplaceAll(fingerprint, ":", "")
	var formatted strings.Builder
	for i := 0; i < len(fingerprint); i += 2 {
		if i > 0 {
			formatted.WriteString(":")
		}
		if i+2 <= len(fingerprint) {
			formatted.WriteString(fingerprint[i : i+2])
		} else {
			formatted.WriteString(fingerprint[i:])
		}
	}
	return formatted.String()
}

// parseFingerprint parses a fingerprint string in various formats
// (with or without colons, uppercase or lowercase) and returns
// a standardized lowercase fingerprint without colons.
//
// Parameters:
// - fingerprint: the fingerprint string to parse
//
// Returns the standardized fingerprint.
func parseFingerprint(fingerprint string) string {
	return strings.ToLower(strings.ReplaceAll(fingerprint, ":", ""))
}

// loadServerCertificate loads a PEM-encoded certificate from a file.
// This function reads a trusted server certificate in X.509 PEM format.
//
// Parameters:
// - certFile: path to the certificate file
//
// Returns the parsed X.509 certificate or an error if loading fails.
func loadServerCertificate(certFile string) (*x509.Certificate, error) {
	certData, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("read certificate file: %w", err)
	}

	// Parse the PEM-encoded certificate
	block, _ := pem.Decode(certData)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("failed to decode PEM block containing certificate")
	}

	// Parse the certificate
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	return cert, nil
}

// LoadServerConfigForSSV combines the functionality of LoadServerTLS and LoadServerConfig.
// It's a convenience function that creates a ready-to-use tls.Config object
// for server connections directly from the configuration options.
func (c *Config) LoadServerConfigForSSV() (*tls.Config, error) {
	// Check if TLS is configured
	if c.ServerKeystoreFile == "" {
		// Return a default TLS config with minimum version set
		return &tls.Config{
			MinVersion: MinTLSVersion,
		}, nil
	}

	// Load server certificate
	password, err := c.getServerKeystorePassword()
	if err != nil {
		return nil, fmt.Errorf("get server keystore password: %w", err)
	}

	certificate, err := loadKeystoreCertificate(c.ServerKeystoreFile, password)
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}

	// Load known clients if provided
	var trustedFingerprints map[string]string
	if c.ServerKnownClientsFile != "" {
		fingerprints, err := loadFingerprintsFile(c.ServerKnownClientsFile)
		if err != nil {
			return nil, fmt.Errorf("load known clients: %w", err)
		}
		trustedFingerprints = fingerprints
	}

	// Create the final TLS configuration using LoadServerConfig
	return LoadServerConfig(certificate, trustedFingerprints)
}
