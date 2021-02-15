package bytesval

import (
	"bytes"
	"errors"

	"github.com/bloxapp/ssv/utils/dataval"
)

// bytesValidation implements val.ValueImplementation interface
// The logic is to compare bytes from the input with the original ones.
type bytesValidation struct {
	val []byte
}

// New is the constructor of bytesValidation
func New(val []byte) dataval.Validator {
	return &bytesValidation{
		val: val,
	}
}

// Validate implements dataval.Validator interface
func (c *bytesValidation) Validate(value []byte) error {
	if !bytes.Equal(value, c.val) {
		return errors.New("msg value is wrong")
	}

	return nil
}
