package preprepare

import (
	"errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/dataval"
)

// ValidatePrePrepareMsg validates pre-prepare message
func ValidatePrePrepareMsg(consensus dataval.Validator, state *proto.State, params *proto.InstanceParams) pipeline.Pipeline {
	return pipeline.WrapFunc(func(signedMessage *proto.SignedMessage) error {
		if len(signedMessage.SignerIds) != 1 {
			return errors.New("invalid number of signers for pre-prepare message")
		}

		if signedMessage.SignerIds[0] != RoundLeader(state.Round, params.CommitteeSize()) {
			return errors.New("pre-prepare message sender is not the round's leader")
		}

		if err := consensus.Validate(signedMessage.Message.Value); err != nil {
			return err
		}

		return nil
	})
}

// RoundLeader checks the round leader
func RoundLeader(round uint64, committeeSize int) uint64 {
	return round % uint64(committeeSize)
}
