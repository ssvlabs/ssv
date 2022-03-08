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
	sk := fromInterfacePrivKey(priv)
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

func TestNsToSubnet(t *testing.T) {
	tests := []struct {
		name     string
		ns       string
		expected uint64
		isSubnet bool
	}{
		{
			"dummy string",
			"xxx",
			uint64(0),
			false,
		},
		{
			"invalid int",
			"ssv.subnets.xxx",
			uint64(0),
			false,
		},
		{
			"invalid",
			"ssv.1",
			uint64(0),
			false,
		},
		{
			"valid",
			"ssv.subnets.21",
			uint64(21),
			true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.isSubnet, isSubnet(test.ns))
			require.Equal(t, test.expected, nsToSubnet(test.ns))
		})
	}
}
