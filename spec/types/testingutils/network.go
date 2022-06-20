package testingutils

import (
	"github.com/bloxapp/ssv/spec/dkg"
	"github.com/bloxapp/ssv/spec/types"
)

type TestingNetwork struct {
	BroadcastedMsgs []*types.SSVMessage
}

func NewTestingNetwork() *TestingNetwork {
	return &TestingNetwork{
		BroadcastedMsgs: make([]*types.SSVMessage, 0),
	}
}

func (net *TestingNetwork) Broadcast(message types.Encoder) error {
	net.BroadcastedMsgs = append(net.BroadcastedMsgs, message.(*types.SSVMessage))
	return nil
}

func (net *TestingNetwork) BroadcastDecided(msg types.Encoder) error {
	return nil
}

// StreamDKGOutput will stream to any subscriber the result of the DKG
func (net *TestingNetwork) StreamDKGOutput(output *dkg.SignedOutput) error {
	return nil
}

// BroadcastDKGMessage will broadcast a msg to the dkg network
func (net *TestingNetwork) BroadcastDKGMessage(msg *dkg.SignedMessage) error {
	return nil
}
