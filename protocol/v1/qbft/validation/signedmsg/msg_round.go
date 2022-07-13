package signedmsg

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateRound validates round
func ValidateRound(round specqbft.Round) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("round", func(signedMessage *specqbft.SignedMessage) error {
		if round != signedMessage.Message.Round {
			return errors.Errorf("message round (%d) does not equal state round (%d)", signedMessage.Message.Round, round)
		}
		return nil
	})
}
