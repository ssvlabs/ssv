package genesis

import (
	"encoding/binary"

	"github.com/bloxapp/ssv/network/forks"
	"github.com/cespare/xxhash/v2"
)

// MsgID returns msg_id for the given message
func (genesis *ForkGenesis) MsgID() forks.MsgIDFunc {
	return func(msg []byte) string {
		if len(msg) == 0 {
			return ""
		}
		b := make([]byte, 12)
		binary.LittleEndian.PutUint64(b, xxhash.Sum64(msg))
		return string(b)
	}
}

// Subnets returns the subnets count for this fork
func (genesis *ForkGenesis) Subnets() int {
	return int(subnetsCount)
}

// Topics returns the available topics for this fork.
func (genesis *ForkGenesis) Topics() []string {
	return genesis.topics
}
