package discovery

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/require"

	"testing"
)

func Test_ENR_NodeTypeEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	sk := fromInterfacePrivKey(priv)
	ip, err := commons.IPAddr()
	require.NoError(t, err)
	node, err := createLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)
	require.NotNil(t, node)

	nodeType, err := getNodeTypeEntry(node.Node().Record())
	require.NoError(t, err)
	require.Equal(t, Unknown, nodeType)
	err = setNodeTypeEntry(node, Exporter)
	require.NoError(t, err)
	fmt.Println(node.Node().String())
	nodeType, err = getNodeTypeEntry(node.Node().Record())
	require.NoError(t, err)
	require.Equal(t, Exporter, nodeType)
}

func Test_ENR_OperatorIDEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	sk := fromInterfacePrivKey(priv)
	ip, err := commons.IPAddr()
	pubkey := genPublicKey()
	require.NoError(t, err)
	node, err := createLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)

	oid, err := getOperatorIDEntry(node.Node().Record())
	require.NoError(t, err)
	require.Len(t, oid, 0)
	err = setOperatorIDEntry(node, operatorID(pubkey.SerializeToHexStr()))
	require.NoError(t, err)

	oid, err = getOperatorIDEntry(node.Node().Record())
	require.NoError(t, err)
	require.True(t, bytes.Equal([]byte(oid), []byte(operatorID(pubkey.SerializeToHexStr()))))
}

func genPublicKey() *bls.PublicKey {
	_ = bls.Init(bls.BLS12_381)
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	return sk.GetPublicKey()
}
