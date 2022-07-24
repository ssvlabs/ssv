package controller

import (
	"strconv"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	protcolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/deterministic"
)

/**
Controller Sequence is the equivalent of block number in a blockchain.
An incremental number for a new iBFT instance.
A fully synced iBFT node must have all sequences to be fully synced, no skips or missing sequences.
*/

func (c *Controller) canStartNewInstance(opts instance.Options) error {
	if ready, err := c.initialized(); !ready {
		return err
	}
	currentInstance := c.GetCurrentInstance()
	if currentInstance != nil {
		return errors.Errorf("current instance (%d) is still running", currentInstance.State().GetHeight())
	}
	if !c.ValidatorShare.OperatorReady() {
		return errors.New("operator share not ready")
	}

	highestKnown, err := c.highestKnownDecided()
	if err != nil {
		return err
	}

	highestSeqKnown := specqbft.Height(0)
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
		minPeers := 1
		c.Logger.Debug("waiting for min peers...", zap.Int("min peers", minPeers))
		if err := protcolp2p.WaitForMinPeers(c.Ctx, c.Logger, c.Network, c.ValidatorShare.PublicKey.Serialize(), minPeers, time.Millisecond*500); err != nil {
			return err
		}
		c.Logger.Debug("found enough peers")
	}

	return nil
}

// NextSeqNumber returns the previous decided instance seq number + 1
// In case it's the first instance it returns 0
func (c *Controller) NextSeqNumber() (specqbft.Height, error) {
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
	leaderSelectionSeed := append(c.Fork.Identifier(c.Identifier.GetPubKey(), c.Identifier.GetRoleType()), []byte(strconv.FormatUint(uint64(opts.SeqNumber), 10))...)
	leaderSelc, err := deterministic.New(leaderSelectionSeed, uint64(c.ValidatorShare.CommitteeSize()))
	if err != nil {
		return nil, err
	}

	return &instance.Options{
		Logger:          opts.Logger,
		ValidatorShare:  c.ValidatorShare,
		Network:         c.Network,
		LeaderSelector:  leaderSelc,
		Config:          c.InstanceConfig,
		Identifier:      c.Identifier,
		Height:          opts.SeqNumber,
		Fork:            c.Fork.InstanceFork(),
		RequireMinPeers: opts.RequireMinPeers,
		SSVSigner:       c.KeyManager,
	}, nil
}
