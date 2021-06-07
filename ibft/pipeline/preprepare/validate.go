package preprepare

import (
	"github.com/bloxapp/ssv/ibft/leader"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ValidatePrePrepareMsg validates pre-prepare message
func ValidatePrePrepareMsg(valueCheck valcheck.ValueCheck, leaderSelector leader.Selector, share *storage.Share) pipeline.Pipeline {
	return pipeline.WrapFunc("validate pre-prepare", func(signedMessage *proto.SignedMessage) error {
		if len(signedMessage.SignerIds) != 1 {
			return errors.New("invalid number of signers for pre-prepare message")
		}

		if signedMessage.SignerIds[0] != leaderSelector.Current(uint64(share.CommitteeSize())) {
			return errors.New("pre-prepare message sender is not the round's leader")
		}

		if err := valueCheck.Check(signedMessage.Message.Value); err != nil {
			return errors.Wrap(err, "failed while validating pre-prepare")
		}

		return nil
	})
}
