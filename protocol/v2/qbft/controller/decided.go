package controller

import (
	"bytes"
	"context"
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

// UponDecided returns "decided msg" if the corresponding QBFT instance is "completed" (marked as decided) by the
// passed message, and nil otherwise.
func (c *Controller) UponDecided(
	logger *zap.Logger,
	msg *specqbft.ProcessingMessage,
	roundTimerF ssv.QBFTRoundTimerF,
) (*spectypes.SignedSSVMessage, error) {
	if err := c.ValidateDecided(msg); err != nil {
		return nil, fmt.Errorf("invalid decided msg: %w", err)
	}

	inst := c.RecentInstances.FindInstance(msg.QBFTMessage.Height)

	if inst == nil {
		// We are not aware of this instance, create it. There is no need to start this instance, we are at the end
		// of instance-lifecycle already - we can just fast-forward it to completion here (mark as decided, etc.).

		inst = instance.NewInstance(
			context.Background(),
			logger,
			c.GetConfig(),
			c.CommitteeMember,
			c.identifierAtHeight(msg.QBFTMessage.Height),
			msg.QBFTMessage.Height,
			c.OperatorSigner,
			roundTimerF,
		)

		if err := inst.MarkDecided(msg.QBFTMessage.Round, msg.SignedMessage.FullData); err != nil {
			return nil, fmt.Errorf("mark as decided: %w", err)
		}
		if err := inst.State.CommitContainer.AddMsg(msg); err != nil {
			return nil, fmt.Errorf("add message to commit-container: %w", err)
		}
		c.RecentInstances.addNewInstance(inst)
		if msg.QBFTMessage.Height > c.LatestInstanceHeight {
			// If the new instance turned out to be "from the future", it means other nodes in our cluster are
			// already ahead working with an instance we haven't gotten to. We accept it as our current (latest
			// known) height.
			// NOTE: we could also mark older instances as irrelevant now - but spectests do not expect this,
			// and we are doing it in `Controller.StartNewInstance` periodically anyway.
			c.LatestInstanceHeight = msg.QBFTMessage.Height
		}

		// Signal "newly decided" to the caller.
		return msg.SignedMessage, nil
	}

	if decided, _ := inst.IsDecided(); !decided {
		// We are aware of this instance, it is currently running, and it hasn't been marked as decided just yet.
		// We can do so now.

		if err := inst.MarkDecided(msg.QBFTMessage.Round, msg.SignedMessage.FullData); err != nil {
			return nil, fmt.Errorf("mark as decided: %w", err)
		}
		if err := inst.State.CommitContainer.AddMsg(msg); err != nil {
			return nil, fmt.Errorf("add message to commit-container: %w", err)
		}

		// Signal "newly decided" to the caller.
		return msg.SignedMessage, nil
	}

	// The instance is known to be decided already - just record the message we got (for bookkeeping purposes).
	signers, _ := inst.State.CommitContainer.LongestUniqueSignersForRoundAndRoot(msg.QBFTMessage.Round, msg.QBFTMessage.Root)
	if len(msg.SignedMessage.OperatorIDs) > len(signers) {
		if err := inst.State.CommitContainer.AddMsg(msg); err != nil {
			return nil, err
		}
	}

	// For a known-decided instance, we've already signaled on the first decided message, so return nil message here.
	return nil, nil
}

func (c *Controller) ValidateDecided(msg *specqbft.ProcessingMessage) error {
	if !c.isDecidedMsg(msg) {
		return fmt.Errorf("not a decided msg")
	}

	if err := instance.BaseCommitValidationVerifySignature(msg, msg.QBFTMessage.Height, c.CommitteeMember.Committee); err != nil {
		return fmt.Errorf("invalid decided msg: %w", err)
	}

	r := qbft.HashDataRoot(msg.SignedMessage.FullData)
	if !bytes.Equal(r[:], msg.QBFTMessage.Root[:]) {
		return spectypes.NewError(spectypes.RootHashInvalidErrorCode, "H(data) != root")
	}

	return nil
}

// isDecidedMsg returns true if signed commit has all quorum sigs
func (c *Controller) isDecidedMsg(msg *specqbft.ProcessingMessage) bool {
	return c.CommitteeMember.HasQuorum(len(msg.SignedMessage.OperatorIDs)) && msg.QBFTMessage.MsgType == specqbft.CommitMsgType
}
