package peers

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/network/commons"
)

// subnetsIndex implements SubnetsIndex
type subnetsIndex struct {
	subnets     [commons.SubnetsCount][]peer.ID
	peerSubnets map[peer.ID]commons.Subnets

	lock *sync.RWMutex
}

func NewSubnetsIndex() SubnetsIndex {
	return &subnetsIndex{
		peerSubnets: map[peer.ID]commons.Subnets{},
		lock:        &sync.RWMutex{},
	}
}

func (si *subnetsIndex) UpdatePeerSubnets(id peer.ID, s commons.Subnets) bool {
	si.lock.Lock()
	defer si.lock.Unlock()

	existing, ok := si.peerSubnets[id]
	var addedSubnets, removedSubnets commons.Subnets
	if !ok {
		// New peer: all subnets in 's' are additions
		addedSubnets = s
		// No subnets were previously set, so no removals
		removedSubnets = commons.ZeroSubnets
	} else {
		// Existing peer: compute diffs
		addedSubnets, removedSubnets = existing.DiffSubnets(s)
	}

	// Determine if any changes occurred (additions or removals)
	hasChanges := !ok || addedSubnets.ActiveCount() > 0 || removedSubnets.ActiveCount() > 0
	if !hasChanges {
		return false
	}

	// Update the peer's subnets
	si.peerSubnets[id] = s

	// Update subnet-peer mappings
	for subnet := uint64(0); subnet < commons.SubnetsCount; subnet++ {
		if addedSubnets.IsSet(subnet) {
			// Add peer to the subnet
			si.subnets[subnet] = append(si.subnets[subnet], id)
		}
		if removedSubnets.IsSet(subnet) {
			// Remove peer from the subnet
			peers := si.subnets[subnet]
			for i, p := range peers {
				if p == id {
					// Remove peer from slice
					si.subnets[subnet] = append(peers[:i], peers[i+1:]...)
					break
				}
			}
		}
	}
	return true
}

func (si *subnetsIndex) GetSubnetPeers(subnet int) []peer.ID {
	si.lock.RLock()
	defer si.lock.RUnlock()

	peers := si.subnets[subnet]
	if len(peers) == 0 {
		return nil
	}
	cp := make([]peer.ID, len(peers))
	copy(cp, peers)
	return cp
}

// GetSubnetsStats collects and returns subnets stats
func (si *subnetsIndex) GetSubnetsStats() *SubnetsStats {
	si.lock.RLock()
	defer si.lock.RUnlock()

	stats := &SubnetsStats{}
	for subnet, peers := range si.subnets {
		stats.PeersCount[subnet] = len(peers)
	}

	return stats
}

func (si *subnetsIndex) GetPeerSubnets(id peer.ID) (commons.Subnets, bool) {
	si.lock.RLock()
	defer si.lock.RUnlock()

	subnets, ok := si.peerSubnets[id]
	if !ok {
		return commons.ZeroSubnets, false
	}

	return subnets, true
}
