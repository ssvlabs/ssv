package preprepare

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ErrInvalidSignersNum represents an error when the number of signers is invalid.
var ErrInvalidSignersNum = errors.New("invalid number of signers for pre-prepare message")

// LeaderResolver resolves round's leader
type LeaderResolver func(round specqbft.Round) uint64

// ValidatePrePrepareMsg validates pre-prepare message
func ValidatePrePrepareMsg(resolver LeaderResolver) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("validate pre-prepare", func(signedMessage *specqbft.SignedMessage) error {
		signers := signedMessage.GetSigners()
		if len(signers) != 1 {
			return ErrInvalidSignersNum
		}

		leader := resolver(signedMessage.Message.Round)
		if uint64(signers[0]) != leader {
			return fmt.Errorf("proposal leader invalid")
		}
		return nil
	})
}
