package day_number_consensus

import (
	"bytes"
	"errors"

	"github.com/bloxapp/ssv/ibft/types"
)

type DayNumberConsensus struct {
	Id uint64
}

func (c *DayNumberConsensus) ValidatePrePrepareMsg(state *types.State, msg *types.SignedMessage) error {
	// validate lambda
	if bytes.Compare(state.Lambda, msg.Message.Lambda) != 0 {
		return errors.New("pre-prepare msg lambda is wrong")
	}

	// validate input value
	if bytes.Compare(state.InputValue, msg.Message.Value) != 0 {
		return errors.New("pre-prepare msg input value is wrong")
	}
	return nil
}

func (c *DayNumberConsensus) ValidatePrepareMsg(state *types.State, msg *types.SignedMessage) error {
	// validate lambda
	if bytes.Compare(state.Lambda, msg.Message.Lambda) != 0 {
		return errors.New("pre-prepare msg lambda is wrong")
	}

	// validate input value
	if bytes.Compare(state.InputValue, msg.Message.Value) != 0 {
		return errors.New("pre-prepare msg input value is wrong")
	}
	return nil
}

func (c *DayNumberConsensus) ValidateCommitMsg(state *types.State, msg *types.SignedMessage) error {
	return nil
}

func (c *DayNumberConsensus) ValidateChangeRoundMsg(state *types.State, msg *types.SignedMessage) error {
	return nil
}
