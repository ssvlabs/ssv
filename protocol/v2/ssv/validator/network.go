package validator

import (
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	protcolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/libp2p/go-libp2p-core/peer"
)

// TODO: move somewhere else

type Network interface {
	ssv.Network
	protcolp2p.Subscriber
}

type nilNetwork struct {
	base ssv.Network
}

func (n nilNetwork) Broadcast(message types.Encoder) error {
	return n.base.Broadcast(message)
}

func (n nilNetwork) Subscribe(pk types.ValidatorPK) error {
	return nil
}

func (n nilNetwork) Unsubscribe(pk types.ValidatorPK) error {
	return nil
}

func (n nilNetwork) Peers(pk types.ValidatorPK) ([]peer.ID, error) {
	return nil, nil
}

func newNilNetwork(network ssv.Network) Network {
	return &nilNetwork{network}
}
