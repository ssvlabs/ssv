package auth

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/validator/types"
)

// AuthorizeMsg is the pipeline to authorize message
func AuthorizeMsg(share *types.Share) pipeline.Pipeline {
	return pipeline.WrapFunc("authorize", func(signedMessage *proto.SignedMessage) error {
		return share.VerifySignedMessage(signedMessage)
	})
}
