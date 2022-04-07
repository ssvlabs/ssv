package preprepare

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"

	"github.com/pkg/errors"
)

type LeaderResolver func(round message.Round) uint64

// ValidatePrePrepareMsg validates pre-prepare message
func ValidatePrePrepareMsg(valueCheck validation.ValueCheck, resolver LeaderResolver) validation.SignedMessagePipeline {
	return validation.WrapFunc("validate pre-prepare", func(signedMessage *message.SignedMessage) error {
		signers := signedMessage.GetSigners()
		if len(signers) != 1 {
			return errors.New("invalid number of signers for pre-prepare message")
		}

		leader := resolver(signedMessage.Message.Round)
		if uint64(signers[0]) != leader {
			return errors.Errorf("pre-prepare message sender (id %d) is not the round's leader (expected %d)", signers[0], leader)
		}

		if err := valueCheck.Check(signedMessage.Message.Data); err != nil {
			return errors.Wrap(err, "failed while validating pre-prepare")
		}

		return nil
	})
}
