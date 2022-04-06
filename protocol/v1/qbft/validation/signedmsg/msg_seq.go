package signedmsg

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/pkg/errors"
)

// ValidateSequenceNumber validates msg seq number
func ValidateSequenceNumber(height message.Height) validation.SignedMessagePipeline {
	return validation.WrapFunc("sequence", func(signedMessage *message.SignedMessage) error {
		if signedMessage.Message.Height != height {
			err := errors.Errorf("expected: %d, actual: %d",
				height, signedMessage.Message.Height)
			return errors.Wrap(err, "invalid message sequence number")
		}
		return nil
	})
}
