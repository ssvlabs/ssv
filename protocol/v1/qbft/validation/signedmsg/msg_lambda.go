package signedmsg

import (
	"bytes"
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
)

// ValidateLambdas validates current and previous lambdas
func ValidateLambdas(lambda []byte) validation.SignedMessagePipeline {
	return validation.WrapFunc("lambda", func(signedMessage *message.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Identifier, lambda) {
			return fmt.Errorf("message Lambda (%s) does not equal expected Lambda (%s)",
				string(signedMessage.Message.Identifier), string(lambda))
		}
		return nil
	})
}
