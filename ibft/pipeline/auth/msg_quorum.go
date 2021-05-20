package auth

import (
	"errors"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// AuthorizeMsg is the pipeline to authorize message
func ValidateQuorum(threshold int) pipeline.Pipeline {
	return pipeline.WrapFunc("quorum", func(signedMessage *proto.SignedMessage) error {
		if len(signedMessage.SignerIds) < threshold {
			return errors.New("quorum not achieved")
		}
		return nil
	})
}
