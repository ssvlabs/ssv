package auth

import (
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ValidateRound validates round
func ValidateRound(round uint64) pipeline.Pipeline {
	return pipeline.WrapFunc("round", func(signedMessage *proto.SignedMessage) error {
		if round != signedMessage.Message.Round {
			return errors.Errorf("message round (%d) does not equal State round (%d)", signedMessage.Message.Round, round)
		}

		return nil
	})
}
