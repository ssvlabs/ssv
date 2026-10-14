package exporter

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/network/records"
)

type nodeInfoIndex interface {
	NodeInfo(id peer.ID) *records.NodeInfo
}

type connectednessProvider interface {
	Connectedness(id peer.ID) network.Connectedness
}

type peerInfoResolver struct {
	index nodeInfoIndex
	net   connectednessProvider
}

// NewPeerInfoResolver resolves a peer's version from its last handshake and its
// connectedness from the host network.
func NewPeerInfoResolver(index nodeInfoIndex, net connectednessProvider) PeerInfoResolver {
	return peerInfoResolver{index: index, net: net}
}

func (r peerInfoResolver) PeerInfo(id peer.ID) (string, bool) {
	connected := r.net.Connectedness(id) == network.Connected
	info := r.index.NodeInfo(id)
	if info == nil || info.Metadata == nil {
		return "", connected
	}
	return info.Metadata.NodeVersion, connected
}
