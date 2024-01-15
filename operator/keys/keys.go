package keys

import (
	"crypto"
	"crypto/rand"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/bloxapp/ssv/utils/rsaencryption"
)

type OperatorPublicKey interface {
	Encrypt(data []byte) ([]byte, error)
	Verify(data []byte, signature []byte) error
	PEM() ([]byte, error)
	Base64() ([]byte, error)
}

type OperatorPrivateKey interface {
	OperatorSigner
	OperatorDecrypter
	StorageHash() (string, error)
	EKMHash() (string, error)
	PEM() []byte
	Base64() []byte
}

type OperatorSigner interface {
	Sign(data []byte) ([]byte, error)
	Public() OperatorPublicKey
}

type OperatorDecrypter interface {
	Decrypt(data []byte) ([]byte, error)
}

func PrivateKeyFromString(privKeyString string) (OperatorPrivateKey, error) {
	operatorKeyByte, err := base64.StdEncoding.DecodeString(privKeyString)
	if err != nil {
		return nil, err
	}

	privKey, err := rsaencryption.ConvertPemToPrivateKey(string(operatorKeyByte))
	if err != nil {
		return nil, err
	}

	return &privateKey{privKey: privKey}, nil
}

func PrivateKeyFromFile(privKeyFilePath, passwordFilePath string) (OperatorPrivateKey, error) {
	// nolint: gosec
	encryptedJSON, err := os.ReadFile(privKeyFilePath)
	if err != nil {
		return nil, fmt.Errorf("read PEM file: %w", err)
	}

	// nolint: gosec
	keyStorePassword, err := os.ReadFile(passwordFilePath)
	if err != nil {
		return nil, fmt.Errorf("read password file: %w", err)
	}

	privKey, err := rsaencryption.ConvertEncryptedPemToPrivateKey(encryptedJSON, string(keyStorePassword))
	if err != nil {
		return nil, fmt.Errorf("decrypt operator private key: %w", err)
	}

	return &privateKey{privKey: privKey}, nil
}

func GeneratePrivateKey() (OperatorPrivateKey, error) {
	const keySize = 2048

	privKey, err := rsa.GenerateKey(crand.Reader, keySize)
	if err != nil {
		return nil, err
	}

	return &privateKey{privKey: privKey}, nil
}

type privateKey struct {
	privKey *rsa.PrivateKey
}

func (p *privateKey) Public() OperatorPublicKey {
	pubKey := p.privKey.PublicKey
	return &publicKey{pubKey: &pubKey}
}

func (p *privateKey) Sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return rsa.SignPKCS1v15(nil, p.privKey, crypto.SHA256, hash[:])
}

func (p *privateKey) Decrypt(data []byte) ([]byte, error) {
	return rsaencryption.DecodeKey(p.privKey, data)
}

func (p *privateKey) Marshal() []byte {
	return x509.MarshalPKCS1PrivateKey(p.privKey)
}

func (p *privateKey) PEM() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: p.Marshal(),
	})
}

func (p *privateKey) Base64() []byte {
	return []byte(base64.StdEncoding.EncodeToString(p.PEM()))
}

func (p *privateKey) StorageHash() (string, error) {
	return rsaencryption.HashRsaKey(rsaencryption.PrivateKeyToByte(p.privKey))
}

func (p *privateKey) EKMHash() (string, error) {
	return rsaencryption.HashRsaKey(p.Marshal())
}

type publicKey struct {
	pubKey *rsa.PublicKey
}

func PublicKeyFromString(pubKeyString string) (OperatorPublicKey, error) {
	pubPem, err := base64.StdEncoding.DecodeString(pubKeyString)
	if err != nil {
		return nil, err
	}

	pubKey, err := rsaencryption.ConvertPemToPublicKey(pubPem)
	if err != nil {
		return nil, err
	}

	return &publicKey{
		pubKey: pubKey,
	}, nil
}

func (p *publicKey) Encrypt(data []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, p.pubKey, data)
}

func (p *publicKey) Verify(data []byte, signature []byte) error {
	messageHash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(p.pubKey, crypto.SHA256, messageHash[:], signature)
}

func (p *publicKey) Marshal() ([]byte, error) {
	return x509.MarshalPKIXPublicKey(p.pubKey)
}

func (p *publicKey) PEM() ([]byte, error) {
	pubKeyBytes, err := p.Marshal()
	if err != nil {
		return nil, err
	}

	pemBytes := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: pubKeyBytes,
		},
	)

	return pemBytes, nil
}

func (p *publicKey) Base64() ([]byte, error) {
	pemBytes, err := p.PEM()
	if err != nil {
		return nil, err
	}

	return []byte(base64.StdEncoding.EncodeToString(pemBytes)), nil
}
