package commons

import (
	"crypto/ecdsa"
	"encoding/hex"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"
)

func TestECDSAPrivFromInterface(t *testing.T) {
	hexKey := "0f042adb4a9b3401e0cebad1ff1865fcb3e849b9f2a4880d1b1c9844ba50c816"

	rawKey, err := hex.DecodeString(hexKey)
	require.NoError(t, err)

	privKey, err := crypto.UnmarshalSecp256k1PrivateKey(rawKey)
	require.NoError(t, err)

	ecdsaPrivKey, err := ECDSAPrivFromInterface(privKey)
	require.NoError(t, err)
	require.NotNil(t, ecdsaPrivKey)

	require.IsType(t, &ecdsa.PrivateKey{}, ecdsaPrivKey)
	require.NotNil(t, ecdsaPrivKey.D)
	require.NotNil(t, ecdsaPrivKey.X)
	require.NotNil(t, ecdsaPrivKey.Y)
	require.NotNil(t, ecdsaPrivKey.Curve)

	require.Equal(t, ecdsaPrivKey.D.String(), "6792055902439951130224479433662882604105028919500185693322687975860017874966")
}
