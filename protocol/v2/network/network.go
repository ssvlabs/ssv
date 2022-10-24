package network

import (
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv/protocol/v1/p2p"
)

type Network interface {
	qbft.Network
	protcolp2p.Subscriber
}
