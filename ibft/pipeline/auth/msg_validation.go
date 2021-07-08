package auth

import (
	"errors"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// BasicMsgValidation is the pipeline to validate basic params in a signed message
func BasicMsgValidation() pipeline.Pipeline {
	return pipeline.WrapFunc("basic msg validation", func(signedMessage *proto.SignedMessage) error {
		if signedMessage == nil {
			return errors.New("signed message is nil")
		}
		if signedMessage.Message == nil {
			return errors.New("message body is nil")
		}
		return nil
	})
}
