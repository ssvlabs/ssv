//go:build linux

package keys

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_VerifyRegularSigWithOpenSSL(t *testing.T) {
	// Check valid signature.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	msg := []byte("hello")
	hashed := sha256.Sum256(msg)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	require.NoError(t, err)

	pk := &privateKey{key, nil, sync.Once{}}
	pub := pk.Public().(*publicKey)

	require.NoError(t, VerifyRSA(pub, msg, sig))

	// Check wrong signature.
	key2, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	hashed2 := sha256.Sum256(msg)
	sig2, err := rsa.SignPKCS1v15(rand.Reader, key2, crypto.SHA256, hashed2[:])
	require.NoError(t, err)

	require.Error(t, VerifyRSA(pub, msg, sig2))
}

func Test_VerifyOpenSSLWithOpenSSL(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	msg := []byte("hello")
	priv := &privateKey{key, nil, sync.Once{}}
	sig, err := priv.Sign(msg)
	require.NoError(t, err)

	pub := priv.Public().(*publicKey)

	require.NoError(t, VerifyRSA(pub, msg, sig[:]))

	// Verify with Go RSA.
	hash := sha256.Sum256(msg)
	err = rsa.VerifyPKCS1v15(pub.pubKey, crypto.SHA256, hash[:], sig[:])
	require.NoError(t, err)
}

func Test_ConversionError(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	key.D = nil
	msg := []byte("hello")
	priv := &privateKey{key, nil, sync.Once{}}
	_, err = priv.Sign(msg)
	require.Error(t, err)

	key2, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	priv2 := &privateKey{key2, nil, sync.Once{}}
	sig, err := priv2.Sign(msg)
	require.NoError(t, err)
	pub := priv2.Public().(*publicKey)

	pub.pubKey.N = nil
	require.Error(t, VerifyRSA(pub, msg, sig[:]))
}

func Test_Caches(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	msg := []byte("hello")
	priv := &privateKey{key, nil, sync.Once{}}
	sig, err := priv.Sign(msg)
	require.NoError(t, err)

	pub := priv.Public().(*publicKey)

	require.NoError(t, VerifyRSA(pub, msg, sig[:]))

	// should sign using cache
	require.NotNil(t, priv.cachedPrivKey)

	sig2, err := priv.Sign(msg)
	require.NoError(t, err)

	require.NotNil(t, pub.cachedPubkey)

	require.NoError(t, VerifyRSA(pub, msg, sig2[:]))
}
