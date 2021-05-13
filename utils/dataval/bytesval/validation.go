package bytesval

import (
	"bytes"
	"errors"
	"github.com/bloxapp/ssv/ibft/valcheck"
)

// bytesValidation implements val.ValueImplementation interface
// The logic is to compare bytes from the input with the original ones.
type bytesValidation struct {
	val []byte
}

// New is the constructor of bytesValidation
func New(val []byte) valcheck.ValueCheck {
	return &bytesValidation{
		val: val,
	}
}

// Validate implements dataval.ValidatorStorage interface
func (c *bytesValidation) Check(value []byte) error {
	if !bytes.Equal(value, c.val) {
		return errors.New("msg value is wrong")
	}

	return nil
}
