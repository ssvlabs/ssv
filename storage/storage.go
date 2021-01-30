package storage

import "github.com/bloxapp/ssv/ibft/proto"

// Storage is an interface for persisting chain data
type Storage interface {
	SavePrepareJustification(lambda []byte, round uint64, msg *proto.Message, signature []byte, signers []uint64)
	SaveDecidedRound(lambda []byte, msg *proto.Message, signature []byte, signers []uint64)
}
