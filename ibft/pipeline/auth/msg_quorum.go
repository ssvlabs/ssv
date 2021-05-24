package auth

import (
	"errors"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ValidateQuorum is the pipeline to validate msg quorum requirement
func ValidateQuorum(threshold int) pipeline.Pipeline {
	return pipeline.WrapFunc("quorum", func(signedMessage *proto.SignedMessage) error {
		if len(signedMessage.SignerIds) < threshold {
			return errors.New("quorum not achieved")
		}
		return nil
	})
}
