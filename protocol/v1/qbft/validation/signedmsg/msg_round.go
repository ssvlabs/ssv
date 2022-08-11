package signedmsg

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateRound validates round
func ValidateRound(round specqbft.Round) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("round", func(signedMessage *specqbft.SignedMessage) error {
		if round != signedMessage.Message.Round {
			return fmt.Errorf("round is wrong")
		}
		return nil
	})
}
