package signedmsg

import (
	"errors"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// BasicMsgValidation is the pipeline to validate basic params in a signed message
func BasicMsgValidation() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("basic msg validation", func(signedMessage *message.SignedMessage) error {
		if signedMessage == nil {
			return errors.New("signed message is nil")
		}
		if signedMessage.Message == nil {
			return errors.New("message body is nil")
		}
		return nil
	})
}
