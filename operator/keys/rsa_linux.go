//go:build linux

package keys

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"math/big"
	"os"
	"runtime"
	"sync"

	"github.com/golang-fips/openssl/v2"
	"github.com/golang-fips/openssl/v2/bbig"
)

type privateKey struct {
	privKey       *rsa.PrivateKey
	cachedPrivKey *openssl.PrivateKeyRSA
	once          sync.Once
}

type publicKey struct {
	pubKey       *rsa.PublicKey
	cachedPubkey *openssl.PublicKeyRSA
	once         sync.Once
}

func init() {
	// TODO: fallback to stdlib when openssl is not available
	version := getVersion()
	if err := openssl.Init(version); err != nil {
		panic(err)
	}
}

// getVersion returns the highest available OpenSSL version.
func getVersion() string {
	v := os.Getenv("GO_OPENSSL_VERSION_OVERRIDE")
	if v != "" {
		if runtime.GOOS == "linux" {
			return "libcrypto.so." + v
		}
		return v
	}
	versions := []string{"3", "1.1.1", "1.1", "11", "111", "1.0.2", "1.0.0", "10"}
	if runtime.GOOS == "windows" {
		if runtime.GOARCH == "amd64" {
			versions = []string{"libcrypto-3-x64", "libcrypto-3", "libcrypto-1_1-x64", "libcrypto-1_1", "libeay64", "libeay32"}
		} else {
			versions = []string{"libcrypto-3", "libcrypto-1_1", "libeay32"}
		}
	}
	for _, v = range versions {
		if runtime.GOOS == "windows" {
			v += ".dll"
		} else if runtime.GOOS == "darwin" {
			v = "libcrypto." + v + ".dylib"
		} else {
			v = "libcrypto.so." + v
		}
		if ok, _ := openssl.CheckVersion(v); ok {
			return v
		}
	}
	return "libcrypto.so"
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
	var err error
	priv.once.Do(func() {
		priv.cachedPrivKey, err = rsaPrivateKeyToOpenSSL(priv.privKey)
	})
	return priv.cachedPrivKey, err
}

func SignRSA(priv *privateKey, data []byte) ([]byte, error) {
	opriv, err := checkCachePrivkey(priv)
	if err != nil {
		return nil, err
	}
	return openssl.SignRSAPKCS1v15(opriv, crypto.SHA256, data)
}

func checkCachePubkey(pub *publicKey) (*openssl.PublicKeyRSA, error) {
	var err error
	pub.once.Do(func() {
		pub.cachedPubkey, err = rsaPublicKeyToOpenSSL(pub.pubKey)
	})
	return pub.cachedPubkey, err
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
