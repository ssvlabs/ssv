package p2p

import (
	"context"
	"crypto/rand"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

const (
	testUA = "test:0.0.0:xxx"
)

func TestNewPeersIndex(t *testing.T) {
	ctx := context.Background()

	host1, pi1 := newHostWithPeersIndex(ctx, t, testUA+"1")
	host2, pi2 := newHostWithPeersIndex(ctx, t, testUA+"2")

	require.NoError(t, host1.Connect(context.TODO(), peer.AddrInfo{
		ID:    host2.ID(),
		Addrs: host2.Addrs(),
	}))

	time.After(1 * time.Second)

	pi1.Run()
	pi2.Run()

	t.Run("non exist peer", func(t *testing.T) {
		ua, found, err := pi1.GetData("xxx", UserAgentKey)
		require.NoError(t, err)
		require.False(t, found)
		require.Equal(t, "", ua.(string))
	})

	t.Run("non exist property", func(t *testing.T) {
		data, found, err := pi1.GetData(host2.ID(), "xxx")
		require.NoError(t, err)
		require.False(t, found)
		require.Equal(t, "", data.(string))
	})

	t.Run("get other peers data", func(t *testing.T) {
		// get peer 2 data from peers index 1
		data, found, err := pi1.GetData(host2.ID(), UserAgentKey)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, testUA+"2", data.(string))
		// get peer 1 data from peers index 2
		data, found, err = pi2.GetData(host1.ID(), UserAgentKey)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, testUA+"1", data.(string))
	})

	t.Run("prune", func(t *testing.T) {
		require.False(t, pi1.Pruned(host1.ID()))
		pi1.Prune(host1.ID())
		require.True(t, pi1.Pruned(host1.ID()))
	})
}

func TestPeersIndex_IndexNode(t *testing.T) {
	ctx := context.Background()
	ua := "test:0.0.0:xxx"

	_, pi := newHostWithPeersIndex(ctx, t, ua+"1")
	node, pid, oid := newNode(t)
	require.False(t, pi.Indexed(pid))
	pi.IndexNode(node.Node())
	require.True(t, pi.Indexed(pid))
	raw, found, err := pi.GetData(pid, NodeRecordKey)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, raw)
	n := new(enode.Node)
	err = n.UnmarshalText(raw.([]byte))
	require.NoError(t, err)
	require.Equal(t, node.Node().String(), n.String())

	oidFromIndex, found, err := pi.GetData(pid, OperatorIDKey)
	require.Equal(t, oid, oidFromIndex.(string))
	require.NoError(t, err)
	require.True(t, found)
	// check reindex
	pi.IndexNode(node.Node())
}

func newNode(t *testing.T) (*enode.LocalNode, peer.ID, string) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	pk := convertFromInterfacePrivKey(priv)
	ip, err := ipAddr()
	pubkey := genPublicKey()
	require.NoError(t, err)
	node, err := createLocalNode(pk, ip, 12000, 13000)
	require.NoError(t, err)

	oid := operatorID(pubkey.SerializeToHexStr())
	node, err = addOperatorIDEntry(node, oid)
	require.NoError(t, err)
	pid, err := peer.IDFromPublicKey(priv.GetPublic())
	require.NoError(t, err)

	return node, pid, oid
}

func newHostWithPeersIndex(ctx context.Context, t *testing.T, ua string) (host.Host, PeersIndex) {
	host, err := libp2p.New(ctx,
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.UserAgent(ua))
	require.NoError(t, setupMdnsDiscovery(ctx, zap.L(), host))
	require.NoError(t, err)
	ids, err := identify.NewIDService(host, identify.UserAgent(ua))
	require.NoError(t, err)
	pi := NewPeersIndex(zaptest.NewLogger(t).With(zap.String("ua", ua)), host, ids)

	return host, pi
}
