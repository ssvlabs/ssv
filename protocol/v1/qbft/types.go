package qbft

import "github.com/bloxapp/ssv/protocol/v1"

type Round uint64
type Height int64

const (
	NoRound     = 0 // NoRound represents a nil/ zero round
	FirstRound  = 1 // FirstRound value is the first round in any QBFT instance start
	FirstHeight = 0
)

// Network is a collection of funcs for the QBFT Network
type Network interface {
	Broadcast(msg v1.MessageEncoder) error
	BroadcastDecided(msg v1.MessageEncoder) error
}

type Storage interface {
	// SaveHighestDecided saves (and potentially overrides) the highest Decided for a specific instance
	SaveHighestDecided(signedMsg *SignedMessage) error
}
