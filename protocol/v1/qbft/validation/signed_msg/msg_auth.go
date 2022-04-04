package signed_msg

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
)

type Share interface {
	VerifySignedMessage(*message.SignedMessage) error
}

// AuthorizeMsg is the pipeline to authorize message
func AuthorizeMsg(share Share) validation.SignedMessagePipeline {
	return validation.WrapFunc("authorize", func(signedMessage *message.SignedMessage) error {
		return share.VerifySignedMessage(signedMessage)
	})
}
