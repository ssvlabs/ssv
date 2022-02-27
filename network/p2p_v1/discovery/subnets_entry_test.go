package discovery

import (
	crand "crypto/rand"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_SubnetsEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	sk := convertFromInterfacePrivKey(priv)
	ip, err := commons.IPAddr()
	require.NoError(t, err)
	node, err := createLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)

	subnets := make([]bool, SubnetsCount)
	for i := 0; i < SubnetsCount; i++ {
		if i%4 == 0 {
			subnets[i] = true
		}
	}
	require.NoError(t, setSubnetsEntry(node, subnets))
	t.Log("ENR with subnets :", node.Node().String())

	subnetsFromEnr, err := getSubnetsEntry(node.Node())
	require.NoError(t, err)
	require.Len(t, subnetsFromEnr, SubnetsCount)
	for i := 0; i < SubnetsCount; i++ {
		if i%4 == 0 {
			require.True(t, subnetsFromEnr[i])
		} else {
			require.False(t, subnetsFromEnr[i])
		}
	}
}
