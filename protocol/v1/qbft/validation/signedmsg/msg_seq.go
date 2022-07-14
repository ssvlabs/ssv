package signedmsg

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// ValidateSequenceNumber validates msg seq number
func ValidateSequenceNumber(height specqbft.Height) pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("sequence", func(signedMessage *specqbft.SignedMessage) error {
		if signedMessage.Message.Height != height {
			err := errors.Errorf("expected: %d, actual: %d",
				height, signedMessage.Message.Height)
			return errors.Wrap(err, "invalid message sequence number")
		}
		return nil
	})
}
