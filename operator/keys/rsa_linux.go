//go:build linux

package keys

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"math/big"

	"github.com/microsoft/go-crypto-openssl/openssl"
	"github.com/microsoft/go-crypto-openssl/openssl/bbig/bridge"
)

type publicKey struct {
	pubKey       *rsa.PublicKey
	cachedPubkey *openssl.PublicKeyRSA
}

func init() {
	// TODO: check multiple versions of openssl
	// TODO: fallback to stdlib when openssl is not available
	if err := openssl.Init(); err != nil {
		panic(err)
	}
}

func rsaPublicKeyToOpenSSL(pub *rsa.PublicKey) (*openssl.PublicKeyRSA, error) {
	return bridge.NewPublicKeyRSA(
		pub.N,
		big.NewInt(int64(pub.E)),
	)
}

func checkCachePubkey(pub *publicKey) (*openssl.PublicKeyRSA, error) {
	if pub.cachedPubkey != nil {
		return pub.cachedPubkey, nil
	}

	opub, err := rsaPublicKeyToOpenSSL(pub.pubKey)
	if err != nil {
		return nil, err
	}
	pub.cachedPubkey = opub

	return opub, nil
}

func VerifyRSA(pub *publicKey, data, signature []byte) error {
	opub, err := checkCachePubkey(pub)
	if err != nil {
		return err
	}
	hashed := sha256.Sum256(data)
	return openssl.VerifyRSAPKCS1v15(opub, crypto.SHA256, hashed[:], signature)
}
