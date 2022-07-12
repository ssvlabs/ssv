package signedmsg

import (
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// AuthorizeMsg is the pipeline to authorize message
func AuthorizeMsg(share *beacon.Share) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("authorize", func(signedMessage *message.SignedMessage) error {
		return share.VerifySignedMessage(signedMessage)
	})
}
