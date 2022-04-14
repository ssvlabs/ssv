package controller

import (
	"strconv"
	"time"

	"github.com/bloxapp/ssv/protocol/v1/message"
	protcolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/deterministic"
	"github.com/pkg/errors"
)

/**
Controller Sequence is the equivalent of block number in a blockchain.
An incremental number for a new iBFT instance.
A fully synced iBFT node must have all sequences to be fully synced, no skips or missing sequences.
*/

func (c *Controller) canStartNewInstance(opts instance.Options) error {
	if !c.initialized() {
		return errors.New("iBFT hasn't initialized yet")
	}
	if c.currentInstance != nil {
		return errors.Errorf("current instance (%d) is still running", c.currentInstance.State().GetIdentifier())
	}

	highestKnown, err := c.highestKnownDecided()
	if err != nil {
		return err
	}

	highestSeqKnown := message.Height(0)
	if highestKnown != nil {
		highestSeqKnown = highestKnown.Message.Height
	}

	if opts.Height == 0 {
		return nil
	}
	if opts.Height != highestSeqKnown+1 {
		return errors.New("instance seq invalid")
	}

	if opts.RequireMinPeers {
		// TODO need to change interval
		if err := protcolp2p.WaitForMinPeers(c.ctx, c.logger, c.network, c.ValidatorShare.PublicKey.Serialize(), 1, time.Millisecond*2); err != nil {
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
	return knownDecided.Message.Height + 1, nil
}

func (c *Controller) instanceOptionsFromStartOptions(opts instance.ControllerStartInstanceOptions) (*instance.Options, error) {
	leaderSelectionSeed := append(c.Identifier, []byte(strconv.FormatUint(uint64(opts.SeqNumber), 10))...)
	leaderSelc, err := deterministic.New(leaderSelectionSeed, uint64(c.ValidatorShare.CommitteeSize()))
	if err != nil {
		return nil, err
	}

	return &instance.Options{
		Logger:          opts.Logger,
		ValidatorShare:  c.ValidatorShare,
		Network:         c.network,
		ValueCheck:      opts.ValueCheck,
		LeaderSelector:  leaderSelc,
		Config:          c.instanceConfig,
		Lambda:          c.Identifier,
		Height:          opts.SeqNumber,
		Fork:            c.fork.InstanceFork(),
		RequireMinPeers: opts.RequireMinPeers,
		Signer:          c.signer,
	}, nil
}
