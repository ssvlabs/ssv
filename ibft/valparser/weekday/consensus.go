package weekday

import (
	"bytes"
	"errors"
	"time"

	"github.com/bloxapp/ssv/ibft/valparser"
)

// weekdayConsensus implements valparser.ValueParser interface
type weekdayConsensus struct {
	weekday string
}

// New is the constructor of weekdayConsensus
func New() valparser.ValueParser {
	return &weekdayConsensus{
		weekday: time.Now().Weekday().String(),
	}
}

func (c *weekdayConsensus) ValidateValue(value []byte) error {
	if !bytes.Equal(value, []byte(c.weekday)) {
		return errors.New("msg value is wrong")
	}

	return nil
}
