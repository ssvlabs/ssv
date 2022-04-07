package signedmsg

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
)

// AuthorizeMsg is the pipeline to authorize message
func AuthorizeMsg(share *message.Share) validation.SignedMessagePipeline {
	return validation.WrapFunc("authorize", func(signedMessage *message.SignedMessage) error {
		return share.VerifySignedMessage(signedMessage)
	})
}
