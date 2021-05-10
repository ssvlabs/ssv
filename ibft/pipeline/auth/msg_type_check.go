package auth

import (
	"errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// MsgTypeCheck is the pipeline to check message type
func MsgTypeCheck(expected proto.RoundState) pipeline.Pipeline {
	return pipeline.WrapFunc("type check", func(signedMessage *proto.SignedMessage) error {
		if signedMessage.Message.Type != expected {
			return errors.New("message type is wrong")
		}
		return nil
	})
}
