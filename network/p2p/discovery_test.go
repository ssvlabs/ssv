package p2p

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/network"
	"github.com/stretchr/testify/require"
	"net"
	"strings"
	"testing"
)

func TestP2pNetwork_GetUserAgent(t *testing.T) {
	commons.SetBuildData("ssvtest", "v0.x.x")

	t.Run("with operator key", func(t *testing.T) {
		_, skBytes, err := rsaencryption.GenerateKeys()
		require.NoError(t, err)
		require.NotNil(t, skBytes)
		sk, err := rsaencryption.ConvertPemToPrivateKey(string(skBytes))
		require.NoError(t, err)
		require.NotNil(t, sk)
		n := p2pNetwork{operatorPrivKey: sk}
		ua := n.getUserAgent()
		parts := strings.Split(ua, ":")
		require.Equal(t, "ssvtest", parts[0])
		require.Equal(t, "v0.x.x", parts[1])
		pk, err := rsaencryption.ExtractPublicKey(sk)
		require.NoError(t, err)
		h := sha256.Sum256([]byte(pk))
		require.Equal(t, hex.EncodeToString(h[:]), parts[2])
	})

	t.Run("without operator key", func(t *testing.T) {
		n := p2pNetwork{}
		require.Equal(t, "ssvtest:v0.x.x", n.getUserAgent())
	})
}

func Test_ENR_OperatorPubKeyEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	pk := convertFromInterfacePrivKey(priv)
	ip, err := getIP()
	require.NoError(t, err)
	node, err := createLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)
	pubkey := genPublicKey()
	node, err = addOperatorPubKeyEntry(node, pubkey)
	require.NoError(t, err)
	fmt.Println("node.String", node.Node().String())

	pkHashRecord, err := extractOperatorPubKeyEntry(node.Node().Record())
	require.NoError(t, err)
	pkHash := []byte(pubKeyHash(pubkey))
	bitL, err := bitfield.NewBitlist64FromBytes(64, pkHash)
	require.NoError(t, err)
	require.True(t, bytes.Equal(pkHashRecord, bitL.ToBitlist().Bytes()))

	filter := filterPeerByOperatorPubKey(nil, pubkey)
	require.True(t, filter(node.Node()))

	node2, err := createLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)
	pubkey2 := genPublicKey()
	node2, err = addOperatorPubKeyEntry(node2, pubkey2)
	require.NoError(t, err)
	require.False(t, filter(node2.Node()))
}

func Test_filterPeerByOperatorPubKey(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	pk := convertFromInterfacePrivKey(priv)
	ip, err := getIP()
	require.NoError(t, err)

	node, err := createLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)
	pubkey := genPublicKey()
	node, err = addOperatorPubKeyEntry(node, pubkey)
	require.NoError(t, err)

	node2, err := createLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)
	pubkey2 := genPublicKey()
	node2, err = addOperatorPubKeyEntry(node2, pubkey2)
	require.NoError(t, err)

	filter := filterPeerByOperatorPubKey(nil, pubkey)
	filter2 := filterPeerByOperatorPubKey(func(node *enode.Node) bool {
		return true
	}, pubkey2)
	require.True(t, filter(node.Node()))
	require.False(t, filter(node2.Node()))
	require.True(t, filter2(node2.Node()))
}

func getIP() (net.IP, error) {
	ip, err := network.ExternalIP()
	if err != nil {
		return nil, err
	}
	return net.ParseIP(ip), nil
}

func genPublicKey() *bls.PublicKey {
	_ = bls.Init(bls.BLS12_381)
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	return sk.GetPublicKey()
}
