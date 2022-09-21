package signedmsg

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ErrWrongRound is returned when round is wrong.
var ErrWrongRound = fmt.Errorf("round is wrong")

// ValidateRound validates round
func ValidateRound(round specqbft.Round) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("round", func(signedMessage *specqbft.SignedMessage) error {
		if round != signedMessage.Message.Round {
			return ErrWrongRound
		}
		return nil
	})
}
