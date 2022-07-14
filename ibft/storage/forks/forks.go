package forks

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// Fork is the interface for fork
type Fork interface {
	EncodeSignedMsg(msg *specqbft.SignedMessage) ([]byte, error)
	DecodeSignedMsg(data []byte) (*specqbft.SignedMessage, error)
	Identifier(pk []byte, role spectypes.BeaconRole) [52]byte
}
