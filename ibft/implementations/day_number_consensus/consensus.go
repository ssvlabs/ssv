package day_number_consensus

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/bloxapp/ssv/ibft/types"
)

type DayNumberConsensus struct {
	Id     uint64
	Leader uint64
}

func (c *DayNumberConsensus) IsLeader(_ *types.State) bool {
	return c.Id == c.Leader
}

func (c *DayNumberConsensus) ValidatePrePrepareMsg(state *types.State, msg *types.SignedMessage) error {
	// validate leader
	if msg.IbftId != c.Leader {
		return errors.New(fmt.Sprintf("pre-prepare msg leader error, expected %d and got %d", c.Leader, msg.IbftId))
	}

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
