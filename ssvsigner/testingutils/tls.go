package testingutils

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// GenerateCertificates generates a CA certificate and a server certificate signed by the CA.
// It returns the certificates and private keys in PEM format.
func GenerateCertificates(t *testing.T, host string) (caCert, caKey, serverCert, serverKey []byte) {
	t.Helper()

	// Generate CA key and certificate
	caPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate CA private key: %v", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		t.Fatalf("Failed to generate serial number: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"SSV Test CA"},
			CommonName:   "SSV Test CA",
		},
		NotBefore:             time.Now().Add(-10 * time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	// Generate server key and certificate
	serverPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate server private key: %v", err)
	}

	serialNumber, err = rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		t.Fatalf("Failed to generate serial number: %v", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"SSV Test Server"},
			CommonName:   "localhost",
		},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-10 * time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		SubjectKeyId: []byte{1, 2, 3, 4, 5},
	}

	if host != "" && host != "localhost" && net.ParseIP(host) == nil {
		serverTemplate.DNSNames = append(serverTemplate.DNSNames, host)
	} else if host != "" && host != "localhost" && net.ParseIP(host) != nil {
		serverTemplate.IPAddresses = append(serverTemplate.IPAddresses, net.ParseIP(host))
	}

	// Create the server certificate
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverPrivKey.PublicKey, caPrivKey)
	if err != nil {
		t.Fatalf("Failed to create server certificate: %v", err)
	}

	// Encode CA cert and key to PEM
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caKeyDER, err := x509.MarshalPKCS8PrivateKey(caPrivKey)
	if err != nil {
		t.Fatalf("Failed to marshal CA private key: %v", err)
	}
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: caKeyDER})

	// Encode server cert and key to PEM
	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})
	serverKeyDER, err := x509.MarshalPKCS8PrivateKey(serverPrivKey)
	if err != nil {
		t.Fatalf("Failed to marshal server private key: %v", err)
	}
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER})

	return caCertPEM, caKeyPEM, serverCertPEM, serverKeyPEM
}

// WriteCertificates writes certificates to temporary files and returns the file paths.
// The caller is responsible for cleaning up the temporary directory.
func WriteCertificates(t *testing.T, caCert, caKey, serverCert, serverKey []byte) (string, string, string, string, string) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "ssv-tls-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	caCertPath := filepath.Join(tempDir, "ca-cert.pem")
	caKeyPath := filepath.Join(tempDir, "ca-key.pem")
	serverCertPath := filepath.Join(tempDir, "server-cert.pem")
	serverKeyPath := filepath.Join(tempDir, "server-key.pem")

	if err := os.WriteFile(caCertPath, caCert, 0644); err != nil {
		t.Fatalf("Failed to write CA certificate: %v", err)
	}
	if caKey != nil {
		if err := os.WriteFile(caKeyPath, caKey, 0600); err != nil {
			t.Fatalf("Failed to write CA key: %v", err)
		}
	}
	if err := os.WriteFile(serverCertPath, serverCert, 0644); err != nil {
		t.Fatalf("Failed to write server certificate: %v", err)
	}
	if err := os.WriteFile(serverKeyPath, serverKey, 0600); err != nil {
		t.Fatalf("Failed to write server key: %v", err)
	}

	return tempDir, caCertPath, caKeyPath, serverCertPath, serverKeyPath
}

// CreateServerTLSConfig creates a TLS configuration for a server using the provided certificates.
func CreateServerTLSConfig(t *testing.T, serverCert, serverKey, caCert []byte, clientAuth bool) *tls.Config {
	t.Helper()

	// Create server certificate
	cert, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatalf("Failed to create X509 key pair: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if clientAuth && caCert != nil {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			t.Fatal("Failed to append CA certificate to cert pool")
		}
		tlsConfig.ClientCAs = caCertPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsConfig
}

// CreateClientTLSConfig creates a TLS configuration for a client using the provided certificates.
func CreateClientTLSConfig(t *testing.T, clientCert, clientKey, caCert []byte, insecureSkipVerify bool) *tls.Config {
	t.Helper()

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipVerify,
		ServerName:         "localhost",
	}

	if clientCert != nil && clientKey != nil {
		cert, err := tls.X509KeyPair(clientCert, clientKey)
		if err != nil {
			t.Fatalf("Failed to create client X509 key pair: %v", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if caCert != nil {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			t.Fatal("Failed to append CA certificate to client cert pool")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig
}

// GetPreGeneratedCertificates returns pre-generated certificates for quick testing.
// Note: This is for testing only, never use these certificates in production.
func GetPreGeneratedCertificates() (caCert, caKey, serverCert, serverKey []byte) {
	caCert = []byte(`-----BEGIN CERTIFICATE-----
MIIBdzCCAR2gAwIBAgIRAOZ0iMc1dRoJGqrpe98cwikwCgYIKoZIzj0EAwIwGDEW
MBQGA1UEAxMNU1NWIFRlc3QgUm9vdDAeFw0yMzA5MDEwMDAwMDBaFw0zMzA4MzAw
MDAwMDBaMBgxFjAUBgNVBAMTDVNTViBUZXN0IFJvb3QwWTATBgcqhkjOPQIBBggq
hkjOPQMBBwNCAARWV3KGAaBfVj+zzJf5pWOz1Cbo91BwkQbS60kMf6q6VEVICxKs
rVY4hPdZV96QnP+RL0YNZTfp+Zs+3ddTnFpFo0UwQzAOBgNVHQ8BAf8EBAMCAQYw
EgYDVR0TAQH/BAgwBgEB/wIBATAdBgNVHQ4EFgQUzK4X+dHLYL/K/ZfZO8h6iCdE
/dQwCgYIKoZIzj0EAwIDSAAwRQIhAK0cF7MAB+DK0XY8tlXNMwWmBZDICymve4rS
Miu4cXvDAiAu8t0GXbJd6pXxcKyiWEGxW+RlVvV6zNZnldvd2nIxQA==
-----END CERTIFICATE-----`)

	caKey = []byte(`-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgWFJYmEEE6MxIw1JP
eQlRHNJ4YBs2xsQfebzU9+N+G+6hRANCAARWV3KGAaBfVj+zzJf5pWOz1Cbo91Bw
kQbS60kMf6q6VEVICxKsrVY4hPdZV96QnP+RL0YNZTfp+Zs+3ddTnFpF
-----END PRIVATE KEY-----`)

	serverCert = []byte(`-----BEGIN CERTIFICATE-----
MIIBiDCCAS6gAwIBAgIQDqIRg8q5mdBZBIKQK4pVcDAKBggqhkjOPQQDAjAYMRYw
FAYDVQQDEw1TU1YgVGVzdCBSb290MB4XDTIzMDkwMTAwMDAwMFoXDTI0MDkwMTAw
MDAwMFowGjEYMBYGA1UEAxMPbG9jYWxob3N0LmxvY2FsMFkwEwYHKoZIzj0CAQYI
KoZIzj0DAQcDQgAErSzN7xL0OMTQhMfJe4hS11bA+R4G8JwEJXcQGwM1Acnajd7G
5NnUFX/gSrHJKhRuxjZQiXrIZjbMY1Wa98vSnaNdMFswDgYDVR0PAQH/BAQDAgWg
MB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAMBgNVHRMBAf8EAjAAMBwG
A1UdEQQVMBOCCWxvY2FsaG9zdIcEfwAAATAKBggqhkjOPQQDAgNIADBFAiB+hTZM
5lyb+EzT/EYGQjKCCjdmFILyQVzT/McVxIDw2QIhAN0GV+uXAMKvLp2yvQOUcbZP
QHxKcr2lr2JZ4Fkp8bzC
-----END CERTIFICATE-----`)

	serverKey = []byte(`-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgcZW+T0cHWrwGgkzR
HsP7HCXwnLdnMK8oSFxxfGSfGg+hRANCAASdxMC5OJwFDIZB2QH96E+DF8ZxqErk
aEQCXKXq6WZCxJWfcvZEawzNDsZXPh7fKTZgBYQKBF3xeSbPUr3PcQqg
-----END PRIVATE KEY-----`)

	return caCert, caKey, serverCert, serverKey
}
