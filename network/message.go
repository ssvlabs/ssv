package network

import "github.com/bloxapp/ssv/ibft/proto"

type BroadcastingType int

const (
	IBFTBroadcastingType = iota + 1
	SignatureBroadcastingType
)

type Message struct {
	Lambda []byte               `json:"lambda"`
	Msg    *proto.SignedMessage `json:"msg"`
	Type   BroadcastingType     `json:"type"`
}
