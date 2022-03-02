package crypto

import (
	"crypto/sha256"
	"hash"
	"sync"
)

var sha256Pool = sync.Pool{New: func() interface{} {
	return sha256.New()
}}

// Sha256Hash does a sha256 on the given input
// it uses sync.Pool to optimize performance
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
