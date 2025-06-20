package rsaencryption

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
)

// GenerateKeyPairPEM generates a new RSA key pair (2048 bits),
// and returns the PEM-encoded public and private keys.
//
// The public key is returned in PKIX format with type "RSA PUBLIC KEY".
// The private key is returned in PKCS#1 format with type "RSA PRIVATE KEY".
//
// The returned error is always expected to be nil as long as keySize remains to be correct.
func GenerateKeyPairPEM() ([]byte, []byte, error) {
	const keySize = 2048

	sk, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, nil, fmt.Errorf("generate RSA key: %w", err)
	}
	skPem := PrivateKeyToPEM(sk)

	pkPem, err := PublicKeyToPEM(&sk.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("public key to PEM: %w", err)
	}

	return pkPem, skPem, nil
}

// Decrypt decrypts the given RSA-encrypted key using the provided private key,
// and returns the original plaintext key.
//
// The input must be a ciphertext encrypted with the corresponding RSA public key,
// and should match the PKCS#1 v1.5 padding format.
func Decrypt(sk *rsa.PrivateKey, encryptedKey []byte) ([]byte, error) {
	return rsa.DecryptPKCS1v15(rand.Reader, sk, encryptedKey)
}

// HashKeyBytes computes the SHA-256 hash of the given key material (typically in DER or PEM format)
// and returns it as a hexadecimal string.
//
// Note: This function does not parse or validate the key — it simply hashes the raw bytes as-is.
func HashKeyBytes(keyBytes []byte) string {
	hash := sha256.Sum256(keyBytes)
	return hex.EncodeToString(hash[:])
}

// PEMToPrivateKey parses a PEM-encoded RSA private key in PKCS#1 format
// and returns the corresponding *rsa.PrivateKey.
//
// Returns an error if the PEM block is missing, has an unexpected type,
// or contains an invalid RSA private key.
func PEMToPrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	if block.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("unexpected PEM block type: got %q, want %q", block.Type, "RSA PRIVATE KEY")
	}

	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	return priv, nil
}

// PEMToPublicKey parses a PEM-encoded RSA public key in PKIX format
// and returns the corresponding *rsa.PublicKey.
//
// Returns an error if the PEM block is invalid, the key cannot be parsed,
// or the key is not an RSA public key.
func PEMToPublicKey(pubPem []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pubPem)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid public key DER: %w", err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("unknown type of public key: %T", pub)
	}

	return rsaPub, nil
}

// PrivateKeyToBytes encodes the given RSA private key using PKCS#1 (DER format)
// and returns the raw byte slice.
func PrivateKeyToBytes(sk *rsa.PrivateKey) []byte {
	return x509.MarshalPKCS1PrivateKey(sk)
}

// PrivateKeyToPEM converts an RSA private key to PEM format.
// It returns the PEM-encoded key as a byte slice using the PKCS#1 encoding scheme.
func PrivateKeyToPEM(sk *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: PrivateKeyToBytes(sk),
		},
	)
}

// PrivateKeyToBase64PEM encodes the given RSA private key using PKCS#1,
// wraps it in a PEM block, and returns the result as a base64-encoded string.
func PrivateKeyToBase64PEM(sk *rsa.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(PrivateKeyToPEM(sk))
}

// PublicKeyToPEM encodes the given RSA public key in PKIX format,
// wraps it in a PEM block with the type "RSA PUBLIC KEY", and returns the PEM-encoded bytes.
//
// Note: While PKIX public keys are typically labeled "PUBLIC KEY" in PEM,
// this function uses "RSA PUBLIC KEY" for compatibility with previously generated keys.
func PublicKeyToPEM(pk *rsa.PublicKey) ([]byte, error) {
	pkBytes, err := x509.MarshalPKIXPublicKey(pk)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pemByte := pem.EncodeToMemory(
		&pem.Block{
			// Intentionally using this type for compatibility with already generated keys,
			// even though key is PKIX.
			Type:  "RSA PUBLIC KEY",
			Bytes: pkBytes,
		},
	)

	return pemByte, nil
}

// PublicKeyToBase64PEM encodes the given RSA public key as a base64 string
// representing its PEM-encoded PKIX format.
//
// This wraps PublicKeyToPEM and base64-encodes the result for storage or transmission.
func PublicKeyToBase64PEM(sk *rsa.PublicKey) (string, error) {
	pem, err := PublicKeyToPEM(sk)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pem), nil
}
