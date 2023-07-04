package peers

import (
	"time"

	"github.com/bloxapp/ssv/network/records"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
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
