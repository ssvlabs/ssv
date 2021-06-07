package auth

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/validator/storage"
)

// AuthorizeMsg is the pipeline to authorize message
func AuthorizeMsg(share *storage.Share) pipeline.Pipeline {
	return pipeline.WrapFunc("authorize", func(signedMessage *proto.SignedMessage) error {
		return share.VerifySignedMessage(signedMessage)
	})
}
