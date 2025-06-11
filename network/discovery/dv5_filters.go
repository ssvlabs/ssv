package discovery

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
)

// limitNodeFilter returns true if the limit is exceeded
func (dvs *DiscV5Service) limitNodeFilter(node *enode.Node) bool {
	return !dvs.conns.AtLimit(libp2pnetwork.DirOutbound)
}

//// forkVersionFilter checks if the node has the same fork version
// func (dvs *DiscV5Service) forkVersionFilter(logger *zap.Logger, node *enode.Node) bool {
//	forkv, err := records.GetForkVersionEntry(node.Record())
//	if err != nil {
//		logger.Warn("could not read fork version from node record", zap.Error(err))
//		return false
//	}
//	return dvs.forkv == forkv
//}

// badNodeFilter checks if the node was pruned or have a bad score
func (dvs *DiscV5Service) badNodeFilter(logger *zap.Logger) func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		pid, err := PeerID(node)
		if err != nil {
			logger.Warn("could not get peer ID from node record", zap.Error(err))
			return false
		}
		return !dvs.conns.IsBad(logger, pid)
	}
}

// badNodeFilter checks if the node was pruned or have a bad score
func (dvs *DiscV5Service) ssvNodeFilter(logger *zap.Logger) func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		var isSSV = new(bool)
		if err := node.Record().Load(enr.WithEntry("ssv", isSSV)); err != nil {
			//TODO: metric
			//logger.Warn("could not read ssv entry from node record", zap.String("enr", node.String()), zap.Error(err))
			return false
		}
		return *isSSV
	}
}

func (dvs *DiscV5Service) alreadyConnectedFilter() func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		pid, err := PeerID(node)
		if err != nil {
			return false
		}
		return dvs.conns.Connectedness(pid) != libp2pnetwork.Connected
	}
}

func (dvs *DiscV5Service) recentlyTrimmedFilter() func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		pid, err := PeerID(node)
		if err != nil {
			return false
		}
		return !dvs.trimmedRecently.Has(pid)
	}
}

// subnetFilter checks if the node has an interest in the given subnet
func (dvs *DiscV5Service) subnetFilter(subnets ...uint64) func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		fromEntry, err := records.GetSubnetsEntry(node.Record())
		if err != nil {
			return false
		}
		for _, subnet := range subnets {
			if fromEntry[subnet] > 0 {
				return true
			}
		}
		return false
	}
}

// sharedSubnetsFilter returns a function that
// returns true if the peer has at least [n] subnets in common
func (dvs *DiscV5Service) sharedSubnetsFilter(n int) func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		if n == 0 {
			return true
		}
		if len(dvs.subnets) == 0 {
			return true
		}
		nodeSubnets, err := records.GetSubnetsEntry(node.Record())
		if err != nil {
			return false
		}
		shared := commons.SharedSubnets(dvs.subnets, nodeSubnets, n)
		// logger.Debug("shared subnets", zap.Ints("shared", shared),
		//	zap.String("node", node.String()))

		return len(shared) >= n
	}
}

// subnetFilter checks if the node has already been discovered recently and filters it out
// if so
func (dvs *DiscV5Service) alreadyDiscoveredFilter(logger *zap.Logger) func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		pID, err := PeerID(node)
		if err != nil {
			logger.Warn("could not get peer ID from node record", zap.Error(err))
			return false
		}
		if dvs.discoveredPeersPool.Has(pID) {
			return false // this peer is already being considered
		}
		return true
	}
}
