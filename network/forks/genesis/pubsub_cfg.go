package genesis

import (
	"github.com/bloxapp/ssv/network/forks"
	scrypto "github.com/bloxapp/ssv/utils/crypto"
)

// MsgID returns msg_id for the given message
func (genesis *ForkGenesis) MsgID() forks.MsgIDFunc {
	return func(msg []byte) string {
		if len(msg) == 0 {
			return ""
		}
		// TODO: check performance
		h := scrypto.Sha256Hash(msg)
		return string(h[20:])
	}
}

// Subnets returns the subnets count for this fork
func (genesis *ForkGenesis) Subnets() int {
	return int(subnetsCount)
}
