package testenv

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	certValidityHours = 24
	testPassword      = "test123"
	certFileMode      = 0600
	keyFileMode       = 0600
)

func generateSelfSignedCert(commonName string, dnsNames []string) ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
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
	if err := os.WriteFile(passwordFile, []byte(password), keyFileMode); err != nil {
		return fmt.Errorf("failed to write password file: %w", err)
	}

	//nolint:gosec // G204: All inputs are from controlled test environment paths
	cmd := exec.Command("openssl", "pkcs12", "-export",
		"-out", outputPath,
		"-inkey", keyPath,
		"-in", certPath,
		"-passout", "pass:"+password)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create PKCS12 keystore: %w, output: %s", err, string(output))
	}

	return nil
}

func (env *TestEnvironment) setupTLSCertificates() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	env.certDir = filepath.Join(cwd, ".tmp", "e2e-certs-"+randomSuffix())
	if err := os.MkdirAll(env.certDir, 0750); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}
	certConfigs := []struct {
		name     string
		dnsNames []string
		certPath *string
		keyPath  *string
		needP12  bool
	}{
		{"localhost", []string{"localhost", "web3signer"}, &env.web3SignerCertPath, &env.web3SignerKeyPath, true},
		{"ssv-signer", []string{"ssv-signer", "localhost"}, &env.ssvSignerCertPath, &env.ssvSignerKeyPath, false},
	}

	for _, config := range certConfigs {
		cert, key, err := generateSelfSignedCert(config.name, config.dnsNames)
		if err != nil {
			return fmt.Errorf("failed to generate %s certificate: %w", config.name, err)
		}

		*config.certPath = filepath.Join(env.certDir, config.name+".crt")
		*config.keyPath = filepath.Join(env.certDir, config.name+".key")

		if err := os.WriteFile(*config.certPath, cert, certFileMode); err != nil {
			return fmt.Errorf("failed to write %s certificate: %w", config.name, err)
		}
		if err := os.WriteFile(*config.keyPath, key, keyFileMode); err != nil {
			return fmt.Errorf("failed to write %s key: %w", config.name, err)
		}

		if config.needP12 {
			p12Path := filepath.Join(env.certDir, config.name+".p12")
			if err := generatePKCS12FromPEM(*config.certPath, *config.keyPath, p12Path, testPassword); err != nil {
				return fmt.Errorf("failed to generate PKCS12 keystore for %s: %w", config.name, err)
			}
		}
	}

	return nil
}
