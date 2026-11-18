package connections_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/streams"
	"github.com/ssvlabs/ssv/utils/ttl"
)

// newRealNode wires the production index, handshaker and conn handler onto a real libp2p host.
func newRealNode(t *testing.T, ctx context.Context) (host.Host, peers.Index) {
	priv, _, err := crypto.GenerateKeyPair(crypto.Secp256k1, 0)
	require.NoError(t, err)
	h, err := libp2p.New(libp2p.Identity(priv), libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	subnets := func() commons.Subnets { return commons.AllSubnets }
	self := records.NewNodeInfo("0x00000000")
	self.Metadata = &records.NodeMetadata{NodeVersion: "test", Subnets: commons.AllSubnets.StringHex()}
	idx := peers.NewPeersIndex(zap.NewNop(), h.Network(), self, func(string) int { return 1000 },
		func() crypto.PrivKey { return priv }, peers.NewGossipScoreIndex())
	handshaker := connections.NewHandshaker(ctx, zap.NewNop(), &connections.HandshakerCfg{
		Network:         h.Network(),
		Streams:         streams.NewStreamController(ctx, h, 5*time.Second, 5*time.Second),
		NodeInfos:       idx,
		PeerInfos:       idx,
		ConnIdx:         idx,
		SubnetsIdx:      idx,
		PeerStateIdx:    idx,
		DomainTypeFn:    func() spectypes.DomainType { return spectypes.DomainType{} },
		SubnetsProvider: subnets,
	}, func() []connections.HandshakeFilter { return nil })
	h.SetStreamHandler(peers.NodeInfoProtocol, handshaker.Handler())
	h.Network().Notify(connections.NewConnHandler(ctx, zap.NewNop(), handshaker, subnets, idx, idx, idx, idx,
		ttl.New[peer.ID, discovery.DiscoveredPeer](ctx, time.Minute, time.Minute)).Handle())
	return h, idx
}

func TestPeerStateIsFreedOnDisconnect(t *testing.T) {
	ctx := t.Context()
	server, idx := newRealNode(t, ctx)

	const n = 45
	ids := make([]peer.ID, n)
	var wg sync.WaitGroup
	for i := range n {
		client, _ := newRealNode(t, ctx)
		ids[i] = client.ID()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer client.Close()
			if !assert.NoError(t, client.Connect(ctx, peer.AddrInfo{ID: server.ID(), Addrs: server.Addrs()})) {
				return
			}
			if i%3 == 0 {
				return // leaves mid-handshake
			}
			assert.Eventually(t, func() bool {
				return idx.State(client.ID()) == peers.StateConnected && idx.NodeInfo(client.ID()) != nil
			}, 10*time.Second, 5*time.Millisecond)
			if i%3 == 2 {
				// The server drops the handshaked peer instead of the client closing. A second
				// NodeInfoProtocol stream wouldn't reach the server's handler here: NewStream
				// negotiates lazily, so nothing is sent until the client writes to or closes it.
				assert.NoError(t, server.Network().ClosePeer(client.ID()))
			}
		}()
	}
	wg.Wait()

	require.Eventually(t, func() bool { return len(server.Network().Peers()) == 0 }, 10*time.Second, 10*time.Millisecond)
	time.Sleep(1500 * time.Millisecond) // the inbound wait loop notices a lost connection on its 1s tick
	// The cleanups run on the server's own goroutines, so give a loaded machine time to finish them.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		for _, id := range ids {
			_, hasSubnets := idx.GetPeerSubnets(id)
			assert.False(c, hasSubnets, "subnets left for %s", id)
			assert.Nil(c, idx.PeerInfo(id), "peer info left for %s", id)
		}
	}, 10*time.Second, 50*time.Millisecond)
}
