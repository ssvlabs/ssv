package signedmsg

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateLambdas validates current and previous lambdas
func ValidateLambdas(lambda []byte) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("lambda", func(signedMessage *message.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Identifier, lambda) {
			return fmt.Errorf("message Lambda (%s) does not equal expected Lambda (%s)",
				signedMessage.Message.Identifier.String(), hex.EncodeToString(lambda))
		}
		return nil
	})
}
