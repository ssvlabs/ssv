package sszutil

import (
	"crypto/sha256"
	"encoding/binary"
)

// ZeroHash returns the SSZ zero-hash at the given depth.
// zeroHash(0) is 32 bytes of 0, and zeroHash(n+1) = sha256(zeroHash(n) || zeroHash(n)).
func ZeroHash(depth int) [32]byte {
	var out [32]byte
	for i := 0; i < depth; i++ {
		sum := sha256.Sum256(append(out[:], out[:]...))
		out = sum
	}
	return out
}

// MixInLength applies SSZ mix_in_length to the given root.
func MixInLength(root [32]byte, length uint64) [32]byte {
	var lengthBytes [32]byte
	binary.LittleEndian.PutUint64(lengthBytes[:8], length)
	return sha256.Sum256(append(root[:], lengthBytes[:]...))
}

// EmptyListRoot returns the SSZ hash_tree_root of an empty list with the given max number of elements.
// This assumes maxElements is a power of 2 (as is the case for the lists we use in this smoke harness).
func EmptyListRoot(maxElements uint64) [32]byte {
	depth := 0
	for n := maxElements; n > 1; n >>= 1 {
		depth++
	}
	return MixInLength(ZeroHash(depth), 0)
}
