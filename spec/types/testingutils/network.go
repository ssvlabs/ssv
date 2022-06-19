package testingutils

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
)

type testingNetwork struct {
}

func NewTestingNetwork() qbft.Network {
	return &testingNetwork{}
}

func (net *testingNetwork) Broadcast(message types.Encoder) error {
	return nil
}

func (net *testingNetwork) BroadcastDecided(msg types.Encoder) error {
	return nil
}
