package peers

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/ssvlabs/ssv/network/records"
)

type PeerInfo struct {
	ID                 peer.ID
	State              PeerState
	Address            ma.Multiaddr
	Direction          network.Direction
	NodeInfo           *records.NodeInfo
	LastHandshake      time.Time
	LastHandshakeError error
}

// PeerState is the state of a peer
type PeerState int32

func (ns PeerState) String() string {
	switch ns {
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateDisconnected:
		return "disconnected"
	default:
		return "unknown"
	}
}

var (
	// StateUnknown is the state for an unknown peer
	StateUnknown PeerState = 0

	// StateDisconnected is the state for a disconnected peer
	StateDisconnected PeerState = 1

	// StateConnecting is the state for a connecting peer
	StateConnecting PeerState = 2

	// StateConnected is the state for a connected peer
	StateConnected PeerState = 3
)

// peerInfoIndex implements a thread-safe PeerInfoIndex.
type peerInfoIndex struct {
	store map[peer.ID]*PeerInfo
	mutex sync.RWMutex
}

func NewPeerInfoIndex() *peerInfoIndex {
	return &peerInfoIndex{
		store: make(map[peer.ID]*PeerInfo),
	}
}

func (pi *peerInfoIndex) AddPeerInfo(id peer.ID, address ma.Multiaddr, direction network.Direction) {
	pi.UpdatePeerInfo(id, func(info *PeerInfo) {
		info.Address = address
		info.Direction = direction
		info.State = StateDisconnected
	})
}

func (pi *peerInfoIndex) PeerInfo(id peer.ID) *PeerInfo {
	pi.mutex.RLock()
	defer pi.mutex.RUnlock()

	if info, ok := pi.store[id]; ok {
		return info
	}
	return nil
}

func (pi *peerInfoIndex) State(id peer.ID) PeerState {
	pi.mutex.RLock()
	defer pi.mutex.RUnlock()

	if info, ok := pi.store[id]; ok {
		return info.State
	}
	return StateUnknown
}

func (pi *peerInfoIndex) SetState(id peer.ID, state PeerState) {
	pi.UpdatePeerInfo(id, func(info *PeerInfo) {
		info.State = state
	})
}

func (pi *peerInfoIndex) UpdatePeerInfo(id peer.ID, update func(*PeerInfo)) {
	pi.mutex.Lock()
	defer pi.mutex.Unlock()

	info := pi.store[id]
	if info == nil {
		info = &PeerInfo{
			ID: id,
		}
	} else {
		cpy := *info
		info = &cpy
	}
	update(info)
	pi.store[id] = info
}
