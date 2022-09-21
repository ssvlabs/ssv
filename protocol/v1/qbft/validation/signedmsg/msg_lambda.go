package signedmsg

import (
	"bytes"
	"encoding/hex"
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateIdentifiers validates current and previous identifiers
func ValidateIdentifiers(identifier []byte) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("identifier", func(signedMessage *specqbft.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Identifier, identifier) {
			return fmt.Errorf("message identifier (%s) does not equal expected identifier (%s)",
				hex.EncodeToString(signedMessage.Message.Identifier), hex.EncodeToString(identifier))
		}
		return nil
	})
}
