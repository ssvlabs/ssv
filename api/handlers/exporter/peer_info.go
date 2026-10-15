package exporter

import (
	"time"

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

// PeerInfo describes a peer as currently known to this node.
type PeerInfo struct {
	Version       string
	VersionSource string // "handshake" | "probe" | ""
	ProbedAt      time.Time
	Connected     bool
}

// PeerInfoResolver describes a peer as currently known to this node.
type PeerInfoResolver interface{ PeerInfo(id peer.ID) PeerInfo }

// ProbedVersions provides versions learned by probing peers this node isn't connected to.
type ProbedVersions interface {
	Version(id peer.ID) (version string, probedAt time.Time, ok bool)
}

type peerInfoResolver struct {
	index  nodeInfoIndex
	net    connectednessProvider
	probes ProbedVersions
}

// NewPeerInfoResolver resolves a peer's version from its last handshake (preferred) or, failing
// that, from a cached probe result, and its connectedness from the host network. probes may be nil.
func NewPeerInfoResolver(index nodeInfoIndex, net connectednessProvider, probes ProbedVersions) PeerInfoResolver {
	return peerInfoResolver{index: index, net: net, probes: probes}
}

func (r peerInfoResolver) PeerInfo(id peer.ID) PeerInfo {
	info := PeerInfo{Connected: r.net.Connectedness(id) == network.Connected}
	if ni := r.index.NodeInfo(id); ni != nil && ni.Metadata != nil && ni.Metadata.NodeVersion != "" {
		info.Version = ni.Metadata.NodeVersion
		info.VersionSource = "handshake"
		return info
	}
	if r.probes != nil {
		if v, at, ok := r.probes.Version(id); ok {
			info.Version = v
			info.VersionSource = "probe"
			info.ProbedAt = at
		}
	}
	return info
}
