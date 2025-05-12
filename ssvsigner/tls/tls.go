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

// LoadClientTLSConfig creates a TLS configuration for client connections.
// This is a complete solution for connecting to Web3Signer with TLS.
//
// Configuration scenarios:
// 1. No TLS configuration - Returns minimal TLS config with modern TLS version
// 2. Client certificate only - Mutual TLS where client identifies itself to server
// 3. Server certificate only - Trust specific server certificate based on fingerprint
// 4. Both certificates - Mutual TLS with specific server trust (most secure)
//
// Parameters used from Config:
// - ClientKeystoreFile: PKCS12 file containing client certificate and private key
// - ClientKeystorePasswordFile: File containing password for the keystore
// - ClientServerCertFile: PEM file with trusted server certificate
func (c *Config) LoadClientTLSConfig() (*tls.Config, error) {
	if err := c.validateClientTLS(); err != nil {
		return nil, fmt.Errorf("invalid client TLS config: %w", err)
	}

	// Case 1: No TLS configuration - use minimum TLS 1.3
	if c.ClientKeystoreFile == "" && c.ClientServerCertFile == "" {
		return &tls.Config{MinVersion: MinTLSVersion}, nil
	}

	// Load client certificate if provided
	var certificate tls.Certificate
	var err error
	if c.ClientKeystoreFile != "" {
		certificate, err = c.loadClientCertificate()
		if err != nil {
			return nil, err
		}
	}

	// Load server fingerprints if provided
	var trustedFingerprints map[string]string
	if c.ClientServerCertFile != "" {
		trustedFingerprints, err = c.loadServerFingerprints()
		if err != nil {
			return nil, err
		}
	}

	return createClientTLSConfig(certificate, trustedFingerprints), nil
}

// LoadServerTLSConfig creates a TLS configuration for server listeners.
// This is a complete solution for setting up the SSV Signer server with TLS.
//
// Configuration scenarios:
// 1. No TLS configuration - Returns minimal TLS config with modern TLS version
// 2. Server certificate only - Basic TLS where server identifies itself to clients
// 3. Server certificate with known clients - Mutual TLS where clients are verified by fingerprint
//
// Parameters used from Config:
// - ServerKeystoreFile: PKCS12 file containing server certificate and private key
// - ServerKeystorePasswordFile: File containing password for the keystore
// - ServerKnownClientsFile: File with list of trusted client fingerprints
func (c *Config) LoadServerTLSConfig() (*tls.Config, error) {
	if err := c.validateServerTLS(); err != nil {
		return nil, fmt.Errorf("invalid server TLS config: %w", err)
	}

	// Case 1: No TLS configuration - use minimum TLS 1.3
	if c.ServerKeystoreFile == "" {
		return &tls.Config{MinVersion: MinTLSVersion}, nil
	}

	// Case 2 & 3: Server certificate required - load it
	certificate, err := c.loadServerCertificate()
	if err != nil {
		return nil, err
	}

	// For Case 3: Load client fingerprints if provided
	var trustedFingerprints map[string]string
	if c.ServerKnownClientsFile != "" {
		trustedFingerprints, err = loadFingerprintsFile(c.ServerKnownClientsFile)
		if err != nil {
			return nil, fmt.Errorf("load known clients: %w", err)
		}
	}

	return createServerTLSConfig(certificate, trustedFingerprints)
}

// validateServerTLS validates the server TLS configuration.
// It verifies that TLS configuration is consistent based on multiple valid use cases.
func (c *Config) validateServerTLS() error {
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

// validateClientTLS validates the client TLS configuration.
// It verifies that TLS configuration is consistent based on multiple valid use cases.
func (c *Config) validateClientTLS() error {
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

// loadClientCertificate loads the client certificate and private key from keystore.
// It handles reading the password file and loading the certificate from the PKCS12 keystore.
func (c *Config) loadClientCertificate() (tls.Certificate, error) {
	password, err := loadPasswordFromFile(c.ClientKeystorePasswordFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load client keystore password: %w", err)
	}

	return loadKeystoreCertificate(c.ClientKeystoreFile, password)
}

// loadServerCertificate loads the server certificate and private key from keystore.
// It handles reading the password file and loading the certificate from the PKCS12 keystore.
func (c *Config) loadServerCertificate() (tls.Certificate, error) {
	password, err := loadPasswordFromFile(c.ServerKeystorePasswordFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load server keystore password: %w", err)
	}

	return loadKeystoreCertificate(c.ServerKeystoreFile, password)
}

// loadServerFingerprints loads the trusted server fingerprints from a PEM certificate file.
// It extracts the certificate's fingerprint and identity (common name or DNS name).
func (c *Config) loadServerFingerprints() (map[string]string, error) {
	serverCert, err := loadPEMCertificate(c.ClientServerCertFile)
	if err != nil {
		return nil, fmt.Errorf("load trusted server certificate: %w", err)
	}

	fingerprint := sha256.Sum256(serverCert.Raw)
	fingerprintHex := hex.EncodeToString(fingerprint[:])

	// Try to get a host identifier from the certificate
	hostID := serverCert.Subject.CommonName
	if hostID == "" && len(serverCert.DNSNames) > 0 {
		hostID = serverCert.DNSNames[0]
	}

	if hostID == "" {
		return nil, fmt.Errorf("server certificate must have a Common Name or DNS name")
	}

	return map[string]string{hostID: fingerprintHex}, nil
}

// createClientTLSConfig creates a client TLS configuration with certificates and fingerprint verification.
// It configures client certificates for mutual TLS and server certificate fingerprint verification
// for additional security. This matches Web3Signer's approach to TLS connection verification.
//
// Parameters:
// - certificate: client certificate to present to the server (can be empty)
// - trustedFingerprints: map of server host:port to expected certificate fingerprints (can be nil)
func createClientTLSConfig(certificate tls.Certificate, trustedFingerprints map[string]string) *tls.Config {
	tlsConfig := &tls.Config{
		MinVersion: MinTLSVersion,
	}

	// Add client certificate if provided
	if certificate.Certificate != nil {
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}

	// Set up certificate verification if fingerprints provided
	if len(trustedFingerprints) > 0 {
		tlsConfig.InsecureSkipVerify = true // We're doing manual verification
		tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
			return verifyServerCertificate(state, trustedFingerprints)
		}
	}

	return tlsConfig
}

// createServerTLSConfig creates a server TLS configuration with certificate and client verification.
// It configures server certificates and client certificate fingerprint verification.
// This matches Web3Signer's approach to TLS authentication for server connections.
//
// Parameters:
// - certificate: server certificate to present to clients (required)
// - trustedFingerprints: map of client common names to expected certificate fingerprints (optional)
func createServerTLSConfig(certificate tls.Certificate, trustedFingerprints map[string]string) (*tls.Config, error) {
	if certificate.Certificate == nil {
		return nil, fmt.Errorf("server certificate is required")
	}

	tlsConfig := &tls.Config{
		MinVersion:   MinTLSVersion,
		Certificates: []tls.Certificate{certificate},
	}

	// Configure client authentication based on fingerprints
	if len(trustedFingerprints) > 0 {
		// Require client certificates but verify them using our custom verification
		tlsConfig.ClientAuth = tls.RequireAnyClientCert
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return verifyClientCertificate(rawCerts, trustedFingerprints)
		}
	} else {
		// If no trusted fingerprints, don't require client certificate
		tlsConfig.ClientAuth = tls.NoClientCert
	}

	return tlsConfig, nil
}

// verifyServerCertificate verifies a server certificate using fingerprints.
// It compares the certificate's fingerprint with the expected fingerprint for the host.
// This provides an additional layer of security beyond standard certificate validation.
func verifyServerCertificate(state tls.ConnectionState, trustedFingerprints map[string]string) error {
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("no server certificate provided")
	}

	cert := state.PeerCertificates[0]
	fingerprint := sha256.Sum256(cert.Raw)
	fingerprintHex := hex.EncodeToString(fingerprint[:])

	// Get the hostname from multiple possible sources
	host := state.ServerName
	if host == "" && len(cert.DNSNames) > 0 {
		host = cert.DNSNames[0]
	} else if host == "" {
		host = cert.Subject.CommonName
	}

	// Check fingerprint against our trusted list
	if expectedFingerprint, ok := trustedFingerprints[host]; ok {
		expectedFingerprint = normalizeFingerprint(expectedFingerprint)
		if expectedFingerprint == fingerprintHex {
			return nil
		}
		return fmt.Errorf("server certificate fingerprint mismatch for %s: expected %s, got %s",
			host,
			formatFingerprint(expectedFingerprint),
			formatFingerprint(fingerprintHex))
	}

	return fmt.Errorf("server certificate fingerprint not trusted: %s", formatFingerprint(fingerprintHex))
}

// verifyClientCertificate verifies a client certificate using fingerprints.
// It compares the certificate's fingerprint with the expected fingerprint for the client's common name.
// This provides an additional layer of security beyond standard certificate validation.
func verifyClientCertificate(rawCerts [][]byte, trustedFingerprints map[string]string) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("no client certificate provided")
	}

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse client certificate: %w", err)
	}

	clientName := cert.Subject.CommonName
	if clientName == "" {
		return fmt.Errorf("client certificate has no common name")
	}

	fingerprint := sha256.Sum256(cert.Raw)
	fingerprintHex := hex.EncodeToString(fingerprint[:])

	// Check fingerprint against our trusted list
	if expectedFingerprint, ok := trustedFingerprints[clientName]; ok {
		expectedFingerprint = normalizeFingerprint(expectedFingerprint)
		if expectedFingerprint == fingerprintHex {
			return nil
		}
		return fmt.Errorf("client certificate fingerprint mismatch for %s: expected %s, got %s",
			clientName,
			formatFingerprint(expectedFingerprint),
			formatFingerprint(fingerprintHex))
	}

	return fmt.Errorf("client certificate common name not in known clients list: %s", clientName)
}

// loadPasswordFromFile loads a password from a file.
// The password is expected to be the first line of the file.
func loadPasswordFromFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read password file: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// loadKeystoreCertificate loads a certificate from a keystore file.
// The keystore file is expected to be in PKCS12 format, matching Web3Signer's approach.
//
// Web3Signer accepts PKCS12 keystores with the following parameters:
// - --key-store-path: Directory containing keystores
// - --keystores-password-file: File containing password to decrypt the keystores
//
// Parameters:
// - keystoreFile: path to the keystore file
// - password: password to decrypt the keystore
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

	return tls.Certificate{
		Certificate: [][]byte{certificate.Raw},
		PrivateKey:  privateKey,
		Leaf:        certificate,
	}, nil
}

// loadPEMCertificate loads a PEM-encoded certificate from a file.
// This function reads a trusted server certificate in X.509 PEM format.
//
// Parameters:
// - certFile: path to the certificate file
func loadPEMCertificate(certFile string) (*x509.Certificate, error) {
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

// loadFingerprintsFile loads a file with lines in the format: <id> <fingerprint>
// This generic function can load both known clients and known servers fingerprint files.
//
// The format matches Web3Signer's fingerprint files format: <hostname>:<port> <fingerprint>
// or <client-id> <fingerprint> for clients.
//
// Web3Signer requires known server fingerprints with the --tls-known-servers-path option
// and allows for tracking trusted clients with --tls-known-clients-path.
//
// Format examples:
// - Server: "10.0.0.1:443 DF:65:B8:02:08:5E:91:82:0F:91:F5:1C:96:56:92:C4:1A:F6:C6:27:FD:6C:FC:31:F2:BB:90:17:22:59:5B:50"
// - Client: "client1 00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF"
//
// Parameters:
// - filePath: a path to the fingerprint file
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
		fingerprints[id] = normalizeFingerprint(fingerprint)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read fingerprints file: %w", err)
	}

	return fingerprints, nil
}

// formatFingerprint formats a fingerprint string with colons between each byte.
// This is useful for display purposes as many tools show fingerprints in this format.
//
// Parameters:
// - fingerprint: the fingerprint as a hexadecimal string without separators
func formatFingerprint(fingerprint string) string {
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
	return strings.ToUpper(formatted.String())
}

// normalizeFingerprint standardizes a fingerprint string by removing colons and
// converting to lowercase. This helps with consistent fingerprint comparison.
//
// Parameters:
// - fingerprint: the fingerprint string to normalize
func normalizeFingerprint(fingerprint string) string {
	return strings.ToLower(strings.ReplaceAll(fingerprint, ":", ""))
}
