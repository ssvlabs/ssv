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
