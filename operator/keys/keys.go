package keys

import (
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"

	"github.com/ssvlabs/ssv/utils/rsaencryption"
)

type OperatorPublicKey interface {
	Encrypt(data []byte) ([]byte, error)
	Verify(data []byte, signature []byte) error
	Base64() ([]byte, error)
}

type OperatorPrivateKey interface {
	OperatorSigner
	OperatorDecrypter
	StorageHash() (string, error)
	EKMHash() (string, error)
	Bytes() []byte
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

	privKey, err := rsaencryption.PemToPrivateKey(operatorKeyByte)
	if err != nil {
		return nil, err
	}

	return &privateKey{privKey: privKey}, nil
}

func PrivateKeyFromBytes(pemData []byte) (OperatorPrivateKey, error) {
	privKey, err := rsaencryption.PemToPrivateKey(pemData)
	if err != nil {
		return nil, fmt.Errorf("can't decode operator private key: %w", err)
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

	//var sig [256]byte
	//copy(sig[:], signature)

	return signature, nil
}

func (p *privateKey) Public() OperatorPublicKey {
	pubKey := p.privKey.PublicKey
	return &publicKey{pubKey: &pubKey}
}

func (p *privateKey) Decrypt(data []byte) ([]byte, error) {
	return rsaencryption.DecodeKey(p.privKey, data)
}

func (p *privateKey) Bytes() []byte {
	return rsaencryption.PrivateKeyToByte(p.privKey)
}

func (p *privateKey) Base64() []byte {
	return []byte(rsaencryption.ExtractPrivateKey(p.privKey))
}

func (p *privateKey) StorageHash() (string, error) {
	return rsaencryption.HashRsaKey(rsaencryption.PrivateKeyToByte(p.privKey))
}

func (p *privateKey) EKMHash() (string, error) {
	return rsaencryption.HashRsaKey(x509.MarshalPKCS1PrivateKey(p.privKey))
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
	return EncryptRSA(p, data)
}

func (p *publicKey) Verify(data []byte, signature []byte) error {
	return VerifyRSA(p, data, signature)
}

func (p *publicKey) Base64() ([]byte, error) {
	b, err := rsaencryption.ExtractPublicKey(p.pubKey)
	if err != nil {
		return nil, err
	}
	return []byte(b), err
}
