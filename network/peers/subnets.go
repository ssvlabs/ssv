package peers

import (
	"github.com/bloxapp/ssv/network/records"
	"github.com/libp2p/go-libp2p-core/peer"
	"sync"
)

// subnetsIndex implements SubnetsIndex
type subnetsIndex struct {
	subnets     [][]peer.ID
	peerSubnets map[peer.ID]records.Subnets

	lock *sync.RWMutex
}

func newSubnetsIndex(count int) SubnetsIndex {
	return &subnetsIndex{
		subnets:     make([][]peer.ID, count),
		peerSubnets: map[peer.ID]records.Subnets{},
		lock:        &sync.RWMutex{},
	}
}

func (si *subnetsIndex) UpdatePeerSubnets(id peer.ID, s records.Subnets) bool {
	si.lock.Lock()
	defer si.lock.Unlock()

	existing, ok := si.peerSubnets[id]
	if !ok {
		existing = make([]byte, 0)
	}
	diff := records.DiffSubnets(existing, s)
	if len(diff) == 0 {
		return false
	}
	si.peerSubnets[id] = s

diffLoop:
	for subnet, val := range diff {
		if subnet >= len(si.subnets) { // out of range
			continue
		}
		peers := si.subnets[subnet]
		if len(peers) == 0 {
			peers = make([]peer.ID, 0)
		}
		for i, p := range peers {
			if p == id {
				// skip if peer is already listed in a subnet to be added
				if val > byte(0) {
					continue diffLoop
				}
				// otherwise, remove peer from the subnet
				if i == 0 {
					if len(peers) == 1 {
						si.subnets[subnet] = make([]peer.ID, 0)
					} else {
						si.subnets[subnet] = peers[1:]
					}
					continue diffLoop
				}
				si.subnets[subnet] = append(peers[:i], peers[i:]...)
				continue diffLoop
			}
		}
		if val > byte(0) {
			si.subnets[subnet] = append(peers, id)
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

	stats := &SubnetsStats{
		PeersCount: make([]int, len(si.subnets)),
	}
	for subnet, peers := range si.subnets {
		stats.PeersCount[subnet] = len(peers)
	}

	return stats
}

func (si *subnetsIndex) GetPeerSubnets(id peer.ID) records.Subnets {
	si.lock.RLock()
	defer si.lock.RUnlock()

	subnets, ok := si.peerSubnets[id]
	if !ok {
		return nil
	}
	cp := make(records.Subnets, len(subnets))
	copy(cp, subnets)
	return cp
}

// GetSubnetsDistributionScores returns current subnets scores based on peers distribution.
// subnets with low peer count would get higher score, and overloaded subnets gets a lower score.
func GetSubnetsDistributionScores(stats *SubnetsStats, minPerSubnet int, mySubnets records.Subnets, topicMaxPeers int) []int {
	allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
	activeSubnets := records.SharedSubnets(allSubs, mySubnets, 0)

	scores := make([]int, len(allSubs))
	for _, s := range activeSubnets {
		var connected int
		if s < len(stats.Connected) {
			connected = stats.Connected[s]
		}
		if connected == 0 {
			scores[s] = 2
		} else if connected <= minPerSubnet {
			scores[s] = 1
		} else if connected >= topicMaxPeers {
			scores[s] = -1
		}
	}
	return scores
}
