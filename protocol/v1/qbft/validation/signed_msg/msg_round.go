package signed_msg

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/pkg/errors"
)

// ValidateRound validates round
func ValidateRound(round message.Round) validation.SignedMessagePipeline {
	return validation.WrapFunc("round", func(signedMessage *message.SignedMessage) error {
		if round != signedMessage.Message.Round {
			return errors.Errorf("message round (%d) does not equal state round (%d)", signedMessage.Message.Round, round)
		}
		return nil
	})
}
