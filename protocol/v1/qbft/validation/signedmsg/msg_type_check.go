package signedmsg

import (
	"errors"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// MsgTypeCheck is the pipeline to check message type
func MsgTypeCheck(msgType specqbft.MessageType) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("type check", func(signedMessage *specqbft.SignedMessage) error {
		if signedMessage.Message.MsgType != msgType {
			return errors.New("message type is wrong")
		}
		return nil
	})
}
