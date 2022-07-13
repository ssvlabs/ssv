package signedmsg

import (
	"bytes"
	"encoding/hex"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateLambdas validates current and previous lambdas
func ValidateLambdas(lambda []byte) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("lambda", func(signedMessage *specqbft.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Identifier, lambda) {
			return fmt.Errorf("message Lambda (%s) does not equal expected Lambda (%s)",
				hex.EncodeToString(signedMessage.Message.Identifier), hex.EncodeToString(lambda))
		}
		return nil
	})
}
