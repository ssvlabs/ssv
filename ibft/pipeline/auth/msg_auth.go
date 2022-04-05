package auth

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
)

// AuthorizeMsg is the pipeline to authorize message
func AuthorizeMsg(share *keymanager.Share) pipeline.Pipeline {
	return pipeline.WrapFunc("authorize", func(signedMessage *proto.SignedMessage) error {
		return share.VerifySignedMessage(signedMessage)
	})
}
