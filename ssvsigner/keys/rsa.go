//go:build !linux

package keys

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
)

type privateKey struct {
	privKey *rsa.PrivateKey
}

type publicKey struct {
	pubKey *rsa.PublicKey
}

func SignRSA(priv *privateKey, data []byte) ([]byte, error) {
	return rsa.SignPKCS1v15(rand.Reader, priv.privKey, crypto.SHA256, data)
}

func EncryptRSA(pub *publicKey, data []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, pub.pubKey, data)
}

func VerifyRSA(pub *publicKey, data, signature []byte) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(pub.pubKey, crypto.SHA256, hash[:], signature)
}
