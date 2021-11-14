package bytesval

import (
	"bytes"
	"errors"
	"github.com/bloxapp/ssv/ibft/valcheck"
)

// notEqualBytesValidation implements val.ValueImplementation interface
// The logic is to compare bytes from the input with the original ones, if not equal return true.
type notEqualBytesValidation struct {
	val []byte
}

// NewNotEqualBytes is the constructor of bytesValidation
func NewNotEqualBytes(val []byte) valcheck.ValueCheck {
	return &notEqualBytesValidation{
		val: val,
	}
}

// Validate implements dataval.ValidatorStorage interface
func (c *notEqualBytesValidation) Check(value []byte, pk []byte) error {
	if bytes.Equal(value, c.val) {
		return errors.New("msg value is wrong")
	}

	return nil
}
