package auth

import (
	"bytes"

	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ValidateLambdas validates current and previous lambdas
func ValidateLambdas(state *proto.State) pipeline.Pipeline {
	return pipeline.PipelineFunc(func(signedMessage *proto.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Lambda, state.Lambda) {
			return errors.Errorf("message Lambda (%s) does not equal State Lambda (%s)", string(signedMessage.Message.Lambda), string(state.Lambda))
		}
		if !bytes.Equal(signedMessage.Message.PreviousLambda, state.PreviousLambda) {
			return errors.New("message previous Lambda does not equal State previous Lambda")
		}
		return nil
	})
}
