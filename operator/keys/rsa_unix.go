//go:build !darwin

package keys

import (
	"crypto"
	"crypto/rsa"
	"math/big"

	openssl "github.com/golang-fips/openssl/v2"
	bbig "github.com/golang-fips/openssl/v2/bbig"
)

func init() {
	// TODO: check multiple versions of openssl
	// TODO: fallback to stdlib when openssl is not available
	if err := openssl.Init("libcrypto.so.3"); err != nil {
		panic(err)
	}
}

func rsaPrivateKeyToOpenSSL(priv *rsa.PrivateKey) (*openssl.PrivateKeyRSA, error) {
	return openssl.NewPrivateKeyRSA(
		bbig.Enc(priv.N),
		bbig.Enc(big.NewInt(int64(priv.E))),
		bbig.Enc(priv.D),
		bbig.Enc(priv.Primes[0]),
		bbig.Enc(priv.Primes[1]),
		bbig.Enc(priv.Precomputed.Dp),
		bbig.Enc(priv.Precomputed.Dq),
		bbig.Enc(priv.Precomputed.Qinv),
	)
}

func rsaPublicKeyToOpenSSL(pub *rsa.PublicKey) (*openssl.PublicKeyRSA, error) {
	return openssl.NewPublicKeyRSA(
		bbig.Enc(pub.N),
		bbig.Enc(big.NewInt(int64(pub.E))),
	)
}

func checkCachePrivkey(priv *privateKey) (*openssl.PrivateKeyRSA, error) {
	if priv.cachedPrivKey != nil {
		opriv, ok := priv.cachedPrivKey.(*openssl.PrivateKeyRSA)
		if ok {
			return opriv, nil
		}
	}
	return rsaPrivateKeyToOpenSSL(priv.privKey)
}

func SignRSA(priv *privateKey, data []byte) ([]byte, error) {
	opriv, err := checkCachePrivkey(priv)
	if err != nil {
		return nil, err
	}
	return openssl.HashSignRSAPKCS1v15(opriv, crypto.SHA256, data)
}

func checkCachePubkey(pub *publicKey) (*openssl.PublicKeyRSA, error) {
	if pub.cachedPubkey != nil {
		opub, ok := pub.cachedPubkey.(*openssl.PublicKeyRSA)
		if ok {
			return opub, nil
		}
	}

	return rsaPublicKeyToOpenSSL(pub.pubKey)
}

func EncryptRSA(pub *publicKey, data []byte) ([]byte, error) {
	opub, err := rsaPublicKeyToOpenSSL(pub.pubKey)
	if err != nil {
		return nil, err
	}
	return openssl.EncryptRSAPKCS1(opub, data)
}

func VerifyRSA(pub *publicKey, data, signature []byte) error {
	opub, err := rsaPublicKeyToOpenSSL(pub.pubKey)
	if err != nil {
		return err
	}
	return openssl.HashVerifyRSAPKCS1v15(opub, crypto.SHA256, data, signature)
}
