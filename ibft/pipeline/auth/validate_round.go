package auth

import (
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ValidateRound validates round
func ValidateRound(state *proto.State) pipeline.Pipeline {
	return pipeline.WrapFunc(func(signedMessage *proto.SignedMessage) error {
		if state.Round != signedMessage.Message.Round {
			return errors.Errorf("message round (%d) does not equal State round (%d)", signedMessage.Message.Round, state.Round)
		}

		return nil
	})
}
