package day_number_consensus

import (
	"bytes"
	"errors"
	"time"
)

type DayNumberConsensus struct {
}

func (c *DayNumberConsensus) ValidateValue(value []byte) error {
	day := time.Now().Weekday().String()

	// validate lambda
	if !bytes.Equal(value, []byte(day)) {
		return errors.New("pre-prepare msg lambda is wrong")
	}
	return nil
}
