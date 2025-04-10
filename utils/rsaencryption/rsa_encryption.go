// TODO: replace ExtractPublicKeyPemBase64 it with ExtractPublicKeyPem
// In fact, we never use base64 representation of public key except of case
// when we use it as a key for database.
// So PEM should be encrypted to base64 only and inside database layer

package rsaencryption

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	"github.com/pkg/errors"
)

var keySize = 2048

// GenerateKeys using rsa random generate keys and return []byte bas64
func GenerateKeys() ([]byte, []byte, error) {
	// generate random private key (secret)
	sk, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to generate rsa key")
	}
	// retrieve public key from the newly generated secret
	pk := &sk.PublicKey

	// convert to bytes
	skPem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(sk),
		},
	)
	pkBytes, err := x509.MarshalPKIXPublicKey(pk)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to marshal public key")
	}
	pkPem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: pkBytes,
		},
	)
	return pkPem, skPem, nil
}

// DecodeKey with secret key (rsa) and hash (base64), return the decrypted key
func DecodeKey(sk *rsa.PrivateKey, hash []byte) ([]byte, error) {
	decryptedKey, err := rsa.DecryptPKCS1v15(rand.Reader, sk, hash)
	if err != nil {
		return nil, errors.Wrap(err, "could not decrypt key")
	}
	return decryptedKey, nil
}

// HashRsaKey return sha256 hash of rsa private key
func HashRsaKey(keyBytes []byte) (string, error) {
	hash := sha256.Sum256(keyBytes)
	keyString := fmt.Sprintf("%x", hash)
	return keyString, nil
}

func PemToPrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("failed to parse PEM block containing the private key")
	}

	if block.Type != "RSA PRIVATE KEY" {
		return nil, errors.New("invalid PEM block type")
	}

	return parsePrivateKey(block.Bytes)
}

func parsePrivateKey(derBytes []byte) (*rsa.PrivateKey, error) {
	parsedSk, err := x509.ParsePKCS1PrivateKey(derBytes)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to parse private key")
	}
	return parsedSk, nil
}

// ConvertPemToPublicKey return rsa public key from public key pem
func ConvertPemToPublicKey(pubPem []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pubPem)
	if block == nil {
		return nil, errors.New("failed to parse PEM block containing the public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse DER encoded public key")
	}

	if pub, ok := pub.(*rsa.PublicKey); ok {
		return pub, nil
	} else {
		return nil, errors.New("unknown type of public key")
	}
}

// PrivateKeyToByte converts privateKey to []byte
func PrivateKeyToByte(sk *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(sk),
		},
	)
}

// ExtractPublicKey get public key from private key and return base64 encoded public key
func ExtractPublicKey(pk *rsa.PublicKey) (string, error) {
	pkBytes, err := x509.MarshalPKIXPublicKey(pk)
	if err != nil {
		return "", errors.Wrap(err, "Failed to marshal private key")
	}
	pemByte := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: pkBytes,
		},
	)

	return base64.StdEncoding.EncodeToString(pemByte), nil
}

// ExtractPrivateKey gets private key and returns base64 encoded private key
func ExtractPrivateKey(sk *rsa.PrivateKey) string {
	pemByte := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(sk),
		},
	)
	return base64.StdEncoding.EncodeToString(pemByte)
}
