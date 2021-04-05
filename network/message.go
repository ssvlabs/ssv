package network

import "github.com/bloxapp/ssv/ibft/proto"

// BroadcastingType defines different types of messages broadcasted over P2P.
type BroadcastingType int

const (
	// IBFTBroadcastingType are all iBFT related messages
	IBFTBroadcastingType = iota + 1
	// SignatureBroadcastingType is an SSV node specific message for broadcasting post consensus signatures on eth2 duties
	SignatureBroadcastingType
)

// Message is a wrapper struct for all network messages types
type Message struct {
	Lambda []byte               `json:"lambda"`
	Msg    *proto.SignedMessage `json:"msg"`
	Type   BroadcastingType     `json:"type"`
}
