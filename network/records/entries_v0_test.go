package records

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/require"

	"testing"
)

// TODO: remove this file in the future as we won't need node type and operator id entries post fork v1

func Test_ENR_NodeTypeEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	sk, err := commons.ConvertFromInterfacePrivKey(priv)
	require.NoError(t, err)
	ip, err := commons.IPAddr()
	require.NoError(t, err)
	node, err := CreateLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)
	require.NotNil(t, node)

	nodeType, err := GetNodeTypeEntry(node.Node().Record())
	require.NoError(t, err)
	require.Equal(t, Unknown, nodeType)
	err = SetNodeTypeEntry(node, Exporter)
	require.NoError(t, err)
	fmt.Println(node.Node().String())
	nodeType, err = GetNodeTypeEntry(node.Node().Record())
	require.NoError(t, err)
	require.Equal(t, Exporter, nodeType)
}

func Test_ENR_OperatorIDEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	sk, err := commons.ConvertFromInterfacePrivKey(priv)
	require.NoError(t, err)
	ip, err := commons.IPAddr()
	pubkey := genPublicKey()
	require.NoError(t, err)
	node, err := CreateLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)

	oid, err := GetOperatorIDEntry(node.Node().Record())
	require.NoError(t, err)
	require.Len(t, oid, 0)
	err = SetOperatorIDEntry(node, operatorID(pubkey.SerializeToHexStr()))
	require.NoError(t, err)

	oid, err = GetOperatorIDEntry(node.Node().Record())
	require.NoError(t, err)
	require.True(t, bytes.Equal([]byte(oid), []byte(operatorID(pubkey.SerializeToHexStr()))))
}

func genPublicKey() *bls.PublicKey {
	_ = bls.Init(bls.BLS12_381)
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	return sk.GetPublicKey()
}

// operatorID returns sha256 (hex) of the given operator public key
func operatorID(pubkeyHex string) string {
	if len(pubkeyHex) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(pubkeyHex)))
}
