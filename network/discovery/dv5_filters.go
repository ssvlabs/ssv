package discovery

import (
	"github.com/bloxapp/ssv/network/records"
	"github.com/ethereum/go-ethereum/p2p/enode"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"go.uber.org/zap"
)

// limitNodeFilter checks if limit exceeded
func (dvs *DiscV5Service) limitNodeFilter(node *enode.Node) bool {
	return !dvs.conns.Limit(libp2pnetwork.DirOutbound)
}

// forkVersionFilter checks if the node has the same fork version
func (dvs *DiscV5Service) forkVersionFilter(node *enode.Node) bool {
	forkv, err := records.GetForkVersionEntry(node.Record())
	if err != nil {
		dvs.logger.Warn("could not read fork version from node record", zap.Error(err))
		return false
	}
	return dvs.forkv == forkv
}

// badNodeFilter checks if the node was pruned or have a bad score
func (dvs *DiscV5Service) badNodeFilter(node *enode.Node) bool {
	pid, err := PeerID(node)
	if err != nil {
		dvs.logger.Warn("could not get peer ID from node record", zap.Error(err))
		return false
	}
	return !dvs.conns.IsBad(pid)
}

// subnetFilter checks if the node has an interest in the given subnet
func (dvs *DiscV5Service) subnetFilter(subnet uint64) func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		subnets, err := records.GetSubnetsEntry(node.Record())
		if err != nil {
			return false
		}
		return subnets[subnet] > 0
	}
}
