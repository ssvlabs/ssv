package preprepare

import (
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ErrInvalidSignersNum represents an error when the number of signers is invalid.
var ErrInvalidSignersNum = errors.New("invalid number of signers for pre-prepare message")

// LeaderResolver resolves round's leader
type LeaderResolver func(round message.Round) uint64

// ValidatePrePrepareMsg validates pre-prepare message
func ValidatePrePrepareMsg(resolver LeaderResolver) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("validate pre-prepare", func(signedMessage *message.SignedMessage) error {
		signers := signedMessage.GetSigners()
		if len(signers) != 1 {
			return ErrInvalidSignersNum
		}

		leader := resolver(signedMessage.Message.Round)
		if uint64(signers[0]) != leader {
			return errors.Errorf("pre-prepare message sender (id %d) is not the round's leader (expected %d)", signers[0], leader)
		}
		return nil
	})
}
