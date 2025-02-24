package discovery

import (
	crand "crypto/rand"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
)

func Test_ToMultiAddr(t *testing.T) {
	node := localNodeMock(t)

	ma, err := ToMultiAddr(node.Node())
	require.NoError(t, err)
	parts := strings.Split(ma.String(), "/")
	require.True(t, len(ma.Protocols()) >= 2) // ip4, tcp
	require.True(t, len(parts) >= 6)
}

func Test_ToPeer(t *testing.T) {
	node := localNodeMock(t)

	ai, err := ToPeer(node.Node())
	require.NoError(t, err)
	require.Equal(t, 1, len(ai.Addrs))
}

func Test_ParseENR(t *testing.T) {
	nodes, err := ParseENR(nil, true,
		"enr:-Km4QH9oua5xsG_0IN3oxiv5PBb10QXMkMvDeg2IrSSDlRxtONu9hShTmAZm2LjjADQOxGzBxd8VzXYFukmJULzcwrkBh2"+
			"F0dG5ldHOIAAAAAAAAAACCaWSCdjSCaXCEA2WKt4Jwa4kxZmY3MmY3OQGJc2VjcDI1NmsxoQMN5-_WgtENfdSLAfS3vToaRI7rlrPZ5u"+
			"ML3-_lQZXLJoN0Y3CCMsiDdWRwgi7g",
	)
	require.NoError(t, err)
	require.Equal(t, 1, len(nodes))
	require.Equal(t, "3.101.138.183", nodes[0].IP().String())
}

func localNodeMock(t *testing.T) *enode.LocalNode {
	sk, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	pk, err := commons.ECDSAPrivFromInterface(sk)
	require.NoError(t, err)
	ip, err := commons.IPAddr()
	require.NoError(t, err)
	node, err := createLocalNode(pk, "", ip, 12000, 13000)
	require.NoError(t, err)
	return node
}
