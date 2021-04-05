package leader

import (
	"bytes"
	"encoding/binary"
)

// Deterministic Round robin leader selection is a fair and sequential leader selection.
// Each instance/ round change the next leader is selected one-by-one.
type Deterministic struct {
	index   uint64
	baseInt uint64
}
// Current returns the current leader
func (rr *Deterministic) Current(committeeSize uint64) uint64 {
	return (rr.baseInt + rr.index) % committeeSize
}

// Bump to the index
func (rr *Deterministic) Bump() {
	rr.index++
}

// SetSeed takes []byte and converts to uint64,returns error if fails.
func (rr *Deterministic) SetSeed(seed []byte, index uint64) error {
	rr.index = index
	return binary.Read(bytes.NewBuffer(maxEightByteSlice(seed)), binary.LittleEndian, &rr.baseInt)
}

func maxEightByteSlice(input []byte) []byte {
	if len(input) > 8 {
		return input[0:8]
	}
	return input
}
