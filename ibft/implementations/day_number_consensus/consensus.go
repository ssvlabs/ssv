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

func (c *DayNumberConsensus) NewPrePrepareMsg(state *types.State) *types.Message {
	return &types.Message{
		Type:       types.MsgType_Preprepare,
		Round:      state.Round,
		Lambda:     state.Lambda,
		InputValue: state.InputValue,
	}
}

func (c *DayNumberConsensus) ValidatePrePrepareMsg(state *types.State, msg *types.Message) error {
	// validate leader
	if msg.IbftId != c.Leader {
		return errors.New(fmt.Sprintf("pre-prepare msg leader error, expected %d and got %d", c.Leader, msg.IbftId))
	}

	// validate lambda
	if bytes.Compare(state.Lambda, msg.Lambda) != 0 {
		return errors.New("pre-prepare msg lambda is wrong")
	}

	// validate input value
	if bytes.Compare(state.InputValue, msg.InputValue) != 0 {
		return errors.New("pre-prepare msg input value is wrong")
	}
	return nil
}

func (c *DayNumberConsensus) NewPrepareMsg(state *types.State) *types.Message {
	return &types.Message{
		Type:       types.MsgType_Prepare,
		Round:      state.Round,
		Lambda:     state.Lambda,
		InputValue: state.InputValue,
	}
}

func (c *DayNumberConsensus) ValidatePrepareMsg(state *types.State, msg *types.Message) error {
	// validate lambda
	if bytes.Compare(state.Lambda, msg.Lambda) != 0 {
		return errors.New("pre-prepare msg lambda is wrong")
	}

	// validate input value
	if bytes.Compare(state.InputValue, msg.InputValue) != 0 {
		return errors.New("pre-prepare msg input value is wrong")
	}
	return nil
}

func (c *DayNumberConsensus) NewCommitMsg(state *types.State) *types.Message {
	return &types.Message{
		Type:       types.MsgType_Commit,
		Round:      state.Round,
		Lambda:     state.Lambda,
		InputValue: state.InputValue,
	}
}

func (c *DayNumberConsensus) ValidateCommitMsg(state *types.State, msg *types.Message) error {
	return nil
}
