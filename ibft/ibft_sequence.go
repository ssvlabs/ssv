package ibft

import (
	"github.com/bloxapp/ssv/ibft/leader/deterministic"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"strconv"
)

/**
IBFT Sequence is the equivalent of block number in a blockchain.
An incremental number for a new iBFT instance.
A fully synced iBFT node must have all sequences to be fully synced, no skips or missing sequences.
*/

func (i *ibftImpl) canStartNewInstance(opts InstanceOptions) error {
	if !i.initFinished {
		return errors.New("iBFT hasn't initialized yet")
	}
	if i.currentInstance != nil {
		return errors.Errorf("current instance (%d) is still running", i.currentInstance.State.SeqNumber)
	}

	highestKnown, err := i.highestKnownDecided()
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
		if err := i.waitForMinPeers(2, true); err != nil {
			return err
		}
	}

	return nil
}

// NextSeqNumber returns the previous decided instance seq number + 1
// In case it's the first instance it returns 0
func (i *ibftImpl) NextSeqNumber() (uint64, error) {
	knownDecided, err := i.highestKnownDecided()
	if err != nil {
		return 0, err
	}
	if knownDecided == nil {
		return 0, nil
	}
	return knownDecided.Message.SeqNumber + 1, nil
}

func (i *ibftImpl) instanceOptionsFromStartOptions(opts StartOptions) (*InstanceOptions, error) {
	leaderSelectionSeed := append(i.Identifier, []byte(strconv.FormatUint(opts.SeqNumber, 10))...)
	leaderSelc, err := deterministic.New(leaderSelectionSeed, uint64(i.ValidatorShare.CommitteeSize()))
	if err != nil {
		return nil, err
	}

	ret := &InstanceOptions{
		Logger:         opts.Logger,
		ValidatorShare: i.ValidatorShare,
		Network:        i.network,
		Queue:          i.msgQueue,
		ValueCheck:     opts.ValueCheck,
		LeaderSelector: leaderSelc,
		Config:         i.instanceConfig,
		Lambda:         i.Identifier,
		SeqNumber:      opts.SeqNumber,
	}

	if err := i.preloadPeersCurrentInstanceMsgs(ret); err != nil {
		return nil, err
	}

	return ret, nil
}

func (i *ibftImpl) preloadPeersCurrentInstanceMsgs(opts *InstanceOptions) error {
	msgs, err := i.ibftStorage.GetPeersCurrentInstanceChangeRoundMsgs(i.Identifier)
	if err != nil {
		return err
	}

	// add to queue if relevant
	for _, msg := range msgs {
		if msg.Message.SeqNumber == opts.SeqNumber {
			opts.Queue.AddMessage(&network.Message{
				SignedMessage: msg,
				Type:          network.NetworkMsg_IBFTType,
			})
		}
	}

	// clear out already used msgs
	if err := i.ibftStorage.SavePeersCurrentInstanceChangeRoundMsgs(i.Identifier, nil); err != nil {
		return err
	}
	return nil
}
