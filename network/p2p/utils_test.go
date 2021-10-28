package p2p

import (
	"crypto/rand"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"strings"
	"testing"
)

func Test_convertToSingleMultiAddr(t *testing.T) {
	node := localnodeMock(t)

	ma, err := convertToSingleMultiAddr(node.Node())
	require.NoError(t, err)
	parts := strings.Split(ma.String(), "/")
	require.True(t, len(ma.Protocols()) >= 2) // ip4, tcp
	require.True(t, len(parts) >= 6)
}

func Test_convertToMultiAddr(t *testing.T) {
	node := localnodeMock(t)

	mas := convertToMultiAddr(zap.L(), []*enode.Node{node.Node()})
	require.Equal(t, 1, len(mas))
}

func Test_convertToAddrInfo(t *testing.T) {
	node := localnodeMock(t)

	ai, ma, err := convertToAddrInfo(node.Node())
	require.NoError(t, err)
	parts := strings.Split(ma.String(), "/")
	require.True(t, len(ma.Protocols()) >= 2) // ip4, tcp
	require.True(t, len(parts) >= 6)
	require.Equal(t, 1, len(ai.Addrs))
}

func Test_parseENRs(t *testing.T) {
	nodes, err := parseENRs([]string{
		"enr:-Km4QH9oua5xsG_0IN3oxiv5PBb10QXMkMvDeg2IrSSDlRxtONu9hShTmAZm2LjjADQOxGzBxd8VzXYFukmJULzcwrkBh2F0dG5ldHOIAAAAAAAAAACCaWSCdjSCaXCEA2WKt4Jwa4kxZmY3MmY3OQGJc2VjcDI1NmsxoQMN5-_WgtENfdSLAfS3vToaRI7rlrPZ5uML3-_lQZXLJoN0Y3CCMsiDdWRwgi7g",
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(nodes))
	require.Equal(t, "3.101.138.183", nodes[0].IP().String())
}

func Test_bootnodes(t *testing.T) {
	n := &p2pNetwork{logger: zap.L(), cfg: &Config{
		Discv5BootStrapAddr: []string{
			"enr:-Km4QH9oua5xsG_0IN3oxiv5PBb10QXMkMvDeg2IrSSDlRxtONu9hShTmAZm2LjjADQOxGzBxd8VzXYFukmJULzcwrkBh2F0dG5ldHOIAAAAAAAAAACCaWSCdjSCaXCEA2WKt4Jwa4kxZmY3MmY3OQGJc2VjcDI1NmsxoQMN5-_WgtENfdSLAfS3vToaRI7rlrPZ5uML3-_lQZXLJoN0Y3CCMsiDdWRwgi7g",
		},
	}}
	nodes, err := n.bootnodes()
	require.NoError(t, err)
	require.Equal(t, 1, len(nodes))
	require.Equal(t, "3.101.138.183", nodes[0].IP().String())
}

func localnodeMock(t *testing.T) *enode.LocalNode {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	pk := convertFromInterfacePrivKey(priv)
	ip, err := getIP()
	require.NoError(t, err)
	node, err := createBaseLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)
	return node
}
