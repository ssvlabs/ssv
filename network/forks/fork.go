package forks

import "github.com/bloxapp/ssv/network"

const (
	baseSyncStream           = "/sync/"
	HighestDecidedStream     = baseSyncStream + "highest_decided"
	DecidedByRangeStream     = baseSyncStream + "decided_by_range"
	LastChangeRoundMsgStream = baseSyncStream + "last_change_round"
	LegacyMsgStream          = "/sync/0.0.1"
)

// Fork is an interface for network specific fork implementations
type Fork interface {
	encoding
	SlotTick(slot uint64)
	pubSubMapping
	rpc
}

type pubSubMapping interface {
	ValidatorTopicID(pk []byte) string
}

type encoding interface {
	EncodeNetworkMsg(msg *network.Message) ([]byte, error)
	DecodeNetworkMsg(data []byte) (*network.Message, error)
}

type rpc interface {
	HighestDecidedStreamProtocol() string
	DecidedByRangeStreamProtocol() string
	LastChangeRoundStreamProtocol() string
}
