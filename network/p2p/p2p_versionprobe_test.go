package p2pv1

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/utils/ttl"
)

func TestDiscoveredPeerAddrs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := peer.ID("peer-a")
	addr := ma.StringCast("/ip4/10.0.0.1/tcp/13001")
	n := &p2pNetwork{discoveredPeersPool: ttl.New[peer.ID, discovery.DiscoveredPeer](ctx, time.Minute, time.Minute)}
	n.discoveredPeersPool.Set(id, discovery.DiscoveredPeer{AddrInfo: peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{addr}}})
	require.Equal(t, []ma.Multiaddr{addr}, n.DiscoveredPeerAddrs(id))
	require.Nil(t, n.DiscoveredPeerAddrs(peer.ID("other")))
	require.Nil(t, (&p2pNetwork{}).DiscoveredPeerAddrs(id))
}

func TestLookupPeerAddrsWithoutNodeProvider(t *testing.T) {
	_, err := (&p2pNetwork{}).LookupPeerAddrs(zap.NewNop(), peer.ID("x"))
	require.Error(t, err)
}

func TestPeerstoreAddrs(t *testing.T) {
	h, err := libp2p.New(libp2p.NoListenAddrs)
	require.NoError(t, err)
	defer h.Close()
	id := peer.ID("peer-b")
	addr := ma.StringCast("/ip4/10.0.0.2/tcp/13001")
	h.Peerstore().AddAddr(id, addr, time.Minute)
	n := &p2pNetwork{}
	n.host.Store(&h) // the field is atomic.Pointer[host.Host] at this commit
	require.Equal(t, []ma.Multiaddr{addr}, n.PeerstoreAddrs(id))
	require.Empty(t, n.PeerstoreAddrs(peer.ID("other")))
	require.Nil(t, (&p2pNetwork{}).PeerstoreAddrs(id))
}

// fakeNodeDisc satisfies discovery.Service via the embedded (nil) interface, which is never
// called here, and discovery.NodeProvider with a fixed node.
type fakeNodeDisc struct {
	discovery.Service
	node   *enode.Node
	lookup []peer.ID
}

func (f *fakeNodeDisc) Self() *enode.LocalNode { return nil }

func (f *fakeNodeDisc) Node(_ *zap.Logger, info peer.AddrInfo) (*enode.Node, error) {
	f.lookup = append(f.lookup, info.ID)
	return f.node, nil
}

// noLookupDisc satisfies discovery.Service but not discovery.NodeProvider (like mdns).
type noLookupDisc struct{ discovery.Service }

func testENRNode(t *testing.T, ip net.IP, tcp int) *enode.Node {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	var r enr.Record
	r.Set(enr.IP(ip))
	r.Set(enr.TCP(tcp))
	require.NoError(t, enode.SignV4(&r, key))
	node, err := enode.New(enode.ValidSchemes, &r)
	require.NoError(t, err)
	return node
}

func TestLookupPeerAddrsFromNodeProvider(t *testing.T) {
	id := peer.ID("peer-c")

	t.Run("found", func(t *testing.T) {
		disc := &fakeNodeDisc{node: testENRNode(t, net.ParseIP("10.0.0.3"), 13001)}
		n := &p2pNetwork{disc: disc}
		addrs, err := n.LookupPeerAddrs(zap.NewNop(), id)
		require.NoError(t, err)
		require.Equal(t, []ma.Multiaddr{ma.StringCast("/ip4/10.0.0.3/tcp/13001")}, addrs)
		require.Equal(t, []peer.ID{id}, disc.lookup)
	})

	t.Run("not found", func(t *testing.T) {
		n := &p2pNetwork{disc: &fakeNodeDisc{}}
		addrs, err := n.LookupPeerAddrs(zap.NewNop(), id)
		require.NoError(t, err)
		require.Nil(t, addrs)
	})

	t.Run("tcp port 0", func(t *testing.T) {
		n := &p2pNetwork{disc: &fakeNodeDisc{node: testENRNode(t, net.ParseIP("10.0.0.3"), 0)}}
		addrs, err := n.LookupPeerAddrs(zap.NewNop(), id)
		require.ErrorContains(t, err, "no tcp port")
		require.Nil(t, addrs)
	})

	t.Run("no node lookup", func(t *testing.T) {
		n := &p2pNetwork{disc: noLookupDisc{}}
		_, err := n.LookupPeerAddrs(zap.NewNop(), id)
		require.EqualError(t, err, "discovery service has no node lookup")
	})
}
