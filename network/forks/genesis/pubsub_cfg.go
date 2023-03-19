package genesis

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/cespare/xxhash/v2"
)

// MsgID returns msg_id for the given message
func (genesis *ForkGenesis) MsgID() forks.MsgIDFunc {
	return func(msg []byte) string {
		if len(msg) == 0 {
			return ""
		}
		// TODO: check performance
		// h := scrypto.Sha256Hash(msg)
		// return string(h[20:])
		return string(xxhash.Sum64(msg))
	}
}

// Subnets returns the subnets count for this fork
func (genesis *ForkGenesis) Subnets() int {
	return int(subnetsCount)
}
