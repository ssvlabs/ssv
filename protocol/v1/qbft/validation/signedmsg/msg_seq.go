package signedmsg

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateSequenceNumber validates msg seq number
func ValidateSequenceNumber(height specqbft.Height) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("sequence", func(signedMessage *specqbft.SignedMessage) error {
		if signedMessage.Message.Height != height {
			return fmt.Errorf("message height is wrong")
		}
		return nil
	})
}
