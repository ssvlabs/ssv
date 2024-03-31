//go:build !linux

package keys

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
)

type privateKey struct {
	privKey *rsa.PrivateKey
}

type publicKey struct {
	pubKey *rsa.PublicKey
}

func VerifyRSA(pub *publicKey, data, signature []byte) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(pub.pubKey, crypto.SHA256, hash[:], signature)
}
