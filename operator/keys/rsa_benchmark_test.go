package keys

import (
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"testing"
)

const (
	keySize = 2048
)

var (
	// msg we'll use for benchmarking.
	msg = []byte("Some message example to be hashed for benchmarking, let's make it at " +
		"least 100 bytes long so we can benchmark against somewhat real-world message size. " +
		"Although it still might be way too small. Lajfklflfaslfjsalfalsfjsla fjlajlfaslkfjaslkf" +
		"lasjflkasljfLFSJLfjsalfjaslfLKFsalfjalsfjalsfjaslfjaslfjlasflfslafasklfjsalfj;eqwfgh442")
	// msgHash is a typical sha256 hash, we use it for benchmarking because it's the most
	// common type of data we work with.
	msgHash = func() []byte {
		hash := sha256.Sum256(msg)
		return hash[:]
	}()
)

func BenchmarkSignRSA(b *testing.B) {
	privKey, _ := genKeypair(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := SignRSA(privKey, msgHash)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncryptRSA(b *testing.B) {
	_, pubKey := genKeypair(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := EncryptRSA(pubKey, msgHash)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyRSA(b *testing.B) {
	privKey, pubKey := genKeypair(b)
	signature, err := SignRSA(privKey, msgHash)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := VerifyRSA(pubKey, msg, signature)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func genKeypair(b *testing.B) (*privateKey, *publicKey) {
	pKey, err := rsa.GenerateKey(crand.Reader, keySize)
	if err != nil {
		b.Fatal(err)
	}
	return &privateKey{privKey: pKey}, &publicKey{pubKey: &pKey.PublicKey}
}
