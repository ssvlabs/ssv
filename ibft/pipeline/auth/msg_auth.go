package auth

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// AuthorizeMsg is the pipeline to authorize message
func AuthorizeMsg(params *proto.InstanceParams) pipeline.Pipeline {
	return pipeline.WrapFunc("authorize", func(signedMessage *proto.SignedMessage) error {
		return params.VerifySignedMessage(signedMessage)
	})
}
