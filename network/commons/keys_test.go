package commons

import (
	"encoding/hex"
	"testing"

	gcrypto "github.com/ethereum/go-ethereum/crypto"
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

	require.Equal(t, ecdsaPrivKey.D.String(), "6792055902439951130224479433662882604105028919500185693322687975860017874966")
	require.Equal(t, ecdsaPrivKey.X.String(), "22653320514410971312249902166871933285664081749262857866749567141267477006697")
	require.Equal(t, ecdsaPrivKey.Y.String(), "103853204202400939811590846319591563498962634102053730872842929232997685705657")
	require.Equal(t, ecdsaPrivKey.Curve, gcrypto.S256())

	require.Equal(t, ecdsaPrivKey.X.String(), "22653320514410971312249902166871933285664081749262857866749567141267477006697")
	require.Equal(t, ecdsaPrivKey.Y.String(), "103853204202400939811590846319591563498962634102053730872842929232997685705657")
	require.Equal(t, ecdsaPrivKey.Curve, gcrypto.S256())
}
