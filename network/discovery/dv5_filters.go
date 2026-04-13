package discovery

import (
	"slices"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/records"
)

func readSSVNodeFlag(node *enode.Node) (bool, error) {
	var isSSV bool
	if err := node.Record().Load(enr.WithEntry("ssv", &isSSV)); err != nil {
		return false, err
	}
	return isSSV, nil
}

// limitNodeFilter returns true if the limit is exceeded
func (dvs *DiscV5Service) limitNodeFilter(node *enode.Node) bool {
	return !dvs.conns.AtLimit(libp2pnetwork.DirOutbound)
}

//// forkVersionFilter checks if the node has the same fork version
// func (dvs *DiscV5Service) forkVersionFilter(node *enode.Node) bool {
//	forkv, err := records.GetForkVersionEntry(node.Record())
//	if err != nil {
//		dvs.logger.Warn("could not read fork version from node record", zap.Error(err))
//		return false
//	}
//	return dvs.forkv == forkv
//}

// badNodeFilter checks if the node was pruned or have a bad score
func (dvs *DiscV5Service) badNodeFilter() func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		pid, err := PeerID(node)
		if err != nil {
			dvs.logger.Warn("could not get peer ID from node record", zap.Error(err))
			return false
		}
		return !dvs.conns.IsBad(pid)
	}
}

// ssvNodeFilter checks if the node is an SSV node
func (dvs *DiscV5Service) ssvNodeFilter() func(node *enode.Node) bool {
	return func(node *enode.Node) bool {
		isSSV, err := readSSVNodeFlag(node)
		if err != nil {
			//TODO: metric
			//logger.Warn("could not read ssv entry from node record", zap.String("enr", node.String()), zap.Error(err))
			return false
		}
		return isSSV
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
		return slices.ContainsFunc(subnets, fromEntry.IsSet)
	}
}
