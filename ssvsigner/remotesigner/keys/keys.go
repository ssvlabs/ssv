package keys

import (
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
)

// Content of this file is copied from https://github.com/ssvlabs/ssv/blob/e12abf7dfbbd068b99612fa2ebbe7e3372e57280/operator/keys/keys.go#L14
// to avoid using CGO because ssv-node requires it.

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

	privKey, err := PemToPrivateKey(operatorKeyByte)
	if err != nil {
		return nil, err
	}

	return &privateKey{privKey: privKey}, nil
}

func PrivateKeyFromBytes(pemData []byte) (OperatorPrivateKey, error) {
	privKey, err := PemToPrivateKey(pemData)
	if err != nil {
		return nil, fmt.Errorf("can't decode operator private key: %w", err)
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
	return DecodeKey(p.privKey, data)
}

func (p *privateKey) Bytes() []byte {
	return PrivateKeyToByte(p.privKey)
}

func (p *privateKey) Base64() []byte {
	return []byte(ExtractPrivateKey(p.privKey))
}

func (p *privateKey) StorageHash() (string, error) {
	return HashRsaKey(PrivateKeyToByte(p.privKey))
}

func (p *privateKey) EKMHash() (string, error) {
	return HashRsaKey(x509.MarshalPKCS1PrivateKey(p.privKey))
}

func PublicKeyFromString(pubKeyString string) (OperatorPublicKey, error) {
	pubPem, err := base64.StdEncoding.DecodeString(pubKeyString)
	if err != nil {
		return nil, err
	}

	pubKey, err := ConvertPemToPublicKey(pubPem)
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
	b, err := ExtractPublicKey(p.pubKey)
	if err != nil {
		return nil, err
	}
	return []byte(b), err
}

type privateKey struct {
	privKey *rsa.PrivateKey
}

type publicKey struct {
	pubKey *rsa.PublicKey
}

func SignRSA(priv *privateKey, data []byte) ([]byte, error) {
	return rsa.SignPKCS1v15(crand.Reader, priv.privKey, crypto.SHA256, data)
}

func EncryptRSA(pub *publicKey, data []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(crand.Reader, pub.pubKey, data)
}

func VerifyRSA(pub *publicKey, data, signature []byte) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(pub.pubKey, crypto.SHA256, hash[:], signature)
}
