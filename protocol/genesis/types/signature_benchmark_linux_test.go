//go:build linux

package types

import (
	"crypto"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/microsoft/go-crypto-openssl/openssl"
	"github.com/microsoft/go-crypto-openssl/openssl/bbig/bridge"
)

func init() {
	if err := openssl.Init(); err != nil {
		panic(err)
	}
}

func BenchmarkVerifyPKCS1v15OpenSSL(b *testing.B) {
	dataOpenSSL := []byte("This is test data for OpenSSL verification.")
	hashedOpenSSL := sha256.Sum256(dataOpenSSL)

	priv, pub := newOpenSSLRSAKey(2048)

	sig, err := openssl.SignRSAPKCS1v15(priv, crypto.SHA256, hashedOpenSSL[:])
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := openssl.VerifyRSAPKCS1v15(pub, crypto.SHA256, hashedOpenSSL[:], sig)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignPKCS1v15OpenSSL(b *testing.B) {
	dataOpenSSL := []byte("This is test data for OpenSSL verification.")
	hashedOpenSSL := sha256.Sum256(dataOpenSSL)

	priv, _ := newOpenSSLRSAKey(2048)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := openssl.SignRSAPKCS1v15(priv, crypto.SHA256, hashedOpenSSL[:])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func newOpenSSLRSAKey(size int) (*openssl.PrivateKeyRSA, *openssl.PublicKeyRSA) {
	N, E, D, P, Q, Dp, Dq, Qinv, err := bridge.GenerateKeyRSA(size)
	if err != nil {
		panic(fmt.Sprintf("GenerateKeyRSA(%d): %v", size, err))
	}
	priv, err := bridge.NewPrivateKeyRSA(N, E, D, P, Q, Dp, Dq, Qinv)
	if err != nil {
		panic(fmt.Sprintf("NewPrivateKeyRSA(%d): %v", size, err))
	}
	pub, err := bridge.NewPublicKeyRSA(N, E)
	if err != nil {
		panic(fmt.Sprintf("NewPublicKeyRSA(%d): %v", size, err))
	}
	return priv, pub
}
