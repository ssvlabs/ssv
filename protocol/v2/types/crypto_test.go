package types

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRSAEncode(t *testing.T) {
	h := sha256.New()
	h.Write([]byte(msg))
	dig := h.Sum(nil)

	require.Equal(t, digest, hex.EncodeToString(dig))

	sign, err := rsa.SignPKCS1v15(rand.Reader, rsaPrivateKey, crypto.SHA256, dig)
	require.NoError(t, err)

	require.Equal(t, encodedSig, hex.EncodeToString(sign))
}

func TestRSADecode(t *testing.T) {
	h := sha256.New()
	h.Write([]byte(msg))
	dig := h.Sum(nil)

	require.Equal(t, digest, hex.EncodeToString(dig))

	require.NoError(t, rsa.VerifyPKCS1v15(&rsaPrivateKey.PublicKey, crypto.SHA256, dig, rawSig))
}

func TestRSADummy(t *testing.T) {
	require.NoError(t, rsa.VerifyPKCS1v15(&rsaPrivateKey.PublicKey, crypto.SHA256, rawDigest, rawSig))
}
