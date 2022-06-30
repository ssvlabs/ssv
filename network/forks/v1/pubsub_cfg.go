package v1

import (
	"github.com/bloxapp/ssv/network/forks"
	scrypto "github.com/bloxapp/ssv/utils/crypto"
)

// subnetsCount returns the subnet count for v1
var subnetsCount uint64 = 128

// MsgID returns msg_id for the given message
func (v1 *ForkV1) MsgID() forks.MsgIDFunc {
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
func (v1 *ForkV1) Subnets() int {
	return int(subnetsCount)
}
