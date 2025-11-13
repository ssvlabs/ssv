package testenv

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ssvtls "github.com/ssvlabs/ssv/ssvsigner/tls"
)

const (
	certValidityHours = 24
	testPassword      = "test123"

	// fileMode and dirMode are set to 0644/0755 (world-readable/accessible) to ensure
	// Web3Signer container (UID 999) can read test certificates. These are ephemeral
	// test certificates generated per-run in .tmp/e2e-certs-*, never production keys.
	// On Linux CI, Docker bind mounts preserve exact host permissions, requiring
	// world-readable permissions for container access.
	fileMode = 0644
	dirMode  = 0755

	rsaKeyBits = 2048
)

func generateSelfSignedCert(commonName string, dnsNames []string) ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	serialNumber := big.NewInt(time.Now().UnixNano())

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(certValidityHours * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	return certPEM, keyPEM, nil
}

func generatePKCS12FromPEM(certPath, keyPath, outputPath, password string) error {
	outputDir := filepath.Dir(outputPath)
	keystoreName := strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath))
	passwordFile := filepath.Join(outputDir, keystoreName+"_password.txt")
	if err := os.WriteFile(passwordFile, []byte(password), fileMode); err != nil {
		return fmt.Errorf("failed to write password file: %w", err)
	}

	//nolint:gosec // G204: All inputs are from controlled test environment paths
	cmd := exec.Command("openssl", "pkcs12", "-export",
		"-out", outputPath,
		"-inkey", keyPath,
		"-in", certPath,
		"-passout", "pass:"+password,
		"-legacy", // Required for Go's pkcs12 package compatibility
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create PKCS12 keystore: %w, output: %s", err, string(output))
	}

	// Explicitly set permissions on PKCS12 file created by OpenSSL.
	// OpenSSL creates files with system umask, not fileMode, so this chmod
	// is required to ensure consistent 0644 permissions for container access.
	if err := os.Chmod(outputPath, fileMode); err != nil {
		return fmt.Errorf("failed to set permissions on PKCS12 keystore: %w", err)
	}

	return nil
}

func (env *TestEnvironment) setupTLSCertificates() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	env.certDir = filepath.Join(cwd, ".tmp", "e2e-certs-"+randomSuffix())
	if err := os.MkdirAll(env.certDir, dirMode); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}
	type certConfig struct {
		name     string
		dnsNames []string
		certPath *string
	}

	certConfigs := []certConfig{
		{
			name:     "web3signer",
			dnsNames: []string{"web3signer"},
			certPath: &env.web3SignerCertPath,
		},
		{
			name:     "ssv-signer",
			dnsNames: []string{"ssv-signer"},
			certPath: &env.ssvSignerCertPath,
		},
		{
			name:     "e2e-client",
			dnsNames: []string{"e2e-client"},
			certPath: &env.e2eClientCertPath,
		},
	}

	fingerprints := make(map[string]string)

	for _, config := range certConfigs {
		cert, key, err := generateSelfSignedCert(config.name, config.dnsNames)
		if err != nil {
			return fmt.Errorf("failed to generate %s certificate: %w", config.name, err)
		}

		*config.certPath = filepath.Join(env.certDir, config.name+".crt")
		keyPath := filepath.Join(env.certDir, config.name+".key")

		if err := os.WriteFile(*config.certPath, cert, fileMode); err != nil {
			return fmt.Errorf("failed to write %s certificate: %w", config.name, err)
		}
		if err := os.WriteFile(keyPath, key, fileMode); err != nil {
			return fmt.Errorf("failed to write %s key: %w", config.name, err)
		}

		p12Path := filepath.Join(env.certDir, config.name+".p12")
		if err := generatePKCS12FromPEM(*config.certPath, keyPath, p12Path, testPassword); err != nil {
			return fmt.Errorf("failed to generate PKCS12 keystore for %s: %w", config.name, err)
		}

		// Calculate fingerprint for all certificates
		certPEM, rest := pem.Decode(cert)
		if certPEM == nil {
			return fmt.Errorf("failed to decode %s certificate PEM", config.name)
		}
		if certPEM.Type != "CERTIFICATE" {
			return fmt.Errorf("expected CERTIFICATE type but got %s for %s", certPEM.Type, config.name)
		}
		if len(rest) > 0 {
			return fmt.Errorf("certificate %s contains extra data after the PEM block", config.name)
		}

		parsedCert, err := x509.ParseCertificate(certPEM.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse %s certificate: %w", config.name, err)
		}

		fingerprint := sha256.Sum256(parsedCert.Raw)
		fingerprintHex := hex.EncodeToString(fingerprint[:])
		fingerprints[config.name] = fingerprintHex
	}

	ssvSignerKnownClientsPath := filepath.Join(env.certDir, "known_clients.txt")
	ssvSignerKnownClientsContent := "# Known client certificates for SSV-Signer E2E tests\n" +
		"# Format: <client-name> <sha256-fingerprint>\n" +
		fmt.Sprintf("e2e-client %s\n", fingerprints["e2e-client"])

	if err := os.WriteFile(ssvSignerKnownClientsPath, []byte(ssvSignerKnownClientsContent), fileMode); err != nil {
		return fmt.Errorf("failed to write SSV-Signer known_clients.txt: %w", err)
	}

	web3SignerKnownClientsPath := filepath.Join(env.certDir, "web3signer_known_clients.txt")
	web3SignerKnownClientsContent := "# Known client certificates for Web3Signer E2E tests\n" +
		"# Format: <client-name> <sha256-fingerprint>\n" +
		fmt.Sprintf("ssv-signer %s\n", fingerprints["ssv-signer"]) +
		fmt.Sprintf("e2e-client %s\n", fingerprints["e2e-client"])

	if err := os.WriteFile(web3SignerKnownClientsPath, []byte(web3SignerKnownClientsContent), fileMode); err != nil {
		return fmt.Errorf("failed to write Web3Signer known_clients.txt: %w", err)
	}

	return nil
}

// createMutualTLSConfig creates a mutual TLS config for secure connections
func createMutualTLSConfig(serverCertPath, clientCertPath string) (*tls.Config, error) {
	clientP12Path := strings.TrimSuffix(clientCertPath, ".crt") + ".p12"
	clientPasswordFile := strings.TrimSuffix(clientCertPath, ".crt") + "_password.txt"

	tlsConf := &ssvtls.Config{
		ClientServerCertFile:       serverCertPath,
		ClientKeystoreFile:         clientP12Path,
		ClientKeystorePasswordFile: clientPasswordFile,
	}

	return tlsConf.LoadClientTLSConfig()
}
