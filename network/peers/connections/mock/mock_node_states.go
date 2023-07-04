package mock

import (
	"github.com/bloxapp/ssv/network/peers"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

var _ peers.PeerInfoIndex = &PeerInfoIndex{}

type PeerInfoIndex struct {
	MockNodeState peers.PeerState
}

func (m PeerInfoIndex) PeerInfo(id peer.ID) *peers.PeerInfo {
	//TODO implement me
	panic("implement me")
}

func (m PeerInfoIndex) AddPeerInfo(id peer.ID, address ma.Multiaddr, direction network.Direction) {
	panic("implement me")
}

func (m PeerInfoIndex) UpdatePeerInfo(id peer.ID, f func(*peers.PeerInfo)) {
	panic("implement me")
}

func (m PeerInfoIndex) State(id peer.ID) peers.PeerState {
	return m.MockNodeState
}

func (m PeerInfoIndex) SetState(id peer.ID, state peers.PeerState) {
	//TODO implement me
	panic("implement me")
}
