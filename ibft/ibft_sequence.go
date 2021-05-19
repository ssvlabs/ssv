package ibft

import (
	"bytes"
	"errors"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/collections"
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

	highestKnown, err := i.HighestKnownDecided()
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

	// verify prev instance lambda + decided.
	// If first instance identifier, check seq number is 0
	// if not first instance identifier, fetch from storage and compare
	if !bytes.Equal(opts.PreviousLambda, FirstInstanceIdentifier()) {
		if highestKnown == nil {
			return errors.New("could not fetch highest decided")
		}
		if !bytes.Equal(highestKnown.Message.Lambda, opts.PreviousLambda) {
			return errors.New("prev lambda doesn't match known lambda")
		}
		if highestKnown.Message.Type != proto.RoundState_Decided {
			return errors.New("previous instance not decided, can't start new instance")
		}
	} else {
		if opts.SeqNumber != 0 {
			return errors.New("previous lambda identifier is for first instance but seq number is not 0")
		}
	}
	return nil
}

// HighestKnownDecided returns the highest known decided instance
func (i *ibftImpl) HighestKnownDecided() (*proto.SignedMessage, error) {
	highestKnown, err := i.ibftStorage.GetHighestDecidedInstance(i.ValidatorShare.ValidatorPK.Serialize())
	if err != nil && err.Error() != collections.EntryNotFoundError {
		return nil, err
	}
	return highestKnown, nil
}

// NextSeqNumber returns the previous decided instance seq number + 1
// In case it's the first instance it returns 0
func (i *ibftImpl) NextSeqNumber() (uint64, error) {
	knownDecided, err := i.HighestKnownDecided()
	if err != nil {
		return 0, err
	}
	if knownDecided == nil {
		return 0, nil
	}
	return knownDecided.Message.SeqNumber + 1, nil
}

func (i *ibftImpl) instanceOptionsFromStartOptions(opts StartOptions) InstanceOptions {
	return InstanceOptions{
		Logger: opts.Logger,
		Me: &proto.Node{
			IbftId: opts.ValidatorShare.NodeID,
			Pk:     opts.ValidatorShare.ShareKey.GetPublicKey().Serialize(),
			Sk:     opts.ValidatorShare.ShareKey.Serialize(),
		},
		Network:        i.network,
		Queue:          i.msgQueue,
		ValueCheck:     opts.ValueCheck,
		LeaderSelector: i.leaderSelector,
		Params: &proto.InstanceParams{
			ConsensusParams: i.params.ConsensusParams,
			IbftCommittee:   opts.ValidatorShare.Committee,
		},
		Lambda:         opts.Identifier,
		SeqNumber:      opts.SeqNumber,
		PreviousLambda: opts.PrevInstance,
		ValidatorPK:    opts.ValidatorShare.ValidatorPK.Serialize(),
	}
}
