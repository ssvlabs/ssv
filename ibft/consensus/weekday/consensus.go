package weekday

import (
	"bytes"
	"errors"
	"time"

	"github.com/bloxapp/ssv/ibft/consensus"
)

// weekdayConsensus implements consensus.ValueImplementation interface
type weekdayConsensus struct {
	weekday string
}

// New is the constructor of weekdayConsensus
func New() consensus.Consensus {
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
