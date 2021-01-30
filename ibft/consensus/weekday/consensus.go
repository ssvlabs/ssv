package weekday

import (
	"bytes"
	"errors"
	"time"
)

type Consensus struct {
}

func (c *Consensus) ValidateValue(value []byte) error {
	day := time.Now().Weekday().String()

	// validate lambda
	if !bytes.Equal(value, []byte(day)) {
		return errors.New("pre-prepare msg lambda is wrong")
	}
	return nil
}
