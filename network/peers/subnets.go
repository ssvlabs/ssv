package peers

import (
	"bytes"
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
	if bytes.Equal(existing, s) {
		return false
	}
	si.peerSubnets[id] = s

	// TODO: diff against existing to make sure we also clean removed subnets
	active := s.GetActive()
	for _, subnet := range active {
		peers := si.subnets[subnet]
		if len(peers) == 0 {
			peers = make([]peer.ID, 0)
		}
	peerExistenceLoop:
		for _, p := range peers {
			if p == id {
				break peerExistenceLoop
			}
		}
		si.subnets[subnet] = append(peers, id)
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
