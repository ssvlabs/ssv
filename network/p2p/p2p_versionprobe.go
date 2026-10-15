package p2pv1

import (
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/discovery"
)

// PeerAddrProvider offers read-only address lookups for the exporter's version prober; never
// dials. None of its methods open connections or change network state.
type PeerAddrProvider interface {
	// DiscoveredPeerAddrs returns the addresses of id from the discovered-peers pool, or nil.
	DiscoveredPeerAddrs(id peer.ID) []ma.Multiaddr
	// PeerstoreAddrs returns the addresses of id known to the main host's peerstore, or nil.
	PeerstoreAddrs(id peer.ID) []ma.Multiaddr
	// LookupPeerAddrs resolves id's ENR through discv5 and returns its TCP address.
	// It returns nil, nil when discovery doesn't know the node.
	LookupPeerAddrs(logger *zap.Logger, id peer.ID) ([]ma.Multiaddr, error)
}

var _ PeerAddrProvider = (*p2pNetwork)(nil)

// DiscoveredPeerAddrs implements PeerAddrProvider.
func (n *p2pNetwork) DiscoveredPeerAddrs(id peer.ID) []ma.Multiaddr {
	if n.discoveredPeersPool == nil {
		return nil
	}
	dp, ok := n.discoveredPeersPool.Get(id)
	if !ok {
		return nil
	}
	return dp.Addrs
}

// PeerstoreAddrs implements PeerAddrProvider.
func (n *p2pNetwork) PeerstoreAddrs(id peer.ID) []ma.Multiaddr {
	h := n.Host()
	if h == nil {
		return nil
	}
	return h.Peerstore().Addrs(id)
}

// LookupPeerAddrs implements PeerAddrProvider. It uses discovery.NodeProvider.Node, which
// checks the discv5 routing table and falls back to a discv5 lookup (UDP queries to other
// discovery nodes only; no libp2p connection to id is made).
func (n *p2pNetwork) LookupPeerAddrs(logger *zap.Logger, id peer.ID) ([]ma.Multiaddr, error) {
	if n.disc == nil {
		return nil, errors.New("discovery service is not set up")
	}
	np, ok := n.disc.(discovery.NodeProvider)
	if !ok {
		return nil, errors.New("discovery service has no node lookup")
	}
	node, err := np.Node(logger, peer.AddrInfo{ID: id})
	if err != nil {
		return nil, fmt.Errorf("discv5 node lookup: %w", err)
	}
	if node == nil {
		return nil, nil
	}
	// discovery.ToPeer accepts a zero TCP port and would yield an undialable /tcp/0 address.
	if node.TCP() == 0 {
		return nil, fmt.Errorf("node %s has no tcp port in its enr", node.ID())
	}
	ai, err := discovery.ToPeer(node)
	if err != nil {
		return nil, err
	}
	return ai.Addrs, nil
}
