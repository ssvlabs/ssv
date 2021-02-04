package weekday

import (
	"bytes"
	"errors"
	"time"

	"github.com/bloxapp/ssv/ibft/val"
)

// weekdayConsensus implements val.ValueImplementation interface
type weekdayConsensus struct {
	weekday string
}

// New is the constructor of weekdayConsensus
func New() val.ValueValidator {
	return &weekdayConsensus{
		weekday: time.Now().Weekday().String(),
	}
}

func (c *weekdayConsensus) Validate(value []byte) error {
	if !bytes.Equal(value, []byte(c.weekday)) {
		return errors.New("msg value is wrong")
	}

	return nil
}
