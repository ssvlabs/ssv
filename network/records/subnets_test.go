package records

import (
	crand "crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
)

func Test_SubnetsEntry(t *testing.T) {
	SubnetsCount := 128
	priv, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	sk, err := commons.ECDSAPrivFromInterface(priv)
	require.NoError(t, err)
	ip, err := commons.IPAddr()
	require.NoError(t, err)
	node, err := CreateLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)

	subnets := make([]byte, SubnetsCount)
	for i := 0; i < SubnetsCount; i++ {
		if i%4 == 0 {
			subnets[i] = 1
		} else {
			subnets[i] = 0
		}
	}
	require.NoError(t, SetSubnetsEntry(node, subnets))
	t.Log("ENR with subnets :", node.Node().String())

	subnetsFromEnr, err := GetSubnetsEntry(node.Node().Record())
	require.NoError(t, err)
	require.Len(t, subnetsFromEnr, SubnetsCount)
	for i := 0; i < SubnetsCount; i++ {
		if i%4 == 0 {
			require.Equal(t, byte(1), subnetsFromEnr[i])
		} else {
			require.Equal(t, byte(0), subnetsFromEnr[i])
		}
	}
}
