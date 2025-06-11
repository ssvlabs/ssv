package keys

import (
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
)

type OperatorPublicKey interface {
	Encrypt(data []byte) ([]byte, error)
	Verify(data []byte, signature []byte) error
	Base64() (string, error)
}

type OperatorPrivateKey interface {
	OperatorSigner
	OperatorDecrypter
	StorageHash() string
	EKMHash() string
	Bytes() []byte
	Base64() string
}

type OperatorSigner interface {
	Sign(data []byte) ([]byte, error)
	Public() OperatorPublicKey
}

type OperatorDecrypter interface {
	Decrypt(data []byte) ([]byte, error)
}

func PrivateKeyFromString(privKeyString string) (OperatorPrivateKey, error) {
	privKeyBytes, err := base64.StdEncoding.DecodeString(privKeyString)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	return PrivateKeyFromBytes(privKeyBytes)
}

func PrivateKeyFromBytes(pemData []byte) (OperatorPrivateKey, error) {
	privKey, err := rsaencryption.PEMToPrivateKey(pemData)
	if err != nil {
		return nil, fmt.Errorf("pem to private key: %w", err)
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

func (p *privateKey) Sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	signature, err := SignRSA(p, hash[:])
	if err != nil {
		return []byte{}, err
	}

	return signature, nil
}

func (p *privateKey) Public() OperatorPublicKey {
	pubKey := p.privKey.PublicKey
	return &publicKey{pubKey: &pubKey}
}

func (p *privateKey) Decrypt(data []byte) ([]byte, error) {
	return rsaencryption.Decrypt(p.privKey, data)
}

func (p *privateKey) Bytes() []byte {
	return rsaencryption.PrivateKeyToPEM(p.privKey)
}

func (p *privateKey) Base64() string {
	return rsaencryption.PrivateKeyToBase64PEM(p.privKey)
}

func (p *privateKey) StorageHash() string {
	return rsaencryption.HashKeyBytes(rsaencryption.PrivateKeyToPEM(p.privKey))
}

func (p *privateKey) EKMHash() string {
	return rsaencryption.HashKeyBytes(rsaencryption.PrivateKeyToBytes(p.privKey))
}

func PublicKeyFromString(pubKeyString string) (OperatorPublicKey, error) {
	pubPem, err := base64.StdEncoding.DecodeString(pubKeyString)
	if err != nil {
		return nil, err
	}

	pubKey, err := rsaencryption.PEMToPublicKey(pubPem)
	if err != nil {
		return nil, err
	}

	return &publicKey{
		pubKey: pubKey,
	}, nil
}

func (p *publicKey) Encrypt(data []byte) ([]byte, error) {
	return EncryptRSA(p, data)
}

func (p *publicKey) Verify(data []byte, signature []byte) error {
	return VerifyRSA(p, data, signature)
}

func (p *publicKey) Base64() (string, error) {
	b, err := rsaencryption.PublicKeyToBase64PEM(p.pubKey)
	if err != nil {
		return "", err
	}
	return b, err
}
