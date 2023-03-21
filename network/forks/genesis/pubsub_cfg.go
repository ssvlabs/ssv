package genesis

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"sync"

	"github.com/bloxapp/ssv/network/forks"
	"github.com/cespare/xxhash/v2"
)

// MsgID returns msg_id for the given message
func (genesis *ForkGenesis) MsgID() forks.MsgIDFunc {
	return func(msg []byte) string {
		if len(msg) == 0 {
			return ""
		}
		// h := Sha256Hash(msg)
		// return string(h[20:])
		b := make([]byte, 12)
		binary.LittleEndian.PutUint64(b, xxhash.Sum64(msg))
		return string(b)
	}
}

// Subnets returns the subnets count for this fork
func (genesis *ForkGenesis) Subnets() int {
	return int(subnetsCount)
}

// sha256Pool holds a pool of hash.Hash
// it should be used to perform sha256 hash
var sha256Pool = sync.Pool{New: func() interface{} {
	return sha256.New()
}}

// Sha256Hash does sha256 on the given input
func Sha256Hash(data []byte) [32]byte {
	h, ok := sha256Pool.Get().(hash.Hash)
	if !ok {
		h = sha256.New()
	}
	defer sha256Pool.Put(h)
	h.Reset()

	var b [32]byte
	// #nosec G104
	h.Write(data)
	h.Sum(b[:0])

	return b
}
