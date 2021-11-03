package p2p

import (
	"bytes"
	"crypto/rand"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"

	"testing"
)

func Test_ENR_OperatorPubKeyEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	pk := convertFromInterfacePrivKey(priv)
	ip, err := ipAddr()
	pubkey := genPublicKey()
	require.NoError(t, err)
	node, err := createLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)
	node, err = addOperatorPubKeyEntry(node, []byte(pubKeyHash(pubkey.SerializeToHexStr())))
	require.NoError(t, err)

	pkHashRecord, err := extractOperatorPubKeyEntry(node.Node().Record())
	require.NoError(t, err)
	pkHash := []byte(pubKeyHash(pubkey.SerializeToHexStr()))
	bitL, err := bitfield.NewBitlist64FromBytes(64, pkHash)
	require.NoError(t, err)
	require.True(t, bytes.Equal(pkHashRecord, bitL.ToBitlist().Bytes()))
}

func genPublicKey() *bls.PublicKey {
	_ = bls.Init(bls.BLS12_381)
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	return sk.GetPublicKey()
}
