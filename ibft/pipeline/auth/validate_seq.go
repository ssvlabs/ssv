package auth

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
)

// ValidateLambdas validates current and previous lambdas
func ValidateSequenceNumber(state *proto.State) pipeline.Pipeline {
	return pipeline.WrapFunc(func(signedMessage *proto.SignedMessage) error {
		if signedMessage.Message.SeqNumber != state.SeqNumber {
			return errors.New("invalid message sequence number")
		}
		return nil
	})
}
