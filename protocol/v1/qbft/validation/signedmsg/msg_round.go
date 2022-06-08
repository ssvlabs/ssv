package signedmsg

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/pkg/errors"
)

// ValidateRound validates round
func ValidateRound(round message.Round) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("round", func(signedMessage *message.SignedMessage) error {
		if round != signedMessage.Message.Round {
			return errors.Errorf("message round (%d) does not equal state round (%d)", signedMessage.Message.Round, round)
		}
		return nil
	})
}
