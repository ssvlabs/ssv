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

func SignRSA(priv *privateKey, data []byte) ([256]byte, error) {
	signature, err := rsa.SignPKCS1v15(rand.Reader, priv.privKey, crypto.SHA256, data)
	if err != nil {
		return [256]byte{}, err
	}

	var sig [256]byte
	copy(sig[:], signature)

	return sig, nil
}

func EncryptRSA(pub *publicKey, data []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, pub.pubKey, data)
}

func VerifyRSA(pub *publicKey, data []byte, signature [256]byte) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(pub.pubKey, crypto.SHA256, hash[:], signature[:])
}
