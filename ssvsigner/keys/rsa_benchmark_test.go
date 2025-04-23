package keys

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
)

const (
	keySize = 2048
)

var (
	// msg we'll use for benchmarking.
	msg = []byte(`Some message example to be hashed for benchmarking, let's make it at 
		least 100 bytes long so we can benchmark against somewhat real-world message size. 
		Although it still might be way too small. Lajfklflfaslfjsalfalsfjsla fjlajlfaslkfjaslkf
		lasjflkasljfLFSJLfjsalfjaslfLKFsalfjalsfjalsfjaslfjaslfjlasflfslafasklfjsalfj;eqwfgh442`)
	// msgHash is a typical sha256 hash, we use it for benchmarking because it's the most
	// common type of data we work with.
	msgHash = func() []byte {
		hash := sha256.Sum256(msg)
		return hash[:]
	}()
)

var (
	privKey   *rsa.PrivateKey
	pubKey    *rsa.PublicKey
	signature []byte
	data      = []byte("This is some test data for verification.")
	hashed    = sha256.Sum256(data)
)

var (
	privKeyPSS   *rsa.PrivateKey
	pubKeyPSS    *rsa.PublicKey
	pssSignature []byte
	dataPSS      = []byte("This is some test data for PSS verification.")
	hashedPSS    = sha256.Sum256(dataPSS)
)

var (
	privKeyFast   *rsa.PrivateKey
	pubKeyFast    *rsa.PublicKey
	signatureFast []byte
	dataFast      = []byte("This is test data for fast verification.")
	hashedFast    = md5.Sum(dataFast)
)

func init() {
	var err error
	privKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	pubKey = &privKey.PublicKey

	signature, err = rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed[:])
	if err != nil {
		panic(err)
	}

	if err := bls.Init(bls.BLS12_381); err != nil {
		panic(err)
	}

	if err := bls.SetETHmode(bls.EthModeLatest); err != nil {
		panic(err)
	}

	privKeyPSS, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	pubKeyPSS = &privKeyPSS.PublicKey

	pssOptions := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthAuto,
		Hash:       crypto.SHA256,
	}

	pssSignature, err = rsa.SignPSS(rand.Reader, privKeyPSS, crypto.SHA256, hashedPSS[:], pssOptions)
	if err != nil {
		panic(err)
	}

	privKeyFast, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	pubKeyFast = &privKeyFast.PublicKey

	signatureFast, err = rsa.SignPKCS1v15(rand.Reader, privKeyFast, crypto.MD5, hashedFast[:])
	if err != nil {
		panic(err)
	}
}

func BenchmarkSignRSA(b *testing.B) {
	privKey, _ := genKeypair(b)

	for b.Loop() {
		_, err := SignRSA(privKey, msgHash)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncryptRSA(b *testing.B) {
	_, pubKey := genKeypair(b)

	for b.Loop() {
		_, err := EncryptRSA(pubKey, msgHash)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyRSA(b *testing.B) {
	privKey, pubKey := genKeypair(b)
	sig, err := SignRSA(privKey, msgHash)
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		err := VerifyRSA(pubKey, msg, sig)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyBLS(b *testing.B) {
	secKey := new(bls.SecretKey)
	secKey.SetByCSPRNG()
	pubKey := secKey.GetPublicKey()
	msg := []byte("This is some test data for verification.")
	sig := secKey.SignByte(msg)

	for b.Loop() {
		if !sig.VerifyByte(pubKey, msg) {
			b.Fatal("Verification failed")
		}
	}
}

func BenchmarkVerifyPKCS1v15(b *testing.B) {
	for b.Loop() {
		err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hashed[:], signature)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyPKCS1v15FastHash(b *testing.B) {
	for b.Loop() {
		err := rsa.VerifyPKCS1v15(pubKeyFast, crypto.MD5, hashedFast[:], signatureFast)
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
	for b.Loop() {
		err := rsa.VerifyPSS(pubKeyPSS, crypto.SHA256, hashedPSS[:], pssSignature, pssOptions)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignBLS(b *testing.B) {
	secKey := new(bls.SecretKey)
	secKey.SetByCSPRNG()
	msg := []byte("This is some test data for verification.")

	for b.Loop() {
		_ = secKey.SignByte(msg)
	}
}

func BenchmarkSignPKCS1v15(b *testing.B) {
	for b.Loop() {
		_, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed[:])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignPKCS1v15FastHash(b *testing.B) {
	for b.Loop() {
		_, err := rsa.SignPKCS1v15(rand.Reader, privKeyFast, crypto.MD5, hashedFast[:])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignPSS(b *testing.B) {
	for b.Loop() {
		pssOptions := &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthAuto,
			Hash:       crypto.SHA256,
		}

		_, err := rsa.SignPSS(rand.Reader, privKeyPSS, crypto.SHA256, hashedPSS[:], pssOptions)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func genKeypair(b *testing.B) (*privateKey, *publicKey) {
	pKey, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		b.Fatal(err)
	}
	return &privateKey{privKey: pKey}, &publicKey{pubKey: &pKey.PublicKey}
}
