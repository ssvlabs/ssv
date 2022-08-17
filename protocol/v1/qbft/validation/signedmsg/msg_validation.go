package signedmsg

import (
	"errors"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// BasicMsgValidation is the pipeline to validate basic params in a signed message
func BasicMsgValidation() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("basic msg validation", func(signedMessage *specqbft.SignedMessage) error {
		if signedMessage == nil {
			return errors.New("signed message is nil")
		}
		if signedMessage.Message == nil {
			return errors.New("message body is nil")
		}
		return nil
	})
}
