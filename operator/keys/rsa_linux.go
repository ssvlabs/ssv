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

type privateKey struct {
	privKey       *rsa.PrivateKey
	cachedPrivKey openssl.PrivateKeyRSA
}

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

func rsaPrivateKeyToOpenSSL(priv *rsa.PrivateKey) (*openssl.PrivateKeyRSA, error) {
	return bridge.NewPrivateKeyRSA(
		priv.N,
		big.NewInt(int64(priv.E)),
		priv.D,
		priv.Primes[0],
		priv.Primes[1],
		priv.Precomputed.Dp,
		priv.Precomputed.Dq,
		priv.Precomputed.Qinv,
	)
}

func rsaPublicKeyToOpenSSL(pub *rsa.PublicKey) (*openssl.PublicKeyRSA, error) {
	return bridge.NewPublicKeyRSA(
		pub.N,
		big.NewInt(int64(pub.E)),
	)
}

func checkCachePrivkey(priv *privateKey) (*openssl.PrivateKeyRSA, error) {
	if priv.cachedPrivKey != nil {
		return priv.cachedPrivKey, nil
	}
	opriv, err := rsaPrivateKeyToOpenSSL(priv.privKey)
	if err != nil {
		return nil, err
	}
	priv.cachedPrivKey = opriv

	return opriv, nil
}

func SignRSA(priv *privateKey, data []byte) ([]byte, error) {
	opriv, err := checkCachePrivkey(priv)
	if err != nil {
		return nil, err
	}
	return openssl.SignRSAPKCS1v15(opriv, crypto.SHA256, data)
}

func checkCachePubkey(pub *publicKey) (*openssl.PublicKeyRSA, error) {
	if pub.cachedPubkey != nil {
		return pub.cachePubkey, nil
	}

	opub, err := rsaPublicKeyToOpenSSL(pub.pubKey)
	if err != nil {
		return nil, err
	}
	pub.cachedPubkey = opub

	return opub, nil
}

func EncryptRSA(pub *publicKey, data []byte) ([]byte, error) {
	opub, err := checkCachePubkey(pub)
	if err != nil {
		return nil, err
	}
	return openssl.EncryptRSAPKCS1(opub, data)
}

func VerifyRSA(pub *publicKey, data, signature []byte) error {
	opub, err := checkCachePubkey(pub)
	if err != nil {
		return err
	}
	hashed := sha256.Sum256(data)
	return openssl.VerifyRSAPKCS1v15(opub, crypto.SHA256, hashed[:], signature)
}
