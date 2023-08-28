package validation

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
)

type metrics interface {
	ConsensusMsgType(msgType specqbft.MessageType, signers int)
}

type nopMetrics struct{}

func (n nopMetrics) ConsensusMsgType(specqbft.MessageType, int) {}
