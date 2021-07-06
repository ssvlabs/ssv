package leader

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
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

	s, err := prepareBytes(seed)
	if err != nil {
		return err
	}
	rr.baseInt = binary.LittleEndian.Uint64(s)
	fmt.Printf("%d\n", rr.baseInt)
	return nil
}

func prepareBytes(input []byte) ([]byte, error) {
	if input == nil || len(input) == 0 {
		return nil, errors.New("input seed can't be nil or of length 0")
	}

	// hash input
	s := sha256.New()
	if _, err := s.Write(input); err != nil {
		return nil, err
	}
	h := s.Sum(nil)

	return h[0:8], nil
}
