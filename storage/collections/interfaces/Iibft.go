package interfaces

import "github.com/bloxapp/ssv/ibft/proto"

// Iibft is an interface for persisting chain data
type Iibft interface {
	SavePrepared(signedMsg *proto.SignedMessage)
	SaveDecided(signedMsg *proto.SignedMessage)
}
