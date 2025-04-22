package tls

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
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
	ServerKeystoreFile         string // web3signer --tls-keystore-file
	ServerKeystorePasswordFile string // web3signer --tls-keystore-password-file
	ServerKnownClientsFile     string // web3signer --tls-known-clients-file

	// Client TLS configuration (for connecting to Web3Signer)
	ClientKeystoreFile         string // web3signer --downstream-http-tls-keystore-file
	ClientKeystorePasswordFile string // web3signer --downstream-http-tls-keystore-password-file
	ClientKnownServersFile     string // web3signer --downstream-http-tls-known-servers-file
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

		cert, err := LoadKeystoreCertificate(c.ServerKeystoreFile, password)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load server certificate: %w", err)
		}
		certificate = cert
	}

	// Load known clients if provided
	if c.ServerKnownClientsFile != "" {
		fingerprints, err = LoadFingerprintsFile(c.ServerKnownClientsFile)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load known clients: %w", err)
		}
	}

	return certificate, fingerprints, nil
}

// LoadClientTLS loads the client TLS configuration from the provided config.
// Returns certificate and trusted fingerprints for client configuration.
func (c *Config) LoadClientTLS() (tls.Certificate, map[string]string, error) {
	var (
		certificate  tls.Certificate
		fingerprints map[string]string
		err          error
	)

	// Load certificate if provided
	if c.ClientKeystoreFile != "" {
		password, err := c.getClientKeystorePassword()
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("get client keystore password: %w", err)
		}

		cert, err := LoadKeystoreCertificate(c.ClientKeystoreFile, password)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load client certificate: %w", err)
		}
		certificate = cert
	}

	// Load known servers if provided
	if c.ClientKnownServersFile != "" {
		fingerprints, err = LoadFingerprintsFile(c.ClientKnownServersFile)
		if err != nil {
			return tls.Certificate{}, nil, fmt.Errorf("load known servers: %w", err)
		}
	}

	return certificate, fingerprints, nil
}

// ValidateServerTLS validates the server TLS configuration.
func (c *Config) ValidateServerTLS() error {
	if c.ServerKeystoreFile == "" {
		return nil
	}

	if c.ServerKeystorePasswordFile == "" {
		return fmt.Errorf("server keystore password file is required when using a keystore file")
	}

	return nil
}

// ValidateClientTLS validates the client TLS configuration.
func (c *Config) ValidateClientTLS() error {
	// Special case: client config can have only known servers without a certificate
	if c.ClientKeystoreFile == "" && c.ClientKnownServersFile == "" {
		return nil
	}

	if c.ClientKeystoreFile != "" && c.ClientKeystorePasswordFile == "" {
		return fmt.Errorf("client keystore password file is required when using a keystore file")
	}

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

	// Trim any newlines or whitespace
	password := strings.TrimSpace(string(data))
	return password, nil
}

// LoadClientConfig creates a client TLS configuration based on the provided parameters.
// It configures certificate for mutual TLS and certificate fingerprint verification for additional security.
// This matches Web3Signer's approach to TLS connection verification.
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
				expectedFingerprint = ParseFingerprint(expectedFingerprint)
				if expectedFingerprint == fingerprintHex {
					return nil
				}
				return fmt.Errorf("server certificate fingerprint mismatch for %s: expected %s, got %s",
					host,
					FormatFingerprintWithColons(expectedFingerprint),
					FormatFingerprintWithColons(fingerprintHex))
			}

			return fmt.Errorf("server certificate fingerprint not trusted: %s", FormatFingerprintWithColons(fingerprintHex))
		}

		// Set InsecureSkipVerify to true since we're doing our own verification
		tlsConfig.InsecureSkipVerify = true
	}

	return tlsConfig, nil
}

// LoadServerConfig creates a server TLS configuration based on the provided parameters.
// It configures server certificates and certificate verification.
// This matches Web3Signer's approach to TLS authentication for server connections.
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
				expectedFingerprint = ParseFingerprint(expectedFingerprint)
				if expectedFingerprint == fingerprintHex {
					return nil
				}
				return fmt.Errorf("client certificate fingerprint mismatch for %s: expected %s, got %s",
					clientName,
					FormatFingerprintWithColons(expectedFingerprint),
					FormatFingerprintWithColons(fingerprintHex))
			}

			return fmt.Errorf("client certificate common name not in known clients list: %s", clientName)
		}
	} else {
		// If no trusted fingerprints, don't require client certificate
		tlsConfig.ClientAuth = tls.NoClientCert
	}

	return tlsConfig, nil
}

// LoadKeystoreCertificate loads a certificate from a keystore file.
// The keystore file is expected to be in PKCS12 format, matching Web3Signer's approach.
//
// Parameters:
// - keystoreFile: path to the keystore file
// - password: password to decrypt the keystore
//
// Returns the loaded certificate or an error if loading fails.
func LoadKeystoreCertificate(keystoreFile, password string) (tls.Certificate, error) {
	p12Data, err := os.ReadFile(keystoreFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read keystore file: %w", err)
	}

	// Decode the P12/PFX data to get the private key and certificate
	privateKey, certificate, err := pkcs12.Decode(p12Data, password)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("decode PKCS12 keystore: %w", err)
	}

	x509Certs := []*x509.Certificate{certificate}

	certChain := make([][]byte, len(x509Certs))
	for i, cert := range x509Certs {
		certChain[i] = cert.Raw
	}

	// Create and return the TLS certificate
	tlsCert := tls.Certificate{
		Certificate: certChain,
		PrivateKey:  privateKey,
		Leaf:        certificate,
	}

	return tlsCert, nil
}

// LoadFingerprintsFile loads a file with lines in the format: <id> <fingerprint>
// This generic function can load both known clients and known servers fingerprint files.
// The format matches Web3Signer's fingerprint files format exactly.
//
// Parameters:
// - filePath: a path to the fingerprint file
//
// Returns a map of IDs to fingerprints or an error if loading fails.
func LoadFingerprintsFile(filePath string) (map[string]string, error) {
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
		fingerprint = ParseFingerprint(fingerprint)

		fingerprints[id] = fingerprint
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read fingerprints file: %w", err)
	}

	return fingerprints, nil
}

// FormatFingerprintWithColons formats a fingerprint string with colons between each byte.
// This is useful for display purposes as many tools show fingerprints in this format.
//
// Parameters:
// - fingerprint: the fingerprint as a hexadecimal string without separators
//
// Returns the fingerprint with colons inserted between each byte.
func FormatFingerprintWithColons(fingerprint string) string {
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

// ParseFingerprint parses a fingerprint string in various formats
// (with or without colons, uppercase or lowercase) and returns
// a standardized lowercase fingerprint without colons.
//
// Parameters:
// - fingerprint: the fingerprint string to parse
//
// Returns the standardized fingerprint.
func ParseFingerprint(fingerprint string) string {
	// Remove all colons and convert to the lowercase
	return strings.ToLower(strings.ReplaceAll(fingerprint, ":", ""))
}
