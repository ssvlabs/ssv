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
	Verify(data []byte, signature []byte) error
	Encrypt(data []byte) ([]byte, error)
	//Bytes() ([]byte, error)
	Encode() ([]byte, error)
}

type OperatorPrivateKey interface {
	//Bytes() ([]byte, error)
	Hash() (string, error)
	OperatorSigner
	OperatorDecrypter
}

type OperatorKeyPair interface {
	OperatorPublicKey
	OperatorPrivateKey
}

type keyPair struct {
	privateKey
	publicKey
}

type OperatorSigner interface {
	Sign(data []byte) ([]byte, error)
	Public() OperatorPublicKey
}

type OperatorDecrypter interface {
	Decrypt(data []byte) ([]byte, error)
}

func KeyPairFromString(privKeyString string) (OperatorKeyPair, error) {
	operatorKeyByte, err := base64.StdEncoding.DecodeString(privKeyString)
	if err != nil {
		return nil, err
	}

	privKey, err := rsaencryption.ConvertPemToPrivateKey(string(operatorKeyByte))
	if err != nil {
		return nil, err
	}

	return &keyPair{
		privateKey: privateKey{privKey: privKey},
		publicKey:  publicKey{pubKey: &privKey.PublicKey},
	}, nil
}

func KeyPairFromFile(privKeyFilePath, passwordFilePath string) (OperatorKeyPair, error) {
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

	return &keyPair{
		privateKey: privateKey{privKey: privKey},
		publicKey:  publicKey{pubKey: &privKey.PublicKey},
	}, nil
}

func GenerateKeyPair() (OperatorKeyPair, error) {
	const keySize = 2048

	privKey, err := rsa.GenerateKey(crand.Reader, keySize)
	if err != nil {
		return nil, err
	}

	return &keyPair{
		privateKey: privateKey{privKey: privKey},
		publicKey:  publicKey{pubKey: &privKey.PublicKey},
	}, nil
}

type privateKey struct {
	privKey *rsa.PrivateKey
}

func (p *privateKey) Public() OperatorPublicKey {
	pubKey := p.privKey.PublicKey
	return &publicKey{pubKey: &pubKey}
}

func (p *privateKey) Bytes() ([]byte, error) {
	return x509.MarshalPKCS1PrivateKey(p.privKey), nil
}

func (p *privateKey) Hash() (string, error) {
	return rsaencryption.HashRsaKey(rsaencryption.PrivateKeyToByte(p.privKey))
}

func (p *privateKey) Sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return rsa.SignPKCS1v15(nil, p.privKey, crypto.SHA256, hash[:])
}

func (p *privateKey) Decrypt(data []byte) ([]byte, error) {
	return rsaencryption.DecodeKey(p.privKey, data)
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

func (p *publicKey) Verify(data []byte, signature []byte) error {
	messageHash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(p.pubKey, crypto.SHA256, messageHash[:], signature)
}

func (p *publicKey) Encrypt(data []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, p.pubKey, data)
}

func (p *publicKey) Bytes() ([]byte, error) {
	return x509.MarshalPKIXPublicKey(p.pubKey)
}

func (p *publicKey) Encode() ([]byte, error) {
	pkBytes, err := p.Bytes()
	if err != nil {
		return nil, err
	}

	pemByte := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: pkBytes,
		},
	)

	return []byte(base64.StdEncoding.EncodeToString(pemByte)), nil
}
