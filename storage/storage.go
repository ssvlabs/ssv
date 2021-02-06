package storage

import "github.com/bloxapp/ssv/ibft/proto"

// Storage is an interface for persisting chain data
type Storage interface {
	SavePrepared(signedMsg *proto.SignedMessage)
	SaveDecided(signedMsg *proto.SignedMessage)
}
