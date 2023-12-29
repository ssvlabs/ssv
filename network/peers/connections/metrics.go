package connections

import (
	"github.com/libp2p/go-libp2p/core/peer"
)

type Metrics interface {
	PeerDisconnected(peer.ID)
	AddNetworkConnection()
	RemoveNetworkConnection()
	FilteredNetworkConnection()
}
