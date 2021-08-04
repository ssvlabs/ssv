package deterministic

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Deterministic Round robin leader selection is a fair and sequential leader selection.
// Each instance/ round change the next leader is selected one-by-one.
type Deterministic struct {
	baseInt       uint64
	committeeSize uint64
}

// New returns a new Deterministic instance or error
func New(seed []byte, committeeSize uint64) (*Deterministic, error) {
	ret := &Deterministic{committeeSize: committeeSize}
	if err := ret.setSeed(seed); err != nil {
		return nil, err
	}
	return ret, nil
}

// Calculate returns the current leader
func (rr *Deterministic) Calculate(index uint64) uint64 {
	return (rr.baseInt + index) % rr.committeeSize
}

// setSeed takes []byte and converts to uint64,returns error if fails.
func (rr *Deterministic) setSeed(seed []byte) error {
	s, err := prepareBytes(seed)
	if err != nil {
		return err
	}
	rr.baseInt = binary.LittleEndian.Uint64(s)
	fmt.Printf("seed - %d\n", rr.baseInt)
	return nil
}

func prepareBytes(input []byte) ([]byte, error) {
	if len(input) == 0 {
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
