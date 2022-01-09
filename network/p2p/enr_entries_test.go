package p2p

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/require"

	"testing"
)

func Test_ENR_NodeTypeEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	pk := convertFromInterfacePrivKey(priv)
	ip, err := ipAddr()
	require.NoError(t, err)
	node, err := createLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)
	require.NotNil(t, node)

	nodeType, err := extractNodeTypeEntry(node.Node().Record())
	require.NoError(t, err)
	require.Equal(t, Unknown, nodeType)
	node, err = addNodeTypeEntry(node, Exporter)
	require.NoError(t, err)
	fmt.Println(node.Node().String())
	nodeType, err = extractNodeTypeEntry(node.Node().Record())
	require.NoError(t, err)
	require.Equal(t, Exporter, nodeType)
}

func Test_ENR_OperatorIDEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	pk := convertFromInterfacePrivKey(priv)
	ip, err := ipAddr()
	pubkey := genPublicKey()
	require.NoError(t, err)
	node, err := createLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)

	pkHash, err := extractOperatorIDEntry(node.Node().Record())
	require.NoError(t, err)
	require.Nil(t, pkHash)
	node, err = addOperatorIDEntry(node, operatorID(pubkey.SerializeToHexStr()))
	require.NoError(t, err)

	pkHash, err = extractOperatorIDEntry(node.Node().Record())
	require.NoError(t, err)
	require.True(t, bytes.Equal([]byte(*pkHash), []byte(operatorID(pubkey.SerializeToHexStr()))))
}

func genPublicKey() *bls.PublicKey {
	_ = bls.Init(bls.BLS12_381)
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	return sk.GetPublicKey()
}
