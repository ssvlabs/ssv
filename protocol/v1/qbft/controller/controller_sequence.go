package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	instance2 "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"strconv"

	instance "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/leader/deterministic"
	"github.com/pkg/errors"
)

/**
Controller Sequence is the equivalent of block number in a blockchain.
An incremental number for a new iBFT instance.
A fully synced iBFT node must have all sequences to be fully synced, no skips or missing sequences.
*/

func (c *Controller) canStartNewInstance(opts instance.InstanceOptions) error {
	if !c.initialized() {
		return errors.New("iBFT hasn't initialized yet")
	}
	if c.currentInstance != nil {
		return errors.Errorf("current instance (%d) is still running", c.currentInstance.State().SeqNumber.Get())
	}

	highestKnown, err := c.highestKnownDecided()
	if err != nil {
		return err
	}

	highestSeqKnown := uint64(0)
	if highestKnown != nil {
		highestSeqKnown = highestKnown.Message.SeqNumber
	}

	if opts.SeqNumber == 0 {
		return nil
	}
	if opts.SeqNumber != highestSeqKnown+1 {
		return errors.New("instance seq invalid")
	}

	if opts.RequireMinPeers {
		if err := c.waitForMinPeers(1, true); err != nil {
			return err
		}
	}

	if !c.ValidatorShare.OperatorReady() {
		return errors.New("operator share not ready")
	}

	return nil
}

// NextSeqNumber returns the previous decided instance seq number + 1
// In case it's the first instance it returns 0
func (c *Controller) NextSeqNumber() (message.Height, error) {
	knownDecided, err := c.highestKnownDecided()
	if err != nil {
		return 0, err
	}
	if knownDecided == nil {
		return 0, nil
	}
	return message.Height(knownDecided.Message.SeqNumber + 1), nil
}

func (c *Controller) instanceOptionsFromStartOptions(opts instance2.ControllerStartInstanceOptions) (*instance.InstanceOptions, error) {
	leaderSelectionSeed := append(c.Identifier, []byte(strconv.FormatUint(opts.SeqNumber, 10))...)
	leaderSelc, err := deterministic.New(leaderSelectionSeed, uint64(c.ValidatorShare.CommitteeSize()))
	if err != nil {
		return nil, err
	}

	return &instance.InstanceOptions{
		Logger:          opts.Logger,
		ValidatorShare:  c.ValidatorShare,
		Network:         c.network,
		Queue:           c.msgQueue,
		ValueCheck:      opts.ValueCheck,
		LeaderSelector:  leaderSelc,
		Config:          c.instanceConfig,
		Lambda:          c.Identifier,
		SeqNumber:       opts.SeqNumber,
		Fork:            c.fork.InstanceFork(),
		RequireMinPeers: opts.RequireMinPeers,
		Signer:          c.signer,
	}, nil
}
