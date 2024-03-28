//go:build linux

package keys

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"math/big"

	//openssl "github.com/golang-fips/openssl/v2"
	//bbig "github.com/golang-fips/openssl/v2/bbig"

	openssl "github.com/microsoft/go-crypto-openssl/openssl"
	bbig "github.com/microsoft/go-crypto-openssl/openssl/bbig"
	"github.com/microsoft/go-crypto-openssl/openssl/bbig/bridge"
)

func init() {
	// TODO: check multiple versions of openssl
	// TODO: fallback to stdlib when openssl is not available
	if err := msopenssl.Init(); err != nil {
		panic(err)
	}
}

func rsaPrivateKeyToOpenSSL(priv *rsa.PrivateKey) (*openssl.PrivateKeyRSA, error) {
	return bridge.NewPrivateKeyRSA(
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
	return bridge.NewPublicKeyRSA(
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
	hashed := sha256.Sum256(data)
	return openssl.SignRSAPKCS1v15(opriv, crypto.SHA256, hashed[:])
}

func checkCachePubkey(pub *publicKey) (*openssl.PublicKeyRSA, error) {
	if pub.cachedPubkey != nil {
		opub, ok := pub.cachedPubkey.(*openssl.PublicKeyRSA)
		if ok {
			return opub, nil
		}
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
