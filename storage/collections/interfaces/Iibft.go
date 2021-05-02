package interfaces

import "github.com/bloxapp/ssv/ibft/proto"

// Storage is an interface for persisting chain data
type Iibft interface {
	SavePrepared(signedMsg *proto.SignedMessage)
	SaveDecided(signedMsg *proto.SignedMessage)
}
