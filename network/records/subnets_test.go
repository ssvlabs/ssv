package records

import (
	crand "crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
)

func Test_SubnetsEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	sk, err := commons.ECDSAPrivFromInterface(priv)
	require.NoError(t, err)
	ip, err := commons.IPAddr()
	require.NoError(t, err)
	node, err := CreateLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)

	subnets := commons.Subnets{}
	for i := uint64(0); i < commons.SubnetsCount; i++ {
		if i%4 == 0 {
			subnets.Set(i)
		} else {
			subnets.Clear(i)
		}
	}
	require.NoError(t, SetSubnetsEntry(node, subnets))
	t.Log("ENR with subnets :", node.Node().String())

	subnetsFromEnr, err := GetSubnetsEntry(node.Node().Record())
	require.NoError(t, err)

	for i := uint64(0); i < commons.SubnetsCount; i++ {
		if i%4 == 0 {
			require.True(t, subnetsFromEnr.IsSet(i))
		} else {
			require.False(t, subnetsFromEnr.IsSet(i))
		}
	}
}
