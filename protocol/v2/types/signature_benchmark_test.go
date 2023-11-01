package types

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
)

var (
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	signature  []byte
	data       = []byte("This is some test data for verification.")
	hashed     = sha256.Sum256(data)
)

var (
	privateKeyPSS *rsa.PrivateKey
	publicKeyPSS  *rsa.PublicKey
	pssSignature  []byte
	dataPSS       = []byte("This is some test data for PSS verification.")
	hashedPSS     = sha256.Sum256(dataPSS)
)

var (
	privateKeyFast *rsa.PrivateKey
	publicKeyFast  *rsa.PublicKey
	signatureFast  []byte
	dataFast       = []byte("This is test data for fast verification.")
	hashedFast     = md5.Sum(dataFast)
)

func init() {
	var err error
	privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	publicKey = &privateKey.PublicKey

	signature, err = rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		panic(err)
	}

	if err := bls.Init(bls.BLS12_381); err != nil {
		panic(err)
	}

	if err := bls.SetETHmode(bls.EthModeLatest); err != nil {
		panic(err)
	}

	privateKeyPSS, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	publicKeyPSS = &privateKeyPSS.PublicKey

	pssOptions := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthAuto,
		Hash:       crypto.SHA256,
	}

	pssSignature, err = rsa.SignPSS(rand.Reader, privateKeyPSS, crypto.SHA256, hashedPSS[:], pssOptions)
	if err != nil {
		panic(err)
	}

	privateKeyFast, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	publicKeyFast = &privateKeyFast.PublicKey

	signatureFast, err = rsa.SignPKCS1v15(rand.Reader, privateKeyFast, crypto.MD5, hashedFast[:])
	if err != nil {
		panic(err)
	}
}

func BenchmarkVerifyBLS(b *testing.B) {
	secKey := new(bls.SecretKey)
	secKey.SetByCSPRNG()
	pubKey := secKey.GetPublicKey()
	msg := []byte("This is some test data for verification.")
	sig := secKey.SignByte(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !sig.VerifyByte(pubKey, msg) {
			b.Fatal("Verification failed")
		}
	}
}

func BenchmarkVerifyPKCS1v15(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hashed[:], signature)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyPKCS1v15FastHash(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := rsa.VerifyPKCS1v15(publicKeyFast, crypto.MD5, hashedFast[:], signatureFast)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyPSS(b *testing.B) {
	pssOptions := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthAuto,
		Hash:       crypto.SHA256,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := rsa.VerifyPSS(publicKeyPSS, crypto.SHA256, hashedPSS[:], pssSignature, pssOptions)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignBLS(b *testing.B) {
	secKey := new(bls.SecretKey)
	secKey.SetByCSPRNG()
	msg := []byte("This is some test data for verification.")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = secKey.SignByte(msg)
	}
}

func BenchmarkSignPKCS1v15(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignPKCS1v15FastHash(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := rsa.SignPKCS1v15(rand.Reader, privateKeyFast, crypto.MD5, hashedFast[:])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignPSS(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pssOptions := &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthAuto,
			Hash:       crypto.SHA256,
		}

		_, err := rsa.SignPSS(rand.Reader, privateKeyPSS, crypto.SHA256, hashedPSS[:], pssOptions)
		if err != nil {
			b.Fatal(err)
		}
	}
}
