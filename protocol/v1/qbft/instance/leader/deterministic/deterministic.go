package deterministic

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// Deterministic Round robin leader selection is a fair and sequential leader selection.
// Each instance/ round change the next leader is selected one-by-one.
type Deterministic struct {
	baseInt     uint64
	operatorIDs []uint64
}

// New returns a new Deterministic instance or error
func New(seed []byte, operatorIDs []uint64) (*Deterministic, error) {
	ret := &Deterministic{
		operatorIDs: operatorIDs,
	}
	if err := ret.setSeed(seed); err != nil {
		return nil, err
	}
	return ret, nil
}

// Calculate returns the current leader
func (rr *Deterministic) Calculate(round uint64) uint64 {
	index := (rr.baseInt + round) % uint64(len(rr.operatorIDs))
	return rr.operatorIDs[index]
}

// setSeed takes []byte and converts to uint64,returns error if fails.
func (rr *Deterministic) setSeed(seed []byte) error {
	s, err := prepareBytes(seed)
	if err != nil {
		return err
	}
	rr.baseInt = binary.LittleEndian.Uint64(s)
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
