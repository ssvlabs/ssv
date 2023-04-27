package mock

import (
	"github.com/bloxapp/ssv/network/peers"
	"github.com/libp2p/go-libp2p/core/peer"
)

var _ peers.NodeStates = NodeStates{}

type NodeStates struct {
	MockNodeState peers.NodeState
}

func (m NodeStates) State(id peer.ID) peers.NodeState {
	return m.MockNodeState
}

func (m NodeStates) EvictPruned(id peer.ID) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStates) Prune(id peer.ID) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStates) GC() {
	//TODO implement me
	panic("implement me")
}
